import Foundation
import SubstrateRuntime

/// A controller's journal as one file in the folder's journals area:
/// `{"format": N, "events": [...], "state": {...}}`, rewritten whole and
/// atomically after every journaled event (a few tens of kilobytes). The
/// events are the truth and the state a cache of their fold, which the
/// runtime checks at open and writes again when it differs.
public struct JournalFile<Event: Codable & Sendable, State: Codable & Sendable>: JournalStore {
    public let folder: RepositoryFolder
    public let name: String
    /// The area the file is in: the journals' own, unless an app kept its
    /// journal elsewhere before the kit.
    public let area: FolderArea
    /// The layout of the events this build writes. A file of another is
    /// refused as unreadable, and the runtime sets it aside.
    public let format: Int

    public init(folder: RepositoryFolder, name: String, area: FolderArea = .journals, format: Int = 1) {
        self.folder = folder
        self.name = name
        self.area = area
        self.format = format
    }

    public func write(events: [Event], state: State) throws {
        try folder.write(Self.encoder.encode(File(format: format, events: events, state: state)), to: name, in: area)
    }

    public func read() throws -> StoredJournal<Event, State>? {
        let data: Data
        do {
            guard let read = try folder.read(name, in: area) else { return nil }
            data = read
        } catch {
            throw JournalUnavailable(reason: String(describing: error))
        }
        let decoder = JSONDecoder()
        let header = try decoder.decode(Header.self, from: data)
        guard header.format == format else { throw JournalFormatMismatch(found: header.format, expected: format) }
        let events = try decoder.decode(EventsOnly.self, from: data).events
        let state = try? decoder.decode(StateOnly.self, from: data).state
        return StoredJournal(events: events, cachedState: state)
    }

    public func delete() throws {
        try folder.remove(name, in: area)
    }

    public func setAside(at instant: Date) throws -> String {
        try folder.setAside(name, in: area, at: instant).name
    }

    // Dates stay `deferredToDate` (a Double), so a stored instant reloads
    // exactly and the fold equals the cached state after a reload.
    private static var encoder: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return encoder
    }

    private struct File: Encodable {
        var format: Int
        var events: [Event]
        var state: State
    }

    private struct Header: Decodable { var format: Int }
    private struct EventsOnly: Decodable { var events: [Event] }
    private struct StateOnly: Decodable { var state: State }
}

/// A journal written by a build with another layout of its events.
public struct JournalFormatMismatch: Error, Equatable, Sendable {
    public var found: Int
    public var expected: Int

    public init(found: Int, expected: Int) {
        self.found = found
        self.expected = expected
    }
}
