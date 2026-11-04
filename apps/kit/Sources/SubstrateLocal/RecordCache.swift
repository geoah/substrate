import Foundation
import SubstrateAPI
import SubstrateCore

/// The records an app keeps of the kinds it shows, as last read, and where
/// the watch over them resumes, in the repository's folder. A list read
/// fills it; each watch row marks its record stale (or removes it) and moves
/// the cursor; the app reads the stale ones again and stores what it read.
/// All of it is the server's and can be read again, so a file that does not
/// decode is set aside and the cache starts empty.
public actor RecordCache {
    public static let fileFormat = 1

    private struct File: Codable {
        var format = RecordCache.fileFormat
        var cursor: WatchCursor?
        var records: [String: Record] = [:]
        var stale: Set<String> = []
    }

    public let folder: RepositoryFolder
    public let name: String
    private var file: File
    /// The file that was there at open and could not be read, set aside.
    public private(set) var unreadable: SetAsideFile?
    /// Why the last save failed, until one succeeds.
    public private(set) var saveProblem: String?

    public init(folder: RepositoryFolder, name: String = "records.json", now: Date = Date()) {
        self.folder = folder
        self.name = name
        let loaded = try? folder.load(File.self, from: name, in: .records, at: now)
        file = loaded?.value.flatMap { $0.format == Self.fileFormat ? $0 : nil } ?? File()
        unreadable = loaded?.aside
    }

    /// Where the watch resumes; nil until a list read or a bookmark set one.
    public var cursor: WatchCursor? { file.cursor }

    /// The records whose change the watch reported and which have not been
    /// read again since, as `kind/id`.
    public var stale: Set<String> { file.stale }

    public func record(_ kind: String, _ id: String) -> Record? { file.records["\(kind)/\(id)"] }

    /// A kind's records, by id.
    public func records(of kind: String) -> [Record] {
        file.records.values.filter { $0.kind == kind }.sorted { $0.id < $1.id }
    }

    /// A list read of `kinds`, whole: their records replaced by the page's.
    /// The watch resumes from `bookmark`, the one it opened at before the
    /// list was read, else from the page's head (an app token's lists carry
    /// none), unless it already follows this history from earlier, so
    /// nothing written between two reads is missed.
    public func listed(_ page: RecordPage, kinds: [String], from bookmark: WatchCursor? = nil) {
        let replaced = Set(kinds)
        file.records = file.records.filter { !replaced.contains($0.value.kind) }
        for record in page.records where record.deletedAt == nil {
            file.records["\(record.kind)/\(record.id)"] = record
        }
        file.stale = file.stale.filter { key in !replaced.contains(where: { key.hasPrefix("\($0)/") }) }
        if let start = bookmark ?? (page.generation.isEmpty ? nil : WatchCursor(seq: page.head, generation: page.generation)) {
            if let cursor = file.cursor, cursor.generation == start.generation {
                file.cursor = WatchCursor(seq: min(cursor.seq, start.seq), generation: start.generation)
            } else {
                file.cursor = start
            }
        }
        save()
    }

    /// The watch's opening bookmark: where it follows from when nothing was
    /// listed under a head (an app token's lists carry none).
    public func bookmark(_ cursor: WatchCursor) {
        if file.cursor?.generation != cursor.generation || file.cursor == nil { file.cursor = cursor }
        save()
    }

    /// One watch row: a removal drops the record, anything else marks it to
    /// be read again. Answers whether it now waits for that read.
    @discardableResult
    public func apply(_ row: WatchRow) -> Bool {
        let key = "\(row.kind)/\(row.recordID)"
        if row.removes {
            file.records[key] = nil
            file.stale.remove(key)
        } else {
            file.stale.insert(key)
        }
        if let cursor = file.cursor {
            file.cursor = WatchCursor(seq: max(cursor.seq, row.seq), generation: cursor.generation)
        }
        save()
        return !row.removes
    }

    /// A record read again: stored, or removed when the read found none.
    public func refreshed(_ kind: String, _ id: String, as record: Record?) {
        let key = "\(kind)/\(id)"
        file.records[key] = record.flatMap { $0.deletedAt == nil ? $0 : nil }
        file.stale.remove(key)
        save()
    }

    /// The cursor addresses no history the server holds (a 410, or a
    /// `compacted` frame): everything is read again and the watch opens at
    /// the new head.
    public func relist() {
        file.cursor = nil
        file.stale = []
        save()
    }

    /// Empties the cache: another repository's, or the owner cleared it.
    public func clear() {
        file = File()
        save()
    }

    private func save() {
        do {
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.withoutEscapingSlashes, .sortedKeys]
            try folder.write(encoder.encode(file), to: name, in: .records)
            saveProblem = nil
        } catch {
            saveProblem = error.localizedDescription
        }
    }
}
