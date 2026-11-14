import Foundation

/// The parts a presentation is made of, each as last made and kept while
/// what it was made from is the same: the list, a window's search, a
/// conversation's screen, a live turn, a tool's row, Settings. A streamed
/// delta then makes its own conversation's live turn again and nothing
/// else, and a part made from the instant (a row's "now") is kept until the
/// instant it stops being true.
///
/// A part is named, and says what it reads as a value that compares: the
/// same reads answer the part kept, other reads make it again. The reads
/// must hold everything the part is made from. `verifies` holds a kept part
/// to that in tests: every part answered from the store is made again and
/// compared, and one that differs stops the process, naming the part.
final class PresenterParts: @unchecked Sendable {
    private struct Held {
        var reads: any Sendable
        var value: Any
        /// The part is true from `from` until `until`; nil where it reads
        /// no instant.
        var from: Date?
        var until: Date?
    }

    private let lock = NSLock()
    private var held: [String: Held] = [:]
    private var made: [String: Int] = [:]
    let verifies: Bool

    init(verifies: Bool = false) {
        self.verifies = verifies
    }

    /// The part `name`, kept while `reads` is what it was made from.
    func part<Reads: Equatable & Sendable, Value: Equatable>(_ name: String, reading reads: Reads, make: () -> Value) -> Value {
        if let hit = lock.withLock({ held[name] }), hit.reads as? Reads == reads, let value = hit.value as? Value {
            if verifies { verify(name, kept: value, fresh: make()) }
            return value
        }
        let value = make()
        lock.withLock {
            held[name] = Held(reads: reads, value: value)
            made[name, default: 0] += 1
        }
        return value
    }

    /// The same, for a part that reads the instant: `make` answers the part
    /// and when it stops being true as made, and the part is kept from the
    /// instant it was made at until then.
    func part<Reads: Equatable & Sendable, Value: Equatable>(
        _ name: String, reading reads: Reads, at now: Date, make: () -> (value: Value, until: Date)
    ) -> Value {
        timed(name, reading: reads, at: now, make: make).value
    }

    /// The same, with the instant the part answered stops being true, kept
    /// or made: for a part inside another that must be made again then.
    func timed<Reads: Equatable & Sendable, Value: Equatable>(
        _ name: String, reading reads: Reads, at now: Date, make: () -> (value: Value, until: Date)
    ) -> (value: Value, until: Date) {
        if let hit = lock.withLock({ held[name] }), hit.reads as? Reads == reads, let value = hit.value as? Value,
            let from = hit.from, let until = hit.until, from <= now, now < until
        {
            if verifies { verify(name, kept: value, fresh: make().value) }
            return (value, until)
        }
        let fresh = make()
        lock.withLock {
            held[name] = Held(reads: reads, value: fresh.value, from: now, until: fresh.until)
            made[name, default: 0] += 1
        }
        return fresh
    }

    /// The part `name`, made no sooner than `interval` after it was last
    /// made: asked sooner with other reads, it answers the part as it was
    /// and the instant it may be made again, when whoever draws it asks
    /// again. A clock that went back makes it at once.
    func part<Reads: Equatable & Sendable, Value: Equatable>(
        _ name: String, reading reads: Reads, at now: Date, every interval: TimeInterval, make: () -> Value
    ) -> (value: Value, due: Date?) {
        if let hit = lock.withLock({ held[name] }), let value = hit.value as? Value {
            if hit.reads as? Reads == reads {
                if verifies { verify(name, kept: value, fresh: make()) }
                return (value, nil)
            }
            if let made = hit.from, made <= now, now < made.addingTimeInterval(interval) {
                return (value, made.addingTimeInterval(interval))
            }
        }
        let value = make()
        lock.withLock {
            held[name] = Held(reads: reads, value: value, from: now)
            self.made[name, default: 0] += 1
        }
        return (value, nil)
    }

    /// Forgets the parts named under `prefix` that `stays` does not keep: a
    /// window closed, a conversation gone, a live turn its rows replaced.
    func keep(under prefix: String, where stays: (String) -> Bool) {
        lock.withLock {
            for name in held.keys where name.hasPrefix(prefix) && !stays(name) { held[name] = nil }
        }
    }

    /// How many times the parts named under `prefix` were made, kept ones
    /// not counted: what a test reads to say a delta made nothing else.
    func made(_ prefix: String) -> Int {
        lock.withLock { made.filter { $0.key.hasPrefix(prefix) }.reduce(0) { $0 + $1.value } }
    }

    private func verify<Value: Equatable>(_ name: String, kept: Value, fresh: Value) {
        precondition(kept == fresh, "The presenter kept the part \(name), which its reads no longer make: something it reads is missing from them.")
    }
}
