import Foundation
import Testing
@testable import SubstrateKit

/// The cases internal/domains/record_test.go holds the server's parser to,
/// so the app reads one record as the server does.
@Suite struct DomainRecords {
    @Test func readsTheKeysItKnowsAndKeepsTheRest() throws {
        let record = try #require(try DomainRecord.parse([
            "google-site-verification=abc",
            "v=substrate1 server=https://Substrate.Example.com/ proof=k3j9x2 doc=https://cdn.example.net/a.json sha256="
                + "9F86D081884C7D659A2FEAA0C55AD015A3BF4F1B2B0B822CD15D6C15B0F00A08 future=yes",
        ]))
        #expect(record.server == "https://substrate.example.com")
        #expect(record.proof == "k3j9x2")
        #expect(record.doc == "https://cdn.example.net/a.json")
        #expect(record.sha256 == "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08")
        #expect(record.other == ["future": "yes"])
        #expect(try DomainRecord.parse(["v=substrate1 server=https://a.example.org:8443"])?.server == "https://a.example.org:8443")
    }

    @Test func findsNothingWithoutTheVersion() throws {
        for txts in [[], ["v=spf1 -all"], ["v=substrate10 server=https://a.example"], ["v=substrate1x"]] {
            #expect(try DomainRecord.parse(txts) == nil, "\(txts)")
        }
    }

    @Test func refusesWhatCannotBeRead() {
        let cases: [String: [String]] = [
            "two records": ["v=substrate1 server=https://a.example", "v=substrate1 server=https://b.example"],
            "no value": ["v=substrate1 server="],
            "not key=value": ["v=substrate1 server"],
            "a key twice": ["v=substrate1 proof=a proof=b"],
            "http server": ["v=substrate1 server=http://a.example"],
            "server with a path": ["v=substrate1 server=https://a.example/repo"],
            "server with a query": ["v=substrate1 server=https://a.example?x=1"],
            "server with a user": ["v=substrate1 server=https://ada@a.example"],
            "http doc": ["v=substrate1 doc=http://a.example/doc.json"],
            "doc with a fragment": ["v=substrate1 doc=https://a.example/doc.json#top"],
            "short sha256": ["v=substrate1 doc=https://a.example/d.json sha256=abcd"],
            "sha256 not hex": ["v=substrate1 doc=https://a.example/d.json sha256=" + String(repeating: "g", count: 64)],
            "sha256 without doc": ["v=substrate1 sha256=9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"],
        ]
        for (name, txts) in cases {
            #expect(throws: DomainRecord.Malformed.self, "\(name)") { try DomainRecord.parse(txts) }
        }
    }

    @Test func asksOnlyTheNamesOwnLabel() {
        #expect(DomainRecord.queryName(for: "Example.org.") == "_substrate.example.org")
        for bad in ["", "localhost", "https://example.org", "a b.example", "ada@example.org", "example.org:443"] {
            #expect(DomainRecord.queryName(for: bad) == nil, "\(bad)")
        }
    }

    /// RFC 1035's TXT rdata: each character-string carries its length, and
    /// one record's strings are one value.
    @Test func joinsARecordSplitAcrossStrings() {
        let first = Array("v=substrate1 server=".utf8)
        let second = Array("https://a.example.org".utf8)
        let rdata = [UInt8(first.count)] + first + [UInt8(second.count)] + second
        #expect(DNSServiceTXTResolver.text(rdata) == "v=substrate1 server=https://a.example.org")
        #expect(DNSServiceTXTResolver.text([]) == "")
        // A length past the end reads what is there.
        #expect(DNSServiceTXTResolver.text([9] + Array("abc".utf8)) == "abc")
    }
}

/// A TXT table, keyed by the name looked up, that keeps every name asked.
final class TableResolver: TXTResolver, @unchecked Sendable {
    private let lock = NSLock()
    private let table: [String: [String]]
    private let failure: Error?
    private var names: [String] = []

    init(_ table: [String: [String]] = [:], failure: Error? = nil) {
        self.table = table
        self.failure = failure
    }

    var asked: [String] {
        lock.lock()
        defer { lock.unlock() }
        return names
    }

