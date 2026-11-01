import SwiftUI

/// The account, as a settings form shows it: which server and repository,
/// the server's version, what the app was approved to do and until when,
/// and the two ways out of it. A password session says it reaches
/// everything. A token this device stopped using that its server has not
/// revoked yet is said, with where to revoke it by hand (`UnrevokedNotices`).
public struct AccountSection: View {
    let session: Session
    let manifest: AppManifest?
    let serverVersion: String?
    let unrevoked: [Session]
    let onChangeAccess: (() -> Void)?
    let onSignOut: () -> Void

    public init(session: Session, manifest: AppManifest? = nil, serverVersion: String? = nil, unrevoked: [Session] = [],
                onChangeAccess: (() -> Void)? = nil, onSignOut: @escaping () -> Void)
    {
        self.session = session
        self.manifest = manifest
        self.serverVersion = serverVersion
        self.unrevoked = unrevoked
        self.onChangeAccess = onChangeAccess
        self.onSignOut = onSignOut
    }

    private var host: String { ServerAddress.display(session.server) }

    public var body: some View {
        Section {
            LabeledContent("Server", value: host)
            LabeledContent("Repository", value: session.repository)
            if let serverVersion { LabeledContent("Server version", value: ServerVersion.display(serverVersion).capitalizedFirst) }
            if let scopes = session.scopes {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Approved access")
                    ForEach(scopes, id: \.self) { scope in
                        Label {
                            Text(words(scope)).fixedSize(horizontal: false, vertical: true)
                        } icon: {
                            Image(systemName: "checkmark").foregroundStyle(.green)
                        }
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    }
                }
                LabeledContent("Until", value: session.expiresAt.map { $0.formatted(date: .abbreviated, time: .shortened) }
                    ?? "You revoke it")
                if let onChangeAccess {
                    Button("Change Access…", action: onChangeAccess)
                }
            } else {
                LabeledContent("Access", value: "Everything, with your password")
            }
            UnrevokedNotices(appName: manifest?.appName ?? "The app", unrevoked: unrevoked)
            Button("Sign Out", role: .destructive, action: onSignOut)
        } header: {
            Text("Account")
        } footer: {
            Text("Sign Out: signs out on this device and revokes \(session.isApp ? "its access" : "its sign-in") on \(host).")
        }
    }

    private func words(_ scope: String) -> String {
        manifest?.scopeWords(scope) ?? ScopeWords.sentence(scope)
    }
}
