/// One committed change as the watch reports it: which record of which kind
/// moved, how, and its position in the repository's history.
public struct WatchRow: Codable, Hashable, Sendable {
    public var seq: Int
    public var kind: String
    public var recordID: String
    /// `put`, `patch`, `delete` or `purge`.
    public var op: String

    public init(seq: Int, kind: String, recordID: String, op: String) {
        self.seq = seq
        self.kind = kind
        self.recordID = recordID
        self.op = op
    }

    /// The record is gone after this change.
    public var removes: Bool { op == "delete" || op == "purge" }
}

/// Where a watch resumes: after `seq`, in the history `generation` names. A
/// seq means nothing without its generation; a watch answered with another
/// relists.
public struct WatchCursor: Codable, Hashable, Sendable {
    public var seq: Int
    public var generation: String

    public init(seq: Int, generation: String) {
        self.seq = seq
        self.generation = generation
    }
}
