/// The account, as a settings form shows it (§5.6): which server and
/// repository, the server's version, what the app was approved to do and
/// until when, and the two ways out of it.
public struct AccountPresentation: Codable, Hashable, Sendable {
    public var title: String
    /// Server, Repository, and the server's version where it answered.
    public var rows: [LabeledValue]
    /// "Approved access" over each scope in words; nil for a password
    /// session.
    public var approvedTitle: String?
    public var approved: [String]
    /// When the approved access ends, or that the owner revokes it.
    public var until: LabeledValue?
    /// A password session's access: everything.
    public var access: LabeledValue?
    public var changeAccess: String?
    public var unrevoked: [ProblemPresentation]
    public var signOut: String
    public var footer: String

    public init(title: String, rows: [LabeledValue], approvedTitle: String?, approved: [String], until: LabeledValue?,
                access: LabeledValue?, changeAccess: String?, unrevoked: [ProblemPresentation], signOut: String, footer: String)
    {
        self.title = title
        self.rows = rows
        self.approvedTitle = approvedTitle
        self.approved = approved
        self.until = until
        self.access = access
        self.changeAccess = changeAccess
        self.unrevoked = unrevoked
        self.signOut = signOut
        self.footer = footer
    }
}
