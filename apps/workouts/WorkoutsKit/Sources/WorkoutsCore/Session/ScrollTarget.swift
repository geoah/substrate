/// A row or a card the session list should bring into view. `serial` makes
/// a repeat request for the same row a change.
public struct ScrollTarget: Codable, Equatable, Sendable {
    public var row: RowID?
    public var slot: String?
    public var serial: Int

    public init(row: RowID? = nil, slot: String? = nil, serial: Int = 0) {
        self.row = row
        self.slot = slot
        self.serial = serial
    }
}
