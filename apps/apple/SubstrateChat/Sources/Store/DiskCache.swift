import CryptoKit
import Foundation

/// What the chat list, Settings and the cards draw from at launch.
struct CachedState: Codable, Sendable {
    static let format = 1

    var format = CachedState.format
    var agents: [Record] = []
    var providers: [Record] = []
    var threads: [Record] = []
    var automated: [Record] = []
    var openings: [String: String] = [:]
    var interactions: [Record] = []
    var requests: [Record] = []
    /// Each chat's `finishedAt` when it was last seen settled.
    var seen: [String: String] = [:]
    /// What has been notified: `reply|<thread>|<finishedAt>`, `card|<kind>/<id>`.
    var notified: Set<String> = []
    /// Whether the first sign-in's history was marked seen.
    var baselined = false
    /// The default agent when Settings names none: the last one chatted with.
    var lastAgent: String?
    /// The feed's cursor: a seq means nothing without its generation.
    var feedSeq: Int?
    var feedGeneration: String?
}

/// One thread's rows as last read, and what they were read under.
struct CachedTranscript: Codable, Sendable {
    static let format = 1

    var format = CachedTranscript.format
    /// The thread's version when the rows were read: they are current only
    /// while it is unchanged and the thread is settled.
    var threadVersion: Int
    /// The read filled its window: there may be older rows it did not hold.
    var full: Bool
    var records: [Record]
    var openedAt: Date?
}

/// The app's saved chats, one directory per signed-in identity under
/// Application Support. Everything here is the server's and can be read
/// again, so it is excluded from backups and, on iOS, protected until the
/// first unlock (the Keychain item's class). Writes are atomic and off the
/// main actor; a write after `close()` (the session ended) is dropped, so a
/// late write never recreates a signed-out repository's files.
actor DiskCache {
    nonisolated let root: URL
    private var closed = false

    static func hash(_ identity: String) -> String {
        SHA256.hash(data: Data(identity.utf8)).prefix(12).map { String(format: "%02x", $0) }.joined()
    }

    static func directory(for identity: String) -> URL {
        let base = (try? FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask,
                                                 appropriateFor: nil, create: true))
            ?? FileManager.default.temporaryDirectory
        return base.appending(path: "SubstrateChat", directoryHint: .isDirectory)
            .appending(path: hash(identity), directoryHint: .isDirectory)
    }

    init(identity: String) {
        root = Self.directory(for: identity)
        Self.prepare(root)
    }

    private static func prepare(_ root: URL) {
        let fm = FileManager.default
        try? fm.createDirectory(at: root.appending(path: "messages"), withIntermediateDirectories: true)
        var url = root
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try? url.setResourceValues(values)
    }

    // MARK: Reads, synchronous: the launch and an opening chat draw from them.

    nonisolated func loadState() -> CachedState? {
        guard let data = try? Data(contentsOf: root.appending(path: "state.json")),
              let state = try? JSONDecoder().decode(CachedState.self, from: data),
              state.format == CachedState.format
        else { return nil }
        return state
    }

    nonisolated func loadTranscript(_ thread: String) -> CachedTranscript? {
        guard let data = try? Data(contentsOf: transcriptURL(thread)),
              let cached = try? JSONDecoder().decode(CachedTranscript.self, from: data),
              cached.format == CachedTranscript.format
        else { return nil }
        return cached
    }

    nonisolated func loadDrafts() -> [String: String] {
        guard let data = try? Data(contentsOf: root.appending(path: "drafts.json")) else { return [:] }
        return (try? JSONDecoder().decode([String: String].self, from: data)) ?? [:]
    }

    nonisolated private func transcriptURL(_ thread: String) -> URL {
        root.appending(path: "messages").appending(path: Self.hash(thread) + ".json")
    }

    // MARK: Writes

    func save(_ state: CachedState) { write(state, to: root.appending(path: "state.json")) }

    func save(_ transcript: CachedTranscript, thread: String) { write(transcript, to: transcriptURL(thread)) }

    func save(drafts: [String: String]) { write(drafts, to: root.appending(path: "drafts.json")) }

    private func write(_ value: some Encodable, to url: URL) {
        guard !closed, let data = try? JSONEncoder().encode(value) else { return }
        #if os(iOS)
        try? data.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        #else
        try? data.write(to: url, options: .atomic)
        #endif
    }

    /// Ends the session's cache: nothing written after this lands, and the
    /// directory is gone.
    func close(deleting: Bool) {
        closed = true
        if deleting { try? FileManager.default.removeItem(at: root) }
    }

    /// Deletes the transcripts only; the list and the choices stay.
    func clearTranscripts() {
        let dir = root.appending(path: "messages")
        try? FileManager.default.removeItem(at: dir)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    }

    /// Drops transcripts not opened in `age`, then the oldest past `budget`.
    func prune(age: TimeInterval = 60 * 86400, budget: Int = 100 * 1024 * 1024) {
        let dir = root.appending(path: "messages")
        let keys: [URLResourceKey] = [.contentAccessDateKey, .contentModificationDateKey, .fileSizeKey]
        guard let files = try? FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: keys) else { return }
        let now = Date()
        var kept: [(URL, Date, Int)] = []
        for file in files {
            let values = try? file.resourceValues(forKeys: Set(keys))
            let used = values?.contentAccessDate ?? values?.contentModificationDate ?? .distantPast
            if now.timeIntervalSince(used) > age {
                try? FileManager.default.removeItem(at: file)
            } else {
                kept.append((file, used, values?.fileSize ?? 0))
            }
        }
        var total = kept.reduce(0) { $0 + $1.2 }
        for (file, _, size) in kept.sorted(by: { $0.1 < $1.1 }) where total > budget {
            try? FileManager.default.removeItem(at: file)
            total -= size
        }
    }

    nonisolated func size() -> Int {
        guard let files = FileManager.default.enumerator(at: root, includingPropertiesForKeys: [.fileSizeKey]) else { return 0 }
        var total = 0
        for case let file as URL in files {
            total += (try? file.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 0
        }
        return total
    }
}
