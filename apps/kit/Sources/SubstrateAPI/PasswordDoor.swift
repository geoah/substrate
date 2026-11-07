import Foundation
import SubstrateCore
import SubstrateDevice

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// A password sign-in's answer: the token record minted, its secret, and
/// the repository's authority.
public struct LoginResult: Decodable, Sendable, Equatable {
    public struct Token: Decodable, Sendable, Equatable {
        public let id: String
    }

    public let token: Token
    public let secret: String
    public let repository: String
}

public extension SubstrateClient {
    /// `POST /login`, for a server without app authorization. At the door a
    /// 401 is the name, the password or the code, not a sign-in ending; one
    /// that asks for a code (`LoginRefused.codeRequired`) is the first step
    /// of an account with a second factor, answered with the code.
    func login(repository: String, password: String, totp: String?, label: String) async throws -> LoginResult {
        var body: JSONObject = ["repository": .string(repository), "password": .string(password), "label": .string(label)]
        if let totp, !totp.isEmpty { body["totpCode"] = .string(totp) }
        let req = try request("POST", "login", body: .jsonValue(.object(body)), timeout: Timeout.quick, authorized: false)
        let response = try await send(req, accept: [401])
        if response.status == 401 {
            throw LoginRefused(server: ServerError(status: 401, envelope: response.data))
        }
        return try response.decode(LoginResult.self)
    }

    /// `DELETE /tokens/{id}`: a password session's own token. Best effort and
    /// quick: a sign-out never waits on a server that is gone or already
    /// refuses the token, and one already revoked is not an error.
    func revokeToken(id: String) async throws {
        try await send(request("DELETE", "tokens/\(segment(id))", timeout: 10), accept: [404], retries: 0)
    }
}
