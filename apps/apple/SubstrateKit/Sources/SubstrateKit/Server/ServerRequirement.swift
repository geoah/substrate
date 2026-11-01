import Foundation

/// The server an app needs: a release to name to the owner, and what the
/// server must be able to do. What it can do decides: a `dev` build, or one
/// between releases, works when its metadata lists the verbs and types, and a
/// release below the minimum is too old even when it lists them
/// (docs/plans/app-setup.md).
public struct ServerRequirement: Hashable, Sendable {
    /// The first release that serves what the app needs.
    public var minimumVersion: ServerVersion?
    /// The scope verbs the app asks for (`agents`, `decide`), each of which
    /// the server's `scopes_supported` must list.
    public var scopeVerbs: [String]
    /// The `authorization_details` types the app sends (`package`,
    /// `account`, `webhook`), each of which the server must list.
    public var detailTypes: [String]

    public init(minimumVersion: String? = nil, scopeVerbs: [String] = [], detailTypes: [String] = []) {
        self.minimumVersion = minimumVersion.flatMap(ServerVersion.init)
        self.scopeVerbs = scopeVerbs
        self.detailTypes = detailTypes
    }

    public enum Verdict: Hashable, Sendable {
        case compatible
        /// `running` is the version the server reports, `needed` the minimum
        /// the app names, and `missing` what the server cannot do yet, in
        /// words that finish "It can't yet …".
        case tooOld(running: String?, needed: String?, missing: [String])

        public var isCompatible: Bool { self == .compatible }
    }

    /// The verdict on a server from its version and its OAuth metadata, nil
    /// when it has no app authorization at all.
    public func verdict(version: String?, metadata: AuthorizationServerMetadata?) -> Verdict {
        var missing: [String] = []
        if let metadata {
            let verbs = metadata.scopeVerbs
            missing += scopeVerbs.filter { !verbs.contains($0) }.map(Self.verbWords)
            missing += detailTypes.filter { !metadata.supports(detailType: $0) }.map(Self.detailWords)
        } else {
            missing.append("give apps access of their own")
        }
        var below = false
        if let running = version.flatMap(ServerVersion.init), running.isRelease, let minimumVersion {
            below = running < minimumVersion
        }
        guard below || !missing.isEmpty else { return .compatible }
        return .tooOld(running: version, needed: minimumVersion?.description, missing: missing)
    }

    /// What a scope verb lets an app do, as what a server without it cannot.
    static func verbWords(_ verb: String) -> String {
        switch verb {
        case "read": "let apps see your records"
        case "write": "let apps change your records"
        case "agents": "let apps talk to your agents"
        case "decide": "let apps apply what your agents suggest"
        case "webhook": "let apps send data to a webhook"
        default: "give apps \(verb) access"
        }
    }

    /// What an `authorization_details` type sets up on approval.
    static func detailWords(_ type: String) -> String {
        switch type {
        case "package": "install what an app needs when you approve it"
        case "account": "create an app's account when you approve it"
        case "webhook": "turn on an app's webhook when you approve it"
        default: "set up \(type) for an app"
        }
    }
}
