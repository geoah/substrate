import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal

#if canImport(CryptoKit)
import CryptoKit
#else
import Crypto
#endif

/// What the device keeps of each repository's chats, under one folder
/// (`Application Support/SubstrateChat/` on an Apple device): a
/// `RepositoryFolder` per signed-in repository, named by its identity's
/// hash as every build has named it. At its top `state.json` (the list and
/// the marks no server holds), `drafts.json` and `link.json` (the link
/// generation); transcripts in the cache area, which the budget and the age
/// bound; the watch's cursor in the record cache. All of it but the marks,
/// the drafts and the generation is the server's, and is read again when it
/// is gone. Synchronous: effects call it.
///
/// A session reaches its folder only through the `Lease` its open answered.
/// Nothing reopens a folder on the way: a lease retired by a sign-out
/// reaches nothing, even when the same repository signs in again and has a
/// folder at the same path, so a write still on its way from the session
/// before lands nowhere. Opening and retiring run one at a time, in the
/// order they were asked, so a sign-out's deletion never meets the next
/// sign-in's folder.
public final class ChatStorage: @unchecked Sendable {
    /// Transcripts of chats not opened in this long are dropped.
    public static let unusedFor: TimeInterval = 60 * 86400
    /// Past this the least recently opened transcripts are dropped.
    public static let budget = 100 * 1024 * 1024

    /// One session's hold on its repository's folder, as the state keeps
    /// it. A value: what it reaches is the folder this open made, and
    /// nothing after it is retired.
    public struct Lease: Codable, Hashable, Sendable {
        public var identity: String
        /// Unique to this open in the process.
        public var number: Int
        /// The link generation the folder held at open (§5.4 of the kit's
        /// plan): background work scheduled under another writes nothing.
        public var generation: Int
    }

    /// Nil keeps nothing: a preview.
    public let base: URL?
    public let writingOptions: Data.WritingOptions
    /// Guards `held`, `tail` and `opens`.
    private let lock = NSLock()
    /// Held by every step that adds, moves or removes a folder's files, so
    /// a lease's check and its step are one: opening, retiring, clearing,
    /// pruning, touching, every write.
    private let files = NSLock()
    private var held: [String: Holding] = [:]
    private var tail: Task<Void, Never>?
    private var opens = 0

    private final class Holding: @unchecked Sendable {
        let lease: Lease
        let folder: RepositoryFolder?
        let records: RecordCache?
        let adoption: TranscriptAdoption

        init(lease: Lease, folder: RepositoryFolder?, records: RecordCache?, adoption: TranscriptAdoption) {
            self.lease = lease
            self.folder = folder
            self.records = records
            self.adoption = adoption
        }
    }

    public init(base: URL?, writingOptions: Data.WritingOptions = []) {
        self.base = base
        self.writingOptions = writingOptions
    }

    /// The first 12 bytes of a SHA-256 as hex: the name of a repository's
    /// folder and of a transcript's file, as every build has spelled it.
    public static func hash(_ text: String) -> String {
        SHA256.hash(data: Data(text.utf8)).prefix(12).map { String(format: "%02x", $0) }.joined()
    }

    private func root(_ identity: String) -> URL? {
        base?.appendingPathComponent(Self.hash(identity), isDirectory: true)
    }

    // MARK: Opening and retiring

    /// Runs after every open and retirement asked before it.
    private func serially<Value: Sendable>(_ body: @escaping @Sendable () -> Value) -> Task<Value, Never> {
        lock.withLock {
            let before = tail
            let task = Task {
                await before?.value
                return body()
            }
            tail = Task { _ = await task.value }
            return task
        }
    }

    /// Opens the repository's folder for a session, after every retirement
    /// asked before it. It is adopted where every build kept it, with no
    /// marker, so a build before ChatKit still reads `state.json` and
    /// `drafts.json`; the transcripts it kept in `messages/` move into the
    /// cache area. A folder that cannot be opened answers a lease that
    /// keeps nothing.
    func open(_ identity: String) -> Task<Lease, Never> {
        serially { [self] in
            files.withLock {
                let previous = lock.withLock { held.removeValue(forKey: identity) }
                previous?.folder?.close(deleting: false)
                var folder: RepositoryFolder?
                var adoption = TranscriptAdoption()
                if let root = root(identity), let opened = try? RepositoryFolder(adopting: root, writingOptions: writingOptions) {
                    folder = opened
                    adoption = Self.adoptTranscripts(in: opened)
                }
                let generation = folder.map { LinkGeneration(folder: $0).current } ?? 0
                let records = folder.map { RecordCache(folder: $0, name: "watch.json") }
                return lock.withLock {
                    opens += 1
                    let lease = Lease(identity: identity, number: opens, generation: generation)
                    held[identity] = Holding(lease: lease, folder: folder, records: records, adoption: adoption)
                    return lease
                }
            }
        }
    }

