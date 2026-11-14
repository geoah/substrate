#if DEBUG
import ChatKit
import Foundation
import SubstrateKit
import SwiftUI

#if os(macOS)
import AppKit
#else
import UIKit
#endif

extension MeterPoint {
    /// A key pressed in the composer, until the pass of the main run loop
    /// it was pressed in has drawn it.
    static let key = MeterPoint("key")
}

/// The app run against its fixture and timed (`mise run apple:measure:chat`,
/// which launches a build with `SUBSTRATE_CHAT_MEASURE` set): a 16 KB reply
/// streamed at a model's pace into a chat of 200 turns and into one of 500
/// rows, then 200 characters typed into the composer. It prints what each
/// cost as a table and exits. The session is in memory and the folder a
/// scratch one, so it shares nothing with a signed-in build beside it.
@MainActor
enum Measure {
    /// What the launch asked for: a label for the build measured.
    static let asked = ProcessInfo.processInfo.environment["SUBSTRATE_CHAT_MEASURE"]

    private static var server: FixtureServer?
    private static let message = "Where does the week stand?"
    private static let reply = ChatFixtures.reply(bytes: 16 * 1024)
    private static let pace = FixtureServer.Pace()

    /// The fixture's parts, where the launch asked for a measurement.
    static func parts() -> ChatHost.Parts? {
        guard asked != nil else { return nil }
        let server = FixtureServer(reply: reply, pace: pace)
        self.server = server
        return .fixture(server)
    }

    /// Runs the scenario once the app's window is up, prints the table and
    /// ends the process.
    static func start(_ host: ChatHost) {
        guard let asked else { return }
        #if os(macOS)
        // No Dock icon and no activation: the window draws without taking
        // the keyboard from whatever is in front.
        NSApplication.shared.setActivationPolicy(.accessory)
        #endif
        Task {
            let report = await run(host, label: asked)
            let text = report ?? "The measurement did not run: the fixture's chats never drew."
            print("<!-- measure:begin -->\n\(text)\n<!-- measure:end -->")
            fflush(stdout)
            try? text.write(to: FileManager.default.temporaryDirectory.appendingPathComponent("chat-measure.md"), atomically: true, encoding: .utf8)
            exit(report == nil ? 1 : 0)
        }
    }

    private struct Phase {
        var name: String
        /// What the figures are divided by: flushes of a stream, keys typed.
        var steps: Int
        var entries: [Meter.Entry]

        func stat(_ point: String, _ detail: String = "") -> Meter.Stat? {
            entries.first { $0.point == point && $0.detail == detail }?.stat
        }
    }

    private static func run(_ host: ChatHost, label: String) async -> String? {
        let signposts = SystemSignposts(subsystem: "dev.reamde.substrate.chat")
        guard let scene = await until({ host.chats?.scenes.keys.first }),
            await until({ (host.chats?.list.sections.reduce(0) { $0 + $1.rows.count } ?? 0) >= 100 ? true : nil }) != nil
        else { return nil }
        // The chats read ahead after the list have landed.
        try? await Task.sleep(for: .seconds(4))
        let parse = standalone { _ = Markdown.parse(reply) }
        let rows = standalone { _ = Markdown.rows(reply, open: true) }

        var phases: [Phase] = []
        var opened: [String] = []
        var shown: ChildKey?
        for (name, thread, held) in [("a 16 KB reply into 200 turns", ChatFixtures.long, 200), ("a 16 KB reply into 500 rows", ChatFixtures.full, 500)] {
            var passes = MainPasses()
            Meter.shared.start(signposts: signposts)
            host.send(.show(thread: thread, scene: scene))
            guard
                let key = await until({ () -> ChildKey? in
                    guard let key = host.chats?.scenes[scene]?.current, let screen = host.conversation(key),
                        screen.thread == thread, screen.turns.count >= held
                    else { return nil }
                    return key
                })
            else { return nil }
            shown = key
            try? await Task.sleep(for: .seconds(2))
            passes.stop()
            let opening = Phase(name: "", steps: 1, entries: Meter.shared.stop())
            opened.append(
                String(
                    format: "Opening the chat of %d rows: %d turn bodies evaluated, %.0f ms of main thread.", held,
                    opening.stat("body", "Turn")?.count ?? 0, opening.stat("main")?.milliseconds ?? 0))
            passes = MainPasses()
            Meter.shared.start(signposts: signposts)
            host.send(.draft(message), to: key)
            host.send(.send, to: key)
            // Streamed, settled, and its rows read in its place.
            let settled = await until(seconds: 300) { () -> Bool? in
                guard let screen = host.conversation(key), !screen.streaming, screen.turns.count >= held + 2,
                    screen.turns.last?.id.hasPrefix("live-") == false
                else { return nil }
                return true
            }
            try? await Task.sleep(for: .milliseconds(500))
            passes.stop()
            let entries = Meter.shared.stop()
            guard settled != nil else { return nil }
            phases.append(Phase(name: name, steps: entries.first { $0.point == "flush" }?.stat.count ?? 0, entries: entries))
        }

        var typed = 0
        if shown != nil, let field = TextInput.find() {
            let passes = MainPasses()
            Meter.shared.start(signposts: signposts)
            for character in String(repeating: "The quick brown fox jumps over the lazy dog. ", count: 5).prefix(200) {
                let pressed = Meter.now
                MainPasses.afterThisPass { Meter.shared.record(.key, nanoseconds: Meter.now - pressed) }
                field.insert(String(character))
                typed += 1
                try? await Task.sleep(for: .milliseconds(80))
            }
            try? await Task.sleep(for: .seconds(1))
            passes.stop()
            phases.append(Phase(name: "200 characters typed", steps: typed, entries: Meter.shared.stop()))
        }
        opened.insert(
            String(format: "The same closed for display, parsed and made into rows: %.2f ms on average, %.2f ms at best.", rows.mean, rows.least),
            at: 0)
        return table(phases, label: label, parse: parse, opened: opened, typed: typed)
    }

