import Foundation
import Testing
@testable import SubstrateKit

@Suite struct Addresses {
    @Test(arguments: [
        ("ada.example.com", "https://ada.example.com"),
        ("  Ada.Example.COM/path?q=1#top  ", "https://ada.example.com"),
        ("ada.example.com:8443", "https://ada.example.com:8443"),
        ("https://ada.example.com/", "https://ada.example.com"),
        ("HTTPS://Ada.Example.com/x/y", "https://ada.example.com"),
        ("http://user:secret@Host.Example.com:8080/a?b#c", "http://host.example.com:8080"),
        ("localhost:8080", "http://localhost:8080"),
        ("localhost", "http://localhost"),
        ("127.0.0.1:8080/console", "http://127.0.0.1:8080"),
        ("127.1.2.3", "http://127.1.2.3"),
        ("[::1]:8080", "http://[::1]:8080"),
        ("https://localhost:8443", "https://localhost:8443"),
        // Only this machine's own names are plain HTTP.
        ("localhost.example.com", "https://localhost.example.com"),
        ("127.0.0.1.example.com", "https://127.0.0.1.example.com"),
    ])
    func aTypedAddressBecomesOneOrigin(typed: String, origin: String) {
        #expect(ServerAddress.origin(of: typed)?.absoluteString == origin)
    }

    @Test(arguments: ["", "   ", "ftp://ada.example.com", "http://", "mailto:ada@example.com", "https://exa mple.com"])
    func anythingButHTTPIsNoAddress(typed: String) {
        #expect(ServerAddress.origin(of: typed) == nil)
    }

    @Test func aServerReadsAsItsHost() throws {
        #expect(ServerAddress.display(try #require(URL(string: "https://ada.example.com"))) == "ada.example.com")
        #expect(ServerAddress.display(try #require(URL(string: "http://localhost:8080"))) == "localhost:8080")
        #expect(ServerAddress.isLoopback(try #require(URL(string: "http://[::1]:8080"))))
        #expect(!ServerAddress.isLoopback(try #require(URL(string: "https://ada.example.com"))))
    }
}

@Suite struct DiscoveryDocument {
    @Test func theServersDocumentDecodesWhole() throws {
        let doc = try JSONDecoder().decode(Discovery.self, from: Data(discoveryJSON(version: "v0.114.0", totp: false).utf8))
        #expect(doc.versions == [Discovery.APIVersion(name: "v1", status: "served")])
        #expect(doc.server.version == "v0.114.0")
        #expect(doc.server.build?.count == 40)
        #expect(doc.version == ServerVersion(0, 114, 0))
        #expect(doc.changelog.horizon == 0)
        #expect(doc.feature("agents")?.stability == "alpha")
        #expect(doc.feature("search")?.surfaces == ["rest"])
        #expect(doc.surfaces.rest?.endpoint == "/api/v1")
        #expect(doc.grammar.actors.contains("app:<host>"))
        #expect(doc.grammar.recordPath == "/api/v1/{authority}/{package}/{kind}/{id}")
        #expect(doc.endpoints.authorize == "/oauth/authorize")
        #expect(doc.endpoints.token == "/oauth/token")
        #expect(doc.registration == Discovery.Registration(inviteRequired: false, totpRequired: false))
        #expect(doc.servesApps)
    }

