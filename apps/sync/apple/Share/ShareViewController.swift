import SubstrateKit
import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// "Send to Substrate" in every app's share sheet. It reads the app's
/// sign-in from the Keychain group the two share, either kind, and saves
/// what was shared as one `share` record: the link, the text and the images,
/// with a note. An app session's token writes `apple/share`, whose images
/// are a `blobref`, so the server stores its blobs as it does the owner's.
final class ShareViewController: UIViewController {
    override func viewDidLoad() {
        super.viewDidLoad()
        let items = extensionContext?.inputItems.compactMap { $0 as? NSExtensionItem } ?? []
        let model = ShareModel(items: items, done: { [weak self] in
            self?.extensionContext?.completeRequest(returningItems: nil)
        }, cancel: { [weak self] in
            self?.extensionContext?.cancelRequest(withError: CocoaError(.userCancelled))
        })
        let host = UIHostingController(rootView: ShareView(model: model))
        addChild(host)
        host.view.frame = view.bounds
        host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(host.view)
        host.didMove(toParent: self)
        Task { await model.load() }
    }
}

/// What the sheet shows and saves.
@MainActor
final class ShareModel: ObservableObject {
    static let kind = "providers.substrate.reamde.dev/apple/share"

    struct Image: Identifiable, Sendable {
        let id = UUID()
        let data: Data
        let mediaType: String
        let name: String?
        let preview: UIImage?
    }

    @Published var url: URL?
    @Published var title: String?
    @Published var text: String?
    @Published var images: [Image] = []
    @Published var note = ""
    /// What the sheet was given and cannot send: images past four, and
    /// attachments that could not be read or are not a link, text or image.
    @Published var leftOut: [String] = []
    @Published var loading = true
    @Published var saving = false
    /// Cancel was pressed during a save, which is undoing what it wrote.
    @Published var stopping = false
    @Published var error: String?

    let session = SessionStore.sync.load()
    private let items: [NSExtensionItem]
    private let done: () -> Void
    private let close: () -> Void
    private var saving_: Task<Void, Never>?
    /// The share's id when it has no link: one per sheet, so a Save tried
    /// again after an error updates the record the first try wrote.
    private let draft = UUID().uuidString.lowercased()
    /// A reader that never answers would hold the sheet on its spinner.
    static let readTimeout: TimeInterval = 20

    init(items: [NSExtensionItem], done: @escaping () -> Void, cancel: @escaping () -> Void) {
        self.items = items
        self.done = done
        self.close = cancel
    }

    var isEmpty: Bool { url == nil && (text ?? "").isEmpty && images.isEmpty }

    /// Reads every attachment: a web link, an image (from a file too), and
    /// text. A file that is not an image is left out: its path means nothing
    /// off this phone.
    func load() async {
        var extraImages = 0
        var unreadable = 0
        var unsupported = 0
        defer {
            leftOut = [
                extraImages > 0 ? "\(extraImages) more \(extraImages == 1 ? "image" : "images") (four at most)" : nil,
                unreadable > 0 ? "\(unreadable) \(unreadable == 1 ? "item" : "items") that could not be read" : nil,
                unsupported > 0 ? "\(unsupported) \(unsupported == 1 ? "item" : "items") that is not a link, text or image" : nil,
            ].compactMap { $0 }
            loading = false
        }
        for item in items {
            if title == nil, let t = item.attributedTitle?.string, !t.isEmpty { title = t }
            if let t = item.attributedContentText?.string, !t.isEmpty, text == nil { text = t }
            for attachment in item.attachments ?? [] {
                // An item provider loads on any thread; it is only not
                // marked so.
                nonisolated(unsafe) let provider = attachment
                if provider.hasItemConformingToTypeIdentifier(UTType.image.identifier) {
                    if images.count >= 4 {
                        extraImages += 1
                    } else if let image = try? await withTimeout(Self.readTimeout, { await Self.image(provider) }) {
                        images.append(image)
                    } else {
                        unreadable += 1
                    }
                } else if provider.hasItemConformingToTypeIdentifier(UTType.url.identifier) {
                    let u = try? await withTimeout(Self.readTimeout) {
                        try await provider.loadItem(forTypeIdentifier: UTType.url.identifier) as? URL
                    }
                    if let u, !u.isFileURL {
                        if url == nil { url = u } else { unsupported += 1 }
                    } else {
                        unsupported += 1
                    }
                } else if provider.hasItemConformingToTypeIdentifier(UTType.plainText.identifier) {
                    if let t = try? await withTimeout(Self.readTimeout, {
                        try await provider.loadItem(forTypeIdentifier: UTType.plainText.identifier) as? String
                    }), !t.isEmpty {
                        text = text.map { $0 == t ? $0 : $0 + "\n\n" + t } ?? t
                    } else {
                        unreadable += 1
                    }
                } else {
                    unsupported += 1
                }
            }
        }
        // An app that shares a page often sends its link as the text too.
        if let u = url?.absoluteString, text == u { text = nil }
    }

    private nonisolated static func image(_ provider: NSItemProvider) async -> Image? {
        let type = provider.registeredContentTypes.first { $0.conforms(to: .image) } ?? .image
        let data: Data? = await withCheckedContinuation { continuation in
            _ = provider.loadDataRepresentation(for: type) { data, _ in continuation.resume(returning: data) }
        }
        guard let data else { return nil }
        return Image(
            data: data, mediaType: type.preferredMIMEType ?? "application/octet-stream",
            name: provider.suggestedName.map { name in type.preferredFilenameExtension.map { "\(name).\($0)" } ?? name },
            preview: UIImage(data: data))
    }

