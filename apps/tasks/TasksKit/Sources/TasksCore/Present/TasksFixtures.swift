import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// Substrate Tasks' screens in the states the app shows them in, made by
/// its presenters from fixed records, an instant and English in UTC: what
/// each UI's gallery draws, and what a renderer is checked against. Two
/// collections are on: the app's own tasks in their lists, and a chore kind
/// of the owner's grouped by area, with a suggestion in each.
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
        case queue
        case suggestion
        case choice
        case collections
        case collection
        case approval
    }

    static let chore = "ada.example.com/work/chore"
    static let area = "ada.example.com/work/area"

    static func record(_ id: String, _ kind: String, _ properties: [String: JSONValue], created: String = "2026-11-01T10:00:00Z")
        -> (String, Record)
    {
        var properties = properties
        if properties["title"] == nil, let name = properties["name"] ?? properties["summary"] { properties["title"] = name }
        return ("\(kind)/\(id)", Record(id: id, kind: kind, properties: properties, version: 2, createdAt: created))
    }

    static func list(_ id: String) -> JSONValue { .string("\(TasksKinds.list)/\(id)") }
    static func garden(_ id: String) -> JSONValue { .string("\(area)/\(id)") }

    /// A chore kind of the owner's: an enum status renamed `state` that
    /// takes suggestions, due at the moment its temporal binding names (a
    /// property the trait supplies), grouped by area.
    static var declarations: [TaskKindDeclaration] {
        let chore: [String: JSONValue] = [
            "traits": ["substrate.reamde.dev/core/temporal(point: dueAt)", "substrate.reamde.dev/core/lifecycle(status: state)"],
            "displayTemplate": "{summary}",
            "properties": .object(
                JSONObject([
                    ("summary", ["type": "string", "required": true]),
                    ("notes", ["type": "markdown"]),
                    (
                        "state",
                        [
                            "type": "enum", "values": ["suggested", "todo", "doing", "closed", "skipped"], "done": ["closed"],
                            "dropped": ["skipped"], "proposed": ["suggested"],
                        ]
                    ),
                    ("priority", ["type": "enum", "values": ["low", "normal", "high"]]),
                    ("area", ["type": "reference", "kind": .string(area)]),
                    ("effort", ["type": "string"]),
                ])),
        ]
        return [TaskKindDeclaration(identity: Self.chore, singular: "Chore", plural: "Chores", definition: chore)]
    }

    /// Two lists, two areas, the tasks of a week, two suggestions, and the
    /// settings that turn the chores on and pin two groups.
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
                ["name": "Renew passport", "status": "open", "priority": "medium", "position": "1", "dueDay": "2026-11-20"]),
            record(
                "insurance", TasksKinds.task,
                ["name": "Renew the car insurance", "list": list("home"), "position": "3", "status": "open", "priority": "high"]),
            record(
                "plants", TasksKinds.task,
                ["name": "Water the plants", "list": list("home"), "position": "4", "status": "open", "priority": "low", "dueDay": "2026-11-05"]),
            record("shelf", TasksKinds.task, ["name": "Put up the shelf", "list": list("home"), "position": "5", "status": "open"]),
            record(
                "maya", TasksKinds.task,
                [
                    "name": "Reply to Maya Chen about the platform role", "status": "proposed",
                    "description": "Maya Chen, a recruiter at Northwind, asked whether you are open to a call about a platform role.",
                ], created: "2026-11-03T07:00:00Z"),
            record("garden", area, ["name": "Garden"]),
            record("garage", area, ["name": "Garage"]),
            record(
                "gutters", chore,
                ["summary": "Clean the gutters", "state": "todo", "dueAt": "2026-11-01T12:00:00Z", "area": garden("garden"), "effort": "An hour"]),
            record("mower", chore, ["summary": "Sharpen the mower", "state": "doing", "priority": "high", "area": garden("garage")]),
            record(
                "fence", chore,
                ["summary": "Fix the fence", "state": "suggested", "area": garden("garden"), "notes": "Two panels blew down in the storm."],
                created: "2026-11-02T16:00:00Z"),
            record(
                "c-chore", TasksKinds.collection, ["kind": .string(chore), "enabled": true, "title": .string(chore)]),
            record(TasksPreference.id, TasksKinds.preference, ["pins": [list("home"), garden("garden")]]),
        ])
    }

    static func state(
        _ selection: ListSelection?, open: String? = nil, confirmation: Confirmation? = nil,
        editors: [ChildKey: EditorSeed] = [:]
    ) -> TasksState {
        var state = TasksState()
        let all = records
        let data = all.filter { !TasksKinds.settings.contains($0.value.kind) }
        let settings = all.filter { TasksKinds.settings.contains($0.value.kind) }
        state.linked.server = data
        state.linked.records = data
        state.linked.loaded = true
        state.linked.link = LinkSnapshot(phase: .signedIn, repository: "ada.example.com", online: true)
        state.linked.phase = .following
        state.settingsLinked.server = settings
        state.settingsLinked.records = settings
        state.settingsLinked.loaded = true
        state.declarations = declarations
        state.settingsRead = true
        state.section = selection == .needsYou || selection == .suggestions ? TasksMenu.needsYou : TasksMenu.lists
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
                timeZone: timeZone, link: state.linked.link), app: app)
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

    static func path(_ kind: String, _ id: String) -> String { "\(kind)/\(id)" }

    public static var all: [Fixture] {
        let lists = present(state(.list("groceries")))
        let needsYou = present(state(.needsYou))
        var completingState = state(.needsYou)
        completingState.pending = [
            path(TasksKinds.task, "rent"): PendingDecision(decision: .complete, value: "done", write: LocalWrite(entries: []))
        ]
        let completing = present(completingState)
        var narrowedState = state(.needsYou)
        narrowedState.filter = .list("home")
        let narrowed = present(narrowedState)
        let detail = present(state(.list("home"), open: path(TasksKinds.task, "plumber")))
        let choreDetail = present(state(.group(path(area, "garden")), open: path(chore, "gutters")))
        let key = ChildKey("editor")
        let plumber = path(TasksKinds.task, "plumber")
        let editState = state(.list("home"), open: plumber, editors: [key: .task(plumber)])
        let edit = present(editState, children: editor(.task(plumber), in: editState).map { [key: $0] } ?? [:])
        var notKeptState = editState
        notKeptState.saves = [key: .notKept("This iPhone could not keep the change: the disk is full.")]
        let notKept = present(notKeptState, children: editor(.task(plumber), in: notKeptState).map { [key: $0] } ?? [:])
        let newState = state(.list("groceries"), editors: [key: .new(list: "groceries", day: nil)])
        let new = present(newState, children: editor(.new(list: "groceries", day: nil), in: newState).map { [key: $0] } ?? [:])
        let confirm = present(state(.list("home"), confirmation: .deleteList(id: "home")))
        var empty = state(.inbox)
        empty.linked.records = empty.linked.records.filter { $0.value.kind != TasksKinds.task }
        let nothing = present(empty)
        let queue = present(state(.suggestions))
        var pickingState = state(.suggestions)
        pickingState.picked = [path(chore, "fence")]
        let picking = present(pickingState)
        var suggestionState = state(.suggestions, open: path(chore, "fence"))
        suggestionState.edits = [path(chore, "fence"): ["priority": "high"]]
        let suggestion = present(suggestionState)
        var choosingState = state(.suggestions)
        choosingState.choice = DecisionChoice(task: path(chore, "fence"), decision: .accept, values: ["todo", "doing"])
        let choosing = present(choosingState)
        let group = present(state(.group(path(area, "garden"))))
        var collectionsState = state(.needsYou)
        collectionsState.section = TasksMenu.settings
        collectionsState.settingsPage = .collections
        let collections = present(collectionsState)
        var collectionState = collectionsState
        collectionState.settingsPage = .collection(chore)
        let collection = present(collectionState)
        var approvalState = collectionsState
        approvalState.approval = ApprovalAsk(collection: chore, scopes: ["write:\(chore)"], held: nil)
        let approval = present(approvalState)
        return [
            Fixture(name: "tasks-sidebar", app: lists, screen: .sidebar),
            Fixture(name: "tasks-list", app: lists, screen: .list),
            Fixture(name: "tasks-needs-you", app: needsYou, screen: .list),
            Fixture(name: "tasks-needs-you-undo", app: completing, screen: .list),
            Fixture(name: "tasks-needs-you-narrowed", app: narrowed, screen: .list),
            Fixture(name: "tasks-empty-inbox", app: nothing, screen: .list),
            Fixture(name: "tasks-detail", app: detail, screen: .detail),
            Fixture(name: "tasks-detail-chore", app: choreDetail, screen: .detail),
            Fixture(name: "tasks-edit", app: edit, screen: .editor(key)),
            Fixture(name: "tasks-edit-not-kept", app: notKept, screen: .editor(key)),
            Fixture(name: "tasks-new", app: new, screen: .editor(key)),
            Fixture(name: "tasks-delete-list", app: confirm, screen: .confirmation),
            Fixture(name: "tasks-settings", app: lists, screen: .settings(settings(lists))),
            Fixture(name: "tasks-suggestions", app: queue, screen: .queue),
            Fixture(name: "tasks-suggestions-select", app: picking, screen: .queue),
            Fixture(name: "tasks-suggestion", app: suggestion, screen: .suggestion),
            Fixture(name: "tasks-accept-as", app: choosing, screen: .choice),
            Fixture(name: "tasks-group", app: group, screen: .list),
            Fixture(name: "tasks-collections", app: collections, screen: .collections),
            Fixture(name: "tasks-collection", app: collection, screen: .collection),
            Fixture(name: "tasks-approval", app: approval, screen: .approval),
        ]
    }
}
