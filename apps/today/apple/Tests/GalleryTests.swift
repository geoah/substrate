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

    /// What the day's lines were written from, from the core's own day: a
    /// project's timeline by day and by source, what is happening and the
    /// night opened, each line with its links; and the same with no page
    /// read yet, where a line has its record alone and a source no channel.
    @Test func theSourcesOfTheDaysLinesDraw() async throws {
        let afternoon = TodayFixtures.present(TodayFixtures.state(at: "13:00"), at: "13:00")
        try await both("sources-project-extapi", size: CGSize(width: 393, height: 2500)) { DayScreen(app: afternoon, path: [.project("extapi")]) }
        try await write("sources-project-security", DayScreen(app: afternoon, path: [.project("security")]), size: CGSize(width: 393, height: 2500))
        try await write("sources-wide-project-extapi", DayScreen(app: afternoon, path: [.project("extapi")]), size: CGSize(width: 1180, height: 1900))
        try await both("sources-happening", size: CGSize(width: 393, height: 2200)) { DayScreen(app: afternoon, opened: ["happening"]) }
        let waking = TodayFixtures.present(TodayFixtures.state(at: "07:05"), at: "07:05")
        try await write(
            "sources-night", DayScreen(app: waking, opened: ["night", "night:extapi", "night:lantern", "night:\(TodayKinds.unfiled)"]),
            size: CGSize(width: 393, height: 2600))
        let unread = TodayFixtures.present(TodayFixtures.state(at: "13:00") { $0.sources = [:] }, at: "13:00")
        try await write("sources-unread-project-extapi", DayScreen(app: unread, path: [.project("extapi")]), size: CGSize(width: 393, height: 2500))
        try await write("sources-unread-happening", DayScreen(app: unread, opened: ["happening"]), size: CGSize(width: 393, height: 2200))
    }

    /// A meeting is eight minutes away and a morning's routines were not
    /// logged: the line that says what waits, closed, and opened to the rows
    /// it holds, one of them opened in turn to be logged.
    @Test func whatWaitsBeforeAMeetingOpensToItsRows() async throws {
        let unlogged = TodayFixtures.state(at: "07:52") { state in
            for (key, record) in state.linked.records where record.kind == TodayKinds.log { state.linked.records[key] = nil }
        }
        let app = TodayFixtures.present(unlogged, at: "07:52")
        let lines = app.pane.items.compactMap { item -> ParkedLine? in if case let .parked(line) = item { line } else { nil } }
        let line = try #require(lines.first)
        let routine = try #require(line.rows.first { if case .routine = $0.body { true } else { false } })
        try await both("waiting-closed", size: CGSize(width: 393, height: 1500)) { DayScreen(app: app) }
        try await both("waiting-open", size: CGSize(width: 393, height: 2000)) { DayScreen(app: app, opened: ["parked:" + line.id]) }
        try await write(
            "waiting-open-routine", DayScreen(app: app, opened: ["parked:" + line.id, "row:" + routine.id]), size: CGSize(width: 393, height: 2300))
    }

    /// Two doses of a routine were not logged yesterday: the card names
    /// them, each with a tick of its own, over the options for the rest;
    /// and again once one is ticked and the other still waits.
    @Test func whatARoutineLeftYesterdayIsDrawnLineByLine() async throws {
        func missed(_ kind: String, _ id: String) -> Bool {
            kind == TodayKinds.log && (id.hasPrefix("mag_20261118") || id.hasPrefix("omega_20261118"))
        }
        let left = TodayFixtures.state(at: "07:05") { state in
            for (key, record) in state.linked.records where missed(record.kind, record.id) { state.linked.records[key] = nil }
        }
        try await both("yesterday-lines", size: CGSize(width: 393, height: 1500)) {
            DayScreen(app: TodayFixtures.present(left, at: "07:05"), opened: ["row:yesterday"])
        }
        // One of the two was ticked this morning.
        let ticked = TodayFixtures.state(at: "07:05") { state in
            for (key, record) in state.linked.records where missed(record.kind, record.id) {
                if record.id.hasPrefix("mag_") {
                    state.linked.records[key]?.properties["at"] = .string("2026-11-19T07:02:00Z")
                } else {
                    state.linked.records[key] = nil
                }
            }
        }
        try await write(
            "yesterday-lines-one-ticked", DayScreen(app: TodayFixtures.present(ticked, at: "07:05"), opened: ["row:yesterday"]),
            size: CGSize(width: 393, height: 1500))
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

    /// The day with something being finished, at a fixed instant of its
    /// wait, and with what is done shown and hidden.
    @Test func finishingDraws() async throws {
        guard let working = TodayFixtures.all.first(where: { $0.name == "today-working" }) else { return }
        let began = Date(timeIntervalSince1970: 1_800_000_000)
        // The first task that is a closed row: it is in view under the current item.
        guard
            let task = working.app.pane.items.compactMap({ item -> Row? in
                if case let .row(row) = item, row.id.hasPrefix("task:"), row.state != .now { row } else { nil }
            }).first
        else { return }
        let command = TodayCommand.done(String(task.id.dropFirst("task:".count)))
        for (name, after) in [("counting", 1.2), ("counted", 4.0)] {
            try await write(
                "finish-\(name)", DayScreen(app: working.app, showsDone: true, finishing: ([command], began, began.addingTimeInterval(after))),
                size: CGSize(width: 393, height: 2600))
        }
        try await both("done-shown", size: CGSize(width: 393, height: 2600)) { DayScreen(app: working.app, showsDone: true) }
        try await write("done-hidden", DayScreen(app: working.app, showsDone: false), size: CGSize(width: 393, height: 2600))
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
        // Chips that wrap stand above the list under them, never over it.
        try await write(
            "large-text-guide-options", ComponentSheets.sheet("options").environment(\.dynamicTypeSize, .accessibility2),
            size: CGSize(width: 393, height: 4200))
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
