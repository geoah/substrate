import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateScreens

/// Everything the app's root draws, as one value: the App menu, the lists
/// column, the selected list, the open task, the editors open over them, a
/// question waiting for an answer, and the app's settings. A UI reads this
/// and sends commands; it decides nothing.
public struct TasksPresentation: Bridged, Equatable, Sendable {
    public var menu: AppMenuPresentation
    public var sidebar: SidebarPresentation
    public var list: ListPresentation?
    public var detail: TaskDetailPresentation?
    public var editors: [ChildKey: TaskEditorPresentation]
    public var confirmation: ConfirmationPresentation<Confirmation>?
    /// The app's own settings sections; `TasksSettings.screen` adds the
    /// link's and these two.
    public var settings: [SettingsSection]
    public var storage: SettingsSection
    public var about: AboutPresentation
    /// What stands in the way above the columns: the tasks not being read.
    public var notices: [NoticeLine]
    public var problem: ProblemPresentation?
    /// What this device should remind of now; the effects schedule it.
    public var reminders: [NotificationRequest]
    /// When the words drawn stop being true (midnight moves "Tomorrow" to
    /// "Today"), so the runtime presents again then.
    public var nextChange: Date?
}

/// The lists column: what needs the owner, the tasks in no list, then each
/// list in the owner's order, with how many tasks are open in each.
public struct SidebarPresentation: Bridged, Equatable, Sendable {
    public var title: String
    public var fixed: [SidebarRow]
    public var listsTitle: String
    public var lists: KeyedList<SidebarRow>
    public var newList: String
    /// "No lists yet", while there are none.
    public var noLists: String?
    public var selected: ListSelection?
}

public struct SidebarRow: Bridged, Hashable, Sendable, Identifiable {
    /// `needs`, `inbox`, or the list's id.
    public var id: String
    public var selection: ListSelection
    public var title: String
    /// The app's name for the row's mark.
    public var symbol: String
    public var open: Int
    /// "3 open tasks", for a screen reader.
    public var openWords: String
    /// What the row offers beside opening it: a list's rename and delete.
    public var actions: [ActionButton<ListAction>]
}

/// What the owner can do to a list.
public enum ListAction: Bridged, Hashable, Sendable {
    case rename(String)
    case delete(String)
}

/// One list's tasks: the open ones in the chosen order, then the closed
/// ones, shown or not. Needs you is every open task across the lists, each
/// row in its band (`TaskRow.band`), the bands' headings in `bands`, and
/// the chips that narrow it to one list.
public struct ListPresentation: Bridged, Equatable, Sendable {
    public var selection: ListSelection
    public var title: String
    /// What the list holds at a glance, above its tasks.
    public var summary: ListSummary
    /// Needs you's chips, All first; empty elsewhere.
    public var chips: [FilterChip]
    /// The bands Needs you's open rows fall in, in order, those with rows
    /// alone; empty elsewhere.
    public var bands: [BandHeading]
    /// "Nothing needs you right now", while Needs you's first band is empty
    /// and others are not.
    public var clear: String?
    public var open: KeyedList<TaskRow>
    /// The open tasks' heading: "To Do".
    public var openTitle: String
    public var closed: KeyedList<TaskRow>
    public var closedTitle: String
    public var showsClosed: Bool
    /// "Show Completed" or "Hide Completed".
    public var toggleClosed: String
    /// The quick add field's prompt: "Add a task to Groceries".
    public var quickAdd: String
    public var newTask: String
    /// Whether rows can be dragged: only in the owner's order.
    public var reorders: Bool
    /// "Sorted by due date", where the order is not the owner's.
    public var orderNote: String?
    public var empty: EmptyPresentation?
    public var actions: [ActionButton<ListAction>]
    /// What a row's swipes and context menu say.
    public var rowMenu: TaskMenuWords
}

