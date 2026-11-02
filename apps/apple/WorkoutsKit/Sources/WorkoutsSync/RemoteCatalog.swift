import Foundation
import WorkoutsCore

/// The exercises, routines and setups of one repository, cached on the
/// phone so the gym needs no signal: the cache draws at launch, and a refresh
/// replaces it whole once the server answers. The phone's own exercises are
/// merged in as `BundledCatalog` merges them. A record that does not decode
/// is skipped and listed in `Catalog.unreadable`.
public final class RemoteCatalog: CatalogSource, @unchecked Sendable {
    public let codec: RecordCodec
    public let cacheURL: URL
    private let localExercises: LocalExercises?
    private let writingOptions: Data.WritingOptions
    // Guards `continuations`; `@unchecked Sendable` rests on it.
    private let lock = NSLock()
    private var continuations: [UUID: AsyncStream<Catalog>.Continuation] = [:]
    /// Held over each read-modify-write of the cache file, so an edit
    /// stored on the main actor and a refresh never lose each other's.
    private let fileLock = NSLock()

    public static let fileName = "catalog.json"

    /// - Parameter directory: the folder of one server and repository.
    public init(directory: URL, localExercises: LocalExercises? = nil, codec: RecordCodec = RecordCodec(),
                writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting)
    {
        cacheURL = directory.appendingPathComponent(Self.fileName)
        self.localExercises = localExercises
        self.codec = codec
        self.writingOptions = writingOptions
    }

    /// Whether a refresh has ever landed.
    public var hasCache: Bool { FileManager.default.fileExists(atPath: cacheURL.path) }

    public func load() async throws -> Catalog { try loadNow() }

    /// The cached catalogue; empty before the first refresh.
    public func loadNow() throws -> Catalog {
        guard hasCache else { return merged(Catalog()) }
        let file = try JSONDecoder().decode(RecordFile.self, from: Data(contentsOf: cacheURL))
        return merged(catalog(from: file.records, source: Self.fileName))
    }

    public func updates() -> AsyncStream<Catalog> {
        let (stream, continuation) = AsyncStream.makeStream(of: Catalog.self, bufferingPolicy: .bufferingNewest(1))
        let key = UUID()
        lock.withLock { continuations[key] = continuation }
        continuation.onTermination = { [weak self] _ in
            guard let self else { return }
            _ = lock.withLock { self.continuations.removeValue(forKey: key) }
        }
        return stream
    }

    /// Reads every exercise, setup and routine from the server, keeps them,
    /// and yields the catalogue to every `updates()` stream. A record whose
    /// delete still waits in the outbox (`hiding`, as `kind/id`) is left
    /// out, so a routine deleted offline does not come back before its
    /// delete lands. `hiding` is asked after the server answers, so a delete
    /// made while the read was on the wire is left out too.
    ///
    /// A routine whose edit or create still waits (`waiting`, by id) is
    /// read with the waiting properties laid over the server's, so an edit
    /// made offline shows until it lands; the cache keeps the server's
    /// version, the one the edit asserts.
    @discardableResult
    public func refresh(_ api: WorkoutsAPI, hiding: @Sendable () async -> Set<String> = { [] },
                        waiting: @Sendable () async -> [String: (creates: Bool, properties: JSONObject)] = { [:] }) async throws -> Catalog
    {
        let kinds = codec.kinds
        // Asked before the read too: an edit that lands while the list is on
        // the wire is in neither the list nor the queue asked after it.
        let before = await waiting()
        let listed = try await api.list([kinds.movement, kinds.exercise, kinds.setup, kinds.routine])
        let hidden = await hiding()
        let pending = before.merging(await waiting()) { _, after in after }
        let records = Self.overlay(listed.filter { !hidden.contains("\($0.kind)/\($0.id)") }, waiting: pending, kind: kinds.routine)
        try fileLock.withLock { try write(records) }
        let catalog = merged(catalog(from: records, source: api.client.server.host() ?? "the server"))
        publish(catalog)
        return catalog
    }

    /// The routine writes still waiting laid over the records: an edit over
    /// its routine, a create added when the server lacks it.
    static func overlay(_ records: [FlatRecord], waiting: [String: (creates: Bool, properties: JSONObject)], kind: String) -> [FlatRecord] {
        guard !waiting.isEmpty else { return records }
        var out = records
        var seen = Set<String>()
        for index in out.indices where out[index].kind == kind {
            guard let write = waiting[out[index].id] else { continue }
            seen.insert(out[index].id)
            out[index].properties = merged(out[index].properties, write.properties)
        }
        for (id, write) in waiting.sorted(by: { $0.key < $1.key }) where write.creates && !seen.contains(id) {
            out.append(FlatRecord(kind: kind, id: id, properties: merged(JSONObject(), write.properties)))
        }
        return out
    }

    static func merged(_ base: JSONObject, _ change: JSONObject) -> JSONObject {
        var out = base
        for (key, value) in change {
            out[key] = value == .null ? nil : value
        }
        return out
    }

