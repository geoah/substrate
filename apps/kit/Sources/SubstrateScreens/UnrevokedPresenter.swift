import SubstrateAuth
import SubstrateCore

/// The tokens a device stopped using that their servers have not revoked
/// yet, said once per server and repository each reaches, never in the
/// current session's words: where the access still works, that the app tries
/// again, and where the owner revokes it by hand.
public enum UnrevokedPresenter {
    /// The one title every screen says it under.
    public static let title = "Server access has not been revoked yet"

    public static func present(appName: String, unrevoked: [SessionFacts]) -> [ProblemPresentation] {
        sentences(appName: appName, unrevoked: unrevoked).map {
            ProblemPresentation(title: title, message: $0, tone: .caution)
        }
    }

    /// One sentence per server and repository, in the order they were met.
    public static func sentences(appName: String, unrevoked: [SessionFacts]) -> [String] {
        var order: [String] = []
        var groups: [String: [SessionFacts]] = [:]
        for session in unrevoked {
            if groups[session.identity] == nil { order.append(session.identity) }
            groups[session.identity, default: []].append(session)
        }
        return order.compactMap { groups[$0].map { words(appName: appName, $0) } }
    }

    /// The repository is named where it is not the server's own host. A
    /// password token is revoked under Signed in, an app's under Apps.
    private static func words(appName: String, _ sessions: [SessionFacts]) -> String {
        let first = sessions[0]
        let host = ServerAddress.display(first.server)
        let repository = first.repository.caseInsensitiveCompare(host) == .orderedSame ? "" : " to \(first.repository)"
        let places = Set(sessions.map { $0.isApp ? "under Apps" : "under Signed in" }).sorted().joined(separator: " and ")
        return "\(appName)'s earlier access\(repository) still works on \(host). It tries again each time it opens; "
            + "you can also revoke it yourself in \(host)'s Settings, \(places)."
    }
}
