#if os(macOS)
import AppKit
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import Testing
import TodayCore

@testable import SubstrateTodayUI

/// The app's screens and the design guide's components, drawn by the views
/// the app draws them with, in its UI framework's tests, which run on the
/// Mac with no host app: each must draw, and with SUBSTRATE_GALLERY naming
/// a folder (`mise run apps:gallery`), each is written there as a PNG, at
/// an iPhone's size, an iPad's and a Mac's, in light and in dark.
@MainActor
@Suite struct GalleryTests {
    static let phone = OffscreenDrawing.phone
    static let tall = CGSize(width: 393, height: 1500)
    static let pad = CGSize(width: 1180, height: 820)

    let folder = ProcessInfo.processInfo.environment["SUBSTRATE_GALLERY"].map { URL(fileURLWithPath: $0) }

    func write(_ name: String, _ view: some View, size: CGSize = phone, scheme: ColorScheme = .light) async throws {
        let png = try await OffscreenDrawing.png(view.environment(\.drawsGlass, false), size: size, scheme: scheme)
        #expect(!png.isEmpty)
        guard let folder else { return }
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        try png.write(to: folder.appendingPathComponent("\(name)\(scheme == .dark ? "-dark" : "").png"))
    }

    func both(_ name: String, size: CGSize = phone, _ view: () -> some View) async throws {
        try await write(name, view(), size: size)
        try await write(name, view(), size: size, scheme: .dark)
    }

    // MARK: The core's scenes

    /// Every scene the core's fixtures hold, as the app draws it: the day
    /// at a phone's size in light and dark, the whole day drawn long, and
    /// each page the fixture names.
    @Test func everyFixtureDraws() async throws {
        for fixture in TodayFixtures.all {
            try await both("scene-\(fixture.name)") { DayScreen(app: fixture.app) }
            try await write("long-scene-\(fixture.name)", DayScreen(app: fixture.app), size: CGSize(width: 393, height: 3600))
            for route in fixture.routes {
                let name = "scene-\(fixture.name)-\(Self.slug(route))"
                try await both(name) { DayScreen(app: fixture.app, path: [route]) }
                try await write("long-\(name)", DayScreen(app: fixture.app, path: [route]), size: CGSize(width: 393, height: 3000))
            }
        }
    }

    /// A hundred suggestions grouped by project are taller than the long
    /// drawing every page gets, which ends before the last group's Save:
    /// this one is drawn to its end.
    @Test func theBacklogPageDrawsDownToItsLastSave() async throws {
        guard let backlog = TodayFixtures.all.first(where: { $0.name == "today-backlog" }) else { return }
        try await write("tall-scene-today-backlog-suggested", DayScreen(app: backlog.app, path: [.suggested]), size: CGSize(width: 393, height: 7200))
    }

    /// The wide layout from the core's own day, at an iPad's size and a Mac's.
    @Test func theCoreDayDrawsWide() async throws {
        guard let working = TodayFixtures.all.first(where: { $0.name == "today-working" }) else { return }
        try await both("wide-ipad-today-working", size: Self.pad) { DayScreen(app: working.app) }
        try await both("wide-mac-today-working", size: OffscreenDrawing.mac) { DayScreen(app: working.app) }
        if let route = working.routes.first(where: { if case .project = $0 { true } else { false } }) {
            try await write("wide-ipad-today-working-project", DayScreen(app: working.app, path: [route]), size: Self.pad)
        }
    }

    static func slug(_ route: TodayRoute) -> String {
        switch route {
        case .priorities: "priorities"
        case .projects: "projects"
        case let .project(id): "project-\(id)"
        case let .person(id): "person-\(id.split(separator: "/").last.map(String.init) ?? id)"
        case let .meeting(id): "meeting-\(id.split(separator: "/").last.map(String.init) ?? id)"
        case let .task(id): "task-\(id)"
        case .plan: "plan"
        case .suggested: "suggested"
        }
    }

    // MARK: Hand-made scenes

    @Test func theDayAtEachMoment() async throws {
        let scenes: [(String, PanePresentation)] = [
            ("wake", HandScenes.wake), ("gate", HandScenes.gate), ("focus", HandScenes.focus()), ("marked", HandScenes.focus(marked: true)),
            ("interrupt", HandScenes.interrupt()), ("unreachable", HandScenes.interrupt(unchecked: true)),
            ("chosen", HandScenes.interrupt(chosen: true)), ("premeet", HandScenes.premeet), ("closing", HandScenes.closing),
        ]
        for (name, pane) in scenes {
            try await both("hand-\(name)") { DayScreen(app: HandScenes.app(pane)) }
        }
    }

