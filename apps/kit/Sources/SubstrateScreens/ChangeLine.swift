import SubstrateAuth

/// One change an approval would make, as a row draws it: its mark, its
/// words, and the fact beside it. A change the server cannot make yet wears
/// the warning mark and says why in full contrast, since that sentence is
/// what the owner acts on.
public struct ChangeLine: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var symbol: ScreenSymbol
    public var tone: Tone
    public var words: String
    public var detail: String?
    /// The detail is the reason the change is blocked, drawn in full contrast.
    public var detailStands: Bool

    public init(id: String, symbol: ScreenSymbol, tone: Tone, words: String, detail: String? = nil, detailStands: Bool = false) {
        self.id = id
        self.symbol = symbol
        self.tone = tone
        self.words = words
        self.detail = detail
        self.detailStands = detailStands
    }

    /// A scope left out last time is not allowed now, so it never wears the
    /// mark an allowed one does.
    public init(_ change: Change) {
        let symbol: ScreenSymbol
        let tone: Tone
        if change.blocked {
            (symbol, tone) = (.warning, .caution)
        } else if change.leftOut {
            (symbol, tone) = (.leftOut, .quiet)
        } else {
            switch change.kind {
            case .scope: (symbol, tone) = (.allowed, .positive)
            case .install: (symbol, tone) = (.install, .informative)
            case .upgrade: (symbol, tone) = (.upgrade, .informative)
            case .account: (symbol, tone) = (.create, .supporting)
            case .webhook: (symbol, tone) = (.webhook, .caution)
            }
        }
        self.init(id: change.id, symbol: symbol, tone: tone, words: change.words, detail: change.detail, detailStands: change.blocked)
    }
}
