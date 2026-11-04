#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDeviceApple

/// One record, read-only: its heading, each property, then what identifies
/// it.
public struct RecordDetailView: View {
    @Environment(\.recordDirectory) private var directory
    @Environment(\.recordTitles) private var titles
    let reference: RecordReference
    /// The name it was opened by, until the record's own is read.
    let title: String?
    @State private var record: Record?
    @State private var declaration: KindDeclaration?
    @State private var problem: Problem?

    enum Problem: Equatable {
        case notAllowed, missing
        case failed(String)
    }

    public init(reference: RecordReference, title: String? = nil) {
        self.reference = reference
        self.title = title
    }

    private var heading: String? { record?.title ?? title ?? titles[reference.path] ?? directory?.title(for: reference) }

    private var kindName: String { declaration?.displayName ?? kindDisplayName(reference.kind) }

    /// "Untitled person"; a slug id names it instead.
    private var untitled: String {
        let name = fallbackName(reference)
        return name == kindDisplayName(reference.kind) ? "Untitled \(kindName.lowercasedFirst)" : name
    }

    public var body: some View {
        List {
            Section {
                HStack(alignment: .center, spacing: 12) {
                    KindGlyph(kind: reference.kind, size: 40)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(heading ?? untitled)
                            .font(.title3.weight(.semibold))
                            .textSelection(.enabled)
                        Text(kindName)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
                .padding(.vertical, 4)
            }
            if let record {
                let rows = properties(record)
                if !rows.isEmpty {
                    Section {
                        ForEach(rows, id: \.self) { key in
                            VStack(alignment: .leading, spacing: 4) {
                                Text(propertyLabel(key))
                                    .font(.caption.weight(.semibold))
                                    .foregroundStyle(.secondary)
                                RecordValueView(record[key] ?? .null, declared: declaration?.properties[key])
                            }
                            .padding(.vertical, 2)
                        }
                    }
                }
            } else if let problem {
                Section { problemView(problem) }
            } else {
                Section { ProgressView().frame(maxWidth: .infinity) }
            }
            Section {
                LabeledContent("Kind") {
                    Text(reference.kind).font(.caption.monospaced()).textSelection(.enabled)
                }
                LabeledContent("ID") {
                    Text(reference.recordID).font(.caption.monospaced()).textSelection(.enabled)
                }
                if let record, let updated = Timestamp.parse(record.updatedAt) {
                    LabeledContent("Changed", value: updated.formatted(date: .abbreviated, time: .shortened))
                }
                if let url = directory?.reader?.webURL(for: reference) {
                    Link(destination: url) { Label("Open in the Console", systemImage: "arrow.up.right.square") }
                }
            } footer: {
                if let description = declaration?.description { Text(description) }
            }
        }
        #if os(iOS)
        .listStyle(.insetGrouped)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .navigationTitle(kindName)
        .task(id: reference) { await load() }
        .refreshable { await load(again: true) }
    }

    @ViewBuilder
    private func problemView(_ problem: Problem) -> some View {
        switch problem {
        case .notAllowed:
            VStack(alignment: .leading, spacing: 6) {
                Label("This app can't read \(kindDisplayPlural(reference.kind).lowercasedFirst)", systemImage: "lock")
                    .font(.subheadline.weight(.semibold))
                if let hint = directory?.reader?.deniedHint {
                    Text(hint).font(.footnote).foregroundStyle(.secondary)
                }
            }
        case .missing:
            Label("This \(kindName.lowercasedFirst) is gone: deleted, or never written.", systemImage: "questionmark.circle")
                .font(.subheadline)
        case let .failed(message):
            Label(message, systemImage: "exclamationmark.triangle").font(.subheadline)
        }
    }

    private func load(again: Bool = false) async {
        guard let directory else { return }
        do {
            let read = again ? try await directory.reload(reference) : try await directory.record(reference)
            if let read {
                record = read
                problem = nil
            } else {
                problem = .missing
            }
        } catch RecordReadError.notAllowed {
            problem = .notAllowed
        } catch is CancellationError {
            return
        } catch let error as URLError where error.code == .cancelled {
            return
        } catch {
            problem = .failed(error.localizedDescription)
        }
        declaration = await directory.declaration(of: reference.kind)
    }

    /// The properties worth a row, short ones first and prose last; the
    /// heading's own property and empty values are left out.
    private func properties(_ record: Record) -> [String] {
        let heading = record.title
        return record.properties.keys.filter { key in
            guard let value = record[key], ValueShape(value) != .empty else { return false }
            if key == "title" { return false }
            return !(value.text != nil && value.text == heading && ["name", "summary", "subject"].contains(key))
        }
        .sorted { a, b in
            let (ra, rb) = (rank(record[a]), rank(record[b]))
            return ra == rb ? a < b : ra < rb
        }
    }

    private func rank(_ value: JSONValue?) -> Int {
        switch ValueShape(value ?? .null) {
        case .longText: 3
        case .references: 2
        case .raw: 4
        default: 1
        }
    }
}
#endif
