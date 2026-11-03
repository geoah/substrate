import Foundation

/// Where new ids come from: the environment's, so a test names each one.
public protocol IDSource: Sendable {
    func next() -> UUID
}

/// UUIDv7 ids (RFC 9562): the clock's millisecond first, so ids minted
/// later sort later, then random bits.
public struct TimeOrderedIDs: IDSource {
    public var clock: any RuntimeClock

    public init(clock: any RuntimeClock = WallClock()) {
        self.clock = clock
    }

    public func next() -> UUID {
        let millis = UInt64(max(0, clock.now.timeIntervalSince1970 * 1000))
        var generator = SystemRandomNumberGenerator()
        let random = (generator.next() as UInt64, generator.next() as UInt64)
        var bytes = [UInt8](repeating: 0, count: 16)
        for index in 0 ..< 6 { bytes[index] = UInt8(truncatingIfNeeded: millis >> (8 * (5 - UInt64(index)))) }
        for index in 6 ..< 16 {
            let word = index < 11 ? random.0 : random.1
            bytes[index] = UInt8(truncatingIfNeeded: word >> (8 * UInt64(index % 8)))
        }
        bytes[6] = 0x70 | (bytes[6] & 0x0F)
        bytes[8] = 0x80 | (bytes[8] & 0x3F)
        return UUID(uuid: (bytes[0], bytes[1], bytes[2], bytes[3], bytes[4], bytes[5], bytes[6], bytes[7],
                           bytes[8], bytes[9], bytes[10], bytes[11], bytes[12], bytes[13], bytes[14], bytes[15]))
    }
}
