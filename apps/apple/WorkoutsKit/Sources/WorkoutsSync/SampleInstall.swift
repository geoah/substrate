import Foundation
import WorkoutsCore

/// Whether the sample routine was written into one server and repository
/// (decision S1, owner 2026-11-02). The app writes the sample once, the first
/// time it finds the repository with no routines, and never again after the
/// owner deletes it, on this device or any other.
///
/// THE SERVER IS THE MARKER. Deleting a routine keeps the exercises it named,
/// since sessions name them too, so a repository that holds any of the
/// sample's exercises has had the sample written, by this device or another,
/// before a reinstall or after one. No kind is added for it: the training
/// package's kinds already say it. `sample.json` in the repository's folder
/// only saves this device the reads once it knows, and marks a write it
/// started (`installing`), so a write cut short is finished here rather than
/// read as another device's.
public final class SampleInstall: @unchecked Sendable {
    public enum State: String, Codable, Sendable {
        /// This device started writing the sample and has not finished.
        case installing
        /// The sample was written, here or by another device; whether it is
        /// still there is the catalogue's to say.
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
        // A file that cannot be read is settled again from the server.
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        current = try? decoder.decode(File.self, from: data).state
    }

    /// Nil until the first run on this repository is settled on this device.
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

    /// The first sync on a repository, in this order:
    ///
    /// 1. it holds a routine: no sample, and `skipped` kept;
    /// 2. it holds any of the sample's exercises: the sample was written
    ///    before, by this device before a reinstall or by another one, so
    ///    nothing is written and `installed` kept;
    /// 3. else `installing` is kept, the sample's exercises and then its
    ///    routine are written, and `installed` kept.
    ///
    /// A write cut short leaves `installing`, and the next sync on this device
    /// finishes it. Does nothing once the first run is settled. Answers
    /// whether it wrote the sample.
    @discardableResult
    public func firstRun(_ api: WorkoutsAPI, remote: RemoteCatalog, bundled: Catalog, now: Date = Date()) async throws -> Bool {
        guard let sample = bundled.routines[SampleRoutine.id] else { return false }
        switch state {
        case .installed, .skipped:
            return false
        case nil:
            // Every routine, readable or not: one the app cannot decode is
            // still the owner's.
            guard try await api.list([remote.codec.kinds.routine]).isEmpty else {
                try record(.skipped, at: now)
                return false
            }
            let held = Set(try await api.list([remote.codec.kinds.exercise]).map(\.id))
            guard held.isDisjoint(with: sample.slots.map(\.exercise.rawValue)) else {
                try record(.installed, at: now)
                return false
            }
            try record(.installing, at: now)
        case .installing:
            break
        }
        try await remote.copySample(api, from: bundled)
        try record(.installed, at: now)
        return true
    }

    /// The sample written back after the owner deleted it ("Add the sample
    /// routine"): a delete of it still waiting is taken back, one already on
    /// the wire is let land first, and the routine is written whether or not
    /// the server still lists it, so the purge never wins.
    public func addBack(_ api: WorkoutsAPI, remote: RemoteCatalog, outbox: Outbox?, bundled: Catalog) async throws {
        try await outbox?.withdrawDelete(routine: SampleRoutine.id, codec: remote.codec)
        await outbox?.settle(routine: SampleRoutine.id, codec: remote.codec)
        try await remote.copySample(api, from: bundled, rewritingRoutine: true)
        try record(.installed)
    }
}

/// What the sync's catalogue step read: the catalogue, and why the sample's
/// first run did not finish, when it did not.
public struct CatalogRead: Sendable {
    public var catalog: Catalog
    public var sampleProblem: String?
}

/// The sync's catalogue step: the routines read, the sample's first run, and
/// the routines read again when it wrote. Only the read can fail it. A first
/// run that fails is reported in `sampleProblem` and tried again at the next
/// sync, and never stops what follows it (the history exchange, the outbox).
public enum CatalogSync {
    public static func read(_ api: WorkoutsAPI, remote: RemoteCatalog, outbox: Outbox?, sample: SampleInstall?,
                            bundled: @Sendable () throws -> Catalog) async throws -> CatalogRead
    {
        let hiding: @Sendable () async -> Set<String> = { await outbox?.waitingDeletes ?? [] }
        var catalog = try await remote.refresh(api, hiding: hiding)
        guard let sample, sample.state == nil || sample.state == .installing else { return CatalogRead(catalog: catalog) }
        do {
            if try await sample.firstRun(api, remote: remote, bundled: bundled()) {
                catalog = try await remote.refresh(api, hiding: hiding)
            }
            return CatalogRead(catalog: catalog)
        } catch {
            return CatalogRead(catalog: catalog, sampleProblem: (error as? LocalizedError)?.errorDescription ?? String(describing: error))
        }
    }
}