    func txt(_ name: String) async throws -> [String]? {
        lock.lock()
        names.append(name)
        lock.unlock()
        if let failure { throw failure }
        return table[name]
    }
}

/// Servers by host: each answers its discovery and its metadata, issued
/// under its own origin, and a host may serve `repository.json` instead.
final class HostTransport: HTTPTransport, @unchecked Sendable {
    private let lock = NSLock()
    private var routes: [String: (Int, String)] = [:]
    private var sentTo: [String] = []

    @discardableResult
    func substrate(_ host: String) -> HostTransport {
        on(host, "/.well-known/substrate/server.json", json: discoveryJSON())
        return on(host, "/.well-known/oauth-authorization-server", json: metadataJSON(issuer: "https://\(host)"))
    }

    @discardableResult
    func on(_ host: String, _ path: String, status: Int = 200, json: String) -> HostTransport {
        lock.lock()
        defer { lock.unlock() }
        routes["\(host)\(path)"] = (status, json)
        return self
    }

    var requested: [String] {
        lock.lock()
        defer { lock.unlock() }
        return sentTo
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        guard let url = request.url, let host = url.host(percentEncoded: false) else { throw URLError(.badURL) }
        let key = "\(host)\(url.path(percentEncoded: true))"
        lock.lock()
        sentTo.append(key)
        let answer = routes[key]
        lock.unlock()
        // A host that serves nothing answers as a web server that is not a
        // substrate: a page.
        let (status, body) = answer ?? (404, "<html>not found</html>")
        return (Data(body.utf8), HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: [:])!)
    }
}

@Suite struct Locating {
    let requirement = ServerRequirement()

    func locator(_ transport: HostTransport, _ resolver: TableResolver) -> ServerLocator {
        ServerLocator(probe: ServerProbe(transport: transport), resolver: resolver)
    }

    @Test func aServerIsAServerAndNoRepository() async throws {
        let transport = HostTransport().substrate("substrate.example.org")
        let resolver = TableResolver()
        let found = try await locator(transport, resolver).locate("substrate.example.org", requirement: requirement, app: "App")
        #expect(found.origin.absoluteString == "https://substrate.example.org")
        #expect(found.repository == nil)
        #expect(resolver.asked.isEmpty)
    }

    @Test func aNamesRecordNamesItsServer() async throws {
        let transport = HostTransport().substrate("substrate.example.org")
        let resolver = TableResolver(["_substrate.ada.example.com": ["v=substrate1 server=https://substrate.example.org"]])
        let found = try await locator(transport, resolver).locate("Ada.Example.com", requirement: requirement, app: "App")
        #expect(found.origin.absoluteString == "https://substrate.example.org")
        #expect(found.repository == "ada.example.com")
        #expect(resolver.asked == ["_substrate.ada.example.com"])
        // The name was asked as a server first, and its web host never.
        #expect(transport.requested.first == "ada.example.com/.well-known/substrate/server.json")
        #expect(!transport.requested.contains("ada.example.com\(ServerLocator.repositoryDocumentPath)"))
    }

