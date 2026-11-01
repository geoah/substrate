import SwiftUI

/// The account, as a settings form shows it: which server and repository,
/// the server's version, what the app was approved to do and until when,
/// and the two ways out of it. A password session says it reaches
/// everything.
public struct AccountSection: View {
    let session: Session
    let manifest: AppManifest?
    let serverVersion: String?
    let onChangeAccess: (() -> Void)?
    let onSignOut: () -> Void

    public init(session: Session, manifest: AppManifest? = nil, serverVersion: String? = nil,
                onChangeAccess: (() -> Void)? = nil, onSignOut: @escaping () -> Void)
    {
        self.session = session
        self.manifest = manifest
        self.serverVersion = serverVersion
        self.onChangeAccess = onChangeAccess
        self.onSignOut = onSignOut
    }

    public var body: some View {
        Section {
            LabeledContent("Server", value: ServerAddress.display(session.server))
            LabeledContent("Repository", value: session.repository)
            if let serverVersion { LabeledContent("Server version", value: serverVersion) }
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
            Button("Sign Out", role: .destructive, action: onSignOut)
        } header: {
            Text("Account")
        } footer: {
            if session.isApp {
                Text("Sign Out revokes \(manifest?.appName ?? "the app")'s access on the server too.")
            }
        }
    }

    private func words(_ scope: String) -> String {
        manifest?.scopeWords(scope) ?? ScopeWords.sentence(scope)
    }
}

public extension AppManifest {
    /// One approved scope in words, a webhook's named by the package it
    /// delivers for where the app asks for that package.
    func scopeWords(_ scope: String) -> String {
        let (verb, pattern) = ScopeWords.split(scope)
        if verb == "webhook" { return ScopeWords.webhookSentence(trigger: pattern, packageName: webhookPackageName(trigger: pattern)) }
        return ScopeWords.sentence(scope)
    }
}
