import CryptoKit
import Foundation
import Security

/// PKCE (RFC 7636): a random verifier and its S256 challenge, so a code
/// caught on its way back is worth nothing without the verifier.
public struct PKCE: Sendable, Equatable {
    public let verifier: String
    public let challenge: String

    /// A fresh verifier from the system's random source. A source that fails
    /// throws: a verifier of zeros is one anybody can guess.
    public init() throws {
        try self.init(verifier: randomBase64URL(bytes: 32))
    }

    public init(verifier: String) {
        self.verifier = verifier
        challenge = base64URL(Data(SHA256.hash(data: Data(verifier.utf8))))
    }

    /// A fresh `state` for one authorization request.
    public static func state() throws -> String {
        try randomBase64URL(bytes: 16)
    }

    /// The system's random source refused.
    public struct RandomUnavailable: LocalizedError, Sendable {
        public let status: OSStatus
        public var errorDescription: String? { "This device couldn't make a secure random value (status \(status)). Try again." }
    }
}

func randomBase64URL(bytes count: Int) throws -> String {
    var bytes = [UInt8](repeating: 0, count: count)
    let status = SecRandomCopyBytes(kSecRandomDefault, count, &bytes)
    guard status == errSecSuccess else { throw PKCE.RandomUnavailable(status: status) }
    return base64URL(Data(bytes))
}

/// Base64url without padding (RFC 4648 §5).
func base64URL(_ data: Data) -> String {
    data.base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_")
        .replacingOccurrences(of: "=", with: "")
}