    @Test func aMissingOrOddFieldTakesItsDefault() throws {
        let bare = try JSONDecoder().decode(Discovery.self, from: Data("{}".utf8))
        #expect(bare.server.version == nil)
        #expect(bare.registration.totpRequired)
        #expect(!bare.servesApps)
        let odd = try JSONDecoder().decode(Discovery.self, from: Data(#"{"server":{"version":3},"features":"x","registration":{"totpRequired":false}}"#.utf8))
        #expect(odd.server.version == nil)
        #expect(odd.features.isEmpty)
        #expect(!odd.registration.totpRequired)
        #expect(odd.registration.inviteRequired)
    }

    @Test func theMetadataSaysWhichVerbsAndDetailsTheServerKnows() throws {
        let meta = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(metadataJSON().utf8))
        #expect(meta.scopeVerbs == ["read", "write", "agents", "decide"])
        #expect(meta.supports(detailType: "webhook"))
        #expect(meta.revocationEndpoint?.absoluteString == "https://ada.example.com/oauth/revoke")
        #expect(meta.authorizationResponseISSParameterSupported)
        // A server from before the details lists none.
        let older = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(#"""
        {"issuer":"https://ada.example.com","authorization_endpoint":"https://ada.example.com/oauth/authorize",
         "token_endpoint":"https://ada.example.com/oauth/token","scopes_supported":["read:*","write:*"]}
        """#.utf8))
        #expect(older.authorizationDetailsTypesSupported.isEmpty)
        #expect(!older.authorizationResponseISSParameterSupported)
        #expect(older.revocation.absoluteString == "https://ada.example.com/oauth/revoke")
    }
}

@Suite struct Versions {
    @Test func everyFormTheServerReportsParses() throws {
        #expect(ServerVersion("v0.113.0") == ServerVersion(0, 113, 0))
        #expect(ServerVersion("0.113.0") == ServerVersion(0, 113, 0))
        #expect(ServerVersion("1.2") == ServerVersion(1, 2, 0))
        #expect(ServerVersion("0.113.0-4-gabc1234") == ServerVersion(0, 113, 0, commitsAfter: 4))
        #expect(ServerVersion("v0.113.0-4-gabc1234-dirty") == ServerVersion(0, 113, 0, commitsAfter: 4))
        #expect(ServerVersion("0.114.0-pre") == ServerVersion(0, 114, 0, prerelease: "pre"))
        #expect(ServerVersion("0.114.0-pre.2+meta") == ServerVersion(0, 114, 0, prerelease: "pre.2"))
        #expect(ServerVersion("v0.113.1-0.20261031120000-abcdef123456")?.prerelease == "0.20261031120000-abcdef123456")
        #expect(try #require(ServerVersion("v0.113.0")).isRelease)
        #expect(!(try #require(ServerVersion("v0.113.0-4-gabc1234")).isRelease))
    }

    @Test(arguments: ["dev", "", "(devel)", "v", "x.y.z", "1", "1.2.3.4", "1.-2.3", "0.113.0-"])
    func aBuildThatNamesNoReleaseIsNotComparable(text: String) {
        #expect(ServerVersion(text) == nil)
    }

    @Test func versionsOrderAsReleasesDo() throws {
        let order = ["0.112.9", "0.113.0-rc.1", "0.113.0", "0.113.0-4-gabc1234", "0.113.0-12-gdef5678",
                     "0.113.1-0.20261031120000-abcdef123456", "0.113.1", "0.114.0-pre", "0.114.0-pre.2",
                     "0.114.0-pre.10", "0.114.0-rc.1", "0.114.0", "1.0.0"]
        let parsed = try order.map { try #require(ServerVersion($0)) }
        for (a, b) in zip(parsed, parsed.dropFirst()) {
            #expect(a < b, "\(a) < \(b)")
            #expect(!(b < a))
        }
        #expect(parsed.shuffled().sorted() == parsed)
    }
}

@Suite struct Requirements {
    let chat = ServerRequirement(minimumVersion: "0.114.0", scopeVerbs: ["agents", "decide"])
    let sync = ServerRequirement(minimumVersion: "0.114.0", detailTypes: ["package", "account", "webhook"])

    func meta(verbs: [String] = ["read", "write", "agents", "decide"], details: [String] = ["package", "account", "webhook"])
        throws -> AuthorizationServerMetadata
    {
        try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(metadataJSON(details: details, verbs: verbs).utf8))
    }

    @Test func aReleaseWithEverythingIsCompatible() throws {
        #expect(chat.verdict(version: "v0.114.0", metadata: try meta()) == .compatible)
        #expect(sync.verdict(version: "v0.115.2", metadata: try meta()) == .compatible)
    }

    @Test func whatTheServerCanDoDecidesForABuildBetweenReleases() throws {
        #expect(chat.verdict(version: "dev", metadata: try meta()) == .compatible)
        #expect(chat.verdict(version: "v0.113.0-4-gabc1234", metadata: try meta()) == .compatible)
        #expect(sync.verdict(version: "0.114.0-pre.1", metadata: try meta()) == .compatible)
        #expect(chat.verdict(version: "dev", metadata: try meta(verbs: ["read", "write", "agents"]))
            == .tooOld(running: "dev", needed: "0.114.0", missing: ["let apps apply what your agents suggest"]))
    }

    @Test func aReleaseBelowTheMinimumIsTooOldEvenWithTheCapabilities() throws {
        #expect(chat.verdict(version: "v0.113.0", metadata: try meta())
            == .tooOld(running: "v0.113.0", needed: "0.114.0", missing: []))
    }

    @Test func whatTheServerLacksIsNamed() throws {
        #expect(sync.verdict(version: "v0.114.0", metadata: try meta(details: ["package"]))
            == .tooOld(running: "v0.114.0", needed: "0.114.0", missing: [
                "create an app's account when you approve it", "turn on an app's webhook when you approve it",
            ]))
        #expect(chat.verdict(version: "v0.112.0", metadata: nil)
            == .tooOld(running: "v0.112.0", needed: "0.114.0", missing: ["give apps access of their own"]))
        #expect(ServerRequirement().verdict(version: nil, metadata: try meta()) == .compatible)
    }

    @Test func aProbeReadsDiscoveryAndMetadata() async throws {
        let stub = StubTransport()
        stub.on("GET", "/.well-known/substrate/server.json", json: discoveryJSON(version: "v0.114.0"))
        stub.on("GET", "/.well-known/oauth-authorization-server", json: metadataJSON())
        let origin = try #require(URL(string: "https://ada.example.com"))
        let result = try await ServerProbe(transport: stub).probe(origin, requirement: sync, app: "Substrate Sync")
        #expect(result.verdict == .compatible)
        #expect(result.metadata?.issuer == "https://ada.example.com")
        #expect(syncManifest().door(for: result) == .app)

        let older = StubTransport()
        older.on("GET", "/.well-known/substrate/server.json", json: discoveryJSON(version: "v0.112.0"))
        older.on("GET", "/.well-known/oauth-authorization-server", status: 404, json: "404 page not found")
        let old = try await ServerProbe(transport: older).probe(origin, requirement: sync, app: "Substrate Sync")
        #expect(old.metadata == nil)
        #expect(syncManifest().door(for: old) == .password)
        var chatOnly = syncManifest()
        chatOnly.passwordFallback = .never
        #expect(chatOnly.door(for: old) == .tooOld)

        let nothing = StubTransport()
        nothing.on("GET", "/.well-known/substrate/server.json", status: 200, json: "<html>parked domain</html>")
        await #expect(throws: ServerProbe.NotASubstrate.self) {
            try await ServerProbe(transport: nothing).probe(origin, requirement: sync, app: "Substrate Sync")
        }
    }
}
