#if os(macOS)
import AppKit
import ChatKit
import Foundation
import SubstrateKit
import SwiftUI
import Testing

@testable import SubstrateChat

/// A reply from its first piece to its stored rows, frame by frame, through
/// the views the app draws it with and its own runtime against the fixture's
/// server, which writes the run a line at a time. One hosted transcript
/// lives through the whole run, as a window's does, so what its views keep
/// (an open Thinking section, an opened tool row) is theirs to lose.
@MainActor
@Suite(.serialized) struct HandoverTests {
    static let scene = "handover"
    static let thread = "t-live"

    /// The transcript's rows laid out whole, at a phone's width: a lazy
    /// stack in a scroll view draws nothing offscreen, and the rows are the
    /// same views either way.
    struct Rows: View {
        @Environment(ChatHost.self) private var host
        let key: ChildKey
        let measured: Measured

        var body: some View {
            VStack(alignment: .leading, spacing: 22) {
                if let screen = host.conversation(key) {
                    TranscriptRows(screen: screen, key: key, onKey: { _ in }, act: { _ in })
                        .environment(\.recordTitles, screen.titles)
                }
            }
            .padding(.horizontal, 20)
            .frame(width: OffscreenDrawing.phone.width, alignment: .topLeading)
            .fixedSize(horizontal: false, vertical: true)
            // The content's height as its own layout has it.
            .onGeometryChange(for: CGFloat.self) {
                $0.size.height
            } action: {
                measured.height = $0
            }
            .replyLinks()
            .recordViewer(host.recordDirectory())
            // No screen draws this window, so nothing drives an animation
            // to its end: a change is its end at once.
            .transaction { $0.animation = nil }
        }
    }

    /// The height the rows last laid out at.
    @MainActor
    final class Measured {
        var height: CGFloat = 0
    }

    /// One frame: the screen as presented, and what its views measured.
    struct Frame {
        var screen: ConversationScreen
        var height: CGFloat
        /// Each row of the transcript, by its id and what it is.
        var rows: [String]
    }

    /// The hosted transcript and every frame it went through.
    @MainActor
    final class Stage {
        let host: ChatHost
        let key: ChildKey
        let hosting: NSHostingView<AnyView>
        let window: NSWindow
        let measured = Measured()
        private(set) var frames: [Frame] = []

        init(_ host: ChatHost, _ key: ChildKey) {
            self.host = host
            self.key = key
            _ = NSApplication.shared
            hosting = NSHostingView(rootView: AnyView(Rows(key: key, measured: measured).environment(host)))
            hosting.sizingOptions = []
            hosting.frame = CGRect(x: 0, y: 0, width: OffscreenDrawing.phone.width, height: 2000)
            window = NSWindow(contentRect: hosting.frame, styleMask: [.borderless], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false
            window.contentView = hosting
        }

        /// Lays the views out for the screen as it stands and keeps the
        /// frame, where it is another than the last.
        func look() {
            guard let screen = host.conversation(key) else { return }
            hosting.layoutSubtreeIfNeeded()
            let rows = TranscriptPiece.pieces(of: screen.turns, failures: screen.failures.turns).map { piece in
                switch piece.part {
                case .turn: "\(piece.id) turn"
                case let .page(page): "\(piece.id) page \(page)"
                case .tail: "\(piece.id) tail"
                case .failure: "\(piece.id) failure"
                }
            }
            let frame = Frame(screen: screen, height: measured.height, rows: rows)
            if frames.last?.screen != frame.screen || frames.last?.height != frame.height { frames.append(frame) }
        }

        /// Every accessibility element of the transcript, depth first.
        var elements: [AnyObject] { LeavingTheApp.elements(under: hosting) }

        /// What a screen reader is told of each element: its label, or
        /// the text it holds. A text that changed where it stands may be
        /// told as it first was: this says what is there, not its latest
        /// words.
        func labels() -> [String] {
            elements.compactMap { element in
                if let label = element.accessibilityLabel?(), !label.isEmpty { return label }
                return (element as? NSAccessibilityProtocol)?.accessibilityValue() as? String
            }
        }

        /// Presses the first element whose label, or whose hint, `named`
        /// holds of. (A button with a spinner in it is no button to the
        /// system while it spins.)
        func press(_ named: (String) -> Bool) -> Bool {
            for element in elements {
                let words = [element.accessibilityLabel?() ?? nil, element.accessibilityHelp?() ?? nil].compactMap { $0 }
                if words.contains(where: named) { return element.accessibilityPerformPress?() ?? false }
            }
            return false
        }

        isolated deinit {
            window.contentView = nil
        }
    }

