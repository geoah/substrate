import Foundation

/// Everything an app keeps on the device for one repository, in one folder:
/// its format, then an area each for the journals, the outbox, the record
/// cache and what else it can read again (§5.3); or, made with `app:`, what
/// the app keeps outside any repository, by the same rules. Every write is atomic, so a
/// crash leaves the old file and never half a new one; a file that cannot be
/// read is set aside beside where it was and listed, never written over; a
/// write after `close` is refused, so a late one never recreates a
/// signed-out repository's files.
///
/// Synchronous and safe from any thread: a journal is written through it
/// with no suspension after its event.
public final class RepositoryFolder: @unchecked Sendable {
    /// The layout this build reads and writes. A folder of another is set
    /// aside whole at open, and the app starts it afresh.
    public static let format = 1

    public let root: URL
    /// The options every write adds to `.atomic`: the app folders' data
    /// protection on an Apple device.
    public let writingOptions: Data.WritingOptions
    /// The whole folder as it was, set aside at open because another build
    /// wrote it in a layout this one does not read.
    public let setAsideAtOpen: SetAsideFile?

    private let lock = NSLock()
    private var closed = false

    public init(root: URL, writingOptions: Data.WritingOptions = [], now: Date = Date()) throws {
        self.root = root
        self.writingOptions = writingOptions
        let manager = FileManager.default
        try manager.createDirectory(at: root, withIntermediateDirectories: true)
        let marker = root.appendingPathComponent(Self.markerName)
        var aside: SetAsideFile?
        let stored = (try? Data(contentsOf: marker)).flatMap { try? JSONDecoder().decode(Marker.self, from: $0) }
        let contents = (try? manager.contentsOfDirectory(atPath: root.path)) ?? []
        if stored?.format != Self.format, contents.contains(where: { $0 != Self.markerName && SetAsideFile.instant(in: $0) == nil }) {
            let name = SetAsideFile.name(for: "folder", at: now)
            let target = root.appendingPathComponent(name)
            try manager.createDirectory(at: target, withIntermediateDirectories: true)
            for item in contents where item != Self.markerName && SetAsideFile.instant(in: item) == nil {
                try manager.moveItem(at: root.appendingPathComponent(item), to: target.appendingPathComponent(item))
            }
            aside = SetAsideFile(area: .root, name: name, at: now)
        }
        if stored?.format != Self.format {
            try JSONEncoder().encode(Marker(format: Self.format)).write(to: marker, options: writingOptions.union(.atomic))
        }
        setAsideAtOpen = aside
    }

    /// A repository's folder an app laid out before it kept a format
    /// (Workouts' account folders, Chat's): its files stay where they are,
    /// nothing is set aside, and no marker is written, so a build before the
    /// kit still reads every file.
    public convenience init(adopting root: URL, writingOptions: Data.WritingOptions = []) throws {
        try self.init(unmarked: root, writingOptions: writingOptions)
    }

    /// The app's own folder, which no repository owns: what it keeps
    /// whoever is signed in, or no one (Workouts' session journal, its
    /// history files). Laid out as the app always has, as `adopting:` is:
    /// no marker, nothing set aside.
    public convenience init(app root: URL, writingOptions: Data.WritingOptions = []) throws {
        try self.init(unmarked: root, writingOptions: writingOptions)
    }

    private init(unmarked root: URL, writingOptions: Data.WritingOptions) throws {
        self.root = root
        self.writingOptions = writingOptions
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        setAsideAtOpen = nil
    }

    private static let markerName = "folder.json"

    private struct Marker: Codable {
        var format: Int
    }

    /// The folder's name under the app's data folder for one repository on
    /// one server: readable, and safe as a single path segment.
    public static func name(server: URL, repository: String) -> String {
        let host = (server.host ?? "server") + (server.port.map { "-\($0)" } ?? "")
        let allowed = Set("abcdefghijklmnopqrstuvwxyz0123456789.-_@")
        return "\(repository)@\(host)".lowercased().map { allowed.contains($0) ? String($0) : "_" }.joined()
    }

    public func url(_ name: String, in area: FolderArea) -> URL {
        directory(area).appendingPathComponent(name, isDirectory: false)
    }

    private func directory(_ area: FolderArea) -> URL {
        area == .root ? root : root.appendingPathComponent(area.rawValue, isDirectory: true)
    }

    // MARK: Reading and writing

    /// The file's bytes, nil when there is none. A file that is there and
    /// cannot be read throws.
    public func read(_ name: String, in area: FolderArea) throws -> Data? {
        let url = url(name, in: area)
        guard FileManager.default.fileExists(atPath: url.path) else { return nil }
        return try Data(contentsOf: url)
    }

    /// Writes the file whole, atomically, creating its area first.
    public func write(_ data: Data, to name: String, in area: FolderArea) throws {
        try lock.withLock {
            guard !closed else { throw FolderClosed() }
            try FileManager.default.createDirectory(at: directory(area), withIntermediateDirectories: true)
            try data.write(to: url(name, in: area), options: writingOptions.union(.atomic))
        }
    }

    /// Removes the file; one already gone is not an error.
    public func remove(_ name: String, in area: FolderArea) throws {
        let url = url(name, in: area)
        guard FileManager.default.fileExists(atPath: url.path) else { return }
        try FileManager.default.removeItem(at: url)
    }