    /// The same scenes drawn long, so what is below the fold is seen too.
    @Test func theDayBelowTheFold() async throws {
        let opened: Set<String> = ["happening", "night", "earlier"]
        try await write("long-focus", DayScreen(app: HandScenes.app(HandScenes.focus()), opened: opened), size: CGSize(width: 393, height: 3400))
        try await write(
            "long-focus", DayScreen(app: HandScenes.app(HandScenes.focus()), opened: opened), size: CGSize(width: 393, height: 3400), scheme: .dark)
        try await write(
            "long-marked", DayScreen(app: HandScenes.app(HandScenes.focus(marked: true)), opened: ["happening", "happening:rest"]),
            size: CGSize(width: 393, height: 2600))
        try await write(
            "long-wake", DayScreen(app: HandScenes.app(HandScenes.wake), opened: ["night", "night:security", "row:y-b12"]),
            size: CGSize(width: 393, height: 3000))
        try await write("long-gate", DayScreen(app: HandScenes.app(HandScenes.gate)), size: CGSize(width: 393, height: 2600))
        try await write("long-premeet", DayScreen(app: HandScenes.app(HandScenes.premeet)), size: CGSize(width: 393, height: 2600))
        try await write("long-closing", DayScreen(app: HandScenes.app(HandScenes.closing)), size: CGSize(width: 393, height: 2400))
        try await write("long-chosen", DayScreen(app: HandScenes.app(HandScenes.interrupt(chosen: true))), size: CGSize(width: 393, height: 2000))
        let asked = AnswerCard(
            id: "a1", heading: "You asked Moss", question: "What can I drop this afternoon?",
            text: "Four slots are double-booked. Henrik and Karim both start at 15:00. Callum at 17:20 sits inside Lantern standup.", state: .done,
            sources: ["Calendar"], button: ButtonPresentation("Close", role: .quiet, command: .closeAnswer("a1")))
        try await both("hand-answer") { DayScreen(app: HandScenes.app(HandScenes.focus(answer: asked))) }
    }

    @Test func everyPageDraws() async throws {
        let app = HandScenes.app(HandScenes.focus())
        let pages: [(String, TodayRoute, CGFloat)] = [
            ("priorities", .priorities, 1500), ("projects", .projects, 1100), ("project", .project("api"), 2600), ("person", .person("tomas"), 1700),
            ("meeting", .meeting("tomas"), 1800), ("task", .task("t-vigil"), 1300), ("plan", .plan, 3400), ("suggested", .suggested, 3400),
            ("missing", .task("gone"), 852),
        ]
        for (name, route, height) in pages {
            try await both("page-\(name)") { DayScreen(app: app, path: [route]) }
            try await write("long-page-\(name)", DayScreen(app: app, path: [route]), size: CGSize(width: 393, height: height))
        }
        try await write(
            "long-page-plan-open", DayScreen(app: app, path: [.plan], opened: ["plan:more:eu", "plan:flag:c1"]),
            size: CGSize(width: 393, height: 3000))
    }

    /// One wide layout of the same pane, at an iPad's size and a Mac's.
    @Test func theWideLayoutDraws() async throws {
        let app = HandScenes.app(HandScenes.focus())
        try await both("wide-ipad", size: Self.pad) { DayScreen(app: app) }
        try await both("wide-mac", size: OffscreenDrawing.mac) { DayScreen(app: app) }
        try await write("wide-ipad-project", DayScreen(app: app, path: [.project("api")]), size: Self.pad)
        try await write("wide-ipad-plan", DayScreen(app: app, path: [.plan]), size: Self.pad)
        try await write("wide-ipad-suggested", DayScreen(app: app, path: [.suggested]), size: Self.pad)
    }

    /// Large text: rows stack and never clip.
    @Test func largeTextDraws() async throws {
        let app = HandScenes.app(HandScenes.focus())
        try await write(
            "large-text-focus", DayScreen(app: app).environment(\.dynamicTypeSize, .accessibility2), size: CGSize(width: 393, height: 2800))
        try await write(
            "large-text-priorities", DayScreen(app: app, path: [.priorities]).environment(\.dynamicTypeSize, .accessibility2),
            size: CGSize(width: 393, height: 2600))
        try await write(
            "large-text-suggested", DayScreen(app: app, path: [.suggested]).environment(\.dynamicTypeSize, .accessibility2),
            size: CGSize(width: 393, height: 5200))
    }

    /// Settings' own part: the assistant's standing instructions, one on,
    /// one off and one the owner wrote, each with its switch and Edit, and
    /// the editor of one.
    @Test func howMossBehavesDraws() async throws {
        let moss = MossSettings(
            rows: [
                MossSettings.Row(
                    id: "a", text: "Suggest, never act. Every change is a proposal the owner accepts. Never send a message; draft it.", on: true,
                    source: "Came with a package"),
                MossSettings.Row(
                    id: "b", text: "Never estimate how long something takes or whether the day fits. Only real times.", on: false,
                    source: "Came with a package"),
                MossSettings.Row(id: "c", text: "Write to me in British English.", on: true, source: "Yours"),
            ], footer: "These are Moss's instructions wherever it works, not this app's alone.")
        try await both("settings-moss", size: CGSize(width: 393, height: 760)) {
            Form { MossSettingsSection(moss: moss, send: { _ in }, edit: { _ in }) }.formStyle(.grouped)
        }
        try await write(
            "settings-moss-empty",
            Form { MossSettingsSection(moss: MossSettings(empty: "Moss has no standing instructions."), send: { _ in }, edit: { _ in }) }.formStyle(.grouped),
            size: CGSize(width: 393, height: 300))
        try await write("settings-moss-edit", InstructionEditor(moss: moss, row: moss.rows[0]) { _ in }, size: CGSize(width: 393, height: 520))
    }

    // MARK: The design guide's components

    @Test func everyComponentDraws() async throws {
        for (name, height) in ComponentSheets.all {
            try await both("guide-\(name)", size: CGSize(width: 393, height: height)) { ComponentSheets.sheet(name) }
        }
    }
}
#endif
