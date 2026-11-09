import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateScreens

/// Everything the app's root draws, as one value: the App menu, the lists
/// column, the selected list or the suggestions, the open task or
/// suggestion, the choice a decision waits on, the editors open over them,
/// a question waiting for an answer, Settings' pages, and an approval the
/// app asks for. A UI reads this and sends commands; it decides nothing.
public struct TasksPresentation: Bridged, Equatable, Sendable {
    public var menu: AppMenuPresentation
    public var sidebar: SidebarPresentation
    /// The list column, for every selection but the suggestions.
    public var list: ListPresentation?
    /// The suggestions, while they are the selection.
    public var queue: QueuePresentation?
    /// The open task, where it is not a suggestion.
    public var detail: TaskDetailPresentation?
    /// The open suggestion.
    public var suggestion: SuggestionPresentation?
    /// A decision with several values, waiting for the owner to pick one.
    public var choice: ChoicePresentation?
    public var editors: [ChildKey: TaskEditorPresentation]
    public var confirmation: ConfirmationPresentation<Confirmation>?
    /// The app's own settings sections; `TasksSettings.screen` adds the
    /// link's and these two.
    public var settings: [SettingsSection]
    public var storage: SettingsSection
    public var about: AboutPresentation
    /// Settings → Collections, and the page past it that is open.
    public var collections: CollectionsPresentation
    public var collection: CollectionPresentation?
    public var settingsPage: SettingsPage?
    public var approval: ApprovalPresentation?
    /// What stands in the way above the columns: the tasks not being read.
    public var notices: [NoticeLine]
    public var problem: ProblemPresentation?
    /// What this device should remind of now; the effects schedule it.
    public var reminders: [NotificationRequest]
    /// When the words drawn stop being true (midnight moves "Tomorrow" to
    /// "Today"), so the runtime presents again then.
    public var nextChange: Date?
}

/// The lists column: what needs the owner, the suggestions and the tasks
/// with no date; the pinned groups; then one section per group kind.
public struct SidebarPresentation: Bridged, Equatable, Sendable {
    public var title: String
    /// Needs you, Suggestions, No date.
    public var fixed: [SidebarRow]
    /// "Search lists and projects".
    public var search: String
    public var pinnedTitle: String
    public var pinned: [SidebarRow]
    public var sections: [GroupSection]
    /// Every collection that is on and groups by nothing, each one list,
    /// under `ungroupedTitle`.
    public var ungroupedTitle: String
    public var ungrouped: [SidebarRow]
    public var newList: String
    /// "No lists yet", while the app's own collection is grouped by its
    /// lists and there are none.
    public var noLists: String?
    public var selected: ListSelection?
}

/// One group kind's groups: those with open tasks, then the tasks in no
/// group and those whose group is gone, then how many hold nothing open.
public struct GroupSection: Bridged, Hashable, Sendable, Identifiable {
    /// The group kind.
    public var id: String
    /// Its plural label: "Task lists", "Projects".
    public var title: String
    /// Its full reference, on a line of its own.
    public var reference: String
    public var rows: [SidebarRow]
    /// The groups with nothing open, and "N more with nothing open".
    public var quiet: [SidebarRow]
    public var more: String?
    /// How many of `rows` a wide sidebar shows before `all`.
    public var busiest: Int
    /// "All 12 task lists…", where a wide sidebar shows fewer than there are.
    public var all: String?
    /// The rows may be dragged: the app's own task lists.
    public var reorders: Bool
}

public struct SidebarRow: Bridged, Hashable, Sendable, Identifiable {
    /// `needs`, `suggestions`, `nodate`, a group's path, or a collection's
    /// row's id.
    public var id: String
    public var selection: ListSelection
    public var title: String
    /// What it is under its title: "Project · Active", "Chores in no area".
    public var detail: String?
    /// The app's name for the row's mark.
    public var symbol: String
    public var open: Int
    /// "3 open tasks", for a screen reader.
    public var openWords: String
    public var pinned: Bool
    /// What the row offers beside opening it: a list's rename and delete.
    public var actions: [ActionButton<ListAction>]
}

/// What the owner can do to a list.
public enum ListAction: Bridged, Hashable, Sendable {
    case rename(String)
    case delete(String)
}

