import Foundation
#if canImport(CryptoKit)
import CryptoKit
#else
import Crypto
#endif
import SubstrateAPI
import SubstrateCore
import SubstrateLocal

/// What the device keeps of each repository's chats, under one folder
/// (`Application Support/SubstrateChat/` on an Apple device): a
/// `RepositoryFolder` per signed-in repository, named by its identity's
/// hash as every build has named it. At its top `state.json` (the list and
/// the marks no server holds) and `drafts.json`; transcripts in the cache
/// area, which the budget and the age bound; the watch's cursor in the
/// record cache. All of it but the marks and the drafts is the server's,
/// and is read again when it is gone. Synchronous: effects call it.
public final class ChatStorage: @unchecked Sendable {
    /// Transcripts of chats not opened in this long are dropped.
    public static let unusedFor: TimeInterval = 60 * 86400
    /// Past this the least recently opened transcripts are dropped.
    public static let budget = 100 * 1024 * 1024

    /// Nil keeps nothing: a preview.
    public let base: URL?
    public let writingOptions: Data.WritingOptions
    private let lock = NSLock()
    private var open: [String: RepositoryFolder] = [:]
    private var caches: [String: RecordCache] = [:]

    public init(base: URL?, writingOptions: Data.WritingOptions = []) {
        self.base = base
        self.writingOptions = writingOptions
    }

    /// The first 12 bytes of a SHA-256 as hex: the name of a repository's
    /// folder and of a transcript's file, as every build has spelled it.
    public static func hash(_ text: String) -> String {
        SHA256.hash(data: Data(text.utf8)).prefix(12).map { String(format: "%02x", $0) }.joined()
    }

    /// The repository's folder, opened once. It is adopted where every
    /// build kept it, with no marker, so a build before ChatKit still reads
    /// `state.json` and `drafts.json`; the transcripts it kept in
    /// `messages/` move into the cache area, which it reads again.
    func folder(_ identity: String) throws -> RepositoryFolder? {
        try lock.withLock {
            if let held = open[identity], !held.isClosed { return held }
            guard let base else { return nil }
            let root = base.appendingPathComponent(Self.hash(identity), isDirectory: true)
            let folder = try RepositoryFolder(adopting: root, writingOptions: writingOptions)
            Self.adoptTranscripts(in: folder)
            open[identity] = folder
            caches[identity] = nil
            return folder
        }
    }

    private static func adoptTranscripts(in folder: RepositoryFolder) {
        let manager = FileManager.default
        let old = folder.root.appendingPathComponent("messages", isDirectory: true)
        guard let names = try? manager.contentsOfDirectory(atPath: old.path) else { return }
        let cache = folder.url("", in: .cache)
        try? manager.createDirectory(at: cache, withIntermediateDirectories: true)
        for name in names {
            let target = cache.appendingPathComponent(name)
            if manager.fileExists(atPath: target.path) { continue }
            try? manager.moveItem(at: old.appendingPathComponent(name), to: target)
        }
        try? manager.removeItem(at: old)
    }

    /// The record cache holding the watch's cursor and the rows it marked
    /// to read again.
    func records(_ identity: String) throws -> RecordCache? {
        guard let folder = try folder(identity) else { return nil }
        return lock.withLock {
            if let held = caches[identity] { return held }
            let cache = RecordCache(folder: folder, name: "watch.json")
            caches[identity] = cache
            return cache
        }
    }

    // MARK: Reading

    func state(_ identity: String) -> CachedState? {
        guard let data = try? folder(identity)?.read("state.json", in: .root),
              let state = try? JSONDecoder().decode(CachedState.self, from: data), state.format == CachedState.format
        else { return nil }
        return state
    }

    func drafts(_ identity: String) -> [String: String] {
        guard let data = try? folder(identity)?.read("drafts.json", in: .root) else { return [:] }
        return (try? JSONDecoder().decode([String: String].self, from: data)) ?? [:]
    }

    func transcript(_ identity: String, thread: String, at now: Date) -> CachedTranscript? {
        guard let folder = try? folder(identity), let data = try? folder.read(Self.transcriptName(thread), in: .cache),
              let cached = try? JSONDecoder().decode(CachedTranscript.self, from: data), cached.format == CachedTranscript.format
        else { return nil }
        // Opened: the last to be dropped.
        folder.touch(Self.transcriptName(thread), in: .cache, at: now)
        return cached
    }

    static func transcriptName(_ thread: String) -> String { hash(thread) + ".json" }

    // MARK: Writing

    func save(_ state: CachedState, _ identity: String) { write(state, "state.json", .root, identity) }

    func save(drafts: [String: String], _ identity: String) { write(drafts, "drafts.json", .root, identity) }

    func save(_ transcript: CachedTranscript, thread: String, _ identity: String) {
        write(transcript, Self.transcriptName(thread), .cache, identity)
    }

    /// A write after the folder closed (the session ended) is refused by
    /// the folder, so a late one never recreates a signed-out repository's
    /// files.
    private func write(_ value: some Encodable, _ name: String, _ area: FolderArea, _ identity: String) {
        guard let folder = try? folder(identity), let data = try? JSONEncoder().encode(value) else { return }
        try? folder.write(data, to: name, in: area)
    }

    // MARK: The budget

    /// Drops the transcripts not opened in `unusedFor`, then the least
    /// recently opened past `budget`.
    func prune(_ identity: String, at now: Date) {
        _ = try? folder(identity)?.evict(.cache, unusedSince: now.addingTimeInterval(-Self.unusedFor), budget: Self.budget)
    }

    func usage(_ identity: String) -> [AreaUsage] {
        (try? folder(identity)?.usage()) ?? []
    }

    /// The transcripts only: the list, the marks and the drafts stay.
    func clearTranscripts(_ identity: String) {
        try? folder(identity)?.clear(.cache)
    }

    /// Ends the repository's folder: nothing written after this lands, and
    /// with `deleting` it is gone.
    func close(_ identity: String, deleting: Bool) {
        let held = lock.withLock { () -> RepositoryFolder? in
            caches[identity] = nil
            return open.removeValue(forKey: identity)
        }
        if let held {
            held.close(deleting: deleting)
        } else if deleting, let base {
            try? FileManager.default.removeItem(at: base.appendingPathComponent(Self.hash(identity), isDirectory: true))
        }
    }
}