    /// Ends the session on the repository's folder: from now no lease of it
    /// reaches anything. Then, after every open asked before it, the link
    /// generation moves (a sign-out), the folder closes, and with
    /// `deleting` everything in it but the generation is gone; `also` runs
    /// in the same step, before any open asked after this one.
    @discardableResult
    func retire(_ identity: String, deleting: Bool, also: (@Sendable () -> Void)? = nil) -> Task<Void, Never> {
        // Fenced now: a write asked after this point finds no lease.
        let now = lock.withLock { held.removeValue(forKey: identity) }
        return serially { [self] in
            files.withLock {
                // An open asked before this one ran since.
                let holding = now ?? lock.withLock { held.removeValue(forKey: identity) }
                var folder = holding?.folder
                if folder == nil, let root = root(identity), FileManager.default.fileExists(atPath: root.path) {
                    folder = try? RepositoryFolder(adopting: root, writingOptions: writingOptions)
                }
                if let folder {
                    _ = try? LinkGeneration(folder: folder).bump()
                    folder.close(deleting: false)
                    if deleting { Self.removeAll(in: folder.root, keeping: [LinkGeneration.fileName]) }
                }
                also?()
            }
        }
    }

    private static func removeAll(in root: URL, keeping kept: Set<String>) {
        let manager = FileManager.default
        for name in (try? manager.contentsOfDirectory(atPath: root.path)) ?? [] where !kept.contains(name) {
            try? manager.removeItem(at: root.appendingPathComponent(name))
        }
    }

    /// Runs `body` with the lease's folder while the lease is the one open,
    /// and nothing else moves the folder's files meanwhile; nil otherwise.
    private func whileHeld<Value>(_ lease: Lease, _ body: (Holding) throws -> Value?) -> Value? {
        files.withLock {
            guard let holding = lock.withLock({ held[lease.identity] }), holding.lease == lease else { return nil }
            return try? body(holding)
        }
    }

    /// Whether the lease is the one open and its folder still holds the
    /// generation it was opened under: work scheduled under it may write.
    func holds(_ lease: Lease) -> Bool {
        whileHeld(lease) { holding in
            guard let folder = holding.folder else { return true }
            try LinkGeneration(folder: folder).check(lease.generation)
            return true
        } ?? false
    }

    /// Runs `body` only while the lease is the one open: a write kept
    /// beside the folder (the owner's choices) is fenced as its files are.
    func whileOpen(_ lease: Lease, _ body: () -> Void) {
        _ = whileHeld(lease) { _ in body() }
    }

    // MARK: The transcripts an earlier build kept

    /// What the move from `messages/` did with each file, by name.
    struct TranscriptAdoption: Equatable, Sendable {
        /// Moved into the cache area.
        var moved: [String] = []
        /// Both areas held one: the copy kept, by the thread version it was
        /// read under, then by the newest change; the other is removed.
        var kept: [String: Source] = [:]
        /// Could not be moved, and stays in `messages/` with the folder.
        var failed: [String] = []

        enum Source: String, Equatable, Sendable {
            case messages
            case cache
        }
    }

    /// Moves `messages/` into the cache area. `messages/` goes only once
    /// every file in it is moved or removed as the older of two; one that
    /// fails leaves it where it is, read again at the next open.
    static func adoptTranscripts(
        in folder: RepositoryFolder,
        move: (URL, URL) throws -> Void = { try FileManager.default.moveItem(at: $0, to: $1) }
    )
        -> TranscriptAdoption
    {
        let manager = FileManager.default
        var adoption = TranscriptAdoption()
        let old = folder.root.appendingPathComponent("messages", isDirectory: true)
        guard let names = try? manager.contentsOfDirectory(atPath: old.path) else { return adoption }
        let cache = folder.url("", in: .cache)
        do {
            try manager.createDirectory(at: cache, withIntermediateDirectories: true)
        } catch {
            adoption.failed = names.sorted()
            return adoption
        }
        for name in names.sorted() {
            let source = old.appendingPathComponent(name)
            let target = cache.appendingPathComponent(name)
            do {
                if manager.fileExists(atPath: target.path) {
                    if newer(source, than: target) {
                        let aside = cache.appendingPathComponent(name + ".adopting")
                        try? manager.removeItem(at: aside)
                        try manager.moveItem(at: target, to: aside)
                        do {
                            try move(source, target)
                        } catch {
                            try? manager.moveItem(at: aside, to: target)
                            throw error
                        }
                        try? manager.removeItem(at: aside)
                        adoption.kept[name] = .messages
                    } else {
                        try manager.removeItem(at: source)
                        adoption.kept[name] = .cache
                    }
                } else {
                    try move(source, target)
                    adoption.moved.append(name)
                }
            } catch {
                adoption.failed.append(name)
            }
        }
        if adoption.failed.isEmpty { try? manager.removeItem(at: old) }
        return adoption
    }

