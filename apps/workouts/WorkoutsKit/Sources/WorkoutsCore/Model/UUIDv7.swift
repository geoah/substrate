import Foundation

/// RFC 9562 version 7 UUIDs, the ids the app mints for sessions, sets, rows
/// and events: they sort by time, and a record's id carries its own instant.
public enum UUIDv7 {
    /// A lowercase, hyphenated UUIDv7: 48-bit big-endian Unix milliseconds of
    /// `date`, version nibble 7, 12 random bits, variant `10`, 62 random bits.
    /// Tests pass a seeded generator so ids are reproducible.
    public static func make(at date: Date, using generator: inout some RandomNumberGenerator) -> String {
        let millis = UInt64(max(0, (date.timeIntervalSince1970 * 1000).rounded(.down)))
        let randA = UInt16(truncatingIfNeeded: generator.next()) & 0x0FFF
        let randB = generator.next() & 0x3FFF_FFFF_FFFF_FFFF

        var bytes = [UInt8](repeating: 0, count: 16)
        for index in 0 ..< 6 {
            bytes[index] = UInt8(truncatingIfNeeded: millis >> (8 * (5 - index)))
        }
        bytes[6] = 0x70 | UInt8(truncatingIfNeeded: randA >> 8)
        bytes[7] = UInt8(truncatingIfNeeded: randA)
        bytes[8] = 0x80 | UInt8(truncatingIfNeeded: randB >> 56)
        for index in 9 ..< 16 {
            bytes[index] = UInt8(truncatingIfNeeded: randB >> (8 * (15 - index)))
        }
        return format(bytes)
    }

    /// A UUIDv7 from the system's random generator.
    public static func make(at date: Date) -> String {
        var generator = SystemRandomNumberGenerator()
        return make(at: date, using: &generator)
    }

    /// The instant in a UUIDv7's first 48 bits, or nil when `id` is not a
    /// version 7 UUID (an importer's key, say).
    public static func date(of id: String) -> Date? {
        let hex = id.replacingOccurrences(of: "-", with: "")
        guard id.count == 36, hex.count == 32, hex.allSatisfy(\.isHexDigit) else { return nil }
        let version = hex[hex.index(hex.startIndex, offsetBy: 12)]
        guard version == "7", let millis = UInt64(hex.prefix(12), radix: 16) else { return nil }
        return Date(timeIntervalSince1970: Double(millis) / 1000)
    }

    private static func format(_ bytes: [UInt8]) -> String {
        let digits = Array("0123456789abcdef")
        var out = ""
        out.reserveCapacity(36)
        for (index, byte) in bytes.enumerated() {
            if index == 4 || index == 6 || index == 8 || index == 10 { out.append("-") }
            out.append(digits[Int(byte >> 4)])
            out.append(digits[Int(byte & 0x0F)])
        }
        return out
    }
}