    /// atproto's order: DNS wins when the record and the web host disagree.
    @Test func theRecordWinsOverTheWebHost() async throws {
        let transport = HostTransport().substrate("substrate.example.org").substrate("other.example.net")
            .on("ada.example.com", ServerLocator.repositoryDocumentPath, json: #"{"server":"https://other.example.net"}"#)
        let resolver = TableResolver(["_substrate.ada.example.com": ["v=substrate1 server=https://substrate.example.org"]])
        let found = try await locator(transport, resolver).locate("ada.example.com", requirement: requirement, app: "App")
        #expect(found.origin.absoluteString == "https://substrate.example.org")
    }

    @Test func withoutARecordTheWebHostNamesTheServer() async throws {
        let transport = HostTransport().substrate("substrate.example.org")
            .on("ada.example.com", ServerLocator.repositoryDocumentPath, json: #"{"server":"https://substrate.example.org/"}"#)
        // No record: NXDOMAIN, or TXT strings none of which is ours.
        for resolver in [TableResolver(), TableResolver(["_substrate.ada.example.com": ["v=spf1 -all"]])] {
            let found = try await locator(transport, resolver).locate("ada.example.com", requirement: requirement, app: "App")
            #expect(found.origin.absoluteString == "https://substrate.example.org")
            #expect(found.repository == "ada.example.com")
        }
    }

    /// A lookup that failed said nothing, so neither the web host nor the
    /// parent may answer for the record it missed.
    @Test func aFailedLookupStopsDiscovery() async throws {
        let failures: [Error] = [
            URLError(.timedOut),
            // SERVFAIL, as DNS-SD reports it.
            DNSServiceTXTResolver.Failed(name: "_substrate.alice.substrate.example.org", code: -65537),
        ]
        for failure in failures {
            let transport = HostTransport().substrate("substrate.example.org").substrate("other.example.net")
                .on("alice.substrate.example.org", ServerLocator.repositoryDocumentPath, json: #"{"server":"https://other.example.net"}"#)
            do {
                _ = try await locator(transport, TableResolver(failure: failure))
                    .locate("alice.substrate.example.org", requirement: requirement, app: "App")
                Issue.record("a failed lookup fell through to another server: \(failure)")
            } catch let failed as ServerLocator.LookupFailed {
                #expect(failed.query == "_substrate.alice.substrate.example.org")
                #expect(failed.errorDescription?.contains("The DNS lookup for _substrate.alice.substrate.example.org failed") == true)
            }
            #expect(!transport.requested.contains("alice.substrate.example.org\(ServerLocator.repositoryDocumentPath)"))
            #expect(!transport.requested.contains("substrate.example.org/.well-known/substrate/server.json"))
        }
    }

    /// A record that answers and names no server (a publisher's `doc=`
    /// alone) is DNS's answer too, not an absence.
    @Test func aRecordWithoutAServerStopsDiscovery() async throws {
        let transport = HostTransport().substrate("substrate.example.org")
            .on("ada.example.com", ServerLocator.repositoryDocumentPath, json: #"{"server":"https://substrate.example.org"}"#)
        let resolver = TableResolver(["_substrate.ada.example.com": ["v=substrate1 doc=https://ada.example.com/a.json"]])
        await #expect(throws: ServerLocator.NoServerInRecord.self) {
            try await locator(transport, resolver).locate("ada.example.com", requirement: requirement, app: "App")
        }
        #expect(!transport.requested.contains("ada.example.com\(ServerLocator.repositoryDocumentPath)"))
    }

    @Test func aDocumentServerMustBeAnHTTPSOrigin() async throws {
        let transport = HostTransport().substrate("substrate.example.org")
            .on("ada.example.com", ServerLocator.repositoryDocumentPath, json: #"{"server":"http://substrate.example.org"}"#)
        await #expect(throws: ServerLocator.NotFound.self) {
            try await locator(transport, TableResolver()).locate("ada.example.com", requirement: requirement, app: "App")
        }
    }

    /// A default name under a server's host needs no record: the parent is
    /// the server.
    @Test func theParentHostIsProbedLast() async throws {
        let transport = HostTransport().substrate("substrate.example.org")
        let resolver = TableResolver()
        let found = try await locator(transport, resolver).locate("alice.substrate.example.org", requirement: requirement, app: "App")
        #expect(found.origin.absoluteString == "https://substrate.example.org")
        #expect(found.repository == "alice.substrate.example.org")
        #expect(resolver.asked == ["_substrate.alice.substrate.example.org"])
        #expect(transport.requested.prefix(3) == [
            "alice.substrate.example.org/.well-known/substrate/server.json",
            "alice.substrate.example.org\(ServerLocator.repositoryDocumentPath)",
            "substrate.example.org/.well-known/substrate/server.json",
        ])
    }

    @Test func nothingFoundSaysWhereItLooked() async throws {
        let transport = HostTransport()
        do {
            _ = try await locator(transport, TableResolver()).locate("ada.example.com", requirement: requirement, app: "App")
            Issue.record("a name with nothing behind it was found")
        } catch let notFound as ServerLocator.NotFound {
            #expect(notFound.name == "ada.example.com")
            #expect(notFound.parent == "example.com")
            #expect(transport.requested.contains("example.com/.well-known/substrate/server.json"))
        }
        // A name of two labels has no parent to probe: it would be a
        // top-level domain.
        do {
            _ = try await locator(transport, TableResolver()).locate("example.org", requirement: requirement, app: "App")
            Issue.record("a name with nothing behind it was found")
        } catch let notFound as ServerLocator.NotFound {
            #expect(notFound.parent == nil)
            #expect(!transport.requested.contains("org/.well-known/substrate/server.json"))
        }
    }

    /// A record that says something unreadable is said, never skipped for
    /// the web host, which DNS outranks.
    @Test func aMalformedRecordIsSaid() async throws {
        let transport = HostTransport().substrate("substrate.example.org")
            .on("ada.example.com", ServerLocator.repositoryDocumentPath, json: #"{"server":"https://substrate.example.org"}"#)
        let resolver = TableResolver(["_substrate.ada.example.com": ["v=substrate1 server=http://substrate.example.org"]])
        await #expect(throws: DomainRecord.Malformed.self) {
            try await locator(transport, resolver).locate("ada.example.com", requirement: requirement, app: "App")
        }
    }

    /// An address with a scheme, a port or a path is a server or nothing.
    @Test func anAddressIsNeverReadAsAName() async throws {
        for typed in ["https://ada.example.com", "ada.example.com:8443", "ada.example.com/x", "127.0.0.1", "localhost"] {
            let resolver = TableResolver(["_substrate.ada.example.com": ["v=substrate1 server=https://substrate.example.org"]])
            await #expect(throws: ServerProbe.NotASubstrate.self, "\(typed)") {
                try await locator(HostTransport().substrate("substrate.example.org"), resolver)
                    .locate(typed, requirement: requirement, app: "App")
            }
            #expect(resolver.asked.isEmpty, "\(typed)")
        }
    }

