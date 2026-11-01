import SubstrateKit
import SwiftUI
import WorkoutsSync

/// Settings → Substrate: signed out, the way in; signed in, where the app
/// stands (the package, what waits), the account and its two ways out.
struct SubstrateSection: View {
    @Environment(SubstrateLink.self) private var link
    @State private var signingIn = false
    @State private var confirmingUpgrade = false

    var body: some View {
        Group { content }
            .signInSheet(isPresented: $signingIn)
    }

    @ViewBuilder
    private var content: some View {
        if let session = link.session {
            Section {
                Label {
                    Text(link.syncStatus.sentence)
                        .fixedSize(horizontal: false, vertical: true)
                } icon: {
                    statusIcon
                }
                LabeledContent("Training package", value: packageText)
                if link.waiting > 0 || link.syncing {
                    Button {
                        Task { await link.sync() }
                    } label: {
                        HStack {
                            Text("Send Now")
                            if link.syncing { Spacer(); ProgressView() }
                        }
                    }
                    .disabled(link.syncing)
                }
                if case .needsApproval = link.syncStatus {
                    Button("Review Access…") { Task { await link.reauthorize() } }
                }
                if case .signedOut = link.syncStatus {
                    Button("Sign In Again…") { signingIn = true }
                }
                if case .accessEndsSoon = link.syncStatus {
                    Button("Renew Access…") { Task { await link.reauthorize() } }
                }
                if case .packageEdited = link.syncStatus {
                    Button("Update the Package Anyway…") { confirmingUpgrade = true }
                        .disabled(link.syncing)
                }
            } header: {
                Text("Substrate")
            } footer: {
                Text("Sessions are written to \(link.host ?? "your substrate") when they finish, and wait on this iPhone while it cannot be reached. Routines and exercises come from it and are kept for the gym.")
            }
            .confirmationDialog("Update the training package over your changes?", isPresented: $confirmingUpgrade, titleVisibility: .visible) {
                Button("Update Anyway", role: .destructive) { Task { await link.upgradeOverEdits() } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("This app\u{2019}s version of the kinds you changed replaces yours. Kinds, properties and values you added stay: your substrate refuses an update that would remove them.")
            }
            if !link.parked.isEmpty {
                Section {
                    ForEach(link.parked) { entry in
                        ParkedRow(entry: entry)
                    }
                } header: {
                    Text("Refused by Your Substrate")
                } footer: {
                    Text("Retry sends the change again. Discard gives up the change and the rest of its session\u{2019}s changes waiting behind it; the session stays on this iPhone.")
                }
            }
            AccountSection(session: session, manifest: link.manifest, unrevoked: link.unrevoked,
                           onChangeAccess: { Task { await link.reauthorize() } }, onSignOut: link.signOut)
            if let problem = link.signOutProblem {
                Section { Text(problem).foregroundStyle(.red) }
            }
        } else {
            Section {
                Text(SyncStatus.local.sentence)
                Button("Sign In to Your Substrate…") { signingIn = true }
                UnrevokedNotices(appName: link.manifest.appName, unrevoked: link.unrevoked)
            } header: {
                Text("Substrate")
            } footer: {
                Text("Signing in asks your substrate to let \(link.manifest.appName) keep a training package there and write its sessions and sets. Sessions logged before you sign in are sent then.")
            }
        }
    }

    private var packageText: String {
        switch link.package {
        case nil: link.syncing ? "Checking" : "Not checked yet"
        case .missing: "Not installed"
        case let .behind(held): "Version \(held), upgrading"
        case let .current(held): "Version \(held)"
        case let .newer(held): "Version \(held), newer than this app"
        case let .edited(held, _): "Version \(held), changed on your substrate"
        case let .refused(held, _): held.map { "Version \($0), update refused" } ?? "Install refused"
        }
    }

    @ViewBuilder
    private var statusIcon: some View {
        switch link.syncStatus {
        case .ready(0): Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
        case .ready, .preparing: Image(systemName: "arrow.triangle.2.circlepath").foregroundStyle(.blue)
        case .offline: Image(systemName: "icloud.slash").foregroundStyle(.orange)
        case .local: Image(systemName: "iphone")
        default: Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange)
        }
    }
}

/// One write the server refused: what it was, the server's words, and the
/// owner's two ways out.
private struct ParkedRow: View {
    @Environment(SubstrateLink.self) private var link
    let entry: OutboxEntry

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title)
                .font(.subheadline.weight(.semibold))
            Text(entry.recordID)
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
            if let words = entry.lastError {
                Text(words)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            HStack(spacing: 16) {
                Button("Retry") { Task { await link.retry(entry) } }
                Button("Discard", role: .destructive) { Task { await link.discard(entry) } }
            }
            .buttonStyle(.borderless)
            .font(.footnote.weight(.semibold))
        }
        .padding(.vertical, 2)
    }

    private var title: String {
        let kind = entry.kind.split(separator: "/").last.map(String.init) ?? entry.kind
        let what = switch kind {
        case "session": "Training session"
        case "set": "Set"
        case "exercise": "Exercise"
        default: kind.capitalized
        }
        return switch entry.method {
        case .put: "\(what), sent"
        case .patch: "\(what), changed"
        case .purge: "\(what), deleted"
        }
    }
}

/// The kit's connect screen against the app's manifest: the address, then
/// the server's own sign-in and approval page in the system sheet.
struct SignInSheet: View {
    @Environment(SubstrateLink.self) private var link
    @Environment(\.dismiss) private var dismiss
    @AppStorage("signin.server") private var address = ""

    var body: some View {
        NavigationStack {
            ConnectView(manifest: link.manifest, address: $address, unrevoked: link.unrevoked) {
                ConnectHeader(appName: link.manifest.appName, tagline: "Your training sessions, in your substrate.",
                              icon: Image("AppMark"))
            } onSignIn: { probe in
                try await link.signIn(probe)
                dismiss()
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
        }
    }
}

extension View {
    /// The sign-in sheet, wherever the app offers it.
    func signInSheet(isPresented: Binding<Bool>) -> some View {
        sheet(isPresented: isPresented) { SignInSheet() }
    }
}