    /// Keeps an edited or new routine in the cache at once, before the
    /// server hears of it, at the version it was read at, and yields the
    /// catalogue with it.
    @discardableResult
    public func store(routine: Routine) throws -> Catalog {
        try store(record: codec.encode(routine))
    }

    /// Keeps one record as the server answered it (a routine read again
    /// after a conflict), or takes it out when the server holds none.
    @discardableResult
    public func store(record: FlatRecord?, removing id: RoutineID? = nil) throws -> Catalog {
        let records = try fileLock.withLock { () throws -> [FlatRecord] in
            var records = hasCache ? try JSONDecoder().decode(RecordFile.self, from: Data(contentsOf: cacheURL)).records : []
            if let record {
                if let index = records.firstIndex(where: { $0.kind == record.kind && $0.id == record.id }) {
                    records[index] = record
                } else {
                    records.append(record)
                }
            } else if let id {
                records.removeAll { $0.kind == codec.kinds.routine && $0.id == id.rawValue }
            }
            try write(records)
            return records
        }
        let catalog = merged(catalog(from: records, source: Self.fileName))
        publish(catalog)
        return catalog
    }

    /// Takes a deleted routine out of the cache at once, before the server
    /// hears of it, and yields the catalogue without it.
    @discardableResult
    public func remove(routine id: RoutineID) throws -> Catalog {
        guard hasCache else { return try loadNow() }
        let records = try fileLock.withLock { () throws -> [FlatRecord] in
            let file = try JSONDecoder().decode(RecordFile.self, from: Data(contentsOf: cacheURL))
            let records = file.records.filter { !($0.kind == codec.kinds.routine && $0.id == id.rawValue) }
            try write(records)
            return records
        }
        let catalog = merged(catalog(from: records, source: Self.fileName))
        publish(catalog)
        return catalog
    }

    /// Writes the sample routine this build carries, and the exercises it
    /// names, into the repository (`SampleInstall`). An id the repository
    /// holds already is left as it is: an exercise the owner or the agent
    /// changed keeps its changes. `rewritingRoutine` writes the routine even
    /// when the server lists it: the sample added back while its purge is
    /// landing. Answers how many records it wrote.
    @discardableResult
    public func copySample(_ api: WorkoutsAPI, from bundled: Catalog, rewritingRoutine: Bool = false) async throws -> Int {
        guard let sample = bundled.routines[SampleRoutine.id] else { return 0 }
        var present = Set(try await api.list([codec.kinds.exercise, codec.kinds.routine]).map { "\($0.kind)/\($0.id)" })
        if rewritingRoutine { present.remove("\(codec.kinds.routine)/\(SampleRoutine.id.rawValue)") }
        let named = Set(sample.slots.map(\.exercise))
        // Exercises first: a routine slot's exercise must exist.
        let exercises = bundled.exercises.values.filter { named.contains($0.id) && !$0.isLocal }.sorted { $0.id.rawValue < $1.id.rawValue }
        var written = 0
        for record in exercises.map(codec.encode) + [codec.encode(sample)] where !present.contains("\(record.kind)/\(record.id)") {
            try await api.write("PUT", record.kind, record.id, properties: record.properties)
            written += 1
        }
        return written
    }

    private func write(_ records: [FlatRecord]) throws {
        try FileManager.default.createDirectory(at: cacheURL.deletingLastPathComponent(), withIntermediateDirectories: true)
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.withoutEscapingSlashes]
        try encoder.encode(RecordFile(format: 1, records: records)).write(to: cacheURL, options: writingOptions)
    }

    private func publish(_ catalog: Catalog) {
        let listeners = lock.withLock { Array(continuations.values) }
        for continuation in listeners {
            continuation.yield(catalog)
        }
    }

    /// Clears the cache: a sign-out, or another repository.
    public func forget() {
        try? FileManager.default.removeItem(at: cacheURL)
    }

    private func catalog(from records: [FlatRecord], source: String) -> Catalog {
        var catalog = Catalog()
        for record in records {
            do {
                switch try codec.decode(record) {
                case let .exercise(exercise): catalog.exercises[exercise.id] = exercise
                case let .routine(routine): catalog.routines[routine.id] = routine
                case let .setup(setup): catalog.setups[setup.id] = setup
                case let .movement(movement): catalog.movements[movement.id] = movement
                case .session, .set: continue
                }
            } catch {
                catalog.unreadable.append(UnreadableRecord(source: source, kind: record.kind, id: record.id, reason: error.description))
            }
        }
        return catalog
    }

    private func merged(_ base: Catalog) -> Catalog {
        var catalog = base
        guard let localExercises else {
            catalog.resolveMovements()
            return catalog
        }
        do {
            let local = try localExercises.load()
            catalog.unreadable += local.unreadable
            for exercise in local.exercises where catalog.exercises[exercise.id] == nil {
                catalog.exercises[exercise.id] = exercise
            }
        } catch {
            catalog.unreadable.append(UnreadableRecord(source: LocalExercises.fileName, reason: String(describing: error)))
        }
        catalog.resolveMovements()
        return catalog
    }
}
