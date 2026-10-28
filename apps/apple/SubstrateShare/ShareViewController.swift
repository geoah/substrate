import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// "Send to Substrate" in every app's share sheet. It reads the app's
/// sign-in from the Keychain group the two share, and saves what was shared
/// as one `share` record: the link, the text and the images, with a note.
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

    struct Image: Identifiable {
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
    @Published var error: String?

    let session = Keychain.load()
    private let items: [NSExtensionItem]
    private let done: () -> Void
    let cancel: () -> Void

    init(items: [NSExtensionItem], done: @escaping () -> Void, cancel: @escaping () -> Void) {
        self.items = items
        self.done = done
        self.cancel = cancel
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
            for provider in item.attachments ?? [] {
                if provider.hasItemConformingToTypeIdentifier(UTType.image.identifier) {
                    if images.count >= 4 {
                        extraImages += 1
                    } else if let image = await Self.image(provider) {
                        images.append(image)
                    } else {
                        unreadable += 1
                    }
                } else if provider.hasItemConformingToTypeIdentifier(UTType.url.identifier) {
                    let u = try? await provider.loadItem(forTypeIdentifier: UTType.url.identifier) as? URL
                    if let u, !u.isFileURL {
                        if url == nil { url = u } else { unsupported += 1 }
                    } else {
                        unsupported += 1
                    }
                } else if provider.hasItemConformingToTypeIdentifier(UTType.plainText.identifier) {
                    if let t = try? await provider.loadItem(forTypeIdentifier: UTType.plainText.identifier) as? String, !t.isEmpty {
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

    private static func image(_ provider: NSItemProvider) async -> Image? {
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

    /// Uploads the images, then writes the record within the minute the
    /// server keeps an unreferenced blob. A link's id is the link, so sharing
    /// it again updates the row and its note.
    func save() async {
        guard let session else { return }
        saving = true
        error = nil
        defer { saving = false }
        let client = Client(server: session.server, secret: session.secret)
        do {
            var props: [String: Any] = ["sharedAt": Formats.utc(Date()), "deviceName": UIDevice.current.name]
            if let url { props["url"] = url.absoluteString }
            if let title, !title.isEmpty { props["shareTitle"] = title }
            if let text, !text.isEmpty { props["text"] = text }
            let note = note.trimmingCharacters(in: .whitespacesAndNewlines)
            if !note.isEmpty { props["note"] = note }
            var digests: [String] = []
            for image in images {
                digests.append(try await client.putBlob(image.data, mediaType: image.mediaType, name: image.name))
            }
            if !digests.isEmpty { props["images"] = digests }
            let id = ExternalID.make("apple", "share", url?.absoluteString ?? UUID().uuidString.lowercased())
            try await client.put(Self.kind, id, props)
            done()
        } catch {
            self.error = error.localizedDescription
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
                        ErrorBox(title: "Not signed in", message: "Open Substrate Sync and sign in first; this sheet uses its sign-in.")
                    }
                } else if model.loading {
                    HStack(spacing: 10) { ProgressView(); Text("Reading what was shared…").foregroundStyle(.secondary) }
                } else if model.isEmpty {
                    Section {
                        ErrorBox(title: "Nothing to send", message: "This app shared nothing Substrate keeps: a link, text or an image.")
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
                    }
                    Section {
                        LabeledContent("Repository", value: model.session?.repository ?? "")
                    }
                }
                if let error = model.error {
                    Section { ErrorBox(title: "Not saved", message: error) }
                }
            }
            .interactiveDismissDisabled(model.saving)
            .navigationTitle("Send to Substrate")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                // Cancel waits while a save is under way: closing the sheet
                // then would not stop what the server already took.
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", action: model.cancel)
                        .disabled(model.saving)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button {
                        Task { await model.save() }
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
