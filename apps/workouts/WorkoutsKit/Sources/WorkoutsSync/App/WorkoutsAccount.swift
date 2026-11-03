import Foundation
import SubstrateLocal
import WorkoutsCore

/// One signed-in repository's files on the phone, in its own folder under
/// `substrate/` beside the journal: its outbox, its tombstones, its cached
/// catalogue, the sample's and the library's standing and the routine edit
/// not saved. Signing in elsewhere opens another; what one holds stays with
/// that repository for its next sign-in.
public final class WorkoutsAccount: Sendable {
    public let ref: AccountRef
    public let directory: URL
    public let outbox: Outbox?
    /// Sessions deleted on the phone while signed in here: purged on this
    /// server and repository only, and never imported from it again.
    public let tombstones: SessionTombstones
    public let remote: RemoteCatalog
    /// Whether the sample routine was written here, so a deleted one never
    /// comes back (decision S1).
    public let sample: SampleInstall
    public let drafts: RoutineDrafts
    public let library: LibraryInstall

    init(ref: AccountRef, directory: URL, localExercises: LocalExercises?) {
        self.ref = ref
        self.directory = directory
        outbox = try? Outbox.workouts(directory: directory)
        tombstones = SessionTombstones(directory: directory)
        remote = RemoteCatalog(directory: directory, localExercises: localExercises)
        sample = SampleInstall(directory: directory)
        drafts = RoutineDrafts(directory: directory)
        library = LibraryInstall(directory: directory)
    }
}

/// A signed-in repository as the app's state names it: no token.
public struct AccountRef: Codable, Hashable, Sendable {
    public var server: URL
    public var repository: String

    public init(server: URL, repository: String) {
        self.server = server
        self.repository = repository
    }

    /// One server and repository, however spelled.
    public var identity: String { server.absoluteString.lowercased() + "|" + repository.lowercased() }

    /// The folder's name, the one every build has used: `<host>[-<port>]-<repository>`.
    public var folderName: String {
        let host = server.host() ?? "server"
        let port = server.port.map { "-\($0)" } ?? ""
        return "\(host)\(port)-\(repository)"
    }
}

/// The accounts' folders, each opened once, so the outbox the link flushes
/// and the one the app queues into are the same actor.
public final class WorkoutsAccounts: @unchecked Sendable {
    /// `Application Support/Workouts/`; nil in a preview, which opens none.
    public let base: URL?
    private let lock = NSLock()
    private var open: [String: WorkoutsAccount] = [:]

    public init(base: URL?) {
        self.base = base
    }

    public func account(_ ref: AccountRef) -> WorkoutsAccount? {
        guard let base else { return nil }
        return lock.withLock {
            if let held = open[ref.identity] { return held }
            let directory = base.appendingPathComponent("substrate", isDirectory: true).appendingPathComponent(ref.folderName, isDirectory: true)
            let account = WorkoutsAccount(ref: ref, directory: directory, localExercises: LocalExercises(directory: base))
            open[ref.identity] = account
            return account
        }
    }
}
