import Foundation
import SubstrateRuntime

/// The app's UUIDv7 at an instant, its random bits drawn from the
/// environment's id source, so a test that names the ids names these too.
struct MintedIDs {
    let source: any IDSource

    func make(at instant: Date) -> String {
        var bits = Bits(source.next())
        return UUIDv7.make(at: instant, using: &bits)
    }

    private struct Bits: RandomNumberGenerator {
        var words: [UInt64]

        init(_ uuid: UUID) {
            let u = uuid.uuid
            let high = [u.0, u.1, u.2, u.3, u.4, u.5, u.6, u.7].reduce(UInt64(0)) { $0 << 8 | UInt64($1) }
            let low = [u.8, u.9, u.10, u.11, u.12, u.13, u.14, u.15].reduce(UInt64(0)) { $0 << 8 | UInt64($1) }
            // The first word gives UUIDv7 its 12 bits after the version, the
            // second its 62: a source that varies only its low bits still
            // varies both.
            words = [high ^ low, low]
        }

        mutating func next() -> UInt64 {
            words.isEmpty ? 0 : words.removeFirst()
        }
    }
}
