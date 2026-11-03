import Foundation
import SubstrateRuntime

/// Ids a test can name: `00000000-0000-7000-8000-000000000001`, then 2, 3.
public final class SequentialIDs: IDSource, @unchecked Sendable {
    private let lock = NSLock()
    private var count = 0

    public init() {}

    public func next() -> UUID {
        lock.lock()
        count += 1
        let number = count
        lock.unlock()
        return Self.id(number)
    }

    /// The id `next()` answers the `number`th time.
    public static func id(_ number: Int) -> UUID {
        let hex = String(number, radix: 16)
        let padded = String(repeating: "0", count: max(0, 12 - hex.count)) + hex
        return UUID(uuidString: "00000000-0000-7000-8000-\(padded)")!
    }
}
