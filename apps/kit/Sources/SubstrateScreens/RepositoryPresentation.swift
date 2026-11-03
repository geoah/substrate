import SubstrateCore

/// The repository the app is signed in to, as a settings form shows it
/// (§5.6): which server and repository, the server's version, what the app
/// was approved to do and until when, and the two ways out of it. Each fact
/// is a field of its own, so a form gives each its row and its id.
public struct RepositoryPresentation: Bridged, Hashable, Sendable {
    public var title: String
    public var server: LabeledValue
    public var repository: LabeledValue
    /// Where the server answered with its version.
    public var serverVersion: LabeledValue?
    /// Each approved scope in words, with when the access ends; nil for a
    /// password session.
    public var approved: ApprovedAccess?
    /// A password session's access: everything.
    public var access: LabeledValue?
    public var changeAccess: String?
    public var unrevoked: [ProblemPresentation]
    public var signOut: String
    public var footer: String

    public init(title: String, server: LabeledValue, repository: LabeledValue, serverVersion: LabeledValue?, approved: ApprovedAccess?,
                access: LabeledValue?, changeAccess: String?, unrevoked: [ProblemPresentation], signOut: String, footer: String)
    {
        self.title = title
        self.server = server
        self.repository = repository
        self.serverVersion = serverVersion
        self.approved = approved
        self.access = access
        self.changeAccess = changeAccess
        self.unrevoked = unrevoked
        self.signOut = signOut
        self.footer = footer
    }

    /// Server, Repository, and the server's version where it answered.
    public var rows: [LabeledValue] { [server, repository] + (serverVersion.map { [$0] } ?? []) }
}

/// What an app session was approved to do: "Approved access" over one line
/// per scope, and until when.
public struct ApprovedAccess: Bridged, Hashable, Sendable {
    public var title: String
    public var scopes: [ApprovedScope]
    /// When the approved access ends, or that the owner revokes it.
    public var until: LabeledValue

    public init(title: String, scopes: [ApprovedScope], until: LabeledValue) {
        self.title = title
        self.scopes = scopes
        self.until = until
    }
}

/// One approved scope, in words.
public struct ApprovedScope: Bridged, Hashable, Sendable, Identifiable {
    public var scope: String
    public var words: String

    public init(scope: String, words: String) {
        self.scope = scope
        self.words = words
    }

    public var id: String { scope }
}
