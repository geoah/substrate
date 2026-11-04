import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// Substrate Tasks' screens in the states the app shows them in, made by
/// its presenters from fixed records, an instant and English in UTC: what
/// each UI's gallery draws, and what a renderer is checked against.
public enum TasksFixtures {
    public static let now = ScreenFixtures.now
    static let locale = Locale(identifier: "en_US")
    static let timeZone = TimeZone(identifier: "UTC")!
    static let build = BuildFacts(version: "0.1.0 (1)")

    /// A screen of the app, by name.
    public struct Fixture: Sendable, Identifiable {
        public var name: String
        public var app: TasksPresentation
        public var screen: Screen

        public var id: String { name }
    }

    public enum Screen: Sendable {
        case sidebar
        case list
        case detail
        case editor(ChildKey)
        case confirmation
        case settings(SettingsPresentation)
    }

    static func record(_ id: String, _ kind: String, _ properties: [String: JSONValue]) -> (String, Record) {
        ("\(kind)/\(id)", Record(id: id, kind: kind, properties: properties, version: 2, createdAt: "2026-11-01T10:00:00Z"))
    }

    static func list(_ id: String) -> JSONValue { .string("\(TasksKinds.list)/\(id)") }

    /// Two lists and the tasks of a week.
    static var records: [String: Record] {
        Dictionary(uniqueKeysWithValues: [
            record("groceries", TasksKinds.list, ["name": "Groceries", "position": "1"]),
            record("home", TasksKinds.list, ["name": "Home", "position": "2"]),
            record("milk", TasksKinds.task, ["name": "Oat milk", "list": list("groceries"), "position": "1", "status": "open", "priority": "none"]),
            record(
                "bread", TasksKinds.task,
                [
                    "name": "Sourdough bread", "list": list("groceries"), "position": "2", "status": "open",
                    "priority": "none", "dueDay": "2026-11-03",
                ]),
            record(
                "coffee", TasksKinds.task,
                [
                    "name": "Coffee beans", "list": list("groceries"), "position": "3", "status": "done",
                    "priority": "none", "completedAt": "2026-11-02T18:00:00Z",
                ]),
            record(
                "rent", TasksKinds.task,
                [
                    "name": "Pay rent", "list": list("home"), "position": "1", "status": "open", "priority": "urgent",
                    "dueDay": "2026-11-02",
                ]),
            record(
                "plumber", TasksKinds.task,
                [
                    "name": "Call the plumber", "list": list("home"), "position": "2", "status": "open",
                    "priority": "high", "dueDay": "2026-11-03", "dueAt": "2026-11-03T15:00:00Z",
                    "description": "About the kitchen tap. Ask for Thursday morning.",
                    "url": "https://example.com/plumbers",
                ]),
            record(
                "passport", TasksKinds.task,
                [
                    "name": "Renew passport", "status": "open", "priority": "medium", "position": "1",
                    "dueDay": "2026-11-20",
                ]),
        ])
    }

    static func state(
        _ selection: ListSelection?, open: String? = nil, confirmation: Confirmation? = nil,
        editors: [ChildKey: EditorSeed] = [:]
    ) -> TasksState {
        var state = TasksState()
        state.linked.server = records
        state.linked.records = records
        state.linked.loaded = true
        state.linked.link = LinkSnapshot(phase: .signedIn, repository: "ada.example.com", online: true)
        state.linked.phase = .following
        state.settingsRead = true
        state.section = selection == .today ? TasksMenu.today : TasksMenu.lists
        state.selection = selection
        state.openTask = open
        state.confirmation = confirmation
        state.editors = editors
        state.permission = .on
        state.usage = [AreaUsage(area: .records, bytes: 18_400, files: 1), AreaUsage(area: .outbox, bytes: 412, files: 1)]
        return state
    }

    static func present(_ state: TasksState, children: [ChildKey: TaskEditorPresentation] = [:]) -> TasksPresentation {
        TasksPresenter.present(
            PresenterInput(
                state: state, transient: NoTransient(), children: children, now: now, locale: locale,
                timeZone: timeZone, link: state.linked.link), linked: app.linked, device: "this iPhone",
            build: build)
    }

    static let app = TasksApp(cache: nil, outbox: nil, words: TasksApp.words(device: "this iPhone"), device: "this iPhone", build: build)

    static func editor(_ seed: EditorSeed, in state: TasksState) -> TaskEditorPresentation? {
        let key = ChildKey("editor")
        var withEditor = state
        withEditor.editors = [key: seed]
        let environment = AppEnvironment(locale: locale, timeZone: timeZone, devices: DeviceServices(transport: URLSessionTransport.shared))
        guard let editor = app.children(in: withEditor, environment: environment)[key] else { return nil }
        return editor.present(PresenterInput(state: editor.initialState, transient: NoTransient(), now: now, locale: locale, timeZone: timeZone))
    }

    /// The Settings screen: the app's sections, then the kit's, as the
    /// link presents them in the kit's own fixtures.
    static func settings(_ app: TasksPresentation) -> SettingsPresentation {
        var status: StatusPresentation?
        var repository: RepositoryPresentation?
        for fixture in ScreenFixtures.all {
            if fixture.name == "status-up-to-date", case let .status(presented) = fixture.presentation { status = presented }
            if fixture.name == "repository", case let .repository(presented) = fixture.presentation { repository = presented }
        }
        guard let status else { return SettingsPresentation(sections: app.settings) }
        let link = LinkPresentation<TasksPresentation>(
            snapshot: LinkSnapshot(phase: .signedIn, repository: "ada.example.com", online: true),
            repository: repository, banner: nil, ended: nil, status: status, signOutProblem: nil,
            app: app)
        return TasksSettings.screen(link, app)
    }

    public static var all: [Fixture] {
        let lists = present(state(.list("groceries")))
        let today = present(state(.today))
        let detail = present(state(.list("home"), open: "plumber"))
        let key = ChildKey("editor")
        let editState = state(.list("home"), open: "plumber", editors: [key: .task("plumber")])
        let edit = present(editState, children: editor(.task("plumber"), in: editState).map { [key: $0] } ?? [:])
        var notKeptState = editState
        notKeptState.saves = [key: .notKept("This iPhone could not keep the change: the disk is full.")]
        let notKept = present(notKeptState, children: editor(.task("plumber"), in: notKeptState).map { [key: $0] } ?? [:])
        let newState = state(.list("groceries"), editors: [key: .new(list: "groceries", day: nil)])
        let new = present(newState, children: editor(.new(list: "groceries", day: nil), in: newState).map { [key: $0] } ?? [:])
        let confirm = present(state(.list("home"), confirmation: .deleteList(id: "home")))
        var empty = state(.inbox)
        empty.linked.records = [:]
        let nothing = present(empty)
        return [
            Fixture(name: "tasks-sidebar", app: lists, screen: .sidebar),
            Fixture(name: "tasks-list", app: lists, screen: .list),
            Fixture(name: "tasks-today", app: today, screen: .list),
            Fixture(name: "tasks-empty-inbox", app: nothing, screen: .list),
            Fixture(name: "tasks-detail", app: detail, screen: .detail),
            Fixture(name: "tasks-edit", app: edit, screen: .editor(key)),
            Fixture(name: "tasks-edit-not-kept", app: notKept, screen: .editor(key)),
            Fixture(name: "tasks-new", app: new, screen: .editor(key)),
            Fixture(name: "tasks-delete-list", app: confirm, screen: .confirmation),
            Fixture(name: "tasks-settings", app: lists, screen: .settings(settings(lists))),
        ]
    }
}
