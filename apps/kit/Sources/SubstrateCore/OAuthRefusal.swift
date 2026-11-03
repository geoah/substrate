import Foundation

/// The authorization server's refusal: RFC 6749's error object, or the
/// redirect's `error`.
public struct OAuthRefusal: LocalizedError, Sendable, Equatable {
    public let code: String
    public let text: String?

    public init(code: String, text: String?) {
        self.code = code
        self.text = text
    }

    public var errorDescription: String? { text ?? "The server refused the sign-in (\(code))." }

    /// The owner chose Deny.
    public var denied: Bool { code == "access_denied" }
}
