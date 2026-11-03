import Foundation

/// What became of a send that started no turn, kept until the owner sends it
/// again or dismisses it: a refusal reads differently from an agent that
/// could not answer, and a send of unknown delivery differently from both.
struct Outcome: Codable, Hashable, Sendable, Identifiable {
    enum Kind: String, Codable, Hashable, Sendable {
        /// The server's own refusal: nothing ran.
        case refused
        /// Reading the thread to find out whether it arrived.
        case checking
        /// It may or may not have arrived.
        case unknown
    }

    var id: UUID
    var kind: Kind
    var reason: String
    var message: String
    /// The text went back into the composer.
    var restored = false
}
