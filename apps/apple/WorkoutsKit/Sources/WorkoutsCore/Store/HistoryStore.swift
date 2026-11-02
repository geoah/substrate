import Foundation

/// One finished session as its history file holds it.
public struct HistoryEntry: Hashable, Sendable, Identifiable {
    public var records: SessionRecords
    /// What only this phone knows, never a record property.
    public var local: HistoryLocal

    public init(records: SessionRecords, local: HistoryLocal = HistoryLocal()) {
        self.records = records
        self.local = local
    }

    public var id: SessionID { records.session.id }
}

/// The history file's `local` part.
public struct HistoryLocal: Codable, Hashable, Sendable {
    public var health: HealthSaveState

    public init(health: HealthSaveState = HealthSaveState()) {
        self.health = health
    }
}

/// Where the session's Apple Health save stands (§9.5), which History shows.
public enum HealthSaveStatus: String, Codable, Sendable, CaseIterable {
    /// Queued, or waiting for access to be asked.
    case pending
    case saved
    /// Sharing was denied; "Try again" after access is given.
    case notSaved
    /// Health data is not available on this device; shown as nothing.
    case notAvailable
}

public struct HealthSaveState: Codable, Hashable, Sendable {
    public var status: HealthSaveStatus
    public var lastError: String?
    public var updatedAt: Date?

    public init(status: HealthSaveStatus = .pending, lastError: String? = nil, updatedAt: Date? = nil) {
        self.status = status
        self.lastError = lastError
        self.updatedAt = updatedAt
    }
}

/// Every history file that read, and every one that did not.
public struct HistoryLoad: Sendable {
    /// Newest first.
    public var entries: [HistoryEntry]
    /// Files that failed to decode: skipped and listed in Settings, never deleted.
    public var unreadable: [UnreadableRecord]
}

/// Finished sessions, one file each: `history/<session id>.json`, holding
/// `{"format": 2, "records": [session, sets...], "local": {"health": ...}}`
/// (§2.3). The records are the codec's flat records, exactly what V2 uploads.
public struct HistoryStore: Sendable {
    public static let directoryName = "history"

    /// The `history/` directory.
    public let directory: URL
    public let codec: RecordCodec
    public let writingOptions: Data.WritingOptions

    /// - Parameter directory: the app's directory; the files go in `history/` inside it.
    public init(directory: URL, codec: RecordCodec = RecordCodec(), writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting) {
        self.directory = directory.appendingPathComponent(Self.directoryName, isDirectory: true)
        self.codec = codec
        self.writingOptions = writingOptions
    }

    public func url(for id: SessionID) -> URL {
        directory.appendingPathComponent("\(id.rawValue).json", isDirectory: false)
    }