    /// The reply's text on a screen, all of its replies in order.
    static func text(_ screen: ConversationScreen?) -> String {
        (screen?.turns ?? []).compactMap { turn in
            if case let .assistant(reply) = turn.body { reply.text } else { nil }
        }.joined()
    }

    /// A host on the fixture, showing the chat the run is written in, and
    /// the stage its transcript is hosted on.
    func staged(paced: Bool) async throws -> (FixtureServer, ChatFixtures.Run, Stage) {
        let run = ChatFixtures.run(thread: Self.thread)
        let server = FixtureServer(reply: "")
        server.script(Self.thread, lines: run.lines, rows: run.rows)
        let host = ChatHost(.fixture(server))
        host.send(.sceneAppeared(scene: Self.scene, restoring: nil))
        try await until { host.chats?.list.loading == false ? true : nil }
        host.send(.pace(paced))
        host.send(.show(thread: Self.thread, scene: Self.scene))
        let key = try await until { host.chats?.scenes[Self.scene]?.current }
        try await until { host.conversation(key) != nil ? true : nil }
        return (server, run, Stage(host, key))
    }

    /// Lets `count` more lines of the run out and looks at every screen
    /// until `reached` holds of the last.
    func step(_ server: FixtureServer, _ stage: Stage, _ count: Int = 1, until reached: (ConversationScreen) -> Bool) async throws {
        server.step(Self.thread, count)
        try await watch(stage, until: reached)
    }

    func watch(_ stage: Stage, until reached: (ConversationScreen) -> Bool) async throws {
        for _ in 0 ..< 6000 {
            stage.look()
            if let screen = stage.host.conversation(stage.key), reached(screen) {
                // A moment more: what follows a state in the same breath.
                for _ in 0 ..< 4 {
                    try await Task.sleep(for: .milliseconds(5))
                    stage.look()
                }
                return
            }
            try await Task.sleep(for: .milliseconds(5))
        }
        throw NotReached()
    }

    /// Holds every frame to what a reader must never see: the content
    /// getting shorter, a row going or changing what it is, a turn or text
    /// being taken back.
    func hold(_ frames: [Frame], sourceLocation: SourceLocation = #_sourceLocation) {
        var height: CGFloat = 0
        var rows: [String] = []
        var shown: [String: String] = [:]
        var turns = 0
        for (index, frame) in frames.enumerated() {
            #expect(frame.height >= height - 0.5, "frame \(index): the content went from \(height) to \(frame.height) points", sourceLocation: sourceLocation)
            #expect(
                rows.allSatisfy(frame.rows.contains), "frame \(index): a row went: \(rows.filter { !frame.rows.contains($0) })", sourceLocation: sourceLocation)
            #expect(frame.rows.filter(rows.contains) == rows, "frame \(index): rows changed places", sourceLocation: sourceLocation)
            // Each reply's own text only ever goes on.
            for turn in frame.screen.turns {
                guard case let .assistant(reply) = turn.body else { continue }
                let now = reply.text ?? ""
                #expect(now.hasPrefix(shown[turn.id] ?? ""), "frame \(index): text of \(turn.id) was taken back", sourceLocation: sourceLocation)
                shown[turn.id] = now
            }
            #expect(frame.screen.turns.count >= turns, "frame \(index): a turn went", sourceLocation: sourceLocation)
            (height, rows, turns) = (max(height, frame.height), frame.rows, frame.screen.turns.count)
        }
    }

