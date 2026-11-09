import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// Substrate Tasks' root, kept by the link (`LinkController<TasksApp>`).
/// Its tasks are the records of every collection the owner turned on, any
/// kind with a status (docs/plans/tasks-sources.md), kept by the kit's
/// `LinkedRecords`: read once, followed through the watch, closed and opened
/// again over a new set of kinds when a collection or its grouping changes,
/// every change written through the outbox. The owner's choices about the
/// collections are a second `LinkedRecords` over the settings kinds, which
/// stays open. Beside them it keeps the kinds the lifecycle trait's
/// implementors say have a status, the reminders this device shows, the
/// device's settings, where the owner is, and the task editors as its
/// children. Its presentation is every screen of the app; the link's is the
/// connection's, beside it.
public struct TasksApp: LinkedRecordsApp {
    public enum Command: Bridged, Equatable {
        case select(section: String)
        /// A list shown, or none: on a phone, back to the lists.
        case show(ListSelection?)
        /// A task or a suggestion open, by its record path.
        case open(task: String?)
        /// Needs you narrowed to one pinned group; nil widens it.
        case filter(ListSelection?)
        case quickAdd(String)
        case newTask
        case edit(String)
        /// Each decision waits `TasksApp.undoWindow` before it is written;
        /// `undo` within it takes it back before anything is written. With
        /// several values, the owner picks one first (`decide`).
        case complete(String)
        case abandon(String)
        case accept(String)
        case dismiss(String)
        /// A done or dropped task open again, at once; within a decision's
        /// window, its undo.
        case reopen(String)
        /// A decision on the value the owner picked.
        case decide(String, TaskDecision, to: String)
        case undo(String)
        case dismissChoice
        /// A suggestion's field set before it is accepted, by the field's
        /// property; nil clears it.
        case setSuggestion(String, property: String, to: JSONValue?)
        /// Picking several suggestions for one decision.
        case pick(Bool)
        case picked(String)
        case acceptPicked
        case dismissPicked
        /// A task's due day moved to a named day, or the editor to pick one.
        case reschedule(String, Reschedule)
        case setPriority(String, TaskPriority)
        case move(String, toList: String?)
        /// A task dragged to `index` among the open rows drawn.
        case reorderTask(String, to: Int)
        case reorderList(String, to: Int)
        case deleteTask(String)
        case createList(String)
        case renameList(String, to: String)
        case deleteList(String)
        case pin(String)
        case unpin(String)
        /// A collection turned on or off, by its kind.
        case setCollection(String, on: Bool)
        /// A collection grouped by one of its references, or by nothing.
        case setGrouping(String, property: String?)
        case setNewTasks(String)
        /// Approve, from a page's line: `write:` on the collection, or with
        /// `group` `read:` on that group kind.
        case requestApproval(String, group: String?)
        case showSettings(SettingsPage?)
        /// The owner went to the approval page (`completed`), whose answer the
        /// next scopes the link holds then give; or it closed with none.
        case approvalAnswered(completed: Bool)
        case dismissApproval
        case confirm
        case dismissConfirmation
        case settings(SettingsCommand)
        case clearSaved
        case refresh
        /// What the system may have changed is read again: the app came to
        /// the front, or the time zone moved.
        case foreground
        /// The owner tapped a reminder: the task's path it was scheduled with.
        case reminderTapped(String)
        /// The updater found a newer build, or none.
        case offered(String?)
        case dismissProblem
    }

