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
            DiagnosticsPage(report: report, text: text)
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
        .frame(minWidth: ChatSize.readingSheet.least.width, minHeight: ChatSize.readingSheet.least.height)
        #endif
        .onAppear { host.send(.diagnostics(shown: true)) }
        .onDisappear { host.send(.diagnostics(shown: false)) }
    }
}

/// What the sheet shows: what the file holds and never holds, then every
/// line of it, to read and to select before it goes anywhere. What is
/// shared is this text.
struct DiagnosticsPage: View {
    let report: DiagnosticsReport?
    let text: String

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: ChatSpace.edge) {
                if let note = report?.note {
                    Text(note).font(ChatType.detail).ink(.quiet, on: .page)
                }
                if let push = report?.push {
                    Text(push).font(ChatType.detail).ink(.quiet, on: .page)
                }
                // A development build's push token, for the relay's operator:
                // shown here alone, never in what is shared.
                if let token = report?.pushToken {
                    VStack(alignment: .leading, spacing: ChatSpace.pair) {
                        if let note = report?.pushTokenNote {
                            Text(note).font(ChatType.detail).ink(.quiet, on: .page)
                        }
                        Text(verbatim: token)
                            .font(ChatType.verbatim)
                            .textSelection(.enabled)
                            .accessibilityLabel("Push token")
                    }
                }
                if let empty = report?.empty {
                    Text(empty).ink(.quiet, on: .page)
                }
                Text(verbatim: text)
                    .font(ChatType.verbatim)
                    .textSelection(.enabled)
                    .accessibilityLabel("The diagnostics, \(text.split(separator: "\n").count) lines")
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(ChatSpace.edge)
        }
    }
}
