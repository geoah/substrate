import SubstrateCore
import SubstrateLocal
import SubstrateAPI
import SubstrateScreens

/// What the device holds for the repository, read at open: the records
/// as last read, where the watch resumes, the outbox's writes, the app's
/// settings and the folder's sizes.
public struct LocalCopy: Codable, Equatable, Sendable {
    public var records: [Record]
    public var cursor: WatchCursor?
    public var queue: [OutboxEntry]
    public var settings: SettingsValues
    public var usage: [AreaUsage]

    public init(records: [Record] = [], cursor: WatchCursor? = nil, queue: [OutboxEntry] = [], settings: SettingsValues = SettingsValues(),
                usage: [AreaUsage] = [])
    {
        self.records = records
        self.cursor = cursor
        self.queue = queue
        self.settings = settings
        self.usage = usage
    }
}
