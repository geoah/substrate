import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateAPI
import SubstrateScreens

/// What the app's root holds. The server's records and the outbox's queue
/// are the truth of what the owner sees; `records` is their fold, kept so a
/// presentation does not fold again.
public struct TasksState: Codable, Equatable, Sendable {
    public var link: LinkSnapshot?
    /// The records as the server last answered them, by `kind/id`.
    public var server: [String: Record] = [:]
    /// The outbox's writes, as it last reported them, and the app's own
    /// ahead of that report.
    public var queue: [OutboxEntry] = []
    /// The ids of writes queued here and not yet seen in a report.
    public var unconfirmed: Set<UUID> = []
    public var records: [String: Record] = [:]
    /// Moved by every event that changes what a list's rows are made of
    /// (the records, the settings, the selection): the keyed lists'
    /// revisions follow it, so the Android bridge sends their deltas.
    public var revision = 0
    /// The cache and the queue were read from the device.
    public var loaded = false
    /// Where the watch resumes, nil before it ever opened.
    public var cursor: WatchCursor?
    public var sync: SyncPhase = .idle
    public var syncDue: SyncDue?
    /// The records a landed or dropped write touched: read again.
    public var rereadDue: [String] = []
    public var settings = SettingsValues()
    public var section = TasksMenu.lists
    public var selection: ListSelection? = .inbox
    public var openTask: String?
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

    /// The folded records of one kind.
    public func records(of kind: String) -> [Record] {
        records.values.filter { $0.kind == kind }
    }
}

/// What an editor opened on.
public enum EditorSeed: Codable, Hashable, Sendable {
    case task(String)
    case new(list: String?, day: CivilDay?)
}
