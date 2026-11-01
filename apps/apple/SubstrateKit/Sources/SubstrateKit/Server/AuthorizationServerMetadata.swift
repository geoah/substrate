import Foundation

/// `GET /.well-known/oauth-authorization-server` (RFC 8414), as a substrate
/// serves it (internal/api's authorize.go). A server that answers 404 has no
/// app authorization: only the password door.
public struct AuthorizationServerMetadata: Codable, Hashable, Sendable {
    public var issuer: String
    public var authorizationEndpoint: URL
    public var tokenEndpoint: URL
    public var revocationEndpoint: URL?
    public var responseTypesSupported: [String]
    public var responseModesSupported: [String]
    public var grantTypesSupported: [String]
    public var codeChallengeMethodsSupported: [String]
    public var tokenEndpointAuthMethodsSupported: [String]
    public var revocationEndpointAuthMethodsSupported: [String]
    /// The widest scope of each verb (`read:*`, `agents:*`): what verbs the
    /// server's grammar knows, never a list of every scope.
    public var scopesSupported: [String]
    /// The `authorization_details` types the server sets up on approval
    /// (RFC 9396). An older server ignores the parameter and approves the
    /// scopes alone, so an app sends details only to one that lists them.
    public var authorizationDetailsTypesSupported: [String]
    /// RFC 9207: the redirect names the issuer, and a redirect that does not
    /// is refused.
    public var authorizationResponseISSParameterSupported: Bool
    public var clientIDMetadataDocumentSupported: Bool

    enum CodingKeys: String, CodingKey {
        case issuer
        case authorizationEndpoint = "authorization_endpoint"
        case tokenEndpoint = "token_endpoint"
        case revocationEndpoint = "revocation_endpoint"
        case responseTypesSupported = "response_types_supported"
        case responseModesSupported = "response_modes_supported"
        case grantTypesSupported = "grant_types_supported"
        case codeChallengeMethodsSupported = "code_challenge_methods_supported"
        case tokenEndpointAuthMethodsSupported = "token_endpoint_auth_methods_supported"
        case revocationEndpointAuthMethodsSupported = "revocation_endpoint_auth_methods_supported"
        case scopesSupported = "scopes_supported"
        case authorizationDetailsTypesSupported = "authorization_details_types_supported"
        case authorizationResponseISSParameterSupported = "authorization_response_iss_parameter_supported"
        case clientIDMetadataDocumentSupported = "client_id_metadata_document_supported"
    }

    public init(issuer: String, authorizationEndpoint: URL, tokenEndpoint: URL, revocationEndpoint: URL? = nil,
                scopesSupported: [String] = [], authorizationDetailsTypesSupported: [String] = [],
                authorizationResponseISSParameterSupported: Bool = true)
    {
        self.issuer = issuer
        self.authorizationEndpoint = authorizationEndpoint
        self.tokenEndpoint = tokenEndpoint
        self.revocationEndpoint = revocationEndpoint
        responseTypesSupported = ["code"]
        responseModesSupported = ["query"]
        grantTypesSupported = ["authorization_code"]
        codeChallengeMethodsSupported = ["S256"]
        tokenEndpointAuthMethodsSupported = ["none"]
        revocationEndpointAuthMethodsSupported = ["none"]
        self.scopesSupported = scopesSupported
        self.authorizationDetailsTypesSupported = authorizationDetailsTypesSupported
        self.authorizationResponseISSParameterSupported = authorizationResponseISSParameterSupported
        clientIDMetadataDocumentSupported = true
    }

    /// The metadata a substrate at `issuer` serves, its endpoints at their
    /// fixed paths.
    public init(issuer server: URL, scopesSupported: [String], authorizationDetailsTypesSupported: [String] = []) {
        let base = server.absoluteString.trimmingTrailingSlashes
        self.init(issuer: base,
                  authorizationEndpoint: URL(string: base + "/oauth/authorize") ?? server,
                  tokenEndpoint: URL(string: base + "/oauth/token") ?? server,
                  revocationEndpoint: URL(string: base + "/oauth/revoke"),
                  scopesSupported: scopesSupported,
                  authorizationDetailsTypesSupported: authorizationDetailsTypesSupported)
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        issuer = try c.decode(String.self, forKey: .issuer)
        authorizationEndpoint = try c.decode(URL.self, forKey: .authorizationEndpoint)
        tokenEndpoint = try c.decode(URL.self, forKey: .tokenEndpoint)
        revocationEndpoint = c.value(.revocationEndpoint, or: nil)
        responseTypesSupported = c.value(.responseTypesSupported, or: [])
        responseModesSupported = c.value(.responseModesSupported, or: [])
        grantTypesSupported = c.value(.grantTypesSupported, or: [])
        codeChallengeMethodsSupported = c.value(.codeChallengeMethodsSupported, or: [])
        tokenEndpointAuthMethodsSupported = c.value(.tokenEndpointAuthMethodsSupported, or: [])
        revocationEndpointAuthMethodsSupported = c.value(.revocationEndpointAuthMethodsSupported, or: [])
        scopesSupported = c.value(.scopesSupported, or: [])
        authorizationDetailsTypesSupported = c.value(.authorizationDetailsTypesSupported, or: [])
        authorizationResponseISSParameterSupported = c.value(.authorizationResponseISSParameterSupported, or: false)
        clientIDMetadataDocumentSupported = c.value(.clientIDMetadataDocumentSupported, or: false)
    }

    /// The scope verbs the server's grammar knows: `read`, `write`,
    /// `agents`, `decide`.
    public var scopeVerbs: Set<String> {
        Set(scopesSupported.compactMap { $0.split(separator: ":", maxSplits: 1).first.map(String.init) })
    }

    public func supports(detailType: String) -> Bool { authorizationDetailsTypesSupported.contains(detailType) }

    /// Where a token is revoked: the advertised endpoint, else the path every
    /// substrate serves it at.
    public var revocation: URL {
        revocationEndpoint ?? URL(string: issuer.trimmingTrailingSlashes + "/oauth/revoke") ?? tokenEndpoint
    }
}

extension String {
    var trimmingTrailingSlashes: String {
        var s = self
        while s.hasSuffix("/") { s.removeLast() }
        return s
    }
}
