import Foundation
import WorkoutsCore

/// Whether the sample routine was written into one server and repository,
/// kept as `sample.json` in its folder (decision S1, owner 2026-11-02). The
/// app writes the sample once, the first time it finds the repository with no
/// routines; the file is what keeps a sample the owner deleted from coming
/// back at the next sync, or after a relaunch.
public final class SampleInstall: @unchecked Sendable {
    public enum State: String, Codable, Sendable {
        /// The sample was written; whether it is still there is the catalogue's to say.
        case installed
        /// The repository had routines of its own the first time: no sample.
        case skipped
    }

    struct File: Codable {
        var format = 1
        var state: State
        var at: Date
    }

    public static let fileName = "sample.json"

    public let url: URL
    private let writingOptions: Data.WritingOptions
    // Guards `current`; `@unchecked Sendable` rests on it.
    private let lock = NSLock()
    private var current: State?

    public init(directory: URL, writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting) {
        url = directory.appendingPathComponent(Self.fileName)
        self.writingOptions = writingOptions
        guard let data = try? Data(contentsOf: url) else { return }
        // A file that cannot be read still says the first run happened: never
        // write the sample again over a repository the owner may have cleared.
        current = (try? JSONDecoder().decode(File.self, from: data).state) ?? .installed
    }

    /// Nil until the first run on this repository is settled.
    public var state: State? { lock.withLock { current } }

    /// Records the first run's outcome, on disk before it returns.
    public func record(_ state: State, at date: Date = Date()) throws {
        try lock.withLock {
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.withoutEscapingSlashes, .sortedKeys]
            encoder.dateEncodingStrategy = .iso8601
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            try encoder.encode(File(state: state, at: date)).write(to: url, options: writingOptions)
            current = state
        }
    }

    /// Whether to offer the sample again: it was written once and the
    /// repository no longer holds it, so the owner deleted it.
    public func offersSample(_ catalog: Catalog) -> Bool {
        Self.offersSample(state: state, catalog: catalog)
    }

    /// The empty Routines screen's "Add the sample routine": only when the
    /// sample was written once and is gone, never on a first run still to
    /// come or a repository that had routines of its own.
    public static func offersSample(state: State?, catalog: Catalog) -> Bool {
        state == .installed && catalog.routines[SampleRoutine.id] == nil
    }

    /// The first sync on a repository: the sample and its exercises written
    /// when the repository holds no routine, else nothing, and the outcome
    /// kept either way. Does nothing once the first run is settled. Answers
    /// whether it wrote the sample. A failed write keeps nothing, so the
    /// next sync tries again.
    @discardableResult
    public func firstRun(_ api: WorkoutsAPI, remote: RemoteCatalog, bundled: Catalog, now: Date = Date()) async throws -> Bool {
        guard state == nil else { return false }
        // Every routine, readable or not: one the app cannot decode is still
        // the owner's.
        let routines = try await api.list([remote.codec.kinds.routine])
        guard routines.isEmpty else {
            try record(.skipped, at: now)
            return false
        }
        try await remote.copySample(api, from: bundled)
        try record(.installed, at: now)
        return true
    }
}
