import Foundation

/// Where the app keeps its files, as a device service.
public protocol AppFolders: Sendable {
    /// For what cannot be read again (a journal, the outbox): kept out of
    /// backups and device transfers, so a restore never revives a queue of
    /// writes for a session that no longer exists, and readable after the
    /// first unlock, so background work reaches it. `noBackupFilesDir` on
    /// Android.
    func data() throws -> URL
    /// For what can be read again; the system may clear it.
    func cache() throws -> URL
    /// What every write there adds to `.atomic`: the data protection class.
    var writingOptions: Data.WritingOptions { get }
}
