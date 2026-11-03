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
    /// - Parameter title: the section's heading; an app that called it
    ///   Account before the kit (`AccountSection`) keeps its word.
    public static func present(session: SessionFacts, manifest: AppManifest?, serverVersion: String?, unrevoked: [SessionFacts],
                               offersChangeAccess: Bool, title: String = "Repository", locale: Locale,
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
            footer: "Sign Out: signs out on this device and revokes \(app ? "its access" : "its sign-in") on \(host).")
    }
}
