import Foundation

/// The server answered with a redirect, which is never followed: what a
/// request carries (a token, a code and its verifier) goes to the server the
/// owner chose alone. `location` is where it pointed, as the server said it.
public struct Redirected: LocalizedError, Sendable, Equatable {
    public let status: Int
    public let location: String?

    public init(status: Int, location: String?) {
        self.status = status
        self.location = location
    }

    /// The host it pointed at, where the location names one.
    public var target: String? { location.flatMap { URL(string: $0)?.host() } }

    public var errorDescription: String? {
        "The server answered with a redirect\(target.map { " to \($0)" } ?? "") (\(status)), which is never followed."
    }
}
