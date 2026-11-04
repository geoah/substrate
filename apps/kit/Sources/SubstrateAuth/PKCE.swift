import Foundation

#if canImport(CryptoKit)
import CryptoKit
#else
import Crypto
#endif

/// PKCE (RFC 7636): a random verifier and its S256 challenge, so a code
/// caught on its way back is worth nothing without the verifier.
public struct PKCE: Sendable, Equatable {
    public let verifier: String
    public let challenge: String

    /// A fresh verifier from the system's cryptographically secure random
    /// source, which stops the process rather than answer a guessable value.
    public init() throws {
        self.init(verifier: randomBase64URL(bytes: 32))
    }

    public init(verifier: String) {
        self.verifier = verifier
        challenge = base64URL(Data(SHA256.hash(data: Data(verifier.utf8))))
    }

    /// A fresh `state` for one authorization request.
    public static func state() throws -> String {
        randomBase64URL(bytes: 16)
    }
}

func randomBase64URL(bytes count: Int) -> String {
    var generator = SystemRandomNumberGenerator()
    return base64URL(Data((0 ..< count).map { _ in UInt8.random(in: .min ... .max, using: &generator) }))
}

/// Base64url without padding (RFC 4648 §5).
func base64URL(_ data: Data) -> String {
    data.base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_")
        .replacingOccurrences(of: "=", with: "")
}
