import Foundation
import SubstrateLocal

/// A write the server refused, as the Status screen lists it: what it was,
/// the server's words, and the owner's two ways out.
public struct ParkedWrite: Codable, Hashable, Sendable, Identifiable {
    public var id: UUID
    public var method: OutboxEntry.Method
    public var kind: String
    public var recordID: String
    public var words: String?
    /// The record changed on the server since the owner's edit.
    public var conflict: Bool

    public init(id: UUID, method: OutboxEntry.Method, kind: String, recordID: String, words: String?, conflict: Bool) {
        self.id = id
        self.method = method
        self.kind = kind
        self.recordID = recordID
        self.words = words
        self.conflict = conflict
    }

    public init(_ entry: OutboxEntry) {
        self.init(id: entry.id, method: entry.method, kind: entry.kind, recordID: entry.recordID, words: entry.lastError,
                  conflict: entry.conflict)
    }
}
