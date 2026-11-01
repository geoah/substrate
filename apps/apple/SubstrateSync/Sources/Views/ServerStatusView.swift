import SubstrateKit
import SwiftUI

/// An app session's Set Up: what the server says about Sync's access, by
/// the kit's check. The owner's approval on the server does the set up, so
/// the phone only says what an approval would change and opens the review
/// that runs it; Root shows it in Home's place while a package, the Health
/// account or the webhook needs one.
struct ServerStatusView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.openURL) private var openURL
    /// Opened from Settings, which already shows the account and Sign Out.
    var fromSettings = false
    @State private var reviewing = false

    var body: some View {
        ServerStatusForm(
            session: model.session, status: model.status.status, checking: model.status.checking,
            authorizing: model.authorizing, problem: model.authorizeError, fromSettings: fromSettings,
            review: { reviewing = true },
            checkAgain: { Task { await model.status.refresh(force: true) } },
            signInAgain: { Task { await model.authorize() } },
            openServer: { if let server = model.session?.server { openURL(server) } },
            signOut: { model.signOut() })
            .sheet(isPresented: $reviewing) {
                ReviewChangesSheet(appName: SyncManifest.appName, host: model.session.map { ServerAddress.display($0.server) } ?? "your server",
                                   changes: model.status.status?.changes ?? [], firstSignIn: false,
                                   onContinue: {
                                       reviewing = false
                                       Task { await model.authorize() }
                                   },
                                   onCancel: { reviewing = false })
            }
    }
}

/// The status screen as it stands, from plain values.
struct ServerStatusForm: View {
    let session: Session?
    let status: AppStatus?
    var checking = false
    var authorizing = false
    var problem: String?
    var fromSettings = false
    var review: () -> Void = {}
    var checkAgain: () -> Void = {}
    var signInAgain: () -> Void = {}
    var openServer: () -> Void = {}
    var signOut: () -> Void = {}

    private var host: String { session.map { ServerAddress.display($0.server) } ?? "your server" }
    private var issues: [AppIssue] { (status?.issues ?? []).filter { $0 != .accessEnded } }
    private var changes: [Change] { status?.changes ?? [] }
    /// Access that ends soon is renewed through the same review.
    private var ending: Bool { issues.contains { if case .accessEndsSoon = $0 { true } else { false } } }
    /// The approval page would refuse a review: what blocks it is the
    /// server's to fix, and the banner says where.
    private var reviewable: Bool { (!changes.isEmpty || ending) && status?.approvalBlocked != true }

    var body: some View {
        Form {
            if !fromSettings, let session {
                Section("Signed in") {
                    LabeledContent("Repository", value: session.repository)
                    LabeledContent("Server", value: session.server.absoluteString)
                }
            }

            if let status {
                if issues.isEmpty {
                    Section {
                        Label {
                            VStack(alignment: .leading, spacing: 2) {
                                Text("Everything is in place")
                                Text("Checked with \(host) \(status.checkedAt.shortAgo).")
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                        } icon: {
                            Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
                        }
                    }
                } else {
                    // The review is the screen's own button, below the banner.
                    Section {
                        AppStatusBanner(issues: issues, appName: SyncManifest.appName, host: host,
                                        actions: AppStatusActions(signInAgain: signInAgain, retry: checkAgain, openServer: openServer))
                    }
                    .listRowInsets(EdgeInsets())
                    .listRowBackground(Color.clear)
                }
            } else {
                Section {
                    HStack(spacing: 10) {
                        ProgressView()
                        Text("Asking \(host) what it holds…").foregroundStyle(.secondary)
                    }
                }
            }

            if reviewable {
                Section {
                    Button(action: review) {
                        WideLabel(title: changes.isEmpty ? "Renew Access" : "Review and Approve", systemImage: "checkmark.shield",
                                  busy: authorizing)
                    }
                    .primaryAction()
                    .disabled(authorizing)
                    .buttonRow()
                } footer: {
                    Text("You approve on \(host), signed in there. What \(SyncManifest.appName) already has keeps working until you do.")
                }
            }

            if let problem {
                Section {
                    ProblemBox(title: "The review didn't finish", message: problem)
                }
            }

            if let session, !session.details.isEmpty {
                Section {
                    ForEach(Array(session.details.enumerated()), id: \.offset) { _, detail in
                        SetUpRow(detail: detail)
                    }
                } header: {
                    Text("Set up when you approved it")
                } footer: {
                    Text("What \(host) set up for this phone. Health goes with \(SyncManifest.appName)'s own access, so no webhook key ever reaches the phone.")
                }
            }

            if !fromSettings {
                Section {
                    Button(role: .destructive, action: signOut) {
                        WideLabel(title: "Sign Out", systemImage: "rectangle.portrait.and.arrow.right")
                    }
                    .destructiveAction()
                    .buttonRow()
                }
            }
        }
        .readableWidth()
        .navigationTitle(fromSettings ? "Server Status" : "Set Up")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                if checking {
                    ProgressView()
                } else {
                    Button("Check Again", systemImage: "arrow.clockwise", action: checkAgain)
                }
            }
        }
    }
}

/// One thing the approval set up, as the token's answer listed it: what it
/// is, the server's name for it, and the version a package is at.
private struct SetUpRow: View {
    let detail: AuthorizationDetail

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                Text(code).font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
            }
            Spacer(minLength: 8)
            if let version = detail.version, detail.type == AuthorizationDetail.Types.package {
                StatusBadge(text: "Version \(version)", color: .green)
            }
        }
        .padding(.vertical, 2)
    }

    private var title: String {
        switch detail.type {
        case AuthorizationDetail.Types.package:
            packageDisplayName(detail.package.flatMap { $0.split(separator: "/").last.map(String.init) } ?? "package")
        case AuthorizationDetail.Types.account: "This phone's Health account"
        case AuthorizationDetail.Types.webhook: "The webhook Health is sent to"
        default: detail.type
        }
    }

    private var code: String {
        switch detail.type {
        case AuthorizationDetail.Types.package: detail.package ?? ""
        case AuthorizationDetail.Types.account: [detail.kind, detail.id].compactMap { $0 }.joined(separator: "/")
        case AuthorizationDetail.Types.webhook: detail.trigger ?? ""
        default: ""
        }
    }
}
