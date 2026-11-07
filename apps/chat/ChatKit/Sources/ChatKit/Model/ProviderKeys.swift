import Foundation
import SubstrateAPI
import SubstrateCore

/// Whether each `llm/provider` row has a key that resolves, as the server's
/// operator read says (`GET /api/v1/operator/defaults`, decision 0235). A row
/// with no key of its own still runs where the operator supplies one, so
/// the row alone cannot say whether an agent can answer.
enum ProviderKeys: Codable, Equatable, Sendable {
    /// No operator read: a server older than it, or a token it refuses.
    /// Each row's own `apiKey` says.
    case rows
    /// Whether a key resolves, by row id. A row the read names no item for
    /// is told by its own `apiKey`.
    case read([String: Bool])

    /// Whether `provider`'s key resolves.
    func resolves(_ provider: Provider) -> Bool {
        switch self {
        case .rows: provider.hasKey
        case let .read(resolved): resolved[provider.id] ?? provider.hasKey
        }
    }

    /// The operator read, nil where it failed: what was held stands, since
    /// a row with no key of its own may still be the operator's. A server
    /// older than the read answers 404, 405 or 501.
    static func read(_ client: SubstrateClient) async -> ProviderKeys? {
        // Every field optional: an item this build cannot read is skipped,
        // never the whole answer.
        struct Item: Decodable {
            var id: String?
            var slot: String?
            var source: String?
            var readiness: String?
        }
        struct Answer: Decodable { var items: [Item]? }
        let older: Set<Int> = [404, 405, 501]
        let response: SubstrateClient.Response
        do {
            response = try await client.send("GET", "api/v1/operator/defaults", accept: older, retries: 0)
        } catch {
            return nil
        }
        if older.contains(response.status) { return .rows }
        guard let answer = try? response.decode(Answer.self) else { return nil }
        var resolved: [String: Bool] = [:]
        for item in answer.items ?? [] {
            guard item.slot == "llm", let id = item.id, id.hasPrefix("llm/"), let source = item.source,
                let readiness = item.readiness
            else { continue }
            // As the console's Providers page reads it: a key that resolves
            // is On even while its calls fail; none, or a row the slot needs
            // gone, is not set up.
            resolved[String(id.dropFirst("llm/".count))] = source != "none" && readiness != "not-set-up"
        }
        return .read(resolved)
    }
}