/// One list's tasks: the open ones in the chosen order, then the closed
/// ones, shown or not. Needs you is every open task across the collections,
/// each row in its band (`TaskRow.band`), the bands' headings in `bands`,
/// the suggestions and the undated as a counted row each, and the chips
/// that narrow it to a pinned group.
public struct ListPresentation: Bridged, Equatable, Sendable {
    public var selection: ListSelection
    public var title: String
    /// What the list holds at a glance, above its tasks.
    public var summary: ListSummary
    /// Needs you's chips, All first; empty elsewhere.
    public var chips: [FilterChip]
    /// The suggestions waiting, as one row; nil with none.
    public var queue: CountedRow?
    /// The bands Needs you's open rows fall in, in order, those with rows
    /// alone; empty elsewhere.
    public var bands: [BandHeading]
    /// "Nothing needs you right now", while Needs you's first band is empty
    /// and others are not.
    public var clear: String?
    /// Needs you's open tasks with no date, as one row; nil with none.
    public var noDate: CountedRow?
    public var open: KeyedList<TaskRow>
    /// The open tasks' heading: "To Do".
    public var openTitle: String
    public var closed: KeyedList<TaskRow>
    public var closedTitle: String
    public var showsClosed: Bool
    /// "Show Completed" or "Hide Completed".
    public var toggleClosed: String
    /// The quick add field's prompt: "Add a task to Groceries"; nil where no
    /// task can be added here.
    public var quickAdd: String?
    public var newTask: String
    /// Whether rows can be dragged: only in the owner's order.
    public var reorders: Bool
    /// "Sorted by due date", where the order is not the owner's.
    public var orderNote: String?
    public var empty: EmptyPresentation?
    public var actions: [ActionButton<ListAction>]
    /// A group's Pin or Unpin.
    public var pin: ActionButton<PinAction>?
    /// What a row's swipes and context menu say.
    public var rowMenu: TaskMenuWords
}

/// A group pinned or unpinned, by its path.
public enum PinAction: Bridged, Hashable, Sendable {
    case pin(String)
    case unpin(String)
}

/// A row that stands for a list of its own, with its count: the
/// suggestions, the tasks with no date.
public struct CountedRow: Bridged, Hashable, Sendable {
    public var title: String
    public var detail: String
    public var count: Int
    public var selection: ListSelection
}

/// A chip above Needs you: every collection, and each pinned group, to
/// narrow it to.
public struct FilterChip: Bridged, Hashable, Sendable, Identifiable {
    /// `all`, or the group's path.
    public var id: String
    public var title: String
    /// Its open tasks, where it is a group.
    public var count: Int?
    /// What choosing it narrows to; nil widens to everything.
    public var selection: ListSelection?
    public var selected: Bool
}

/// A band's heading on Needs you.
public struct BandHeading: Bridged, Hashable, Sendable {
    public var band: TaskBand
    /// "Late or urgent", "Coming up", "Later".
    public var title: String
    public var count: Int
}

/// A list at a glance: how many tasks are left, the day on Needs you, and
/// how many are late.
public struct ListSummary: Bridged, Hashable, Sendable {
    /// "3 to do", "All done", "3 open".
    public var headline: String
    /// "Thursday, 5 November" on Needs you; "Project · Active" on a group.
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
    public var accept: String
    public var dismiss: String
    public var undo: String
}

/// A task as a row: its title, one meta line, and one trailing control.
public struct TaskRow: Bridged, Hashable, Sendable, Identifiable {
    /// The record path.
    public var id: String
    public var title: String
    public var standing: KindStatus.Standing
    /// A suggestion: Dismiss and Accept trail it, never a check.
    public var suggestion: Bool
    /// Decided, its undo window open: the decision drawn, Undo takes it
    /// back. "Accepted", "Dismissed", "Done", "Won't do".
    public var pending: Bool
    public var pendingWords: String?
    /// "Mark as Done", "Reopen", "Undo": what the round mark does; nil
    /// where the mark does nothing (a view-only collection).
    public var toggle: String?
    /// Its collection and when it was written, on a suggestion's row.
    public var written: String?
    public var due: DueBadge?
    public var priority: PriorityBadge?
    /// The group it is in, where the column mixes groups.
    public var listName: String?
    /// Its collection, where more than one is on.
    public var collection: String?
    /// Its band on Needs you; nil in a list.
    public var band: TaskBand?
    /// Where it can be moved to from its row, where it waits on a day:
    /// "Move" with the days, or "Pick a day" for a task with none.
    public var reschedule: RescheduleMenu?
    /// What else its row offers: Won't Do where it waits on a decision;
    /// Dismiss and Accept on a suggestion.
    public var decisions: [ActionButton<TaskAction>]
    /// Picked for one decision, while the suggestions are being picked.
    public var picked: Bool?
    /// The row can be edited and deleted: the app's own kind, writable.
    public var editable: Bool
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

/// The open task: its title, then each fact under the label the editor
/// gives it too, the list and the priority as the choices that change them
/// in place, every other property under Details, and what can be done to
/// it. Edit is the toolbar's, for the app's own kind.
public struct TaskDetailPresentation: Bridged, Equatable, Sendable {
    public var id: String
    public var title: String
    public var standing: KindStatus.Standing
    /// Decided, its undo window open, as `TaskRow.pending`.
    public var pending: Bool
    public var fields: [DetailField]
    /// "Details": every other property, read only.
    public var detailsTitle: String
    public var details: [DetailFact]
    public var actions: [ActionButton<TaskAction>]
    /// The toolbar's button that opens the editor; nil where there is none.
    public var edit: String?
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
    /// The property a suggestion's field sets before it is accepted
    /// (`TasksApp.Command.setSuggestion`); nil elsewhere.
    public var property: String?

