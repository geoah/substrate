import Foundation
import SubstrateCore
import SubstrateDevice

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// One row of a webhook trigger's parked list: a delivery its function
/// refused, kept with its reason, or one accepted and not settled yet,
/// which the list carries too and which is no failure.
public struct ParkedDelivery: Decodable, Sendable, Equatable {
    public let fireID: String?
    public let lastError: String
    public let running: Bool
    /// A delivery the door accepted that is still filing, as the server
    /// marks it; nil where the server marks none.
    public let pending: Bool?

    enum CodingKeys: String, CodingKey {
        case fireID = "fireId"
        case lastError, running, pending
    }

    public init(fireID: String?, lastError: String, running: Bool = false, pending: Bool? = nil) {
        self.fireID = fireID
        self.lastError = lastError
        self.running = running
        self.pending = pending
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        fireID = c.value(.fireID, or: nil)
        lastError = c.value(.lastError, or: "")
        running = c.value(.running, or: false)
        pending = c.value(.pending, or: nil)
    }

    /// The words a server that marks no row pending gives a delivery it
    /// accepted and has not settled (internal/engine's webhooks.go
    /// pendingWebhookError), read only where the mark is absent.
    public static let pendingPrefix = "delivery accepted:"

    /// Still filing: marked so, or, on a row that carries no mark, said so
    /// in the server's words.
    public var isPending: Bool { pending ?? lastError.hasPrefix(Self.pendingPrefix) }

    /// Refused for good: neither waiting to run nor running.
    public var refused: Bool { !running && !isPending }
}

public extension SubstrateClient {
    /// One delivery to `POST /webhooks/{authority}/{trigger}`, answering the
    /// fire id it was stored under. A 202 means stored, not filed: the
    /// trigger's function runs after it and files the delivery or parks it.
    /// The client's token is the credential (it holds `webhook:<trigger>`);
    /// `credential` names another, such as a trigger's own key under a
    /// password session. The door answers 503 while eight deliveries are in
    /// flight across every sender, before it reads one, so a busy door is
    /// waited out and the delivery sent again.
    @discardableResult
    func deliver(
        authority: String, trigger: String, body: Data, contentType: String = "application/json",
        headers: [String: String] = [:], credential: String? = nil, retries: Int = 8
    ) async throws -> String {
        var req = try request(
            "POST", "webhooks/\(segment(authority))/\(segment(trigger))",
            body: .bytes(body, contentType: contentType), timeout: Timeout.upload)
        for (name, value) in headers { req.setValue(value, forHTTPHeaderField: name) }
        if let credential { req.setValue("Bearer \(credential)", forHTTPHeaderField: "Authorization") }
        let response = try await send(req, retries: retries, longestWait: 30)
        struct Accepted: Decodable { let fire: String }
        guard let fire = try? response.decode(Accepted.self).fire, !fire.isEmpty else { throw URLError(.cannotParseResponse) }
        return fire
    }

    /// A webhook trigger's parked deliveries, every sender's: readable with
    /// `webhook:<trigger>`.
    func parked(trigger: String, timeout: TimeInterval = Timeout.read) async throws -> [ParkedDelivery] {
        struct Page: Decodable { let items: [ParkedDelivery] }
        return try await send(
            "GET", "api/v1/substrate.reamde.dev/core/trigger/\(segment(trigger))/parked", timeout: timeout,
            retries: 0
        ).decode(Page.self).items
    }
}
