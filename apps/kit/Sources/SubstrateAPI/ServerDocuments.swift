import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import SubstrateCore
import SubstrateDevice

public extension SubstrateClient {
    /// `GET /.well-known/substrate/server.json`. An answer that is not a JSON
    /// object is not a substrate's, whatever its status.
    func discovery() async throws -> Discovery {
        let response = try await send(request("GET", ".well-known/substrate/server.json", timeout: Timeout.quick, authorized: false))
        guard (try? JSONSerialization.jsonObject(with: response.data)) is [String: Any] else {
            throw URLError(.cannotParseResponse)
        }
        return try response.decode(Discovery.self)
    }

    /// `GET /.well-known/oauth-authorization-server`; nil when the server
    /// predates app authorization (a 404) and has only the password door.
    func authorizationServerMetadata() async throws -> AuthorizationServerMetadata? {
        let response = try await send(request("GET", ".well-known/oauth-authorization-server", timeout: Timeout.quick,
                                              authorized: false), accept: [404])
        if response.status == 404 { return nil }
        return try response.decode(AuthorizationServerMetadata.self)
    }
}
