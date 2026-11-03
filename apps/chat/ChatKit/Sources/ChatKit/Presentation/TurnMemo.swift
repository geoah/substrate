import Foundation

/// The last transcript a conversation was presented with, kept while what
/// it was made from is the same: a streamed reply redraws its own turn, and
/// the rows above it are presented once per read.
final class TurnMemo: @unchecked Sendable {
    struct Made {
        var fingerprint: Int
        var turns: [TurnPresentation]
        var titles: [String: String]
    }

    private let lock = NSLock()
    private var made: [String: Made] = [:]

    func turns(for key: String, fingerprint: Int, make: () -> (turns: [TurnPresentation], titles: [String: String]))
        -> (turns: [TurnPresentation], titles: [String: String])
    {
        if let hit = lock.withLock({ made[key] }), hit.fingerprint == fingerprint { return (hit.turns, hit.titles) }
        let fresh = make()
        lock.withLock { made[key] = Made(fingerprint: fingerprint, turns: fresh.turns, titles: fresh.titles) }
        return fresh
    }
}
