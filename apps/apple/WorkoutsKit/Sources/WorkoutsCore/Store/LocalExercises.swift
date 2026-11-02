import Foundation

/// `local-exercises.json`: exercises the owner added on the phone, as flat
/// exercise records (`{"format": 1, "records": [...]}`), written on add
/// (§2.3). V2 writes each to substrate before anything names it.
public struct LocalExercises: Sendable {
    public static let fileName = "local-exercises.json"

    public let url: URL
    public let codec: RecordCodec
    public let writingOptions: Data.WritingOptions

    /// - Parameter directory: the app's directory.
    public init(directory: URL, codec: RecordCodec = RecordCodec(), writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting) {
        url = directory.appendingPathComponent(Self.fileName, isDirectory: false)
        self.codec = codec
        self.writingOptions = writingOptions
    }

    /// The exercises, each marked `isLocal`, and the records that did not
    /// decode. Empty when there is no file.
    public func load() throws -> LocalExercisesLoad {
        var out = LocalExercisesLoad(exercises: [], unreadable: [])
        for record in try records() {
            do {
                var exercise = try codec.decodeExercise(record)
                exercise.isLocal = true
                out.exercises.append(exercise)
            } catch {
                out.unreadable.append(UnreadableRecord(source: Self.fileName, kind: record.kind, id: record.id, reason: error.description))
            }
        }
        return out
    }

    /// Adds the exercise, or replaces the one with its id. Records this build
    /// cannot read are kept as they are.
    public func add(_ exercise: Exercise) throws {
        var records = try records()
        let record = codec.encode(exercise)
        if let index = records.firstIndex(where: { $0.kind == record.kind && $0.id == record.id }) {
            records[index] = record
        } else {
            records.append(record)
        }
        let file = RecordFile(format: 1, records: records)
        try WorkoutsFiles.write(file.json.data(), to: url, options: writingOptions)
    }

    private func records() throws -> [FlatRecord] {
        guard let data = try WorkoutsFiles.read(url) else { return [] }
        return try JSONDecoder().decode(RecordFile.self, from: data).records
    }
}

public struct LocalExercisesLoad: Sendable {
    public var exercises: [Exercise]
    public var unreadable: [UnreadableRecord]
}
