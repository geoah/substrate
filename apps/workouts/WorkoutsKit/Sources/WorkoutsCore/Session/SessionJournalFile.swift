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

    let file: JournalFile<EventEnvelope, SessionState>

    public init(folder: RepositoryFolder) {
        file = JournalFile(folder: folder, name: Self.fileName, area: .root, format: Self.format)
    }

    public func write(events: [SessionController.Event], state: SessionController.State) throws {
        let envelopes = events.compactMap { event -> EventEnvelope? in
            if case let .engine(envelope) = event { envelope } else { nil }
        }
        // A journal is only written while a session is open.
        guard let session = state.session else { return try file.delete() }
        try file.write(events: envelopes, state: session)
    }

    public func read() throws -> StoredJournal<SessionController.Event, SessionController.State>? {
        guard let stored = try file.read() else { return nil }
        let cached = stored.cachedState.map {
            SessionController.State(session: $0, lastAt: stored.events.last?.at, revision: stored.events.count)
        }
        return StoredJournal(events: stored.events.map(SessionController.Event.engine), cachedState: cached)
    }

    public func delete() throws {
        try file.delete()
    }

    public func setAside(at instant: Date) throws -> String {
        try file.setAside(at: instant)
    }
}