    /// The file decoded, nil when there is none. One that does not decode
    /// is set aside and answered as `aside`, so the caller starts afresh and
    /// says so; one that cannot be read at all stays where it is and throws.
    public func load<Value: Decodable>(_ type: Value.Type, from name: String, in area: FolderArea,
                                       at now: Date = Date()) throws -> (value: Value?, aside: SetAsideFile?)
    {
        guard let data = try read(name, in: area) else { return (nil, nil) }
        do {
            return (try JSONDecoder().decode(type, from: data), nil)
        } catch {
            return (nil, try setAside(name, in: area, at: now))
        }
    }

    /// Moves a file that cannot be read out of the way, beside it.
    @discardableResult
    public func setAside(_ name: String, in area: FolderArea, at now: Date = Date()) throws -> SetAsideFile {
        let target = SetAsideFile.name(for: name, at: now)
        let destination = url(target, in: area)
        try? FileManager.default.removeItem(at: destination)
        try FileManager.default.moveItem(at: url(name, in: area), to: destination)
        return SetAsideFile(area: area, name: target, at: now)
    }

    /// Every file set aside, in every area, oldest first.
    public var setAside: [SetAsideFile] {
        var out: [SetAsideFile] = []
        for area in areas + [.root] {
            for name in names(in: area) {
                if let at = SetAsideFile.instant(in: name) { out.append(SetAsideFile(area: area, name: name, at: at)) }
            }
        }
        return out.sorted { ($0.at, $0.name) < ($1.at, $1.name) }
    }

    /// Removes a file set aside once the owner has looked.
    public func discard(_ file: SetAsideFile) throws {
        try? FileManager.default.removeItem(at: url(file.name, in: file.area))
    }

    // MARK: Sizes and budgets

    /// The areas there are on disk, by name.
    public var areas: [FolderArea] {
        let names = (try? FileManager.default.contentsOfDirectory(atPath: root.path)) ?? []
        return names.filter { name in
            var directory: ObjCBool = false
            return FileManager.default.fileExists(atPath: root.appendingPathComponent(name).path, isDirectory: &directory)
                && directory.boolValue && SetAsideFile.instant(in: name) == nil
        }.map { FolderArea($0) }.sorted()
    }

    /// What each area holds, set-aside files included.
    public func usage() -> [AreaUsage] {
        areas.map { area in
            let files = entries(in: area)
            let bytes = files.reduce(0) { total, file in total + file.bytes }
            return AreaUsage(area: area, bytes: bytes, files: files.count)
        }
    }

    /// Marks a file used now, so eviction takes it last.
    public func touch(_ name: String, in area: FolderArea, at now: Date = Date()) {
        try? FileManager.default.setAttributes([.modificationDate: now], ofItemAtPath: url(name, in: area).path)
    }

    /// Drops the area's files not used since `cutoff`, then the least
    /// recently used past `budget` bytes. Only for an area whose files can
    /// be read again: never the journals or the outbox. Answers the names
    /// dropped.
    @discardableResult
    public func evict(_ area: FolderArea, unusedSince cutoff: Date, budget: Int) -> [String] {
        precondition(area != .journals && area != .outbox && area != .root, "\(area.rawValue) holds what cannot be read again")
        var dropped: [String] = []
        var kept: [Entry] = []
        for file in entries(in: area) {
            if file.used < cutoff {
                try? FileManager.default.removeItem(at: url(file.name, in: area))
                dropped.append(file.name)
            } else {
                kept.append(file)
            }
        }
        var total = kept.reduce(0) { total, file in total + file.bytes }
        for file in kept.sorted(by: { ($0.used, $0.name) < ($1.used, $1.name) }) where total > budget {
            try? FileManager.default.removeItem(at: url(file.name, in: area))
            dropped.append(file.name)
            total -= file.bytes
        }
        return dropped.sorted()
    }

    /// Removes everything in an area that can be read again.
    public func clear(_ area: FolderArea) throws {
        precondition(area != .journals && area != .outbox && area != .root, "\(area.rawValue) holds what cannot be read again")
        let url = directory(area)
        guard FileManager.default.fileExists(atPath: url.path) else { return }
        try FileManager.default.removeItem(at: url)
    }

    private func names(in area: FolderArea) -> [String] {
        ((try? FileManager.default.contentsOfDirectory(atPath: directory(area).path)) ?? []).sorted()
    }

    private struct Entry {
        var name: String
        var used: Date
        var bytes: Int
    }

    private func entries(in area: FolderArea) -> [Entry] {
        names(in: area).compactMap { name -> Entry? in
            guard let attributes = try? FileManager.default.attributesOfItem(atPath: url(name, in: area).path),
                  (attributes[.type] as? FileAttributeType) == .typeRegular
            else { return nil }
            let bytes = (attributes[.size] as? NSNumber)?.intValue ?? 0
            let used = attributes[.modificationDate] as? Date ?? .distantPast
            return Entry(name: name, used: used, bytes: bytes)
        }
    }

    // MARK: Closing

    /// Ends the folder's use: every later write is refused, and with
    /// `deleting` the folder is gone (a sign-out).
    public func close(deleting: Bool) {
        lock.withLock { closed = true }
        if deleting { try? FileManager.default.removeItem(at: root) }
    }

    public var isClosed: Bool { lock.withLock { closed } }
}

/// A write to a repository's folder after it was closed.
public struct FolderClosed: Error, Equatable, Sendable {
    public init() {}
}
