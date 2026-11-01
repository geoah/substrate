import Foundation

/// One thing the owner's approval would change: a scope to allow, or set up
/// to run on their server, in the console's everyday words.
public struct Change: Hashable, Sendable, Identifiable {
    public enum Kind: String, Hashable, Sendable {
        case scope, install, upgrade, account, webhook
    }

    public var kind: Kind
    /// What approving does: "Install Apple Health", "See your Apple places".
    public var words: String
    /// A fact beside it: "Version 2 to 3", or a blocked row's problem.
    public var detail: String?
    /// Why the approval page cannot do it as things stand: the server's
    /// reason, its sentence (the detail) and where the owner fixes it.
    public var block: AuthorizationSetup.Block?
    /// A scope the owner left out of the last approval, which the request
    /// asks for again.
    public var leftOut: Bool

    public init(kind: Kind, words: String, detail: String? = nil, block: AuthorizationSetup.Block? = nil,
                leftOut: Bool = false)
    {
        self.kind = kind
        self.words = words
        self.detail = detail
        self.block = block
        self.leftOut = leftOut
    }

    public var id: String { "\(kind.rawValue)|\(leftOut)|\(words)" }

    /// The approval page cannot do it as things stand; the detail says why.
    public var blocked: Bool { block != nil }

    /// Whether it is set up on the server, rather than access to allow.
    public var isSetup: Bool { kind != .scope }
}

/// Something about an app's access the owner may need to act on. Several
/// can hold at once: a server update and an approval, access ending soon
/// and a review.
public enum AppIssue: Hashable, Sendable, Identifiable {
    /// The server cannot do what the app needs: `running` is its version,
    /// `needed` the release the app names, `missing` what it cannot do yet.
    case serverTooOld(running: String?, needed: String?, missing: [String])
    /// The app's request has grown past what was approved, or set up on the
    /// server has gone or fallen behind: the owner reviews the difference.
    case needsApproval(changes: [Change])
    /// The approved access ends on this date.
    case accessEndsSoon(Date)
    /// The approved access ended, or was revoked: only signing in again
    /// gets it back.
    case accessEnded
    /// The server could not be asked; the words say why.
    case unreachable(message: String)
    /// A password session on a server that can now give the app access of
    /// its own.
    case legacyAccess

    public var id: String {
        switch self {
        case .serverTooOld: "serverTooOld"
        case .needsApproval: "needsApproval"
        case .accessEndsSoon: "accessEndsSoon"
        case .accessEnded: "accessEnded"
        case .unreachable: "unreachable"
        case .legacyAccess: "legacyAccess"
        }
    }
}

/// The app's standing with its server, as one check found it.
public struct AppStatus: Hashable, Sendable {
    public var issues: [AppIssue]
    /// What the server said it runs, where it answered.
    public var serverVersion: String?
    public var checkedAt: Date

    public init(issues: [AppIssue] = [], serverVersion: String? = nil, checkedAt: Date = Date()) {
        self.issues = issues
        self.serverVersion = serverVersion
        self.checkedAt = checkedAt
    }

    public var isClear: Bool { issues.isEmpty }

    /// The changes an approval would make, where one is needed.
    public var changes: [Change] {
        issues.flatMap { issue -> [Change] in
            if case .needsApproval(let changes) = issue { return changes }
            return []
        }
    }

    /// Whether the approval page would refuse a review now.
    public var approvalBlocked: Bool { issues.approvalBlocked }
}

public extension [AppIssue] {
    /// Whether the approval page would refuse a review now: it disables
    /// Approve while any set up row is blocked, and a server too old for what
    /// the app asks blocks one.
    var approvalBlocked: Bool {
        contains { issue in
            switch issue {
            case .serverTooOld: true
            case let .needsApproval(changes): changes.contains(where: \.blocked)
            default: false
            }
        }
    }

    /// The console path where what blocks the approval is fixed, where the
    /// server named one.
    var fix: String? {
        lazy.compactMap { issue -> String? in
            guard case let .needsApproval(changes) = issue else { return nil }
            return changes.lazy.compactMap { $0.block?.fix }.first
        }.first
    }

    /// Whether only a server update stands in the way: the owner waits, and
    /// nothing they open would help.
    var waitsForUpdate: Bool {
        contains { if case .serverTooOld = $0 { true } else { false } }
            && !contains { if case let .needsApproval(changes) = $0 { changes.contains(where: \.blocked) } else { false } }
    }
}

/// How a review of changes heads itself, by what changed since the access
/// the app holds (the console's re-authorization headings), or by what the
/// owner asked for.
public enum ReviewHeading: Hashable, Sendable {
    case firstSignIn
    case renew
    case moreAccess
    case serverSetup
    case moreAccessAndServerSetup
    /// The owner chose to change what the app may do.
    case changeAccess

    /// A scope left out last time is offered back, never what heads a
    /// review.
    public init(changes: [Change], firstSignIn: Bool) {
        let scopes = changes.contains { $0.kind == .scope && !$0.leftOut }
        let setup = changes.contains(where: \.isSetup)
        switch (firstSignIn, scopes, setup) {
        case (true, _, _): self = .firstSignIn
        case (false, true, true): self = .moreAccessAndServerSetup
        case (false, true, false): self = .moreAccess
        case (false, false, true): self = .serverSetup
        case (false, false, false): self = .renew
        }
    }

    /// The heading; `appName` names the app where the heading does.
    public func title(appName: String) -> String {
        switch self {
        case .firstSignIn: "Connect to your substrate"
        case .renew: "Renew access"
        case .moreAccess: "Allow more access"
        case .serverSetup: "Update server setup"
        case .moreAccessAndServerSetup: "Allow more access and update server setup"
        case .changeAccess: "Change what \(appName) may do"
        }
    }
}
