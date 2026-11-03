import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore

/// The Account screen, in the words `AccountSection` said it.
public enum AccountPresenter {
    /// A password session says it reaches everything. A token this device
    /// stopped using that its server has not revoked yet is said, with where
    /// to revoke it by hand.
    public static func present(session: SessionFacts, manifest: AppManifest?, serverVersion: String?, unrevoked: [SessionFacts],
                               offersChangeAccess: Bool, locale: Locale, timeZone: TimeZone) -> AccountPresentation
    {
        let host = ServerAddress.display(session.server)
        var rows = [LabeledValue("Server", host), LabeledValue("Repository", session.repository)]
        if let serverVersion { rows.append(LabeledValue("Server version", ServerVersion.display(serverVersion).capitalizedFirst)) }
        let words = { (scope: String) in manifest?.scopeWords(scope) ?? ScopeWords.sentence(scope) }
        let app = session.scopes != nil
        return AccountPresentation(
            title: "Account", rows: rows, approvedTitle: app ? "Approved access" : nil, approved: (session.scopes ?? []).map(words),
            until: app ? LabeledValue("Until", session.expiresAt.map { DateWords.moment($0, locale: locale, timeZone: timeZone) }
                ?? "You revoke it") : nil,
            access: app ? nil : LabeledValue("Access", "Everything, with your password"),
            changeAccess: app && offersChangeAccess ? "Change Access…" : nil,
            unrevoked: UnrevokedPresenter.present(appName: manifest?.appName ?? "The app", unrevoked: unrevoked),
            signOut: "Sign Out",
            footer: "Sign Out: signs out on this device and revokes \(app ? "its access" : "its sign-in") on \(host).")
    }
}
