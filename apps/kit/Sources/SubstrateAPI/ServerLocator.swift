import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import SubstrateCore
import SubstrateDevice

/// Finds the server behind what a person typed into the connect screen: a
/// server's address, or a repository's name, whose own DNS says where it is
/// served (docs/plans/authorities.md, decision 1). In order:
///
/// 1. the address itself, when it serves `/.well-known/substrate/server.json`;
/// 2. the `server=` of the TXT record at `_substrate.<name>`;
/// 3. the `server` of `https://<name>/.well-known/substrate/repository.json`;
/// 4. the parent host as a server, which covers a default name under a
///    server's host (`alice.substrate.example.org`) without a record: whoever
///    controls the parent controls its subdomains.
///
/// Steps 2 to 4 apply only to a bare domain name, or one typed as an
/// `https://` origin with nothing after its host, and the result carries
/// that name as the repository the sign-in asks for and holds the token to.
/// Steps 3 and 4 run only after the lookup answered that the name has no
/// `v=substrate1` record: a lookup that failed stops here, since the record
/// it missed may name another server than the web host or the parent would.
public struct ServerLocator: Sendable {
    public var probe: ServerProbe
    public var resolver: any TXTResolver

    public init(probe: ServerProbe = ServerProbe(), resolver: any TXTResolver) {
        self.probe = probe
        self.resolver = resolver
    }

    /// The path a repository's own web host serves its server at, the
    /// fallback for a name whose DNS carries no record.
    public static let repositoryDocumentPath = "/.well-known/substrate/repository.json"

    /// Nothing was found where the steps looked.
    public struct NotFound: LocalizedError, Sendable {
        public let name: String
        public let parent: String?
        public var errorDescription: String? {
            var text = "\(name) is not a Substrate server, and it does not say where its repository is served: "
                + "it has no _substrate record and no \(ServerLocator.repositoryDocumentPath)."
            if let parent { text += " \(parent) is not a Substrate server either." }
            return text
        }
    }

    /// The lookup of the name's record failed (a timeout, SERVFAIL): it
    /// said nothing, so nothing after it may stand in for it.
    public struct LookupFailed: LocalizedError, Sendable {
        /// `_substrate.<name>`.
        public let query: String
        public let underlying: String
        /// The system resolver said the name has no record and the plain
        /// resolver could not confirm it, as a VPN, an encrypted DNS profile
        /// or iCloud Private Relay can leave it.
        public var unconfirmedNegative = false

        public var errorDescription: String? {
            "The DNS lookup for \(query) failed, so the server it names is unknown. Check your connection and try again."
        }
    }

    /// The name's record answered and names no server, such as a publisher's
    /// record carrying only `doc=`.
    public struct NoServerInRecord: LocalizedError, Sendable {
        public let name: String
        public var errorDescription: String? {
            "The _substrate record of \(name) names no server. Type your server's address instead."
        }
    }

    /// What was typed is neither an address nor a name.
    public struct Unreadable: LocalizedError, Sendable {
        public var errorDescription: String? { "That is not a server address or a repository name." }
    }

    /// `repository.json`: the server holding the repository of the name that
    /// serves it.
    struct RepositoryDocument: Decodable {
        var server: String
    }

    /// The server `typed` names, probed and judged against `requirement`,
    /// with `repository` set when it was found from a repository's name.
    public func locate(_ typed: String, requirement: ServerRequirement, app: String) async throws -> ServerProbe.Result {
        guard let origin = ServerAddress.origin(of: typed) else { throw Unreadable() }
        do {
            return try await probe.probe(origin, requirement: requirement, app: app)
        } catch let notASubstrate as ServerProbe.NotASubstrate {
            guard let name = Self.repositoryName(typed) else { throw notASubstrate }
            return try await locate(repository: name, requirement: requirement, app: app)
        }
    }