    public enum Event: Codable, Equatable, Sendable, LinkedRecordsEvent {
        case records(LinkedRecords.Event)
        /// The settings link's own.
        case settings(LinkedRecords.Event)
        /// The kinds with a status the grant reads, or why they were not read.
        case discovered([TaskKindDeclaration])
        case discoveryFailed(String)
        case settingsRead(SettingsValues, usage: [AreaUsage])
        case sectionSelected(String)
        case shown(ListSelection?)
        case filtered(ListSelection?)
        case opened(String?)
        /// A decision made: the write its undo window ends in.
        case deciding(String, PendingDecision)
        /// Taken back within the window.
        case undone(String)
        case chose(DecisionChoice?)
        case suggestionSet(String, JSONObject)
        case picking([String]?)
        case editorOpened(ChildKey, EditorSeed)
        /// An editor's save: its write's entries, on their way to the outbox.
        case editorSaving(ChildKey, Set<UUID>)
        case editorClosed(ChildKey)
        case asked(Confirmation)
        case answered
        case settingsShown(SettingsPage?)
        case approvalAsked(ApprovalAsk?)
        case approvalAnswered(completed: Bool)
        /// A task could not be added, and why.
        case refused(String)
        case settingsChanged(SettingsValues)
        case permission(NotificationPermission)
        case scheduled([NotificationRequest])
        case tapped(String)
        case usage([AreaUsage])
        case offered(String?)
        case problemDismissed

        public var recordsEvent: LinkedRecords.Event? {
            if case let .records(event) = self { event } else { nil }
        }
    }

    public enum Effect: Equatable, Sendable, LinkedRecordsEffect {
        case records(LinkedRecords.Effect)
        case settings(LinkedRecords.Effect)
        /// The lifecycle trait's implementors the grant reads.
        case discover
        case readSettings
        case readPermission
        case askPermission
        case saveSettings(SettingsValues)
        case schedule([NotificationRequest])
        case readUsage
        /// Waits out a decision's undo window, then writes it.
        case settle(String, LocalWrite)
        /// Stops the wait: the decision was taken back.
        case unsettle(String)

        public var recordsEffect: LinkedRecords.Effect? {
            if case let .records(effect) = self { effect } else { nil }
        }
    }

    public typealias State = TasksState
    public typealias Presentation = TasksPresentation
    public typealias Child = TaskEditor

    public static let appName = "Substrate Tasks"
    /// How long a decision stays on its row before the write goes: long
    /// enough to see it and take it back.
    public static let undoWindow: TimeInterval = 3
    /// Where the app's settings are kept in the repository's folder.
    public static let settingsFile = SettingsFile<SettingsValues>()

    /// The data link: the collections that are on, and their group kinds.
    public let linked: LinkedRecords
    /// The settings link, over the settings kinds, which stays open.
    public let settingsLinked: LinkedRecords
    /// The app's own kinds' declarations, from the closure it carries.
    public let closure: [TaskKindDeclaration]
    /// "this iPhone", "this iPad", "this Mac".
    public let device: String
    public let build: BuildFacts

    /// - Parameters:
    ///   - words: the Status screen's words, as the shell made them
    ///     (`AppShell.Parts.words`, from `words(device:)`).
    ///   - settingsCache: the settings link's cache (`settingsCache(in:)`).
    ///   - closure: the app's own kinds' declarations, where the build read
    ///     its closure from a bundle of its own (Android's copied-out
    ///     resources).
    public init(
        cache: RecordCache?, outbox: Outbox?, words: StatusWords, device: String, build: BuildFacts,
        line: WriteLine = WriteLine(), settingsCache: RecordCache? = nil, closure: [TaskKindDeclaration]? = nil
    ) {
        linked = LinkedRecords(kinds: TasksKinds.all, cache: cache, outbox: outbox, words: words, line: line)
        settingsLinked = LinkedRecords(kinds: TasksKinds.settings, cache: settingsCache, outbox: outbox, words: words, line: line)
        self.closure = closure ?? (try? TasksPackage.bundled())?.declarations ?? []
        self.device = device
        self.build = build
    }

    /// The settings link's cache in a repository's folder: a file of its
    /// own, since a record cache keeps one cursor and each link follows its
    /// own watch.
    public static func settingsCache(in folder: RepositoryFolder?) -> RecordCache? {
        folder.map { RecordCache(folder: $0, name: "settings.json") }
    }

    /// The Status screen's words, and the linked records'.
    public static func words(device: String) -> StatusWords {
        StatusWords(app: appName, device: device, kept: "tasks", packageNames: [TasksKinds.packageID: "Tasks"])
    }

    public var initialState: TasksState { TasksState() }

