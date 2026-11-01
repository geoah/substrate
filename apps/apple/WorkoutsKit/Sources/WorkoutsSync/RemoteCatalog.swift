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
    /// and yields the catalogue to every `updates()` stream.
    @discardableResult
    public func refresh(_ api: WorkoutsAPI) async throws -> Catalog {
        let kinds = codec.kinds
        let records = try await api.list([kinds.exercise, kinds.setup, kinds.routine])
        try FileManager.default.createDirectory(at: cacheURL.deletingLastPathComponent(), withIntermediateDirectories: true)
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.withoutEscapingSlashes]
        try encoder.encode(RecordFile(format: 1, records: records)).write(to: cacheURL, options: writingOptions)
        let catalog = merged(catalog(from: records, source: api.client.server.host() ?? "the server"))
        let listeners = lock.withLock { Array(continuations.values) }
        for continuation in listeners {
            continuation.yield(catalog)
        }
        return catalog
    }

    /// Writes the routines this build carries, and the exercises they name,
    /// into the repository: for a repository that has none to train from. An
    /// id the repository holds already is left as it is.
    public func copyStarter(_ api: WorkoutsAPI, from bundled: Catalog) async throws -> Int {
        let present = Set(try await api.list([codec.kinds.exercise, codec.kinds.routine]).map { "\($0.kind)/\($0.id)" })
        var written = 0
        let exercises = bundled.exercises.values.filter { !$0.isLocal }.sorted { $0.id.rawValue < $1.id.rawValue }
        // Exercises first: a routine slot's exercise must exist.
        for record in exercises.map(codec.encode) + bundled.routines.values.sorted { $0.id.rawValue < $1.id.rawValue }.map(codec.encode)
            where !present.contains("\(record.kind)/\(record.id)")
        {
            try await api.write("PUT", record.kind, record.id, properties: record.properties)
            written += 1
        }
        return written
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
        guard let localExercises else { return catalog }
        do {
            let local = try localExercises.load()
            catalog.unreadable += local.unreadable
            for exercise in local.exercises where catalog.exercises[exercise.id] == nil {
                catalog.exercises[exercise.id] = exercise
            }
        } catch {
            catalog.unreadable.append(UnreadableRecord(source: LocalExercises.fileName, reason: String(describing: error)))
        }
        return catalog
    }
}
