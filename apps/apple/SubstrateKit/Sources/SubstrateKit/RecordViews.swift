import SwiftUI

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

/// A linked record as a pill: its kind's glyph and its name. A tap opens it
/// read-only where a `recordViewer` is above; elsewhere it only names it.
public struct RecordPill: View {
    @Environment(\.recordDirectory) private var directory
    @Environment(\.recordTitles) private var titles
    @Environment(\.openRecord) private var open
    private let reference: RecordReference
    private let title: String?

    public init(_ reference: RecordReference, title: String? = nil) {
        self.reference = reference
        self.title = title
    }

    private var known: String? { title ?? titles[reference.path] ?? directory?.title(for: reference) }

    public var body: some View {
        let name = known ?? fallbackName(reference)
        let label = HStack(spacing: 5) {
            KindGlyph(kind: reference.kind, size: 16)
            Text(name)
                .lineLimit(1)
                .truncationMode(.tail)
        }
        .padding(.leading, 3)
        .padding(.trailing, 8)
        .padding(.vertical, 3)
        .background(.fill.tertiary, in: .capsule)
        .contentShape(.capsule)
        Group {
            if let open {
                Button { open(reference, title: known) } label: { label }
                    .buttonStyle(.plain)
                    .accessibilityHint("Opens it")
            } else {
                label
            }
        }
        .accessibilityLabel("\(kindDisplayName(reference.kind)): \(name)")
        .task(id: reference) {
            if known == nil { await directory?.resolve(reference) }
        }
    }
}

/// What a record is called before it is read: a slug id names it, a minted
/// one says only what kind of thing it is.
public func fallbackName(_ reference: RecordReference) -> String {
    let id = reference.recordID
    let minted = id.count == 12 && id.allSatisfy { ("a" ... "z").contains($0) || ("2" ... "7").contains($0) }
    return minted ? kindDisplayName(reference.kind) : id
}

/// One property's value, drawn by its shape: a linked record as a pill,
/// several as wrapping pills, a date in the reader's calendar, a link, prose.
public struct RecordValueView: View {
    let shape: ValueShape
    var struck = false

    public init(_ value: JSONValue, declared: PropertyDeclaration? = nil, struck: Bool = false) {
        shape = ValueShape(value, declared: declared)
        self.struck = struck
    }

    public init(shape: ValueShape, struck: Bool = false) {
        self.shape = shape
        self.struck = struck
    }

    public var body: some View {
        switch shape {
        case .empty:
            Text("Empty").foregroundStyle(.secondary)
        case let .reference(ref):
            RecordPill(ref).opacity(struck ? 0.5 : 1)
        case let .references(refs):
            WrapLayout(spacing: 6) {
                ForEach(refs, id: \.self) { RecordPill($0) }
            }
            .opacity(struck ? 0.5 : 1)
        case let .words(s):
            styled(Text(s))
        case let .text(s), let .longText(s):
            // Prose may carry inline Markdown; plain text reads the same.
            styled(Text(markdown(s)))
        case let .link(url):
            Link(url.absoluteString, destination: url).lineLimit(2)
        case let .date(date, time):
            styled(Text(time ? date.formatted(date: .abbreviated, time: .shortened) : date.formatted(date: .abbreviated, time: .omitted)))
        case let .raw(s):
            styled(Text(s).font(.caption.monospaced())).lineLimit(8)
        }
    }

    private func styled(_ text: Text) -> some View {
        text
            .strikethrough(struck)
            .foregroundStyle(struck ? .secondary : .primary)
            .textSelection(.enabled)
    }

    private func markdown(_ s: String) -> AttributedString {
        (try? AttributedString(markdown: s, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(s)
    }
}

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
        case notAllowed, missing, failed(String)
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

/// Lays its children out in rows, wrapping where the width runs out.
public struct WrapLayout: Layout {
    var spacing: CGFloat

    public init(spacing: CGFloat = 6) {
        self.spacing = spacing
    }

    public func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? .infinity
        var x: CGFloat = 0, y: CGFloat = 0, row: CGFloat = 0, widest: CGFloat = 0
        for view in subviews {
            let size = view.sizeThatFits(.unspecified)
            if x > 0, x + size.width > width {
                y += row + spacing
                x = 0
                row = 0
            }
            x += size.width + spacing
            row = max(row, size.height)
            widest = max(widest, x - spacing)
        }
        return CGSize(width: min(widest, width), height: y + row)
    }

    public func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, row: CGFloat = 0
        for view in subviews {
            let size = view.sizeThatFits(.unspecified)
            if x > bounds.minX, x + size.width > bounds.maxX {
                y += row + spacing
                x = bounds.minX
                row = 0
            }
            view.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(size))
            x += size.width + spacing
            row = max(row, size.height)
        }
    }
}