    // MARK: The table

    private static func table(
        _ phases: [Phase], label: String, parse: (mean: Double, least: Double), opened: [String], typed: Int
    ) -> String {
        func cell(_ phase: Phase, _ point: String, _ detail: String = "", time: Bool) -> String {
            guard let stat = phase.stat(point, detail), phase.steps > 0 else { return "0" }
            let steps = Double(phase.steps)
            if time { return String(format: "%.2f (%.1f)", stat.milliseconds / steps, Double(stat.longest) / 1_000_000) }
            return String(format: "%.1f", Double(stat.count) / steps)
        }
        func details(_ point: String) -> [String] {
            Set(phases.flatMap { $0.entries.filter { $0.point == point }.map(\.detail) }).sorted()
        }
        var rows: [(String, (Phase) -> String)] = [
            ("steps: flushes of the stream, or keys typed", { "\($0.steps)" }),
            ("main thread, ms per step (longest pass)", { cell($0, "main", time: true) }),
        ]
        for detail in details("present") {
            rows.append(("presenter `\(detail)`, ms per step (longest run)", { cell($0, "present", detail, time: true) }))
            rows.append(("presenter `\(detail)`, runs per step", { cell($0, "present", detail, time: false) }))
        }
        rows.append(("flush on the runtime's executor, ms per step (longest)", { cell($0, "flush", time: true) }))
        rows.append(("publish, runtime's half, ms per step (longest)", { cell($0, "publish", time: true) }))
        rows.append(("publish, model's half on the main thread, ms per step (longest)", { cell($0, "republish", time: true) }))
        rows.append(("publishes taken by the model, per step", { cell($0, "republish", time: false) }))
        for detail in details("body") {
            rows.append(("body evaluations per step: `\(detail)`", { cell($0, "body", detail, time: false) }))
        }
        rows.append(("Markdown parses per step", { cell($0, "parse", time: false) }))
        rows.append(("Markdown parse, ms per step (longest parse)", { cell($0, "parse", time: true) }))
        rows.append(("closing unfinished Markdown, ms per step (longest)", { cell($0, "close", time: true) }))
        rows.append(("inline Markdown made into text, per step", { cell($0, "inline", time: false) }))
        rows.append(("inline Markdown, ms per step (longest)", { cell($0, "inline", time: true) }))
        rows.append(("key to drawn, ms (longest)", { cell($0, "key", time: true) }))

        var out = "Measured on \(machine), build `\(label)`, the reply written \(pace.perSecond) characters a second.\n"
        out += String(format: "The 16 KB reply parsed whole, 20 times: %.2f ms on average, %.2f ms at best.\n", parse.mean, parse.least)
        out += opened.map { $0 + "\n" }.joined()
        if typed == 0 { out += "The composer's field was not found, so nothing was typed.\n" }
        out += "\n| | " + phases.map(\.name).joined(separator: " | ") + " |\n"
        out += "| --- |" + phases.map { _ in " ---: |" }.joined() + "\n"
        for (name, value) in rows {
            out += "| \(name) | " + phases.map(value).joined(separator: " | ") + " |\n"
        }
        return out
    }

    /// The hardware and the system, with no name a person gave either.
    private static var machine: String {
        var size = 0
        sysctlbyname("hw.model", nil, &size, nil, 0)
        var model = [CChar](repeating: 0, count: max(size, 1))
        sysctlbyname("hw.model", &model, &size, nil, 0)
        let hardware = String(cString: model)
        let system = ProcessInfo.processInfo.operatingSystemVersionString
        #if os(macOS)
        return "a Mac (\(hardware), macOS \(system))"
        #else
        if let simulated = ProcessInfo.processInfo.environment["SIMULATOR_MODEL_IDENTIFIER"] {
            return "an iOS simulator (\(simulated), iOS \(system)) on a Mac (\(hardware)): not a device"
        }
        return "a device (\(hardware), iOS \(system))"
        #endif
    }