    @Test(.timeLimit(.minutes(5)), arguments: [false, true])
    func fromTheFirstPieceToTheStoredRowsNothingShrinksNoRowIsTornDownAndAnAttachEmptiesNothing(paced: Bool) async throws {
        let (server, run, stage) = try await staged(paced: paced)
        let host = stage.host
        host.send(.draft(run.message), to: stage.key)
        host.send(.send, to: stage.key)
        try await watch(stage) { $0.turns.count == 1 }

        // The model thinks: its window shows, and the reader opens the
        // whole of it.
        try await step(server, stage, 3) { screen in
            if case let .assistant(reply)? = screen.turns.last?.body { reply.reasoning?.thinking == true } else { false }
        }
        #expect(stage.press { $0.hasPrefix("Thinking") })
        let thinking = { stage.labels().contains { $0.hasPrefix("The balcony list is short this week.") } }
        try await watch(stage) { _ in thinking() }
        try await step(server, stage, 2) { screen in
            if case let .assistant(reply)? = screen.turns.last?.body { reply.reasoning?.text == run.thinking } else { false }
        }

        // It answers, and calls a tool, whose row the reader opens.
        try await step(server, stage, 2) { screen in
            if case let .assistant(reply)? = screen.turns.last?.body { reply.tools.first?.status == .running } else { false }
        }
        #expect(stage.press { $0 == "Shows the details" })
        try await watch(stage) { _ in stage.labels().contains("Details") }
        try await step(server, stage, 1) { screen in
            screen.turns.contains { turn in
                if case let .assistant(reply) = turn.body { reply.tools.first?.status == .done } else { false }
            }
        }

        // The second reply is part way when the app is sent away and comes
        // back: the stream is joined again and replays the run from its
        // start.
        try await step(server, stage, 2) { Self.text($0).contains("Water the basil") }
        let before = stage.frames.count
        host.send(.phase(.background))
        host.send(.phase(.active))
        try await step(server, stage, 1) { Self.text($0).contains("Repot the mint") }
        #expect(stage.frames.count > before)

        // The run ends, and its rows are read.
        try await step(server, stage, 20) { !$0.streaming && Self.text($0) == run.first + run.second && $0.composer.status == nil }
        try await watch(stage) { screen in
            if case let .assistant(reply)? = screen.turns.last?.body { !reply.live } else { false }
        }

        hold(stage.frames)
        let last = try #require(stage.frames.last)
        #expect(last.rows == ["sent-1 turn", "turn-1 page 0", "turn-1/tail tail", "turn-3 page 0"])
        #expect(stage.frames.count > 12)
        // What the reader opened is still open: the Thinking section shows
        // its text, which a section opened afresh would not, and the tool's
        // row its details.
        let labels = stage.labels()
        #expect(thinking())
        #expect(labels.contains("Details"))
        #expect(labels.contains { $0.hasPrefix("Thought for") })
    }

    @Test(.timeLimit(.minutes(5)))
    func theThinkingWindowGivesItsHeightToTheAnswer() async throws {
        let (server, run, stage) = try await staged(paced: false)
        stage.host.send(.draft(run.message), to: stage.key)
        stage.host.send(.send, to: stage.key)
        try await watch(stage) { $0.turns.count == 1 }
        // Thinking: the window is as tall with one line of it as with all.
        try await step(server, stage, 2) { screen in
            if case let .assistant(reply)? = screen.turns.last?.body { reply.reasoning?.thinking == true } else { false }
        }
        let one = try #require(stage.frames.last).height
        try await step(server, stage, 3) { screen in
            if case let .assistant(reply)? = screen.turns.last?.body { reply.reasoning?.text == run.thinking } else { false }
        }
        let all = try #require(stage.frames.last).height
        #expect(abs(all - one) < 0.5, "the window grew with the thinking: \(one) then \(all)")
        // The answer starts: in the window's place, the page as tall as it was.
        try await step(server, stage, 1) { Self.text($0) == run.first }
        let answering = try #require(stage.frames.last).height
        #expect(abs(answering - all) < 0.5, "the page jumped when the answer started: \(all) then \(answering)")
        hold(stage.frames)
        server.step(Self.thread, 20)
        try await watch(stage) { !$0.streaming && Self.text($0) == run.first + run.second }
    }

    /// The first answer `read` gives; fails after a minute without one.
    @discardableResult
    func until<T>(_ read: () -> T?) async throws -> T {
        for _ in 0 ..< 1200 {
            if let value = read() { return value }
            try await Task.sleep(for: .milliseconds(50))
        }
        throw NotReached()
    }

