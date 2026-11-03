import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// What the app's root holds: the records as `LinkedRecords` keeps them,
/// and where the owner is in the app.
public struct TasksState: Codable, Equatable, Sendable, LinkedRecordsHolding {
    public var linked = LinkedRecordsState()
    /// Moved by every event that changes what a list's rows are made of
    /// (the records, the settings, the selection): the keyed lists'
    /// revisions follow it, so the Android bridge sends their deltas.
    public var revision = 0
    /// The settings and the folder's sizes were read from the device.
    public var settingsRead = false
    public var settings = SettingsValues()
    public var section = TasksMenu.today
    /// The list column's; nil in Lists before one is chosen, where a phone
    /// shows the lists themselves.
    public var selection: ListSelection? = .today
    public var openTask: String?
    /// A reminder's task, tapped before its record was read (a cold
    /// launch): opened as soon as the record is there, unless the owner
    /// went elsewhere first.
    public var tapped: String?
    /// What each open editor edits: a task's id, or nil for a new task in
    /// the list it names.
    public var editors: [ChildKey: EditorSeed] = [:]
    public var confirmation: Confirmation?
    public var permission: NotificationPermission?
    /// What this device was last told to remind of.
    public var scheduled: [NotificationRequest]?
    public var usage: [AreaUsage]?
    /// A newer build on the feed, by its version.
    public var offered: String?
    public var problem: String?

    public init() {}

    /// The folded records, by `kind/id`.
    public var records: [String: Record] { linked.records }
}

/// What an editor opened on.
public enum EditorSeed: Codable, Hashable, Sendable {
    case task(String)
    case new(list: String?, day: CivilDay?)
}
