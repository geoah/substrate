import Foundation
import SubstrateCore

/// One write waiting for the server.
public struct OutboxEntry: Codable, Sendable, Hashable, Identifiable {
    public enum Method: String, Codable, Sendable {
        case put = "PUT"
        case patch = "PATCH"
        /// `DELETE ?purge=true`.
        case purge = "DELETE"
    }

    public var id: UUID
    public var method: Method
    public var kind: String
    public var recordID: String
    public var properties: JSONObject?
    /// Sent with `ifVersion`: the version the outbox last saw the record at,
    /// read from the server when it has none.
    public var guarded: Bool
    /// The writes that stand or fall together (Workouts' session and its
    /// sets): a discard drops the batch, and a refusal holds the rest of it
    /// back.
    public var batch: String?
    public var attempts = 0
    /// The server's words for the last refusal, kept while the entry waits.
    public var lastError: String?
    /// Refused by the server for itself: the same write would be refused
    /// again, so it is not sent until the owner retries it, and the rest of
    /// its batch waits behind it. The owner may discard it instead.
    public var parked = false
    /// An edit the owner made on what they saw at this version: sent with it
    /// as `ifVersion`, or with the version the app's own last write of the
    /// record answered when that is newer. A 409 is never merged over: the
    /// entry is parked as a `conflict` for the owner to choose.
    public var baseVersion: Int?
    /// Parked because the record changed on the server since `baseVersion`.
    public var conflict = false

    public init(id: UUID = UUID(), method: Method, kind: String, recordID: String, properties: JSONObject? = nil,
                guarded: Bool = false, batch: String? = nil, baseVersion: Int? = nil)
    {
        self.id = id
        self.method = method
        self.kind = kind
        self.recordID = recordID
        self.properties = properties
        self.guarded = guarded
        self.batch = batch
        self.baseVersion = baseVersion
    }

    /// `kind/id`: how the outbox keys a record's versions.
    public var key: String { "\(kind)/\(recordID)" }

    enum CodingKeys: String, CodingKey {
        case id, method, kind, recordID, properties, guarded, batch, attempts, lastError, parked, baseVersion, conflict
        /// What Workouts' outbox calls the batch, read and written so a
        /// build of it from before the kit's outbox reads the file back.
        case session
        /// What the kit's outbox called it before `batch`, read only.
        case group
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(UUID.self, forKey: .id)
        method = try c.decode(Method.self, forKey: .method)
        kind = try c.decode(String.self, forKey: .kind)
        recordID = try c.decode(String.self, forKey: .recordID)
        properties = try c.decodeIfPresent(JSONObject.self, forKey: .properties)
        guarded = try c.decodeIfPresent(Bool.self, forKey: .guarded) ?? false
        batch = try c.decodeIfPresent(String.self, forKey: .batch) ?? c.decodeIfPresent(String.self, forKey: .group)
            ?? c.decodeIfPresent(String.self, forKey: .session)
        attempts = try c.decodeIfPresent(Int.self, forKey: .attempts) ?? 0
        lastError = try c.decodeIfPresent(String.self, forKey: .lastError)
        // A file from before parking held every refusal and retried it.
        parked = try c.decodeIfPresent(Bool.self, forKey: .parked) ?? (lastError != nil)
        baseVersion = try c.decodeIfPresent(Int.self, forKey: .baseVersion)
        conflict = try c.decodeIfPresent(Bool.self, forKey: .conflict) ?? false
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
        try c.encode(method, forKey: .method)
        try c.encode(kind, forKey: .kind)
        try c.encode(recordID, forKey: .recordID)
        try c.encodeIfPresent(properties, forKey: .properties)
        try c.encode(guarded, forKey: .guarded)
        try c.encodeIfPresent(batch, forKey: .batch)
        try c.encodeIfPresent(batch, forKey: .session)
        try c.encode(attempts, forKey: .attempts)
        try c.encodeIfPresent(lastError, forKey: .lastError)
        try c.encode(parked, forKey: .parked)
        try c.encodeIfPresent(baseVersion, forKey: .baseVersion)
        try c.encode(conflict, forKey: .conflict)
    }
}
