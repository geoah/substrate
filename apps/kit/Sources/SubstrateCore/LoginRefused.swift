import Foundation

/// The password door refused a sign-in: the name, the password or the code.
public struct LoginRefused: LocalizedError, Sendable, Equatable {
    public let server: ServerError?
    public init(server: ServerError?) { self.server = server }
    public var errorDescription: String? { server?.text ?? "The repository name, password or code was not accepted." }
}
