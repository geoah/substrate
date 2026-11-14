import Foundation

#if canImport(CryptoKit)
import CryptoKit
#else
import Crypto
#endif

/// A record id every device derives alike from a name, so two devices that
/// create "the same" record name one row and the second create meets the
/// first (RFC 9562's UUIDv8, its name-based example): the first 128 bits of
/// the name's SHA-256, version 8, variant 10, as canonical lowercase text.
/// The web kit derives the same text from the same name.
public enum DerivedID {
    public static func uuid(_ name: String) -> String {
        var bytes = Array(SHA256.hash(data: Data(name.utf8)).prefix(16))
        bytes[6] = (bytes[6] & 0x0F) | 0x80
        bytes[8] = (bytes[8] & 0x3F) | 0x80
        let hex = bytes.map { String(format: "%02x", $0) }.joined()
        let parts = [0 ..< 8, 8 ..< 12, 12 ..< 16, 16 ..< 20, 20 ..< 32].map { range in
            String(hex[hex.index(hex.startIndex, offsetBy: range.lowerBound) ..< hex.index(hex.startIndex, offsetBy: range.upperBound)])
        }
        return parts.joined(separator: "-")
    }
}
