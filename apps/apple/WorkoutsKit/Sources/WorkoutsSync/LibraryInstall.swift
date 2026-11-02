import Foundation
import SubstrateKit
import WorkoutsCore

/// The exercise library seeded into one server and repository: its
/// movements and their exercises, written into the training package as
/// ordinary records the coach and the console read.
///
/// THE SERVER IS THE MARKER. Every library record carries the library's code
/// with the version it first appeared in, so the repository's library
/// version is the highest such code it holds. At a newer build's version
/// the app writes only the records that appeared after it, so a record the
/// owner deleted is never written back, on this device or another; and on a
/// record the repository already holds, the owner's, the coach's or an
/// earlier library's, it fills the properties the record lacks and never
/// changes one it has. `library.json` in the repository's folder saves the
/// list once a version is settled on this device and keeps the starting
/// version of a run cut short, which the next sync finishes.
public final class LibraryInstall: @unchecked Sendable {
    public enum State: String, Codable, Sendable {
        case installing, installed
    }

    struct File: Codable {
        var format = 1
        var version: Int
        var state: State
        /// The repository's library version when this run began.
        var from: Int
    }

    /// What a run did.
    public struct Outcome: Sendable, Equatable {
        public var written = 0
        public var filled = 0
        public var current: Bool { written == 0 && filled == 0 }
    }

    public static let fileName = "library.json"

    public let url: URL
    private let writingOptions: Data.WritingOptions
    // Guards `file`; `@unchecked Sendable` rests on it.
    private let lock = NSLock()
    private var file: File?

    public init(directory: URL, writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting) {
        url = directory.appendingPathComponent(Self.fileName)
        self.writingOptions = writingOptions
        guard let data = try? Data(contentsOf: url) else { return }
        file = try? JSONDecoder().decode(File.self, from: data)
    }

    /// The library version settled here, nil until one is.
    public var installedVersion: Int? { lock.withLock { file?.state == .installed ? file?.version : nil } }

    /// Seeds `library` where the repository is behind it. A record the
    /// server refuses stops the run with the server's words; the next sync
    /// goes on from where this one began.
    @discardableResult
    public func ensure(_ api: WorkoutsAPI, library: ExerciseLibrary) async throws -> Outcome {
        if installedVersion ?? 0 >= library.version { return Outcome() }
        let codec = RecordCodec()
        let listed = try await api.list([codec.kinds.movement, codec.kinds.exercise])
        let held = Dictionary(listed.map { ("\($0.kind)/\($0.id)", $0) }, uniquingKeysWith: { first, _ in first })
        var from = listed.compactMap { ExerciseLibrary.since($0.properties) }.max() ?? 0
        let saved = lock.withLock { file }
        if let saved, saved.state == .installing, saved.version == library.version { from = saved.from }
        try record(File(version: library.version, state: .installing, from: from))

        var outcome = Outcome()
        for record in library.records {
            let key = "\(record.kind)/\(record.id)"
            if let existing = held[key] {
                let patch = Self.fill(existing.properties, from: record.properties)
                guard !patch.isEmpty else { continue }
                do {
                    try await api.write("PATCH", record.kind, record.id, properties: patch, ifVersion: existing.version)
                    outcome.filled += 1
                } catch let error as ServerError where error.status == 409 {
                    // Written since the list: the next run fills it from what it holds then.
                    continue
                }
            } else if (ExerciseLibrary.since(record.properties) ?? library.version) > from {
                try await api.write("PUT", record.kind, record.id, properties: record.properties)
                outcome.written += 1
            }
        }
        try self.record(File(version: library.version, state: .installed, from: from))
        return outcome
    }

    /// The properties a held record lacks, from the library's record; its
    /// codes gain the library's that it lacks. Nothing it holds changes.
    static func fill(_ held: JSONObject, from library: JSONObject) -> JSONObject {
        var patch = JSONObject()
        for (key, value) in library where key != "codes" && held[key] == nil {
            patch[key] = value
        }
        guard case let .array(theirs)? = library["codes"] else { return patch }
        var codes: [WorkoutsCore.JSONValue] = []
        if case let .array(mine)? = held["codes"] { codes = mine }
        func identity(_ value: WorkoutsCore.JSONValue) -> String? {
            guard case let .object(coding) = value, case let .string(system)? = coding["system"], case let .string(code)? = coding["code"]
            else { return nil }
            return "\(system)\u{1F}\(code)"
        }
        let present = Set(codes.compactMap(identity))
        let missing = theirs.filter { identity($0).map { !present.contains($0) } ?? false }
        if !missing.isEmpty { patch["codes"] = .array(codes + missing) }
        return patch
    }

    private func record(_ value: File) throws {
        try lock.withLock {
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.withoutEscapingSlashes, .sortedKeys]
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            try encoder.encode(value).write(to: url, options: writingOptions)
            file = value
        }
    }
}
