import Foundation

#if canImport(CryptoKit)
import CryptoKit
#else
import Crypto
#endif

/// An id in the minted shape computed from parts, for a record the app must
/// name the same way again: the library's and the sample's records, and
/// what an import brings from Hevy, so an import run twice lands on the same
/// records. A server refuses a client's create under an id of another shape,
/// so a name is never slugged into an id.
///
/// The same function as substrate's `vocabulary.DerivedMintedID` and the
/// library script's `derived_minted_id`: the first 16 bytes of the SHA-256
/// of the parts joined by a NUL, marked as a UUIDv8, written as 26 lowercase
/// Crockford base32 characters. Its vectors are in `DerivedMintedIDTests`.
public enum DerivedMintedID {
    /// The minted shape: 26 characters, the first 0 to 7.
    public static let length = 26

    private static let alphabet = Array("0123456789abcdefghjkmnpqrstvwxyz")

    public static func make(_ parts: String...) -> String { make(parts) }

    public static func make(_ parts: [String]) -> String {
        let digest = SHA256.hash(data: Data(parts.joined(separator: "\u{0}").utf8))
        var bytes = Array(digest.prefix(16))
        bytes[6] = bytes[6] & 0x0F | 0x80  // version 8
        bytes[8] = bytes[8] & 0x3F | 0x80  // RFC 9562 variant
        return encode(bytes)
    }

    /// 16 bytes as 26 characters, most significant first: 130 bits, the two
    /// before the first byte zero.
    static func encode(_ bytes: [UInt8]) -> String {
        var out = ""
        out.reserveCapacity(length)
        for index in 0 ..< length {
            var value = 0
            for bit in index * 5 - 2 ..< index * 5 + 3 {
                value <<= 1
                if bit >= 0 { value |= Int(bytes[bit / 8] >> (7 - UInt8(bit % 8)) & 1) }
            }
            out.append(alphabet[value])
        }
        return out
    }
}
