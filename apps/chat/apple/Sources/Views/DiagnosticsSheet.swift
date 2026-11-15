import ChatKit
import SubstrateKit
import SwiftUI

/// Share Diagnostics: the diagnostics file, whole, as it would be shared,
/// and then the system's share sheet for exactly that text. Nothing is
/// shared that is not on this screen first.
struct DiagnosticsSheet: View {
    @Environment(ChatHost.self) private var host
    @Environment(\.dismiss) private var dismiss
    /// The build's version, which the text names under its heading.
    let version: String

    var body: some View {
        let report = host.chats?.diagnostics
        let text = report?.shared(version: version) ?? ""
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: space(.content)) {
                    if let note = report?.note {
                        Text(note).font(.subheadline).foregroundStyle(.quietText)
                    }
                    if let empty = report?.empty {
                        Text(empty).foregroundStyle(.quietText)
                    }
                    // Every line, to read and to select before it goes
                    // anywhere: what is shared is this text.
                    Text(verbatim: text)
                        .font(.caption.monospaced())
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .accessibilityLabel("The diagnostics, \(text.split(separator: "\n").count) lines")
                }
                .padding(space(.content))
            }
            .navigationTitle(report?.title ?? "Diagnostics")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(report?.done ?? "Done") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    ShareLink(item: text) { Text(report?.share ?? "Share") }
                        .disabled(report == nil)
                }
            }
        }
        #if os(macOS)
        .frame(minWidth: DiagnosticsMetrics.width, minHeight: DiagnosticsMetrics.height)
        #endif
        .onAppear { host.send(.diagnostics(shown: true)) }
        .onDisappear { host.send(.diagnostics(shown: false)) }
    }

    private func space(_ token: DesignTokens.Spacing) -> CGFloat { CGFloat(token.rawValue) }
}

/// The room a Mac's sheet takes, which has no size of its own.
private enum DiagnosticsMetrics {
    static let width: CGFloat = 520
    static let height: CGFloat = 420
}
