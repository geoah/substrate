import Foundation
import SubstrateDevice

/// A TXT table, keyed by the name looked up, that keeps every name asked: a
/// name in the table answers its records, any other is confirmed absent, and
/// `answer` stands for every lookup where it is set. Every field is read and
/// written under `lock`.
public final class TableResolver: TXTResolver, @unchecked Sendable {
    private let lock = NSLock()
    private let table: [String: [String]]
    private let answer: TXTAnswer?
    private var names: [String] = []

    public init(_ table: [String: [String]] = [:], answer: TXTAnswer? = nil) {
        self.table = table
        self.answer = answer
    }

    public var asked: [String] {
        lock.lock()
        defer { lock.unlock() }
        return names
    }

    public func txt(_ name: String) async throws -> TXTAnswer {
        lock.withLock { names.append(name) }
        if let answer { return answer }
        return table[name].map(TXTAnswer.records) ?? .confirmedAbsent
    }
}