    /// Reads every file. A file that fails is skipped and reported, never
    /// deleted; a missing directory is an empty history.
    public func loadAll() -> HistoryLoad {
        let files = (try? FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)) ?? []
        var entries: [HistoryEntry] = []
        var unreadable: [UnreadableRecord] = []
        for file in files where file.pathExtension == "json" {
            do {
                try entries.append(decode(Data(contentsOf: file)))
            } catch {
                let name = "\(Self.directoryName)/\(file.lastPathComponent)"
                unreadable.append(UnreadableRecord(source: name, kind: codec.kinds.session, id: file.deletingPathExtension().lastPathComponent, reason: String(describing: error)))
            }
        }
        entries.sort(by: Self.newestFirst)
        unreadable.sort { $0.source < $1.source }
        return HistoryLoad(entries: entries, unreadable: unreadable)
    }

    public func read(_ id: SessionID) throws -> HistoryEntry {
        try decode(Data(contentsOf: url(for: id)))
    }

    /// Writes the session's file whole, at Finish and when the Health save returns.
    public func write(_ entry: HistoryEntry) throws {
        let file: JSONObject = try [
            "format": .number(Decimal(WorkoutsFormats.historyFormat)),
            "records": .array(codec.encode(entry.records).map(\.json)),
            "local": JSONValue(encoding: entry.local),
        ]
        try WorkoutsFiles.write(JSONValue.object(file).data(), to: url(for: entry.id), options: writingOptions)
    }

    /// Records the Health save's outcome. A save links the workout: the
    /// session keeps HealthKit's UUID and its revision rises by one (§9.5).
    @discardableResult
    public func recordHealthSave(_ id: SessionID, status: HealthSaveStatus, healthKitWorkout: String? = nil, error: String? = nil, at date: Date) throws -> HistoryEntry {
        var entry = try read(id)
        if let healthKitWorkout, entry.records.session.healthKitWorkout != healthKitWorkout {
            entry.records.session.healthKitWorkout = healthKitWorkout
            entry.records.session.revision = (entry.records.session.revision ?? 0) + 1
        }
        entry.local.health = HealthSaveState(status: status, lastError: error, updatedAt: date)
        try write(entry)
        return entry
    }

    /// Deletes the session's file; the Apple Health workout stays.
    public func delete(_ id: SessionID) throws {
        try WorkoutsFiles.remove(url(for: id))
    }

    private func decode(_ data: Data) throws -> HistoryEntry {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let header = try decoder.decode(Header.self, from: data)
        guard header.format == WorkoutsFormats.historyFormat || header.format == 1 else {
            throw HistoryError.unsupportedFormat(header.format)
        }
        let file = try decoder.decode(File.self, from: data)
        let flat = header.format == 1 ? HistoryMigration.fromFormat1(file.records, kinds: codec.kinds) : file.records
        let sessions = try codec.sessionRecords(from: flat)
        guard sessions.count == 1, let records = sessions.first else { throw HistoryError.notOneSession(sessions.count) }
        guard records.sets.count == flat.count - 1 else { throw HistoryError.strayRecords }
        return HistoryEntry(records: records, local: file.local ?? HistoryLocal())
    }

    static func newestFirst(_ lhs: HistoryEntry, _ rhs: HistoryEntry) -> Bool {
        let (a, b) = (lhs.records.session, rhs.records.session)
        return a.at != b.at ? a.at > b.at : a.id > b.id
    }

    private struct Header: Decodable { var format: Int }

    private struct File: Decodable {
        var records: [FlatRecord]
        var local: HistoryLocal?
    }
}

/// Reads a format 1 history file, written before the data-model review
/// (2026-10-31), as format 2: the session's `timeZone` becomes `timezone` and
/// a Workouts session gets `sport: strength`; a set target's `rpe` and `rir`
/// become ranges of one value (`rpeMin` = `rpeMax`); an `rpe` or `rir` with
/// no method is `entered`, since V1 stored one only when it was tapped.
/// Nothing V1 did not record is guessed: the load meaning, the completion
/// method and the time basis stay absent, and readers fall back as for any
/// set written before those fields existed. The file is rewritten as format
/// 2 the next time it is saved.
public enum HistoryMigration {
    public static func fromFormat1(_ records: [FlatRecord], kinds: KindNames) -> [FlatRecord] {
        records.map { record in
            var record = record
            var properties = record.properties
            switch record.kind {
            case kinds.session:
                if properties["timezone"] == nil, let zone = properties["timeZone"] {
                    properties["timezone"] = zone
                }
                properties["timeZone"] = nil
                if properties["sport"] == nil, properties["adapter"]?.stringValue?.hasPrefix("substrate-workouts@") == true {
                    properties["sport"] = .string(Sport.strength.rawValue)
                }
            case kinds.set:
                for (value, method) in [("rpe", "rpeMethod"), ("rir", "rirMethod")]
                    where properties[value] != nil && properties[method] == nil
                {
                    properties[method] = .string(EffortMethod.entered.rawValue)
                }
                if var target = properties["target"]?.objectValue {
                    for (old, low, high) in [("rpe", "rpeMin", "rpeMax"), ("rir", "rirMin", "rirMax")] {
                        guard let value = target[old] else { continue }
                        target[old] = nil
                        if target[low] == nil { target[low] = value }
                        if target[high] == nil { target[high] = value }
                    }
                    properties["target"] = .object(target)
                }
            default:
                break
            }
            record.properties = properties
            return record
        }
    }
}

public enum HistoryError: Error, Hashable, Sendable, CustomStringConvertible {
    case unsupportedFormat(Int)
    /// The file holds no session record, or more than one.
    case notOneSession(Int)
    /// A record that is neither the session nor one of its sets.
    case strayRecords

    public var description: String {
        switch self {
        case let .unsupportedFormat(format): "history format \(format) is not supported"
        case let .notOneSession(count): "the file holds \(count) sessions, not one"
        case .strayRecords: "the file holds records that are not the session's sets"
        }
    }
}
