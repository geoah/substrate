import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import SubstrateCore
import SubstrateDevice

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
    /// 401 is the name, the password or the code, not a sign-in ending.
    func login(repository: String, password: String, totp: String?, label: String) async throws -> LoginResult {
        var body: JSONObject = ["repository": .string(repository), "password": .string(password), "label": .string(label)]
        if let totp, !totp.isEmpty { body["totpCode"] = .string(totp) }
        let req = try request("POST", "login", body: .jsonValue(.object(body)), timeout: Timeout.quick, authorized: false)
        do {
            return try await send(req).decode(LoginResult.self)
        } catch let expired as SessionExpired {
            throw LoginRefused(server: expired.message.map { ServerError(status: 401, message: $0) })
        }
    }

    /// `DELETE /tokens/{id}`: a password session's own token. Best effort and
    /// quick: a sign-out never waits on a server that is gone or already
    /// refuses the token, and one already revoked is not an error.
    func revokeToken(id: String) async throws {
        try await send(request("DELETE", "tokens/\(segment(id))", timeout: 10), accept: [404], retries: 0)
    }
}