    struct NotReached: Error {}
}
#endif

#if os(macOS)
/// A chat that is at its end when the app leaves the front is at its end
/// when it returns, whatever arrived meanwhile; one its reader scrolled up
/// stays where it was. Through the conversation's own view in a window of
/// its own, which no screen draws: the scroll view under it is asked where
/// it stands.
@MainActor
@Suite(.serialized) struct FollowTests {
    static let scene = "follow"
    static let thread = "t-live"

    @MainActor
    final class Stage {
        /// A window short enough that a few turns outgrow it.
        static let size = CGSize(width: 280, height: 150)
        let host: ChatHost
        let key: ChildKey
        let window: NSWindow

        init(_ host: ChatHost, _ key: ChildKey) {
            self.host = host
            self.key = key
            _ = NSApplication.shared
            let view = NavigationStack { ConversationView(key: key, scene: FollowTests.scene) }
                .environment(host)
                .recordViewer(host.recordDirectory())
                .transaction { $0.animation = nil }
            let controller = NSHostingController(rootView: view)
            controller.sizingOptions = []
            window = NSWindow(
                contentRect: CGRect(origin: .zero, size: Self.size), styleMask: [.titled, .resizable, .fullSizeContentView],
                backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false
            window.contentViewController = controller
            window.setContentSize(Self.size)
            // Far off every screen: ordered in so its views are laid out.
            window.setFrameOrigin(NSPoint(x: -20000, y: -20000))
            window.orderFrontRegardless()
        }

        func settle() async throws {
            for _ in 0 ..< 6 {
                window.contentView?.layoutSubtreeIfNeeded()
                try await Task.sleep(for: .milliseconds(20))
            }
        }

        /// The transcript's scroll view: the tallest one in the window.
        var scroll: NSScrollView? {
            func all(_ view: NSView) -> [NSScrollView] { ((view as? NSScrollView).map { [$0] } ?? []) + view.subviews.flatMap(all) }
            return window.contentView.map(all)?.max { ($0.documentView?.frame.height ?? 0) < ($1.documentView?.frame.height ?? 0) }
        }

        /// How far the content goes on below what the scroll view shows
        /// above the composer, how tall the content is, and how far down
        /// it the scroll view stands.
        var reach: (below: CGFloat, content: CGFloat, offset: CGFloat)? {
            guard let scroll, let document = scroll.documentView else { return nil }
            let visible = scroll.documentVisibleRect
            let insets = scroll.contentView.contentInsets
            let below = document.isFlipped ? document.frame.height - visible.maxY + insets.bottom : visible.minY + insets.bottom
            return (below, document.frame.height, document.isFlipped ? visible.minY : document.frame.height - visible.maxY)
        }

        /// Turns a mouse's wheel up over the transcript by `points`: the
        /// watcher behind the scroll view is told as the event's monitor
        /// tells it, and the scroll view moves as the wheel moves it. (A
        /// wheel's event cannot be made for a window no screen shows.)
        func wheel(up points: CGFloat) {
            guard let content = window.contentView, let scroll, let document = scroll.documentView else { return }
            func all(_ view: NSView) -> [WheelWatcher.Watching] {
                ((view as? WheelWatcher.Watching).map { [$0] } ?? []) + view.subviews.flatMap(all)
            }
            for watcher in all(content) { watcher.turned(by: points, at: CGPoint(x: content.bounds.midX, y: content.bounds.midY)) }
            var origin = scroll.contentView.bounds.origin
            origin.y += document.isFlipped ? -points : points
            scroll.contentView.scroll(to: origin)
            scroll.reflectScrolledClipView(scroll.contentView)
        }

        isolated deinit {
            window.orderOut(nil)
            window.contentViewController = nil
        }
    }