/// A chip above Needs you: every list, and the inbox, to narrow it to.
public struct FilterChip: Bridged, Hashable, Sendable, Identifiable {
    /// `all`, `inbox`, or the list's id.
    public var id: String
    public var title: String
    /// What choosing it narrows to; nil widens to every list.
    public var selection: ListSelection?
    public var selected: Bool
}

/// A band's heading on Needs you.
public struct BandHeading: Bridged, Hashable, Sendable {
    public var band: TaskBand
    /// "Needs you", "Coming up", "Later".
    public var title: String
    public var count: Int
}

/// A list at a glance: how many tasks are left, the day on Needs you, and
/// how many are late.
public struct ListSummary: Bridged, Hashable, Sendable {
    /// "3 to do", or "All done".
    public var headline: String
    /// "Thursday, 5 November", on Needs you.
    public var detail: String?
    /// "1 overdue", where any is.
    public var overdue: String?
}

/// The words of what a task offers from its row: its state, its priority,
/// its list, edit and delete.
public struct TaskMenuWords: Bridged, Hashable, Sendable {
    public var complete: String
    public var abandon: String
    public var reopen: String
    public var edit: String
    public var priority: String
    public var priorities: [EditorOption]
    public var move: String
    public var delete: String
    /// The swipe's shorter Delete.
    public var swipeDelete: String
}

/// A task as a row: its name, whether it is done, and each fact beside it
/// labelled by its role.
public struct TaskRow: Bridged, Hashable, Sendable, Identifiable {
    public var id: String
    public var title: String
    public var status: TaskStatus
    /// Marked done, its undo window open: the mark is drawn filled, and a
    /// tap on it takes the mark back.
    public var pending: Bool
    /// "Mark as Done", "Reopen", "Undo": what the round mark does.
    public var toggle: String
    public var due: DueBadge?
    public var priority: PriorityBadge?
    /// The list it is in, where the column mixes lists (Needs you).
    public var listName: String?
    /// Its band on Needs you; nil in a list.
    public var band: TaskBand?
    /// Where it can be moved to from its row, where it waits on a day:
    /// "Move" with the days, or "Pick a day" for a task with none.
    public var reschedule: RescheduleMenu?
    /// What else its row offers where it waits on a decision: Won't Do.
    public var decisions: [ActionButton<TaskAction>]
    public var hasNotes: Bool
    /// The notes' opening, on one line: what a row shows under its name.
    public var notes: String?
    /// The row read aloud: "Buy milk, due Today, priority High".
    public var spoken: String
}

public struct DueBadge: Bridged, Hashable, Sendable {
    public var text: String
    public var tone: DueWords.Tone
}

public struct PriorityBadge: Bridged, Hashable, Sendable {
    public var text: String
    public var priority: TaskPriority
}

/// A row's menu of days to move its task to.
public struct RescheduleMenu: Bridged, Hashable, Sendable {
    public var title: String
    public var options: [ActionButton<Reschedule>]
}

/// The open task: its name, then each fact under the label the editor gives
/// it too, the list and the priority as the choices that change them in
/// place, and what can be done to it. Edit is the toolbar's.
public struct TaskDetailPresentation: Bridged, Equatable, Sendable {
    public var id: String
    public var title: String
    public var status: TaskStatus
    /// Marked done, its undo window open, as `TaskRow.pending`.
    public var pending: Bool
    public var fields: [DetailField]
    public var actions: [ActionButton<TaskAction>]
    /// The toolbar's button that opens the editor.
    public var edit: String
}

/// One fact of the open task: read only, or a choice that changes it.
public struct DetailField: Bridged, Hashable, Sendable, Identifiable {
    public var field: TaskField
    public var label: String
    public var value: String
    /// A link the owner can open.
    public var url: String?
    public var tone: DueWords.Tone?
    /// The choices the fact is changed with where it stands, nil for one
    /// the editor alone changes.
    public var choice: EditorChoice?

    public var id: TaskField { field }
}

/// What the owner can do to a task.
public enum TaskAction: Bridged, Hashable, Sendable {
    case complete
    case reopen
    case abandon
    case delete
}