    public var id: TaskField { field }
}

/// A property under Details, by its label, with a line under it where it
/// names something in full (a collection's reference).
public struct DetailFact: Bridged, Hashable, Sendable, Identifiable {
    public var id: String
    public var label: String
    public var value: String
    public var note: String?
}

/// What the owner can do to a task.
public enum TaskAction: Bridged, Hashable, Sendable {
    case complete
    case reopen
    case abandon
    case delete
    case accept
    case dismiss
    case undo
}

/// The suggestions: each with Dismiss and Accept, and Select to decide
/// several at once.
public struct QueuePresentation: Bridged, Equatable, Sendable {
    public var title: String
    public var intro: String
    public var rows: KeyedList<TaskRow>
    /// "Select", or while picking "Cancel".
    public var select: String
    public var picking: Bool
    /// While picking: "2 selected".
    public var picked: String?
    /// While picking with any picked: "Accept" and "Dismiss".
    public var acceptPicked: String?
    public var dismissPicked: String?
    public var empty: EmptyPresentation?
}

/// An open suggestion: its collection and when it was written, its title,
/// the fields set before accepting, its description, every other property,
/// and Dismiss and Accept docked.
public struct SuggestionPresentation: Bridged, Equatable, Sendable {
    public var id: String
    /// "Suggestion in Chores · written 2 hours ago".
    public var header: String
    public var title: String
    /// The fields the owner may set before accepting.
    public var fields: [DetailField]
    /// The description under the property's label.
    public var description: DetailFact?
    public var detailsTitle: String
    public var details: [DetailFact]
    /// "1 of 6", its place among the suggestions.
    public var place: String?
    public var pending: Bool
    public var pendingWords: String?
    /// Dismiss and Accept where the collection is writable; Undo while
    /// decided.
    public var actions: [ActionButton<TaskAction>]
}

/// A decision with several values: a sheet of each by its declared label.
public struct ChoicePresentation: Bridged, Equatable, Sendable {
    public var task: String
    public var decision: TaskDecision
    /// "Accept as", "Reopen as".
    public var title: String
    public var options: [EditorOption]
    public var cancel: String
}

/// Settings → Collections: each candidate with its switch, and where new
/// tasks go.
public struct CollectionsPresentation: Bridged, Equatable, Sendable {
    public var title: String
    public var intro: String
    public var showTitle: String
    public var rows: [CollectionRow]
    public var footer: String
    public var newTasksTitle: String
    public var newTasks: EditorChoice
}

public struct CollectionRow: Bridged, Hashable, Sendable, Identifiable {
    /// The kind.
    public var id: String
    public var title: String
    public var reference: String
    /// "103 open · 6 suggested · grouped by task list", "View only".
    public var meta: String?
    public var on: Bool
    public var viewOnly: Bool
}

/// One collection: how it is grouped, how its states read, and what the
/// app reads each field from.
public struct CollectionPresentation: Bridged, Equatable, Sendable {
    public var kind: String
    public var title: String
    public var reference: String
    public var groupTitle: String
    public var groups: [GroupChoice]
    public var statesTitle: String
    public var states: [LabeledValue]
    public var fieldsTitle: String
    public var fields: [LabeledValue]
}

/// One way to group a collection: a single reference by its label with its
/// target kind under it, or nothing.
public struct GroupChoice: Bridged, Hashable, Sendable, Identifiable {
    /// The reference property; "" for nothing.
    public var id: String
    public var title: String
    public var detail: String
    public var selected: Bool
}

/// The app asking for the owner's approval to change a collection, or
/// saying it was not given.
public struct ApprovalPresentation: Bridged, Equatable, Sendable {
    public var collection: String
    public var message: String
    /// The scopes the approval asks for beside the app's own.
    public var scopes: [String]
    public var approve: String
    public var cancel: String
    /// "Not approved. Chores stays view only.", once turned down.
    public var refused: String?
    /// The owner is on the approval page, or came back from it and the
    /// scopes have not said yet: nothing is asked meanwhile.
    public var waiting: Bool
}
