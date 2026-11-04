import Foundation

/// The token is good but its approval does not reach what was asked: the
/// server answered 403 with `insufficient_scope`. The owner approves more on
/// the approval page; signing in again would not change what they approved.
public struct InsufficientScope: LocalizedError, Sendable, Equatable {
    public let message: String?
    public init(message: String? = nil) { self.message = message }
    public var errorDescription: String? { "This needs your approval first." }
}
