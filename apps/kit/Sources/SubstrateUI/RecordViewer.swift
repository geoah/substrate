#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDeviceApple

public extension EnvironmentValues {
    /// The records the views read, shared by a scene (`recordViewer`).
    @Entry var recordDirectory: RecordDirectory?
    /// Names the surrounding view already knows, by `kind/id`: what a chat's
    /// own lookups returned, say. They win over a read, which they spare.
    @Entry var recordTitles: [String: String] = [:]
    /// Opens a record read-only; nil where nothing can (`recordViewer`).
    @Entry var openRecord: OpenRecordAction?
}

public struct OpenRecordAction {
    let handler: @MainActor (OpenedRecord) -> Void

    public init(_ handler: @escaping @MainActor (OpenedRecord) -> Void) {
        self.handler = handler
    }

    /// Opens the record; `title` is the name the opener showed, which the
    /// view keeps until it reads the record's own.
    @MainActor public func callAsFunction(_ reference: RecordReference, title: String? = nil) {
        handler(OpenedRecord(reference: reference, title: title))
    }
}

/// A record being shown, with the name it was opened by.
public struct OpenedRecord: Hashable, Identifiable, Sendable {
    public var reference: RecordReference
    public var title: String?
    public var id: String { reference.path }
}

public extension View {
    /// Lets every `RecordPill` below open its record, read-only, in a sheet
    /// whose own pills push within it.
    func recordViewer(_ directory: RecordDirectory) -> some View {
        modifier(RecordViewer(directory: directory))
    }
}

private struct RecordViewer: ViewModifier {
    let directory: RecordDirectory
    @State private var shown: OpenedRecord?

    func body(content: Content) -> some View {
        content
            .environment(\.recordDirectory, directory)
            .environment(\.openRecord, OpenRecordAction { shown = $0 })
            .sheet(item: $shown) { opened in
                RecordSheet(root: opened)
                    .environment(\.recordDirectory, directory)
            }
    }
}
#endif
