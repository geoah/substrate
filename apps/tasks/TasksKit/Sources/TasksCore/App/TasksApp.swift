import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// Substrate Tasks' root, kept by the link (`LinkController<TasksApp>`).
/// It holds the repository's tasks and lists as the server last answered
/// them with the outbox's writes laid over, reads them once and follows the
/// watch, writes every change through the outbox, keeps the reminders this
/// device shows in step with the tasks, and keeps the task editors as its
/// children. Its presentation is every screen of the app; the link's is the
/// connection's, beside it.
public struct TasksApp: LinkedApp {
    public enum Command: Codable, Equatable, Sendable {
        case select(section: String)
        case show(ListSelection?)
        case open(task: String?)
        case quickAdd(String)
        case newTask
        case edit(String)
        case complete(String)
        case reopen(String)
        case abandon(String)
        case setPriority(String, TaskPriority)
        case move(String, toList: String?)
        /// A task dragged to `index` among the open rows drawn.
        case reorderTask(String, to: Int)
        case reorderList(String, to: Int)
        case deleteTask(String)
        case createList(String)
        case renameList(String, to: String)
        case deleteList(String)
        case confirm
        case dismissConfirmation
        case settings(SettingsCommand)
        case clearSaved
        case refresh
        /// The app came to the front: what the system may have changed
        /// meanwhile is read again.
        case foreground
        /// The updater found a newer build, or none.
        case offered(String?)
        case dismissProblem
    }

    public enum Event: Codable, Equatable, Sendable {
        case linked(LinkSnapshot)
        case opening
        case loaded(LocalCopy)
        case listed([Record], cursor: WatchCursor?)
        case readFailed(String, LinkReport?)
        case changed(String, Record?)
        case watchOpened(WatchCursor)
        case watchInterrupted(String)
        case watchEnded(WatchStop, String)
        case relistNeeded
        case queueReported([OutboxEntry])
        case enqueued(Set<UUID>, queue: [OutboxEntry])
        case notQueued(Set<UUID>, String)
        case wrote(LocalWrite)
        case sectionSelected(String)
        case shown(ListSelection?)
        case opened(String?)
        case editorOpened(ChildKey, EditorSeed)
        case editorSaved(ChildKey, LocalWrite, task: String)
        case editorClosed(ChildKey)
        case asked(Confirmation)
        case answered
        case settingsChanged(SettingsValues)
        case permission(NotificationPermission)
        case scheduled([NotificationRequest])
        case tapped(String)
        case usage([AreaUsage])
        case clearAsked
        case cleared
        case refreshAsked
        case foregrounded
        case offered(String?)
        case problemDismissed
    }

    public enum Effect: Equatable, Sendable {
        case load
        case followOutbox
        case followTaps
        case readPermission
        case askPermission
        case read
        case follow(WatchCursor?)
        case stopFollowing
        case reread([String])
        case enqueue(LocalWrite)
        case saveSettings(SettingsValues)
        case schedule([NotificationRequest])
        case readUsage
        case clearSaved
    }

    public typealias State = TasksState
    public typealias Presentation = TasksPresentation
    public typealias Child = TaskEditor

    public static let appName = "Substrate Tasks"
    /// Where the app's settings are kept in the repository's folder.
    public static let settingsFile = "settings.json"

    /// Nil while no repository is signed in: nothing is read or written.
    public let cache: RecordCache?
    public let outbox: Outbox?
    /// "this iPhone", "this iPad", "this Mac".
    public let device: String
    public let build: BuildFacts
    public let line: WriteLine

    public init(cache: RecordCache?, outbox: Outbox?, device: String, build: BuildFacts, line: WriteLine = WriteLine()) {
        self.cache = cache
        self.outbox = outbox
        self.device = device
        self.build = build
        self.line = line
    }

    public static func == (lhs: TasksApp, rhs: TasksApp) -> Bool {
        lhs.cache === rhs.cache && lhs.outbox === rhs.outbox && lhs.device == rhs.device && lhs.build == rhs.build && lhs.line === rhs.line
    }

    public var initialState: TasksState { TasksState() }

    public func event(atOpen replayed: Bool) -> Event? { .opening }

    public func event(forLink link: LinkSnapshot) -> Event? { .linked(link) }

    public func event(fromChild key: ChildKey, _ event: TaskEditor.Event) -> Event? {
        switch event {
        case let .saved(write, task): .editorSaved(key, write, task: task)
        case .cancelled: .editorClosed(key)
        default: nil
        }
    }

    /// A 401 or a 403 `insufficient_scope` from the read or the watch.
    public func report(of event: Event) -> LinkReport? {
        switch event {
        case let .readFailed(_, report): report
        case .watchEnded(.signedOut, _): .accessEnded
        case .watchEnded(.needsApproval, _): .needsApproval
        default: nil
        }
    }

    /// A burst of watch rows or outbox reports is drawn once per window.
    public func coalescing(of event: Event) -> Coalescing {
        switch event {
        case .changed, .queueReported: .within(.milliseconds(100))
        default: .now
        }
    }

    public func nextChange(of presentation: TasksPresentation) -> Date? { presentation.nextChange }

    // MARK: Composition

    public func children(in state: TasksState) -> [ChildKey: TaskEditor] {
        guard !state.editors.isEmpty else { return [:] }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let model = TasksModel(state.records, in: calendar)
        let lists = model.arrangedLists.map { EditorOption(id: $0.id, title: $0.name) }
        var last: [String: Decimal] = [:]
        for task in model.tasks.values {
            guard let position = task.position else { continue }
            last[task.list ?? ""] = max(last[task.list ?? ""] ?? position, position)
        }
        var out: [ChildKey: TaskEditor] = [:]
        for (key, seed) in state.editors {
            switch seed {
            case let .task(id):
                // A task deleted while it was being edited closes its editor.
                guard let task = model.tasks[id] else { continue }
                out[key] = TaskEditor(original: task, lists: lists, lastPositions: last)
            case let .new(list, day):
                out[key] = TaskEditor(original: nil, seedList: list, seedDay: day, lists: lists, lastPositions: last)
            }
        }
        return out
    }

    // MARK: The presenter

    public func present(_ input: PresenterInput<TasksApp>) -> TasksPresentation {
        TasksPresenter.present(input, device: device, build: build)
    }
}
