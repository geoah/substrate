/// What one owner's action writes, as the outbox keeps it: the writes, each
/// in the batch of the record it writes (a refusal holds back that record's
/// later writes, never another's), and for a delete the batch whose waiting
/// writes it replaces in the same save.
public struct LocalWrite: Codable, Equatable, Sendable {
    public var entries: [OutboxEntry]
    public var replacing: String?

    public init(entries: [OutboxEntry], replacing: String? = nil) {
        self.entries = entries
        self.replacing = replacing
    }

    /// The batch a record's writes are in: its key.
    public static func batch(_ kind: String, _ id: String) -> String { "\(kind)/\(id)" }
}