    // MARK: Waiting and timing

    /// The first answer `read` gives, asked every 50 ms; nil after `seconds`.
    private static func until<T>(seconds: Double = 60, _ read: () -> T?) async -> T? {
        let deadline = Date().addingTimeInterval(seconds)
        while Date() < deadline {
            if let value = read() { return value }
            try? await Task.sleep(for: .milliseconds(50))
        }
        return nil
    }

    private static func standalone(_ body: () -> Void) -> (mean: Double, least: Double) {
        var took: [Double] = []
        for _ in 0 ..< 20 {
            let began = Meter.now
            body()
            took.append(Double(Meter.now - began) / 1_000_000)
        }
        return (took.reduce(0, +) / Double(took.count), took.min() ?? 0)
    }
}

/// What the main thread spends: each pass of its run loop from waking to
/// waiting again, recorded as `MeterPoint.mainPass`.
@MainActor
final class MainPasses {
    private var observers: [CFRunLoopObserver] = []
    private var woke = Meter.now

    init() {
        let wake = CFRunLoopObserverCreateWithHandler(nil, CFRunLoopActivity.afterWaiting.rawValue, true, CFIndex.min) { [weak self] _, _ in
            MainActor.assumeIsolated { self?.woke = Meter.now }
        }
        let wait = CFRunLoopObserverCreateWithHandler(nil, CFRunLoopActivity.beforeWaiting.rawValue, true, CFIndex.max) { [weak self] _, _ in
            MainActor.assumeIsolated {
                guard let self else { return }
                Meter.shared.record(.mainPass, nanoseconds: Meter.now - self.woke)
                // A pass that never waits again is counted from here.
                self.woke = Meter.now
            }
        }
        for observer in [wake, wait].compactMap({ $0 }) {
            CFRunLoopAddObserver(CFRunLoopGetMain(), observer, .commonModes)
            observers.append(observer)
        }
    }

    func stop() {
        for observer in observers { CFRunLoopRemoveObserver(CFRunLoopGetMain(), observer, .commonModes) }
        observers = []
    }

    /// Runs `work` once, as the pass under way ends: after SwiftUI has
    /// updated and the frame was committed.
    static func afterThisPass(_ work: @escaping @MainActor () -> Void) {
        let observer = CFRunLoopObserverCreateWithHandler(nil, CFRunLoopActivity.beforeWaiting.rawValue, false, CFIndex.max) { _, _ in
            MainActor.assumeIsolated { work() }
        }
        CFRunLoopAddObserver(CFRunLoopGetMain(), observer, .commonModes)
    }
}

/// The composer's field as the system's text input, found in the window, so
/// a measurement types through the field's own text system and the app
/// carries no hook for it.
@MainActor
struct TextInput {
    let insert: (String) -> Void

    static func find() -> TextInput? {
        #if os(macOS)
        for window in NSApplication.shared.windows {
            guard let root = window.contentView else { continue }
            if let view = first(NSTextView.self, in: root, where: { $0.isEditable }) {
                window.makeFirstResponder(view)
                return TextInput { view.insertText($0, replacementRange: NSRange(location: NSNotFound, length: 0)) }
            }
            if let field = first(NSTextField.self, in: root, where: { $0.isEditable && !($0 is NSSearchField) }) {
                window.makeFirstResponder(field)
                guard let editor = field.currentEditor() as? NSTextView else { continue }
                return TextInput { editor.insertText($0, replacementRange: NSRange(location: NSNotFound, length: 0)) }
            }
        }
        return nil
        #else
        let windows = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.flatMap(\.windows)
        for window in windows {
            if let view = first(UITextView.self, in: window, where: { $0.isEditable }) {
                view.becomeFirstResponder()
                return TextInput { view.insertText($0) }
            }
        }
        for window in windows {
            if let field = first(UITextField.self, in: window, where: { !($0 is UISearchTextField) }) {
                field.becomeFirstResponder()
                return TextInput { field.insertText($0) }
            }
        }
        return nil
        #endif
    }

    #if os(macOS)
    private static func first<V: NSView>(_ type: V.Type, in view: NSView, where matches: (V) -> Bool) -> V? {
        if let found = view as? V, matches(found) { return found }
        for child in view.subviews {
            if let found = first(type, in: child, where: matches) { return found }
        }
        return nil
    }
    #else
    private static func first<V: UIView>(_ type: V.Type, in view: UIView, where matches: (V) -> Bool) -> V? {
        if let found = view as? V, matches(found) { return found }
        for child in view.subviews {
            if let found = first(type, in: child, where: matches) { return found }
        }
        return nil
    }
    #endif
}
#endif
