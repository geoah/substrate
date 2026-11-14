import Foundation
import SubstrateCore
import Testing

/// The kit's half of the wire guard for a refusal. `ServerError(status:envelope:)` reads a refusal's body by hand; the
/// server's word is the console's two goldens, which Go writes: `wire.golden.json` from the structs internal/substrate
/// serializes (a field and whether it is always there), `errorcodes.golden.json` from the codes internal/api answers
/// with. What a reader recovers from a `410 compacted` with is the code, and the head and generation to start over from:
/// each must be a field Go sends, under that name, and the kit must read a refusal that leaves out what Go may leave out.
/// When this fails, fix the kit: the goldens are the server's word.
@Suite("The wire of a refusal")
struct WireGoldenTests {
    static let api = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
        .appendingPathComponent("../../../../web/console/src/lib/api").standardizedFileURL

    static func golden<T: Decodable>(_ name: String, as type: T.Type) throws -> T {
        try JSONDecoder().decode(T.self, from: Data(contentsOf: api.appendingPathComponent(name)))
    }

    @Test func aCompactedRefusalReadsByTheFieldsTheServerSends() throws {
        let codes = try Self.golden("errorcodes.golden.json", as: [String].self)
        #expect(codes.contains("compacted"))
        let wire = try Self.golden("wire.golden.json", as: [String: [String: Bool]].self)
        #expect(wire["ErrorEnvelope"] == ["error": true])
        let payload = try #require(wire["ErrorPayload"])
        // What a reader recovers with, and whether the server always sends it.
        #expect(payload["code"] == true)
        #expect(payload["head"] == false)
        #expect(payload["generation"] == false)

        func refusal(_ fields: [String: Any]) throws -> ServerError {
            let body = try JSONSerialization.data(withJSONObject: ["error": fields])
            return try #require(ServerError(status: 410, envelope: body))
        }
        let sent = try refusal(["code": "compacted", "message": "seq 3 is below the repository's horizon 9", "head": 12, "generation": "g1"])
        #expect(sent.isCompacted)
        #expect(sent.head == 12)
        #expect(sent.generation == "g1")

        // Only what the golden says is always there.
        let always = ["code": "compacted", "message": "a cursor the server can no longer resume"].filter { payload[$0.key] == true }
        let bare = try refusal(always)
        #expect(bare.isCompacted)
        #expect(bare.head == nil)
        #expect(bare.generation == nil)
    }
}
