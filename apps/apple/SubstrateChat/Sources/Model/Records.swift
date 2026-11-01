import Foundation
import SubstrateKit

/// The kinds this app reads and writes, each named in full on every surface
/// (decision 0101). The agent row is core's; the runtime rows it produces are
/// the seeded `llm` package's (decision 0077).
enum Kinds {
    static let core = "substrate.reamde.dev/core"
    static let llm = "substrate.reamde.dev/llm"
    static let agent = "\(core)/agent"
    static let request = "\(core)/recordpatchrequest"
    static let provider = "\(llm)/provider"
    static let thread = "\(llm)/thread"
    static let message = "\(llm)/message"
    static let interaction = "\(llm)/interaction"
    /// The interaction kind before record 0077 moved it out of core: a
    /// stamp written then still spells it, and must still find its card.
    static let interactionBeforeMove = "\(core)/interaction"
}

/// One page of `GET /api/v1/records`. `head` and `generation` are the change
/// feed's handoff: a watch resumed from them sees every write after the read.
struct Page: Decodable, Sendable {
    var records: [Record]
    var cursor: String?
    var head: Int
    var generation: String

    enum CodingKeys: String, CodingKey { case records, cursor, head, generation }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        records = try c.decodeIfPresent([Record].self, forKey: .records) ?? []
        cursor = try? c.decodeIfPresent(String.self, forKey: .cursor)
        head = (try? c.decodeIfPresent(Int.self, forKey: .head)) ?? 0
        generation = (try? c.decodeIfPresent(String.self, forKey: .generation)) ?? ""
    }
}
