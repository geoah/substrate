import Foundation

#if canImport(CryptoKit)
import CryptoKit
#else
import Crypto
#endif

/// Names one pushed alert by its event id, as the server and the relay do
/// (`pushwire.EventFingerprint`, whose vectors the kit's tests read): the
/// first 16 bytes of SHA-256 over a fixed prefix and the event id's
/// characters, in base64url without padding. A `read` lists the
/// fingerprints of the alerts it takes back, and the app computes the same
/// from the event id in each delivered notification, so it removes exactly
/// those, a fallback alert whose extension never ran among them.
public enum PushFingerprint {
    static let prefix = "substrate-push-event-fingerprint-v1\n"

    public static func of(eventID: String) -> String {
        base64URL(Data(SHA256.hash(data: Data((prefix + eventID).utf8)).prefix(16)))
    }
}
