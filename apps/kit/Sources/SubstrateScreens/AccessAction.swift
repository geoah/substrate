import SubstrateCore

/// What the owner can do about an issue with the app's access, as the
/// command a button sends.
public enum AccessAction: Bridged, Hashable, Sendable {
    /// The review of what changed, then the approval page.
    case review
    case signInAgain
    /// A password session's way to an app session.
    case moveToAppAccess
    case retry
    /// The console page, by its root-relative path on the server, where what
    /// blocks an approval is fixed.
    case openFix(String)

    /// The action without its path: what an app offers.
    public var offer: Offer {
        switch self {
        case .review: .review
        case .signInAgain: .signInAgain
        case .moveToAppAccess: .moveToAppAccess
        case .retry: .retry
        case .openFix: .openFix
        }
    }

    public enum Offer: String, Bridged, Hashable, Sendable, CaseIterable {
        case review, signInAgain, moveToAppAccess, retry, openFix
    }
}

/// A button: its title and what it sends.
public struct ActionButton<Action: Codable & Hashable & Sendable>: Bridged, Hashable, Sendable {
    public var title: String
    public var action: Action

    public init(_ title: String, _ action: Action) {
        self.title = title
        self.action = action
    }
}
