/// One line above an app's column about what stands in its way, with the
/// mark and tone it is drawn in: the records not being read, a change the
/// owner must answer.
public struct NoticeLine: Codable, Hashable, Sendable {
    public var text: String
    public var symbol: ScreenSymbol
    public var tone: ScreenTone

    public init(_ text: String, symbol: ScreenSymbol, tone: ScreenTone) {
        self.text = text
        self.symbol = symbol
        self.tone = tone
    }
}
