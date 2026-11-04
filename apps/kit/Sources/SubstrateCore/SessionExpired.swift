import Foundation

/// The server no longer accepts the token: revoked, expired, or the
/// repository was reset. Signing in again is the way out.
public struct SessionExpired: LocalizedError, Sendable, Equatable {
    public let message: String?
    public init(message: String? = nil) { self.message = message }
    public var errorDescription: String? { "Your sign-in is no longer valid. Sign in again." }
}
