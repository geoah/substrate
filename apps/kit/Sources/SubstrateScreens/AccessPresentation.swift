import SubstrateCore

/// The app's status issues as a banner: one line each, with its action.
public struct AccessBanner: Bridged, Hashable, Sendable {
    /// Orange when any issue stops the app, or will, or blocks an approval;
    /// blue otherwise.
    public var tone: ScreenTone
    public var lines: [IssueLine]

    public init(tone: ScreenTone, lines: [IssueLine]) {
        self.tone = tone
        self.lines = lines
    }
}

/// One issue: what needs the owner, in words, and the one thing to press.
public struct IssueLine: Bridged, Hashable, Sendable {
    public var symbol: ScreenSymbol
    public var tone: ScreenTone
    public var title: String
    public var message: String
    /// The first few changes an approval would make.
    public var changes: [ChangeLine]
    /// "and 2 more", where there were more than shown.
    public var more: String?
    public var action: ActionButton<AccessAction>?

    public init(
        symbol: ScreenSymbol, tone: ScreenTone, title: String, message: String, changes: [ChangeLine] = [], more: String? = nil,
        action: ActionButton<AccessAction>? = nil
    ) {
        self.symbol = symbol
        self.tone = tone
        self.title = title
        self.message = message
        self.changes = changes
        self.more = more
        self.action = action
    }
}

/// The access ended or was revoked: what happened, and the one way out.
public struct AccessEndedPresentation: Bridged, Hashable, Sendable {
    public var symbol: ScreenSymbol
    public var title: String
    public var message: String
    public var signInAgain: String

    public init(symbol: ScreenSymbol, title: String, message: String, signInAgain: String) {
        self.symbol = symbol
        self.title = title
        self.message = message
        self.signInAgain = signInAgain
    }
}