    /// Saves in the background, so Cancel can stop it.
    func startSave() {
        guard !saving else { return }
        saving = true
        saving_ = Task { await save() }
    }

    /// Closes the sheet. A save under way is stopped first, and a record it
    /// already wrote for a share without a link is deleted again: Cancel
    /// leaves nothing behind.
    func cancel() {
        guard let task = saving_, saving else { return close() }
        stopping = true
        task.cancel()
        Task {
            await task.value
            close()
        }
    }

    /// Uploads the images one at a time, and writes the record again
    /// whenever the oldest image it does not name yet nears the minute the
    /// server keeps an unreferenced blob: over a slow uplink four photos
    /// outlast it. A link's id is the link, so sharing it again updates the
    /// row and its note.
    private func save() async {
        guard let session else { return }
        error = nil
        defer {
            saving = false
            saving_ = nil
        }
        let client = Client(session)
        let id = ExternalID.make("apple", "share", url?.absoluteString ?? draft)
        var wrote = false
        do {
            var props: [String: Any] = ["sharedAt": Formats.utc(Date()), "deviceName": UIDevice.current.name]
            if let url { props["url"] = url.absoluteString }
            if let title, !title.isEmpty { props["shareTitle"] = title }
            if let text, !text.isEmpty { props["text"] = text }
            let note = note.trimmingCharacters(in: .whitespacesAndNewlines)
            if !note.isEmpty { props["note"] = note }
            var digests: [String] = []
            var oldestUnnamed: Date?
            for image in images {
                try Task.checkCancellation()
                digests.append(try await client.putBlob(image.data, mediaType: image.mediaType, name: image.name))
                oldestUnnamed = oldestUnnamed ?? Date()
                if let oldest = oldestUnnamed, Date().timeIntervalSince(oldest) > 40 {
                    props["images"] = digests
                    try await client.put(Self.kind, id, props)
                    wrote = true
                    oldestUnnamed = nil
                }
            }
            if !digests.isEmpty { props["images"] = digests }
            try Task.checkCancellation()
            try await client.put(Self.kind, id, props)
            done()
        } catch where stopping || error is CancellationError || (error as? URLError)?.code == .cancelled {
            // Undone outside the cancelled task, whose requests would fail
            // at once; a link's row may be an earlier share, and stays.
            if wrote, url == nil {
                await Task.detached { try? await client.delete(Self.kind, id, retries: 0, timeout: 5) }.value
            }
        } catch {
            self.error = explain(error, server: session.server)
        }
    }
}

struct ShareView: View {
    @ObservedObject var model: ShareModel

    var body: some View {
        NavigationStack {
            Form {
                if model.session == nil {
                    Section {
                        ProblemBox(title: "Not signed in", message: "Open Substrate Sync and sign in first; this sheet uses its sign-in.")
                    }
                } else if model.loading {
                    HStack(spacing: 10) { ProgressView(); Text("Reading what was shared…").foregroundStyle(.secondary) }
                } else if model.isEmpty {
                    Section {
                        ProblemBox(title: "Nothing to send", message: "This app shared nothing Substrate keeps: a link, text or an image.")
                    }
                } else {
                    Section("Sending") {
                        if let title = model.title { LabeledContent("Title", value: title) }
                        if let url = model.url {
                            LabeledContent("Link") { Text(url.absoluteString).lineLimit(3).multilineTextAlignment(.trailing) }
                        }
                        if let text = model.text { Text(text).font(.callout).lineLimit(6) }
                        if !model.images.isEmpty {
                            ScrollView(.horizontal) {
                                HStack {
                                    ForEach(model.images) { image in
                                        if let preview = image.preview {
                                            Image(uiImage: preview).resizable().scaledToFill()
                                                .frame(width: 88, height: 88).clipShape(RoundedRectangle(cornerRadius: 8))
                                        }
                                    }
                                }
                            }
                        }
                    }
                    if !model.leftOut.isEmpty {
                        Section {
                            Label("Not sent: " + model.leftOut.formatted(.list(type: .and)) + ".", systemImage: "info.circle")
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        }
                    }
                    Section("Note") {
                        TextField("Why you saved it (optional)", text: $model.note, axis: .vertical)
                            .lineLimit(3 ... 8)
                            .disabled(model.saving)
                    }
                    Section {
                        LabeledContent("Repository", value: model.session?.repository ?? "")
                    }
                }
                if let error = model.error {
                    Section { ProblemBox(title: "Not saved", message: error) }
                }
            }
            .navigationTitle("Send to Substrate")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                // Cancel stops a save under way: the record is written last,
                // and only if the save was not cancelled first.
                ToolbarItem(placement: .cancellationAction) {
                    Button(model.stopping ? "Stopping…" : "Cancel") { model.cancel() }
                        .disabled(model.stopping)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button {
                        model.startSave()
                    } label: {
                        ZStack {
                            Text("Save").opacity(model.saving ? 0 : 1)
                            if model.saving { ProgressView() }
                        }
                    }
                    .disabled(model.session == nil || model.loading || model.isEmpty || model.saving)
                    .accessibilityLabel(model.saving ? "Saving" : "Save")
                }
            }
        }
    }
}
