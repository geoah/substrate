import Foundation
import SubstrateCore
import SubstrateDevice

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// A password sign-in's answer: the token record minted, its secret, the
/// repository's authority, and the name of the person it acts for, which a
/// server from before identities (decision 0274) does not send.
public struct LoginResult: Decodable, Sendable, Equatable {
    public struct Token: Decodable, Sendable, Equatable {
        public let id: String
    }

    public let token: Token
    public let secret: String
    public let repository: String
    public let identity: String?
}

public extension SubstrateClient {
    /// `POST /login`, for a server without app authorization. At the door a
    /// 401 is the name, the password or the code, not a sign-in ending; one
    /// that asks for a code (`LoginRefused.codeRequired`) is the first step
    /// of an account with a second factor, answered with the code. A person
    /// who may open several repositories and named none is refused with
    /// `RepositoryChoiceRequired`, which lists them: the same sign-in naming
    /// one, with the same factors, is the answer.
    ///
    /// `door` is the server's discovery, which says how the door names the
    /// person (`Discovery.doorNamesAPerson`): by `identity`, with the
    /// repository where the sign-in names one, or on an older server by
    /// `repository`, the name the person signs in with there.
    func login(
        name: String, repository: String? = nil, password: String, totp: String?, label: String, door: Discovery
    ) async throws -> LoginResult {
        var body: JSONObject = ["password": .string(password), "label": .string(label)]
        if door.doorNamesAPerson {
            body["identity"] = .string(name)
            if let repository, !repository.isEmpty { body["repository"] = .string(repository) }
        } else {
            body["repository"] = .string(name)
        }
        if let totp, !totp.isEmpty { body["totpCode"] = .string(totp) }
        let req = try request("POST", "login", body: .jsonValue(.object(body)), timeout: Timeout.quick, authorized: false)
        let response = try await send(req, accept: [401, 409])
        if response.status == 401 {
            throw LoginRefused(server: ServerError(status: 401, envelope: response.data))
        }
        if response.status == 409 {
            if let choice = RepositoryChoiceRequired(envelope: response.data) { throw choice }
            throw ServerError(status: 409, envelope: response.data)
                ?? ServerError(status: 409, message: "The sign-in was refused as a conflict.")
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
