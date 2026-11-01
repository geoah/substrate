import Foundation

/// Asks a server what it is before an app signs in to it or tells the owner
/// it is out of date: its discovery document, and its OAuth metadata.
public struct ServerProbe: Sendable {
    public var transport: any HTTPTransport

    public init(transport: any HTTPTransport = URLSessionTransport.shared) {
        self.transport = transport
    }

    /// What a server answered.
    public struct Result: Hashable, Sendable {
        public var origin: URL
        public var discovery: Discovery
        /// Nil when the server has no app authorization: its metadata
        /// answered 404, and only the password door is open.
        public var metadata: AuthorizationServerMetadata?
        public var verdict: ServerRequirement.Verdict

        public init(origin: URL, discovery: Discovery, metadata: AuthorizationServerMetadata?,
                    verdict: ServerRequirement.Verdict)
        {
            self.origin = origin
            self.discovery = discovery
            self.metadata = metadata
            self.verdict = verdict
        }

        public var host: String { ServerAddress.display(origin) }
        public var version: String? { discovery.server.version }
    }

    /// The server did not answer as a substrate: the address names something
    /// else, or nothing.
    public struct NotASubstrate: LocalizedError, Sendable {
        public let origin: URL
        public let reason: String
        public var errorDescription: String? {
            "\(ServerAddress.display(origin)) did not answer as a Substrate server. \(reason)"
        }
    }

    /// Reads discovery, then the metadata, and judges the server against
    /// `requirement`. A discovery read that fails is `NotASubstrate`, with
    /// `app`'s words for why. Metadata whose issuer is not `origin`, or whose
    /// endpoints are elsewhere, is `ServerMismatch`: an app signing in
    /// through it would hand the code and its verifier to another server.
    public func probe(_ origin: URL, requirement: ServerRequirement, app: String) async throws -> Result {
        let client = SubstrateClient(server: origin, transport: transport)
        let discovery: Discovery
        do {
            discovery = try await client.discovery()
        } catch {
            if isCancellation(error) { throw error }
            throw NotASubstrate(origin: origin, reason: explain(error, app: app, server: origin))
        }
        let metadata = try await client.authorizationServerMetadata()
        if let problem = metadata?.mismatch(server: origin) { throw ServerMismatch(problem) }
        return Result(origin: origin, discovery: discovery, metadata: metadata,
                      verdict: requirement.verdict(version: discovery.server.version, metadata: metadata))
    }
}
