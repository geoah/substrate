import Foundation

/// Finished sessions. V1: the local history files (`HistoryStore`). V2 adds
/// sets from substrate (imports, other devices).
public protocol HistorySource: Sendable {
    func sessions() async throws -> [SessionRecords]
}

extension HistoryStore: HistorySource {
    /// Every readable history file's records, newest first. Unreadable files
    /// are left out here; `loadAll()` lists them.
    public func sessions() async throws -> [SessionRecords] {
        loadAll().entries.map(\.records)
    }
}