    /// Steps 2 to 4 for a name that is not a server itself.
    ///
    /// A negative answer the plain resolver could not confirm skips the web
    /// host, which only a confirmed absence lets answer, and still probes the
    /// parent: whoever controls the parent's zone controls the name, so the
    /// parent is not a weaker answer than the record it may have missed, and
    /// a default name needs no record at all. Only when the parent is no
    /// substrate does the failed lookup stand.
    func locate(repository name: String, requirement: ServerRequirement, app: String) async throws -> ServerProbe.Result {
        var unconfirmed: LookupFailed?
        do {
            if let server = try await recordServer(name) {
                return try await found(server, repository: name, requirement: requirement, app: app)
            }
        } catch let failed as LookupFailed where failed.unconfirmedNegative {
            unconfirmed = failed
        }
        if unconfirmed == nil, let server = try await documentServer(name) {
            return try await found(server, repository: name, requirement: requirement, app: app)
        }
        let parent = Self.parent(of: name)
        if let parent, let parentOrigin = URL(string: "https://\(parent)") {
            do {
                return try await found(parentOrigin, repository: name, requirement: requirement, app: app)
            } catch is ServerProbe.NotASubstrate {}
        }
        if let unconfirmed { throw unconfirmed }
        throw NotFound(name: name, parent: parent)
    }

    /// Step 2: the record's `server=`, or nil only when the lookup answered
    /// that the name has no `v=substrate1` record. DNS wins over the web host,
    /// so whatever else the lookup comes to stops discovery: a failed lookup
    /// (`LookupFailed`), a record that cannot be read (`Malformed`) and one
    /// that names no server (`NoServerInRecord`).
    func recordServer(_ name: String) async throws -> URL? {
        guard let query = DomainRecord.queryName(for: name) else { return nil }
        let answer: TXTAnswer
        do {
            answer = try await resolver.txt(query)
        } catch {
            if isCancellation(error) { throw error }
            throw LookupFailed(query: query, underlying: (error as? LocalizedError)?.errorDescription ?? "\(error)")
        }
        let txts: [String]
        switch answer {
        case let .records(records): txts = records
        case .confirmedAbsent: return nil
        case let .unconfirmedAbsent(words):
            var failed = LookupFailed(query: query, underlying: words)
            failed.unconfirmedNegative = true
            throw failed
        case let .failed(words): throw LookupFailed(query: query, underlying: words)
        }
        guard let record = try DomainRecord.parse(txts) else { return nil }
        guard let server = record.server, let url = URL(string: server) else { throw NoServerInRecord(name: name) }
        return url
    }

    /// Step 3: `repository.json` on the name's own host, its `server` held
    /// to the record's rule (an https origin). Anything but a 200 with that
    /// is no answer; the transport never follows a redirect.
    func documentServer(_ name: String) async throws -> URL? {
        guard let url = URL(string: "https://\(name)\(Self.repositoryDocumentPath)") else { return nil }
        var request = URLRequest(url: url)
        request.timeoutInterval = SubstrateClient.Timeout.quick
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        do {
            let (data, response) = try await probe.transport.data(for: request)
            guard response.statusCode == 200,
                  let document = try? JSONDecoder().decode(RepositoryDocument.self, from: data),
                  let origin = DomainRecord.httpsOrigin(document.server)
            else { return nil }
            return URL(string: origin)
        } catch {
            if isCancellation(error) { throw error }
            return nil
        }
    }

    private func found(_ server: URL, repository: String, requirement: ServerRequirement, app: String) async throws -> ServerProbe.Result {
        var result = try await probe.probe(server, requirement: requirement, app: app)
        result.repository = repository
        return result
    }

    /// The repository name `typed` could be: a bare domain name of two
    /// labels or more, lower-cased, without a trailing dot, or that name as
    /// an `https://` origin with nothing after its host, the way a browser
    /// shows it. Nil for any other scheme, a port, a path or a user, for a
    /// loopback host and for an IP address, which are servers or nothing.
    public static func repositoryName(_ typed: String) -> String? {
        var name = typed.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if name.hasPrefix("https://") {
            name.removeFirst("https://".count)
            if name.hasSuffix("/") { name.removeLast() }
        }
        if name.hasSuffix(".") { name.removeLast() }
        guard !name.isEmpty, name.count <= 253, name.contains("."),
              name.allSatisfy({ $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == "." || $0 == "_") }),
              !name.split(separator: ".", omittingEmptySubsequences: false).contains(where: \.isEmpty),
              name != "localhost",
              !name.split(separator: ".").allSatisfy({ $0.allSatisfy(\.isNumber) })
        else { return nil }
        return name
    }

    /// The name without its first label, while two labels or more remain:
    /// `substrate.example.org` for `alice.substrate.example.org`, nil for
    /// `example.org`, whose parent is a top-level domain.
    public static func parent(of name: String) -> String? {
        let labels = name.split(separator: ".")
        guard labels.count > 2 else { return nil }
        return labels.dropFirst().joined(separator: ".")
    }
}