    @Test func namesAndParents() {
        #expect(ServerLocator.repositoryName(" Ada.Example.com. ") == "ada.example.com")
        for typed in ["https://ada.example.com", "ada.example.com:443", "ada", "localhost", "10.0.0.1", "a..b", "ada@example.com"] {
            #expect(ServerLocator.repositoryName(typed) == nil, "\(typed)")
        }
        #expect(ServerLocator.parent(of: "alice.substrate.example.org") == "substrate.example.org")
        #expect(ServerLocator.parent(of: "example.org") == nil)
    }
}

@Suite struct ClaimedCallback {
    let redirect = URL(string: "https://substrate.example.org/apps/workouts/callback")!

    @Test func aReleaseClaimsItsRedirectAndALocalBuildDoesNot() {
        #expect(AppManifest.claimedRedirect("https://substrate.example.org/apps/workouts/callback") == redirect)
        for text in ["", "http://substrate.example.org/apps/workouts/callback", "https://substrate.example.org",
                     "https://substrate.example.org/", "https://substrate.example.org/cb?x=1", "dev.reamde.substrate:/cb"]
        {
            #expect(AppManifest.claimedRedirect(text) == nil, "\(text)")
        }
        let release = AppManifest(appName: "Workouts", clientID: "https://substrate.example.org/apps/workouts/client.json",
                                  scopes: [], claimedRedirect: redirect)
        #expect(release.signInRedirect == redirect)
        // A loopback client id has no document to list the redirect in.
        let local = AppManifest(appName: "Workouts", clientID: "http://127.0.0.1/substrate-workouts", scopes: [],
                                claimedRedirect: redirect)
        #expect(local.signInRedirect == nil)
    }

    @Test func theCallbackIsReadOnlyFromTheClaimedAddress() throws {
        let back = try #require(URL(string: "https://substrate.example.org/apps/workouts/callback?code=c&state=s&iss=https%3A%2F%2Fada.example.com"))
        #expect(try WebAuthenticator.query(back, redirect: redirect) == ["code": "c", "state": "s", "iss": "https://ada.example.com"])
        for other in ["https://evil.example.com/apps/workouts/callback?code=c",
                      "https://substrate.example.org/apps/chat/callback?code=c",
                      "http://substrate.example.org/apps/workouts/callback?code=c",
                      "https://substrate.example.org:8443/apps/workouts/callback?code=c"]
        {
            #expect(throws: OAuthRefusal.self, "\(other)") { try WebAuthenticator.query(URL(string: other)!, redirect: redirect) }
        }
    }

