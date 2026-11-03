/// What the app will ask the owner for, before it opens the browser: the
/// access it needs and what it sets up on their server, in the words the
/// approval page uses, so nothing there is a surprise.
public struct ReviewPresentation: Codable, Hashable, Sendable {
    public var title: String
    public var intro: String
    public var sections: [ReviewSection]
    public var continueTitle: String
    /// Under the button: what the owner can do on the page and after it.
    public var footer: String
    public var cancelTitle: String

    public init(title: String, intro: String, sections: [ReviewSection], continueTitle: String, footer: String, cancelTitle: String) {
        self.title = title
        self.intro = intro
        self.sections = sections
        self.continueTitle = continueTitle
        self.footer = footer
        self.cancelTitle = cancelTitle
    }
}

/// One list of a review: the access asked for, what was left out last
/// time, or what is set up on the server.
public struct ReviewSection: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var title: String
    public var rows: [ChangeLine]
    public var footer: String?

    public init(id: String, title: String, rows: [ChangeLine], footer: String? = nil) {
        self.id = id
        self.title = title
        self.rows = rows
        self.footer = footer
    }
}