    /// Whether `a` is the newer copy of one thread's rows: read under a
    /// later thread version, else changed later.
    private static func newer(_ a: URL, than b: URL) -> Bool {
        let version = { (url: URL) -> Int? in
            (try? Data(contentsOf: url)).flatMap { try? JSONDecoder().decode(CachedTranscript.self, from: $0) }?.threadVersion
        }
        switch (version(a), version(b)) {
        case let (x?, y?) where x != y: return x > y
        case (_?, nil): return true
        case (nil, _?): return false
        default:
            let changed = { (url: URL) in
                ((try? FileManager.default.attributesOfItem(atPath: url.path))?[.modificationDate] as? Date) ?? .distantPast
            }
            return changed(a) > changed(b)
        }
    }

    /// What the open's move from `messages/` did.
    func adoption(_ lease: Lease) -> TranscriptAdoption? {
        whileHeld(lease) { $0.adoption }
    }

    // MARK: Reading

    /// The record cache holding the watch's cursor and the rows it marked
    /// to read again.
    func records(_ lease: Lease) -> RecordCache? {
        whileHeld(lease) { $0.records }
    }

    func state(_ lease: Lease) -> CachedState? {
        whileHeld(lease) { holding in
            guard let data = try holding.folder?.read("state.json", in: .root) else { return nil }
            let state = try JSONDecoder().decode(CachedState.self, from: data)
            return state.format == CachedState.format ? state : nil
        }
    }

    func drafts(_ lease: Lease) -> [String: String] {
        whileHeld(lease) { holding in
            guard let data = try holding.folder?.read("drafts.json", in: .root) else { return nil }
            return try JSONDecoder().decode([String: String].self, from: data)
        } ?? [:]
    }

    func transcript(_ lease: Lease, thread: String, at now: Date) -> CachedTranscript? {
        whileHeld(lease) { holding in
            guard let folder = holding.folder, let data = try folder.read(Self.transcriptName(thread), in: .cache) else { return nil }
            let cached = try JSONDecoder().decode(CachedTranscript.self, from: data)
            guard cached.format == CachedTranscript.format else { return nil }
            // Opened: the last to be dropped.
            folder.touch(Self.transcriptName(thread), in: .cache, at: now)
            return cached
        }
    }

    static func transcriptName(_ thread: String) -> String { hash(thread) + ".json" }

    // MARK: Writing

    func save(_ state: CachedState, _ lease: Lease) { write(state, "state.json", .root, lease) }

    func save(drafts: [String: String], _ lease: Lease) { write(drafts, "drafts.json", .root, lease) }

    func save(_ transcript: CachedTranscript, thread: String, _ lease: Lease) {
        write(transcript, Self.transcriptName(thread), .cache, lease)
    }

    private func write(_ value: some Encodable, _ name: String, _ area: FolderArea, _ lease: Lease) {
        guard let data = try? JSONEncoder().encode(value) else { return }
        _ = whileHeld(lease) { holding in try holding.folder?.write(data, to: name, in: area) }
    }

    // MARK: The budget

    /// Drops the transcripts not opened in `unusedFor`, then the least
    /// recently opened past `budget`.
    func prune(_ lease: Lease, at now: Date) {
        _ = whileHeld(lease) { holding in
            holding.folder?.evict(.cache, unusedSince: now.addingTimeInterval(-Self.unusedFor), budget: Self.budget)
        }
    }

    func usage(_ lease: Lease) -> [AreaUsage] {
        whileHeld(lease) { $0.folder?.usage() } ?? []
    }

    /// The transcripts only: the list, the marks and the drafts stay.
    func clearTranscripts(_ lease: Lease) {
        _ = whileHeld(lease) { holding in try holding.folder?.clear(.cache) }
    }
}

extension ChatStorage.Lease {
    /// What a refresh job scheduled under this lease carries: never a
    /// secret, since the system keeps it.
    var payload: [String: String] { ["repository": identity, "generation": String(generation)] }

    /// Whether the run's job was scheduled for this repository under this
    /// generation.
    func scheduled(_ run: BackgroundRun) -> Bool {
        run.payload["repository"] == identity && run.payload["generation"] == String(generation)
    }
}
