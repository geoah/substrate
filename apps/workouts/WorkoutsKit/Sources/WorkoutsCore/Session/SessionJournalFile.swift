import Foundation
import SubstrateLocal
import SubstrateRuntime

/// The session's journal, `active-session.json` in the app's folder:
/// `{"format": 1, "events": [...], "state": {...}}` (§2.3), the file every
/// build since the first writes, through the kit's `JournalFile`. It keeps
/// the engine's events and their fold; the controller's other events are
/// never journaled, so a stored event is always an engine one.
public struct SessionJournalFile: JournalStore {
    public static let fileName = "active-session.json"
    public static let format = WorkoutsFormats.journalFormat

    private let folder: FolderOpening

    public init(folder: RepositoryFolder) {
        self.folder = FolderOpening(folder)
    }

    /// The journal in the app's folder at `directory`, which is opened at
    /// its first use and again at every use until it opens. A folder that
    /// cannot be made reads as a journal the device cannot read, never as
    /// none: the runtime then refuses the session's changes, each after
    /// trying again, rather than keep them where a crash loses them.
    public init(directory: URL, writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting) {
        folder = FolderOpening { try RepositoryFolder(app: directory, writingOptions: writingOptions) }
    }

    private func file() throws -> JournalFile<EventEnvelope, SessionState> {
        JournalFile(folder: try folder.open(), name: Self.fileName, area: .root, format: Self.format)
    }

    public func write(events: [SessionController.Event], state: SessionController.State) throws {
        let envelopes = events.compactMap { event -> EventEnvelope? in
            if case let .engine(envelope) = event { envelope } else { nil }
        }
        // A journal is only written while a session is open.
        guard let session = state.session else { return try file().delete() }
        try file().write(events: envelopes, state: session)
    }

    public func read() throws -> StoredJournal<SessionController.Event, SessionController.State>? {
        let file: JournalFile<EventEnvelope, SessionState>
        do {
            file = try self.file()
        } catch {
            throw JournalUnavailable(reason: "its folder could not be opened (\(error.localizedDescription))")
        }
        guard let stored = try file.read() else { return nil }
        let cached = stored.cachedState.map {
            SessionController.State(session: $0, lastAt: stored.events.last?.at, revision: stored.events.count)
        }
        return StoredJournal(events: stored.events.map(SessionController.Event.engine), cachedState: cached)
    }

    public func delete() throws {
        try file().delete()
    }

    public func setAside(at instant: Date) throws -> String {
        try file().setAside(at: instant)
    }
}

/// A folder made once it can be: the first open that succeeds is kept, and
/// one that fails is tried again at the next.
private final class FolderOpening: @unchecked Sendable {
    private let lock = NSLock()
    private let make: @Sendable () throws -> RepositoryFolder
    private var opened: RepositoryFolder?

    init(_ folder: RepositoryFolder) {
        opened = folder
        make = { folder }
    }

    init(_ make: @escaping @Sendable () throws -> RepositoryFolder) {
        self.make = make
    }

    func open() throws -> RepositoryFolder {
        try lock.withLock {
            if let opened { return opened }
            let made = try make()
            opened = made
            return made
        }
    }
}
