import Foundation
import SubstrateCore

/// One kind as the trait route serves it (internal/substrate's `KindInfo`):
/// its reference, its declared label where it declares one, and its
/// declaration's data map, from which a status (`KindStatus`) and a grant's
/// reach (`GrantKind`) are read.
public struct KindInfo: Decodable, Hashable, Sendable {
    public struct Label: Decodable, Hashable, Sendable {
        public var singular: String
        public var plural: String

        public init(singular: String, plural: String) {
            self.singular = singular
            self.plural = plural
        }
    }

    public var identity: String
    public var label: Label?
    public var definition: [String: JSONValue]

    public init(identity: String, label: Label? = nil, definition: [String: JSONValue] = [:]) {
        self.identity = identity
        self.label = label
        self.definition = definition
    }

    enum CodingKeys: String, CodingKey { case identity, label, definition }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        identity = try c.decode(String.self, forKey: .identity)
        label = try? c.decodeIfPresent(Label.self, forKey: .label)
        definition = (try? c.decodeIfPresent([String: JSONValue].self, forKey: .definition)) ?? [:]
    }
}

public extension SubstrateClient {
    /// The kinds that bind `trait` and that the token reads, each with its
    /// declaration. An app's token is answered only the implementors its
    /// grant reaches, so the answer is what the app may show, never every
    /// kind with the trait.
    func implementors(of trait: String, timeout: TimeInterval = Timeout.read) async throws -> [KindInfo] {
        struct Answer: Decodable { var items: [KindInfo]? }
        let path = "api/v1/substrate.reamde.dev/core/trait/\(segment(trait))/implementors"
        return try await send("GET", path, timeout: timeout).decode(Answer.self).items ?? []
    }
}
