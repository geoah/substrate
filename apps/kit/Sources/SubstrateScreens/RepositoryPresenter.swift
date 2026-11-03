import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore

/// The Repository screen: the repository the app is signed in to.
public enum RepositoryPresenter {
    /// A password session says it reaches everything. A token this device
    /// stopped using that its server has not revoked yet is said, with where
    /// to revoke it by hand.
    ///
    /// - Parameters:
    ///   - title: the section's heading; an app that called it Account
    ///     before the kit (`AccountSection`) keeps its word.
    ///   - signOutMessage: what else signing out gives up on the device, in
    ///     the app's words (`StatusWords.signOut`), after the kit's own.
    public static func present(session: SessionFacts, manifest: AppManifest?, serverVersion: String?, unrevoked: [SessionFacts],
                               offersChangeAccess: Bool, title: String = "Repository", signOutMessage: String? = nil, locale: Locale,
                               timeZone: TimeZone) -> RepositoryPresentation
    {
        let host = ServerAddress.display(session.server)
        let words = { (scope: String) in manifest?.scopeWords(scope) ?? ScopeWords.sentence(scope) }
        let app = session.scopes != nil
        let until = LabeledValue("Until", session.expiresAt.map { DateWords.moment($0, locale: locale, timeZone: timeZone) } ?? "You revoke it")
        return RepositoryPresentation(
            title: title, server: LabeledValue("Server", host), repository: LabeledValue("Repository", session.repository),
            serverVersion: serverVersion.map { LabeledValue("Server version", ServerVersion.display($0).capitalizedFirst) },
            approved: session.scopes.map { scopes in
                ApprovedAccess(title: "Approved access", scopes: scopes.map { ApprovedScope(scope: $0, words: words($0)) }, until: until)
            },
            access: app ? nil : LabeledValue("Access", "Everything, with your password"),
            changeAccess: app && offersChangeAccess ? "Change Access…" : nil,
            unrevoked: UnrevokedPresenter.present(appName: manifest?.appName ?? "The app", unrevoked: unrevoked),
            signOut: "Sign Out",
            signOutConfirmation: ConfirmationPresentation(
                question: .repository(.signOut), title: "Sign out of \(session.repository)?",
                message: (app ? "\(manifest?.appName ?? "The app")'s access on \(host) is revoked." : "Its sign-in on \(host) is revoked.")
                    + (signOutMessage.map { " " + $0 } ?? ""),
                confirm: "Sign Out"),
            footer: "Sign Out: signs out on this device and revokes \(app ? "its access" : "its sign-in") on \(host).")
    }
}
