#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDeviceApple

/// A sheet over one record whose linked records push within it.
public struct RecordSheet: View {
    @Environment(\.dismiss) private var dismiss
    let root: OpenedRecord
    @State private var path: [OpenedRecord] = []

    public init(root: OpenedRecord) {
        self.root = root
    }

    public var body: some View {
        NavigationStack(path: $path) {
            RecordDetailView(reference: root.reference, title: root.title)
                .navigationDestination(for: OpenedRecord.self) { RecordDetailView(reference: $0.reference, title: $0.title) }
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
        }
        .environment(\.openRecord, OpenRecordAction { path.append($0) })
        #if os(macOS)
        .frame(minWidth: 420, minHeight: 480)
        #endif
        .presentationDetents([.medium, .large])
    }
}
#endif
