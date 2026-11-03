#if os(macOS)
import AppKit
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore
import Testing
@testable import SubstrateTasksUI

/// Substrate Tasks' screens drawn from TasksKit's fixtures by the views the
/// app draws them with: each fixture must have its view, and with
/// SUBSTRATE_TASKS_GALLERY naming a folder (`mise run apps:gallery`), each
/// is written there as a PNG at an iPhone's size, and the split layout
/// at a Mac window's, in light mode. A system
/// dialog (the delete-list question) has no offscreen drawing; its words are
/// held by TasksKit's tests.
@MainActor
@Suite struct GalleryTests {
    static let size = CGSize(width: 393, height: 852)
    static let wide = CGSize(width: 1180, height: 760)

    var link: LinkPresentation<TasksPresentation>? {
        for fixture in ScreenFixtures.all where fixture.name == "status-up-to-date" {
            if case let .status(status) = fixture.presentation {
                return LinkPresentation(snapshot: LinkSnapshot(phase: .signedIn, repository: "ada.example.com", online: true),
                                        repository: nil, banner: nil, ended: nil, status: status, signOutProblem: nil, app: nil)
            }
        }
        return nil
    }

    func view(_ fixture: TasksFixtures.Fixture, link: LinkPresentation<TasksPresentation>) -> AnyView? {
        let app = fixture.app
        switch fixture.screen {
        case .sidebar:
            return AnyView(NavigationStack { SidebarView(sidebar: app.sidebar, link: link, app: app, menu: app.menu, select: { _ in }) })
        case .list:
            return app.list.map { list in AnyView(NavigationStack { TaskListView(list: list, app: app, link: link) }) }
        case .detail:
            return app.detail.map { detail in AnyView(NavigationStack { TaskDetailView(detail: detail) }) }
        case let .editor(key):
            return app.editors[key].map { AnyView(TaskEditorView(key: key, editor: $0)) }
        case .confirmation:
            return nil
        case let .settings(settings):
            return AnyView(NavigationStack { SettingsScreen(settings) { _ in }.navigationTitle("Settings") })
        }
    }

    @Test func everyFixtureHasItsView() throws {
        let link = try #require(link)
        for fixture in TasksFixtures.all {
            if case .confirmation = fixture.screen { continue }
            #expect(view(fixture, link: link) != nil, "\(fixture.name) has no view")
        }
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["SUBSTRATE_TASKS_GALLERY"] != nil))
    func everyFixtureDraws() async throws {
        let directory = URL(fileURLWithPath: ProcessInfo.processInfo.environment["SUBSTRATE_TASKS_GALLERY"] ?? NSTemporaryDirectory())
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let link = try #require(link)
        let shell = TasksShell(drawing: ())
        for fixture in TasksFixtures.all {
            guard let view = view(fixture, link: link) else { continue }
            try await draw(view.environment(shell), to: directory.appendingPathComponent("\(fixture.name).png"))
        }
        // The Mac's and the iPad's layout: the sidebar, a list and the open
        // task side by side, at a Mac window's size.
        for fixture in TasksFixtures.all where fixture.name == "tasks-detail" || fixture.name == "tasks-today" {
            try await draw(SplitRoot(link: link, app: fixture.app).environment(shell),
                           to: directory.appendingPathComponent("\(fixture.name)-wide.png"), size: Self.wide)
        }
    }

    /// Hosts the view in an offscreen window, lets it settle, and writes
    /// what it drew, as the kit's gallery does.
    func draw(_ view: some View, to file: URL, size: CGSize = GalleryTests.size) async throws {
        _ = NSApplication.shared
        let hosting = NSHostingView(rootView: view
            .frame(width: size.width, height: size.height)
            .background(Color(nsColor: .windowBackgroundColor))
            .environment(\.colorScheme, .light)
            .environment(\.controlActiveState, .key))
        hosting.frame = CGRect(origin: .zero, size: size)
        let window = NSWindow(contentRect: hosting.frame, styleMask: [.borderless], backing: .buffered, defer: false)
        window.appearance = NSAppearance(named: .aqua)
        window.contentView = hosting
        hosting.layoutSubtreeIfNeeded()
        try await Task.sleep(for: .milliseconds(300))
        hosting.layoutSubtreeIfNeeded()
        let rep = try #require(hosting.bitmapImageRepForCachingDisplay(in: hosting.bounds))
        hosting.cacheDisplay(in: hosting.bounds, to: rep)
        let png = try #require(rep.representation(using: .png, properties: [:]))
        try png.write(to: file)
        window.contentView = nil
    }
}
#endif
