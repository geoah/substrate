import Foundation

/// What the bridge counts and keeps of its traffic: the counters a contract
/// test reads, and the recent messages, redacted, for a problem report. A
/// message is recorded only through `Redaction`, so neither holds a secret.
public final class BridgeRecord: @unchecked Sendable {
    private let lock = NSLock()
    private var stats = BridgeStats()
    private var recent: [String] = []
    private let keep: Int
    private var writer: (@Sendable (String) -> Void)?

    init(keep: Int = 64) {
        self.keep = keep
    }

    /// Where a debuggable build writes each recorded message.
    var log: (@Sendable (String) -> Void)? {
        get { lock.withLock { writer } }
        set { lock.withLock { writer = newValue } }
    }

    var snapshot: BridgeStats { lock.withLock { stats } }

    /// The last messages recorded, oldest first, already redacted.
    public var messages: [String] { lock.withLock { recent } }

    func count(_ field: WritableKeyPath<BridgeStats, Int>) {
        lock.withLock { stats[keyPath: field] += 1 }
    }

    func record(_ direction: String, _ message: String, call: Redaction.Call) {
        let line = "\(direction) \(Redaction.redact(message, call: call))"
        lock.withLock {
            recent.append(line)
            if recent.count > keep { recent.removeFirst(recent.count - keep) }
        }
        log?(line)
    }
}