    /// The model of `state`: its records through the declarations the
    /// server gave, the closure's where it gave none, under the grant the
    /// link holds (none known yet is a password session's whole access).
    public func model(_ state: TasksState, calendar: Calendar) -> TasksModel {
        TasksModel(
            declarations: state.declarations + closure, records: state.records.values, grant: Self.grant(state.linked.link),
            calendar: calendar)
    }

    static func grant(_ link: LinkSnapshot?) -> Grants {
        link?.scopes.map(Grants.init) ?? Grants([ScopeWords.full])
    }

    /// A save is the owner's write, as any other, and its editor stays open
    /// until the outbox has it, so a write the device could not keep leaves
    /// the draft to send again. With nothing to send, or no outbox to answer,
    /// the editor closes at once.
    public func events(fromChild key: ChildKey, _ event: TaskEditor.Event) -> [Event] {
        switch event {
        case let .saved(write, _):
            guard linked.outbox != nil, !write.entries.isEmpty else { return [.editorClosed(key), .records(.wrote(write))] }
            return [.editorSaving(key, Set(write.entries.map(\.id))), .records(.wrote(write))]
        case .cancelled: return [.editorClosed(key)]
        default: return []
        }
    }

    public func nextChange(of presentation: TasksPresentation) -> Date? { presentation.nextChange }

    // MARK: The settings link beside the data link

    /// The data link's events every link hears.
    static func shared(_ event: LinkedRecords.Event) -> Bool {
        switch event {
        case .opened, .linked, .foregrounded, .refreshAsked, .clearAsked: true
        default: false
        }
    }

    public func report(of event: Event) -> LinkReport? {
        switch event {
        case let .records(records): linked.report(of: records)
        case let .settings(settings): settingsLinked.report(of: settings)
        default: nil
        }
    }

    public func coalescing(of event: Event) -> Coalescing {
        switch event {
        case let .records(records): linked.coalescing(of: records)
        case let .settings(settings): settingsLinked.coalescing(of: settings)
        default: .now
        }
    }

    // MARK: Composition

    /// The editors, each made from the task as it stands, read in the
    /// device's time zone (a moment's day is the device's), over the task's
    /// collection, or for a new one the collection new tasks go to.
    public func children(in state: TasksState, environment: AppEnvironment) -> [ChildKey: TaskEditor] {
        guard !state.editors.isEmpty else { return [:] }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = environment.timeZone
        let model = model(state, calendar: calendar)
        var out: [ChildKey: TaskEditor] = [:]
        for (key, seed) in state.editors {
            switch seed {
            case let .task(path):
                // A task deleted while it was being edited closes its editor.
                guard let task = model.tasks[path], let collection = model.collection(task.kind) else { continue }
                let (lists, last) = Self.groups(of: collection, in: model)
                out[key] = TaskEditor(original: task, collection: collection, lists: lists, lastPositions: last, saving: state.saves[key])
            case let .new(list, day):
                guard let collection = model.newTasksTarget else { continue }
                let (lists, last) = Self.groups(of: collection, in: model)
                out[key] = TaskEditor(
                    original: nil, collection: collection, seedList: list, seedDay: day, lists: lists, lastPositions: last,
                    saving: state.saves[key])
            }
        }
        return out
    }

    /// A collection's groups as the editor offers them, by path, and the
    /// highest position of its tasks in each ("" for none).
    static func groups(of collection: TaskCollection, in model: TasksModel) -> ([EditorOption], [String: Decimal]) {
        let kind = collection.group?.kind ?? ""
        let lists = model.ordered(model.groups.values.filter { $0.kind == kind }, of: kind).map { EditorOption(id: $0.id, title: $0.name) }
        var last: [String: Decimal] = [:]
        for task in model.tasks.values where task.kind == collection.kind {
            guard let position = task.position else { continue }
            let path: String = if case let .record(path)? = task.group { path } else { "" }
            last[path] = max(last[path] ?? position, position)
        }
        return (lists, last)
    }

    // MARK: The presenter

    public func present(_ input: PresenterInput<TasksApp>) -> TasksPresentation {
        TasksPresenter.present(input, app: self)
    }
}