    @Test func theAuthorizeURLHintsTheRepository() throws {
        let meta = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(metadataJSON().utf8))
        let oauth = OAuthClient(manifest: syncManifest(), metadata: meta)
        let url = try oauth.authorizeURL(redirect: redirect.absoluteString, pkce: PKCE(verifier: "v"), state: "s",
                                         loginHint: "ada.example.com")
        let items = Dictionary(uniqueKeysWithValues: (URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? [])
            .map { ($0.name, $0.value ?? "") })
        #expect(items["login_hint"] == "ada.example.com")
        #expect(items["redirect_uri"] == redirect.absoluteString)
        let bare = try oauth.authorizeURL(redirect: redirect.absoluteString, pkce: PKCE(verifier: "v"), state: "s")
        #expect(!bare.absoluteString.contains("login_hint"))
    }

    /// A token for the wrong repository is kept to revoke, never as the
    /// session, so a revocation that fails leaves it for `revokePending`
    /// rather than live with nothing holding it, and the person is told.
    @Test func aRejectedTokenWhoseRevocationFailsIsKeptToRevoke() async throws {
        let server = try #require(URL(string: "https://ada.example.com"))
        let meta = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(metadataJSON().utf8))
        let token = try JSONDecoder().decode(TokenResponse.self, from: Data(
            #"{"access_token":"sub_app_bob","scope":"read:*","repository":"bob.example.com"}"#.utf8))
        let session = Session(server: server, token: token, clientID: "https://sync.substrate.example.com/client.json",
                              revocation: nil, now: Date())
        let mismatch = try #require(RepositoryMismatch(resolved: "ada.example.com", token: session.repository, server: server))

        let failing = StubTransport().on("POST", "/oauth/revoke", status: 503, json: "")
        let store = SessionStore(service: "test", storage: MemoryStorage())
        let pending = await OAuthClient(manifest: syncManifest(), metadata: meta, transport: failing)
            .reject(session, mismatch, keepingIn: store)
        #expect(failing.sent("POST", "/oauth/revoke").count == 1)
        #expect(pending.revocation == .pending)
        #expect(store.pendingRevocations() == [session])
        #expect(store.load() == nil)
        #expect(pending.errorDescription?.contains("You signed in to bob.example.com, not ada.example.com.") == true)
        #expect(pending.errorDescription?.contains("ada.example.com didn't confirm revoking the access it gave for bob.example.com") == true)

        // The next try revokes it and forgets it.
        let answering = StubTransport().on("POST", "/oauth/revoke", status: 200, json: "")
        #expect(await store.revokePending(current: nil, transport: answering) == [])
        #expect(store.pendingRevocations().isEmpty)

        // Revoked at once: nothing is left kept.
        let revoked = await OAuthClient(manifest: syncManifest(), metadata: meta, transport: answering)
            .reject(session, mismatch, keepingIn: store)
        #expect(revoked.revocation == .revoked)
        #expect(store.pendingRevocations().isEmpty)
        #expect(revoked.errorDescription?.contains("didn't confirm") == false)

        // Neither kept nor revoked: the person is told to revoke it.
        let refusing = SessionStore(service: "test", storage: MemoryStorage(refusing: ["session.revocations"]))
        let live = await OAuthClient(manifest: syncManifest(), metadata: meta, transport: failing)
            .reject(session, mismatch, keepingIn: refusing)
        #expect(live.revocation == .live)
        #expect(live.errorDescription?.contains("Revoke it in ada.example.com's Settings, under Apps.") == true)
    }

    @Test func aTokenForAnotherRepositoryIsRefused() {
        #expect(RepositoryMismatch(resolved: nil, token: "bob.example.com") == nil)
        #expect(RepositoryMismatch(resolved: "Ada.Example.com.", token: "ada.example.com") == nil)
        let mismatch = RepositoryMismatch(resolved: "ada.example.com", token: "bob.example.com")
        #expect(mismatch?.errorDescription?.contains("You signed in to bob.example.com, not ada.example.com.") == true)
    }
}
