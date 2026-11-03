/// Where the app stands with its repository (§5.6's Status): a sentence,
/// the package, what the owner can do about it, and the writes the server
/// refused, each with Retry and Discard.
public struct StatusPresentation: Codable, Hashable, Sendable {
    public var title: String
    public var symbol: ScreenSymbol
    public var tone: Tone
    public var sentence: String
    public var rows: [LabeledValue]
    public var actions: [ActionButton<StatusAction>]
    /// Whether a send is running now.
    public var busy: Bool
    public var footer: String?
    public var refused: RefusedWrites?

    public init(title: String, symbol: ScreenSymbol, tone: Tone, sentence: String, rows: [LabeledValue], actions: [ActionButton<StatusAction>],
                busy: Bool, footer: String?, refused: RefusedWrites?)
    {
        self.title = title
        self.symbol = symbol
        self.tone = tone
        self.sentence = sentence
        self.rows = rows
        self.actions = actions
        self.busy = busy
        self.footer = footer
        self.refused = refused
    }
}

/// What the owner can do from the Status screen.
public enum StatusAction: Codable, Hashable, Sendable {
    case sendNow
    case review
    case signIn
    case renew
    case retry(ParkedWrite.ID)
    case discard(ParkedWrite.ID)
}

/// The writes the server refused, listed for the owner.
public struct RefusedWrites: Codable, Hashable, Sendable {
    public var title: String
    public var rows: [RefusedWrite]
    public var footer: String

    public init(title: String, rows: [RefusedWrite], footer: String) {
        self.title = title
        self.rows = rows
        self.footer = footer
    }
}

/// One refused write: what it was, its record, the server's words.
public struct RefusedWrite: Codable, Hashable, Sendable, Identifiable {
    public var id: ParkedWrite.ID
    public var title: String
    public var recordID: String
    public var words: String?
    public var retry: ActionButton<StatusAction>
    public var discard: ActionButton<StatusAction>

    public init(id: ParkedWrite.ID, title: String, recordID: String, words: String?, retry: ActionButton<StatusAction>,
                discard: ActionButton<StatusAction>)
    {
        self.id = id
        self.title = title
        self.recordID = recordID
        self.words = words
        self.retry = retry
        self.discard = discard
    }
}
