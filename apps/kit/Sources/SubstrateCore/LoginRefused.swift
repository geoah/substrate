import Foundation

/// The password door refused a sign-in: the name, the password or the code.
public struct LoginRefused: LocalizedError, Sendable, Equatable {
    public let server: ServerError?
    public init(server: ServerError?) { self.server = server }
    public var errorDescription: String? { server?.text ?? "The repository name, password or code was not accepted." }

    /// The password was right and the account has a second factor: the same
    /// sign-in with a code from its authenticator succeeds.
    public var codeRequired: Bool { server?.code == "totp_required" }
}