    func staged() async throws -> (FixtureServer, ChatFixtures.Run, Stage) {
        let run = ChatFixtures.run(thread: Self.thread)
        let server = FixtureServer(reply: "")
        server.script(Self.thread, lines: run.lines, rows: run.rows)
        let host = ChatHost(.fixture(server))
        host.send(.sceneAppeared(scene: Self.scene, restoring: nil))
        try await until { host.chats?.list.loading == false ? true : nil }
        host.send(.show(thread: Self.thread, scene: Self.scene))
        let key = try await until { host.chats?.scenes[Self.scene]?.current }
        try await until { host.conversation(key) != nil ? true : nil }
        let stage = Stage(host, key)
        try await stage.settle()
        host.send(.draft(run.message), to: key)
        host.send(.send, to: key)
        try await until { host.conversation(key)?.turns.count == 1 ? true : nil }
        return (server, run, stage)
    }

    func text(_ stage: Stage) -> String { HandoverTests.text(stage.host.conversation(stage.key)) }

    @Test(.timeLimit(.minutes(5)))
    func aChatAtItsEndIsAtItsEndAfterTheAppWasAwayWhateverArrivedMeanwhile() async throws {
        let (server, run, stage) = try await staged()
        // The reply outgrows the window, and the transcript follows it.
        server.step(Self.thread, 10)
        try await until { text(stage).contains("Water the basil") ? true : nil }
        try await stage.settle()
        let before = try #require(stage.reach)
        try #require(before.offset > 40, "the content does not outgrow the window: \(before)")
        #expect(abs(before.below) < 1, "not at its end while the reply was written: \(before)")

        // Away; the rest of the reply arrives, and its rows; back.
        stage.host.send(.phase(.inactive))
        stage.host.send(.phase(.background))
        server.step(Self.thread, 20)
        try await until { stage.host.conversation(stage.key)?.streaming == false && text(stage) == run.first + run.second ? true : nil }
        try await stage.settle()
        stage.host.send(.phase(.active))
        try await stage.settle()
        let after = try #require(stage.reach)
        #expect(after.content > before.content, "nothing arrived meanwhile: \(before) then \(after)")
        #expect(abs(after.below) < 1, "not at its end on return: \(after)")
    }

    @Test(.timeLimit(.minutes(5)))
    func aChatItsReaderScrolledUpStaysWhereItWasAndSaysHowManyMessagesAreBelow() async throws {
        let (server, _, stage) = try await staged()
        server.step(Self.thread, 12)
        try await until { text(stage).contains("The tomatoes can wait") ? true : nil }
        try await stage.settle()
        let end = try #require(stage.reach)
        try #require(abs(end.below) < 1 && end.offset > 115, "not at the end of something to scroll: \(end)")
        // Up, by a mouse's wheel.
        stage.wheel(up: end.offset + 60)
        try await stage.settle()
        let up = try #require(stage.reach)
        #expect(up.offset < end.offset - 80 && up.below > TranscriptFollow.near, "not far enough up to be let go of: \(end) then \(up)")
        // More arrives: it stays, and the content goes on below.
        server.step(Self.thread, 20)
        try await until { stage.host.conversation(stage.key)?.streaming == false ? true : nil }
        try await stage.settle()
        let later = try #require(stage.reach)
        #expect(abs(later.offset - up.offset) < 1, "the transcript moved under its reader: \(up) then \(later)")
        #expect(later.below > TranscriptFollow.near)
        // Jump to Latest says what is below, and goes there.
        func jumps() -> AnyObject? {
            LeavingTheApp.elements(under: stage.window.contentView!).first { ($0.accessibilityLabel?() ?? nil)?.hasPrefix("Jump to Latest") == true }
        }
        try await until { (jumps()?.accessibilityLabel?() ?? nil)?.contains("below") == true ? true : nil }
        let jump = try #require(jumps())
        // The reply being written is below, and the first where the window
        // is too short to show it.
        #expect(["Jump to Latest, 1 message below", "Jump to Latest, 2 messages below"].contains((jump.accessibilityLabel?() ?? nil) ?? ""))
        #expect(jump.accessibilityPerformPress?() == true)
        try await stage.settle()
        #expect(abs(try #require(stage.reach).below) < 1)
    }

    @discardableResult
    func until<T>(_ read: () -> T?) async throws -> T {
        for _ in 0 ..< 1200 {
            if let value = read() { return value }
            try await Task.sleep(for: .milliseconds(25))
        }
        throw HandoverTests.NotReached()
    }
}
#endif
