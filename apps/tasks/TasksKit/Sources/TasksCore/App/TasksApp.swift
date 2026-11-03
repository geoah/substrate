import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// Substrate Tasks' root, kept by the link (`LinkController<TasksApp>`).
/// Its tasks and lists are the kit's `LinkedRecords` over the two kinds: read
/// once, followed through the watch, every change written through the
/// outbox. Beside them it keeps the reminders this device shows in step
/// with the tasks, the settings, where the owner is, and the task editors as
/// its children. Its presentation is every screen of the app; the link's is
/// the connection's, beside it.
public struct TasksApp: LinkedApp {
    public enum Command: Codable, Equatable, Sendable {
        case select(section: String)
        /// A list shown, or none: on a phone, back to the lists.
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
        /// What the system may have changed is read again: the app came to
        /// the front, or the time zone moved.
        case foreground
        /// The owner tapped a reminder: the task's id it was scheduled with.
        case reminderTapped(String)
        /// The updater found a newer build, or none.
        case offered(String?)
        case dismissProblem
    }

    public enum Event: Codable, Equatable, Sendable {
        case records(LinkedRecords.Event)
        case opening
        case settingsRead(SettingsValues, usage: [AreaUsage])
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
        case offered(String?)
        case problemDismissed
    }

    public enum Effect: Equatable, Sendable {
        case records(LinkedRecords.Effect)
        case readSettings
        case readPermission
        case askPermission
        case saveSettings(SettingsValues)
        case schedule([NotificationRequest])
        case readUsage
    }

    public typealias State = TasksState
    public typealias Presentation = TasksPresentation
    public typealias Child = TaskEditor

    public static let appName = "Substrate Tasks"
    /// Where the app's settings are kept in the repository's folder.
    public static let settingsFile = SettingsFile<SettingsValues>()

    public let linked: LinkedRecords
    /// "this iPhone", "this iPad", "this Mac".
    public let device: String
    public let build: BuildFacts

    public init(cache: RecordCache?, outbox: Outbox?, device: String, build: BuildFacts, line: WriteLine = WriteLine()) {
        linked = LinkedRecords(kinds: TasksKinds.all, cache: cache, outbox: outbox, words: Self.words(device: device), line: line)
        self.device = device
        self.build = build
    }

    /// The Status screen's words, and the linked records'.
    public static func words(device: String) -> StatusWords {
        StatusWords(app: appName, device: device, kept: "tasks", packageNames: [TasksKinds.packageID: "Tasks"])
    }

    public var initialState: TasksState { TasksState() }

    public func event(atOpen replayed: Bool) -> Event? { .opening }

    public func event(forLink link: LinkSnapshot) -> Event? { .records(.linked(link)) }

    public func event(fromChild key: ChildKey, _ event: TaskEditor.Event) -> Event? {
        switch event {
        case let .saved(write, task): .editorSaved(key, write, task: task)
        case .cancelled: .editorClosed(key)
        default: nil
        }
    }

    public func report(of event: Event) -> LinkReport? {
        guard case let .records(event) = event else { return nil }
        return linked.report(of: event)
    }

    public func coalescing(of event: Event) -> Coalescing {
        guard case let .records(event) = event else { return .now }
        return linked.coalescing(of: event)
    }

    public func nextChange(of presentation: TasksPresentation) -> Date? { presentation.nextChange }

    // MARK: Composition

    /// The editors, each made from the task as it stands, read in the
    /// device's time zone (a moment's day is the device's).
    public func children(in state: TasksState, environment: AppEnvironment) -> [ChildKey: TaskEditor] {
        guard !state.editors.isEmpty else { return [:] }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = environment.timeZone
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
        TasksPresenter.present(input, linked: linked, device: device, build: build)
    }
}
