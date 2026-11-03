import Foundation
import SubstrateCore

/// `routine-draft.json`: the routine edit in progress, kept on every change
/// so an edit survives the app being killed before Save. One draft at a
/// time, the routine as it stood and as edited, both as flat records, so
/// the codec that round-trips the server's records keeps the draft too. A
/// format 1 draft, from before the package was published, is read through
/// `LegacyKinds` and written back once at this format.
public struct RoutineDrafts: Sendable {
    public static let fileName = "routine-draft.json"

    struct File: Codable {
        var format = WorkoutsFormats.draftFormat
        var isNew: Bool
        var base: FlatRecord
        var routine: FlatRecord

        /// The draft in the published spelling, or nil when it is at the
        /// current format and has nothing to respell.
        var respelled: File? {
            guard format < WorkoutsFormats.draftFormat else { return nil }
            return File(isNew: isNew, base: LegacyKinds.respell(base), routine: LegacyKinds.respell(routine))
        }
    }

    public let url: URL
    public let codec: RecordCodec
    public let writingOptions: Data.WritingOptions

    /// - Parameter directory: the folder of one server and repository.
    public init(directory: URL, codec: RecordCodec = RecordCodec(), writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting) {
        url = directory.appendingPathComponent(Self.fileName, isDirectory: false)
        self.codec = codec
        self.writingOptions = writingOptions
    }

    /// The draft kept, or nil when there is none. A file that does not read
    /// is set aside and reported.
    public func load(now: Date = Date()) throws -> RoutineEditor? {
        guard let data = try WorkoutsFiles.read(url) else { return nil }
        do {
            let stored = try JSONDecoder().decode(File.self, from: data)
            let file = stored.respelled ?? stored
            var editor = try RoutineEditor(codec.decodeRoutine(file.base))
            editor.routine = try codec.decodeRoutine(file.routine)
            editor.isNew = file.isNew
            // Written back once in the published spelling, best effort.
            if stored.respelled != nil { try? save(editor) }
            return editor
        } catch {
            let aside = try? WorkoutsFiles.setAside(url, at: now)
            throw UnreadableDraft(record: UnreadableRecord(source: aside?.lastPathComponent ?? Self.fileName,
                                                           reason: "A routine edit that was not saved could not be read: \(error)"))
        }
    }

    public func save(_ editor: RoutineEditor) throws {
        let file = File(isNew: editor.isNew, base: codec.encode(editor.base), routine: codec.encode(editor.routine))
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.withoutEscapingSlashes, .sortedKeys]
        try WorkoutsFiles.write(encoder.encode(file), to: url, options: writingOptions)
    }

    public func clear() throws {
        try WorkoutsFiles.remove(url)
    }
}

/// The draft file was there and could not be read; it was set aside.
public struct UnreadableDraft: Error, Sendable {
    public let record: UnreadableRecord
}
