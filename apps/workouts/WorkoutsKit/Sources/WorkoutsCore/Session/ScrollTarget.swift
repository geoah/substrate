/// A row or a card the session list should bring into view: a view's own
/// request, or the session's, which reaches the list as a `Signal`.
public struct ScrollTarget: Codable, Equatable, Sendable {
    public var row: RowID?
    public var slot: String?

    public init(row: RowID? = nil, slot: String? = nil) {
        self.row = row
        self.slot = slot
    }
}
