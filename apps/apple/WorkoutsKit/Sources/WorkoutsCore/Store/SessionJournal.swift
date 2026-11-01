import Foundation

/// The session in progress, `active-session.json`:
/// `{"format": 1, "events": [...], "state": {...}}` (§2.3). The events are the
/// truth and the state a cache of their fold. Generic over the engine's event
/// and state types, so the store does not depend on the engine.
public struct SessionJournal<Event: Codable & Sendable, State: Codable & Equatable & Sendable>: Sendable {
    public static var fileName: String { "active-session.json" }

    public let url: URL
    public let writingOptions: Data.WritingOptions

    /// - Parameters:
    ///   - directory: the app's directory, `Application Support/Workouts/` in the app.
    ///   - writingOptions: `WorkoutsFiles.protectedWriting` in the app; `.atomic` in Mac tests.
    public init(directory: URL, writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting) {
        url = directory.appendingPathComponent(Self.fileName, isDirectory: false)
        self.writingOptions = writingOptions
    }

    /// Whether a session is open.
    public var exists: Bool { FileManager.default.fileExists(atPath: url.path) }

    /// Rewrites the whole file atomically: a few tens of kilobytes, after every event.
    public func write(events: [Event], state: State) throws {
        let file = File(format: WorkoutsFormats.journalFormat, events: events, state: state)
        try WorkoutsFiles.write(Self.encoder.encode(file), to: url, options: writingOptions)
    }

    /// The file as stored, or nil when no session is open. The cached state
    /// is nil when it no longer decodes (a format change); the events must.
    public func read() throws -> JournalContents<Event, State>? {
        guard let data = try WorkoutsFiles.read(url) else { return nil }
        let decoder = Self.decoder
        let header = try decoder.decode(Header.self, from: data)
        guard header.format == WorkoutsFormats.journalFormat else { throw JournalError.unsupportedFormat(header.format) }
        let events = try decoder.decode(EventsOnly.self, from: data).events
        let state = try? decoder.decode(StateOnly.self, from: data).state
        return JournalContents(events: events, cachedState: state)
    }

    /// Reads the file, folds its events and compares the fold with the cached
    /// state. On a difference the fold wins and the file is rewritten with it,
    /// best effort: a rewrite that fails (a full disk) keeps the session open,
    /// and the next event writes the file again. `cacheMatched` tells the
    /// controller to log the difference (§2.3).
    public func load(fold: ([Event]) throws -> State) throws -> JournalLoad<Event, State>? {
        guard let contents = try read() else { return nil }
        let folded = try fold(contents.events)
        let matched = contents.cachedState == folded
        if !matched {
            try? write(events: contents.events, state: folded)
        }
        return JournalLoad(events: contents.events, state: folded, cachedState: contents.cachedState, cacheMatched: matched)
    }

    /// Removes the file at Finish and Discard.
    public func delete() throws {
        try WorkoutsFiles.remove(url)
    }

    /// Moves a journal that does not load (another format, an event that no
    /// longer decodes, a fold the engine refuses) aside as
    /// `active-session.unreadable-<seconds>.json`, so the next Start cannot
    /// overwrite it. Returns the file's new place.
    public func setAside(at date: Date) throws -> URL {
        try WorkoutsFiles.setAside(url, at: date)
    }

    // Dates stay `deferredToDate` (a Double), so a stored instant reloads
    // exactly and `fold(events) == state` survives a reload.
    private static var encoder: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return encoder
    }

    private static var decoder: JSONDecoder { JSONDecoder() }

    private struct File: Encodable {
        var format: Int
        var events: [Event]
        var state: State
    }

    private struct Header: Decodable { var format: Int }
    private struct EventsOnly: Decodable { var events: [Event] }
    private struct StateOnly: Decodable { var state: State }
}

/// The journal as stored.
public struct JournalContents<Event: Sendable, State: Sendable>: Sendable {
    public var events: [Event]
    /// Nil when the stored state did not decode.
    public var cachedState: State?
}

/// The journal after a fold.
public struct JournalLoad<Event: Sendable, State: Sendable>: Sendable {
    public var events: [Event]
    /// The fold of `events`: the session's state.
    public var state: State
    /// What the file held before, for the log line on a mismatch.
    public var cachedState: State?
    /// False when the cache differed from the fold and the file was rewritten.
    public var cacheMatched: Bool
}

public enum JournalError: Error, Hashable, Sendable {
    /// The file was written by a build with another journal format.
    case unsupportedFormat(Int)
}
