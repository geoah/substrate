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
