/// One problem in words, and what fixes it (§5.6's Problems): an icon, a
/// short title and the server's or the system's own words. It sits in a
/// card of its own or inside one, never as a box inside a box.
public struct ProblemPresentation: Codable, Hashable, Sendable {
    public var title: String?
    public var message: String
    public var tone: ScreenTone
    public var symbol: ScreenSymbol
    public var fix: ProblemFix?

    public init(title: String? = nil, message: String, tone: ScreenTone = .critical, symbol: ScreenSymbol = .warning, fix: ProblemFix? = nil) {
        self.title = title
        self.message = message
        self.tone = tone
        self.symbol = symbol
        self.fix = fix
    }
}

/// The one thing that fixes a problem, as a button's title and the command
/// it sends.
public struct ProblemFix: Codable, Hashable, Sendable {
    public enum Action: String, Codable, Hashable, Sendable {
        case signInAgain
        case review
        case tryAgain
    }

    public var title: String
    public var action: Action

    public init(title: String, action: Action) {
        self.title = title
        self.action = action
    }
}
