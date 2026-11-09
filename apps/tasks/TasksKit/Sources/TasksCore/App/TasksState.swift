import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// What the app's root holds: the records of the collections that are on as
/// `LinkedRecords` keeps them, the settings records on a link of their own,
/// the kinds the server says have a status, and where the owner is in the
/// app.
public struct TasksState: Codable, Equatable, Sendable, LinkedRecordsHolding {
    public var linked = LinkedRecordsState()
    /// The owner's choices about the collections and across them: read
    /// first, on a link that stays open while the data link is closed and
    /// opened again over new kinds.
    public var settingsLinked = LinkedRecordsState()
    /// Every kind with a status the grant reads, as the lifecycle trait's
    /// implementors last said; empty before the first answer, when the app
    /// reads its own kinds from the closure it carries.
    public var declarations: [TaskKindDeclaration] = []
    /// Why the kinds could not be read, until they are.
    public var discoveryProblem: String?
    /// Moved by every event that changes what a list's rows are made of
    /// (the records, the settings, the selection): the keyed lists'
    /// revisions follow it, so the Android bridge sends their deltas.
    public var revision = 0
    /// The settings and the folder's sizes were read from the device.
    public var settingsRead = false
    public var settings = SettingsValues()
    public var section = TasksMenu.needsYou
    /// The list column's; nil in Lists before one is chosen, where a phone
    /// shows the lists themselves.
    public var selection: ListSelection? = .needsYou
    /// Needs you narrowed to one list (or the inbox); nil is every list.
    public var filter: ListSelection?
    /// The task open, by record path.
    public var openTask: String?
    /// Each task decided whose undo window is still open (accepted,
    /// dismissed, done, won't do), with the write the window ends in: the
    /// row shows it decided, Undo takes it back, and nothing reaches the
    /// outbox until the window closes.
    public var pending: [String: PendingDecision] = [:]
    /// A decision with several values, waiting for the owner to pick one.
    public var choice: DecisionChoice?
    /// A suggestion's fields as the owner set them before accepting, by
    /// path: written in the same patch as the accept.
    public var edits: [String: JSONObject] = [:]
    /// The suggestions picked for one decision, while picking.
    public var picked: [String]?
    /// The settings page open past Settings.
    public var settingsPage: SettingsPage?
    /// An approval the app asked for to change a collection or read its
    /// groups, until it is granted or turned down.
    public var approval: ApprovalAsk?
    /// What each section showed and had open when the owner left it.
    public var stacks: [String: SectionStack] = [:]
    /// Why a task could not be added, until dismissed.
    public var refusal: String?
    /// A reminder's task, tapped before its record was read (a cold
    /// launch): opened as soon as the record is there, unless the owner
    /// went elsewhere first.
    public var tapped: String?
    /// What each open editor edits: a task's id, or nil for a new task in
    /// the list it names.
    public var editors: [ChildKey: EditorSeed] = [:]
    /// Each editor's last save, until the outbox has its write: the editor
    /// stays open with its draft until then.
    public var saves: [ChildKey: EditorSave] = [:]
    public var confirmation: Confirmation?
    public var permission: NotificationPermission?
    /// What this device was last told to remind of.
    public var scheduled: [NotificationRequest]?
    public var usage: [AreaUsage]?
    /// A newer build on the feed, by its version.
    public var offered: String?
    public var problem: String?

    public init() {}

    /// The folded records of both links, by `kind/id`.
    public var records: [String: Record] { linked.records.merging(settingsLinked.records) { a, _ in a } }
}

/// A decision in its undo window.
public struct PendingDecision: Codable, Equatable, Sendable {
    public var decision: TaskDecision
    public var value: String
    public var write: LocalWrite

    public init(decision: TaskDecision, value: String, write: LocalWrite) {
        self.decision = decision
        self.value = value
        self.write = write
    }
}

/// A decision whose move has several values: the owner picks one.
public struct DecisionChoice: Codable, Hashable, Sendable {
    public var task: String
    public var decision: TaskDecision
    public var values: [String]
}

/// A page past Settings.
public enum SettingsPage: Bridged, Hashable, Sendable {
    case collections
    case collection(String)
}

/// An approval the app asked for: the collection it is for, the scopes it
/// asks, the scopes held when it asked, and how it stands.
public struct ApprovalAsk: Codable, Hashable, Sendable {
    public var collection: String
    public var scopes: [String]
    public var held: [String]?
    /// The group kind it asks to read, where it is for a grouping rather
    /// than for changing the collection.
    public var group: String?
    /// The owner went through the approval page; the next scopes the link
    /// reports say whether it was granted.
    public var answered = false
    /// Not granted: said once, until dismissed.
    public var refused = false
}

/// What a section's stack holds.
public struct SectionStack: Codable, Hashable, Sendable {
    public var selection: ListSelection?
    public var openTask: String?
}

/// What an editor opened on: a task by its path, or a new one in the group
/// it names, by path.
public enum EditorSeed: Codable, Hashable, Sendable {
    case task(String)
    case new(list: String?, day: CivilDay?)
}

/// An editor's save, as the root follows its write to the outbox.
public enum EditorSave: Codable, Hashable, Sendable {
    /// The write's entries, on their way to the outbox.
    case sending(Set<UUID>)
    /// The device could not keep the write, and why: the draft stays, and
    /// Save sends it again.
    case notKept(String)
}
