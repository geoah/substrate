import Foundation
import Testing
@testable import SubstrateKit

@Suite struct Proofs {
    /// RFC 7636, appendix B.
    @Test func theChallengeIsTheVerifiersSHA256() {
        let pkce = PKCE(verifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
        #expect(pkce.challenge == "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM")
    }

    @Test func aFreshVerifierIsLongEnoughAndURLSafe() throws {
        let verifier = try PKCE().verifier
        #expect(verifier.count == 43)
        #expect(verifier.allSatisfy { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" })
        #expect(try PKCE().verifier != verifier)
        #expect(try PKCE.state().count == 22)
    }
}

@Suite struct Authorization {
    func client(details: [String] = ["package", "account", "webhook"], iss: Bool = true) throws -> OAuthClient {
        var meta = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(metadataJSON(details: details).utf8))
        meta.authorizationResponseISSParameterSupported = iss
        return OAuthClient(manifest: syncManifest(), metadata: meta)
    }

    func query(_ url: URL) -> [String: String] {
        var out: [String: String] = [:]
        for item in URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? [] { out[item.name] = item.value }
        return out
    }

    @Test func theAuthorizeURLCarriesTheDetailsAsCompactJSON() throws {
        let url = try client().authorizeURL(redirect: "http://127.0.0.1:5000/oauth/callback",
                                            pkce: PKCE(verifier: "v"), state: "st")
        #expect(url.absoluteString.hasPrefix("https://ada.example.com/oauth/authorize?response_type=code&client_id=https%3A%2F%2Fsync"))
        let items = query(url)
        #expect(items["scope"] == "write:providers.substrate.reamde.dev/apple/* read:providers.substrate.reamde.dev/applehealth/account read:providers.substrate.reamde.dev/applehealth/export")
        #expect(items["code_challenge_method"] == "S256")
        #expect(items["redirect_uri"] == "http://127.0.0.1:5000/oauth/callback")
        #expect(items["state"] == "st")
        let details = try #require(items["authorization_details"])
        #expect(details == #"[{"package":"providers.substrate.reamde.dev/apple","type":"package","version":3},{"package":"providers.substrate.reamde.dev/applehealth","type":"package","version":5},{"id":"substrate-sync-1a2b3c4d","kind":"providers.substrate.reamde.dev/applehealth/account","properties":{"displayName":"Ada's iPhone","timezone":"Europe/London"},"type":"account"},{"trigger":"applehealth-webhook","type":"webhook"}]"#)
        // Encoded once: JSON's punctuation is escaped in the URL itself.
        #expect(url.absoluteString.contains("authorization_details=%5B%7B%22package%22%3A%22providers.substrate.reamde.dev%2Fapple%22"))
        let decoded = try JSONDecoder().decode([AuthorizationDetail].self, from: Data(details.utf8))
        #expect(decoded == syncManifest().details.map(\.sent))
        // The package a webhook delivers to words it here and is never sent.
        #expect(!details.contains("deliversTo"))
    }

    /// A server that would ignore the details would approve the scopes alone,
    /// which the app would read as set up: it is never sent the request.
    @Test func aServerThatCannotSetUpIsNeverSentTheRequest() throws {
        #expect(throws: ServerMismatch.self) {
            try client(details: ["package"]).authorizeURL(redirect: "http://127.0.0.1:1/oauth/callback",
                                                          pkce: PKCE(verifier: "v"), state: "s")
        }
        // Its requirement says so first: the details' types and the scopes'
        // verbs are required whatever the app named.
        let manifest = syncManifest()
        #expect(manifest.requirement.detailTypes == ["package", "account", "webhook"])
        #expect(manifest.requirement.scopeVerbs == ["write", "read"])
        let meta = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(metadataJSON(details: ["package"]).utf8))
        let probe = ServerProbe.Result(origin: try #require(URL(string: "https://ada.example.com")),
                                       discovery: Discovery(server: .init(version: "v0.114.0")), metadata: meta,
                                       verdict: manifest.requirement.verdict(version: "v0.114.0", metadata: meta))
        #expect(manifest.door(for: probe) == .password)
        var strict = manifest
        strict.passwordFallback = .never
        #expect(strict.door(for: probe) == .tooOld)
        // An explicit verb the scopes do not use still counts.
        var more = manifest
        more.requirement = ServerRequirement(scopeVerbs: ["agents"])
        #expect(more.requirement.scopeVerbs == ["agents", "write", "read"])
    }

    /// The issuer is the server the owner chose and every endpoint is on its
    /// origin, or nothing is sent: an answer naming another server's sign-in
    /// would hand the code and its verifier to whoever answers there.
    @Test func metadataOfAnotherOriginIsRefusedBeforeAnythingIsSent() async throws {
        let origin = try #require(URL(string: "https://evil.example.com"))
        let elsewhere = StubTransport()
        elsewhere.on("GET", "/.well-known/substrate/server.json", json: discoveryJSON())
        elsewhere.on("GET", "/.well-known/oauth-authorization-server", json: metadataJSON(issuer: "https://ada.example.com"))
        await #expect(throws: ServerMismatch.self) {
            try await ServerProbe(transport: elsewhere).probe(origin, requirement: ServerRequirement(), app: "Substrate Sync")
        }
        let handedOn = StubTransport()
        handedOn.on("GET", "/.well-known/substrate/server.json", json: discoveryJSON())
        handedOn.on("GET", "/.well-known/oauth-authorization-server",
                    json: metadataJSON(issuer: "https://evil.example.com", endpoints: "https://ada.example.com"))
        await #expect(throws: ServerMismatch.self) {
            try await ServerProbe(transport: handedOn).probe(origin, requirement: ServerRequirement(), app: "Substrate Sync")
        }

        // Driven by hand, the client still never sends the code elsewhere.
        let meta = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(
            metadataJSON(issuer: "https://ada.example.com", endpoints: "https://evil.example.com").utf8))
        let stub = StubTransport()
        stub.on("POST", "/oauth/token", json: #"{"access_token":"t","scope":"read:*","repository":"ada.example.com"}"#)
        let oauth = OAuthClient(manifest: syncManifest(), metadata: meta, transport: stub)
        await #expect(throws: ServerMismatch.self) {
            try await oauth.exchange(code: "c", redirect: "http://127.0.0.1:1/oauth/callback", pkce: PKCE(verifier: "v"))
        }
        #expect(throws: ServerMismatch.self) {
            try oauth.authorizeURL(redirect: "http://127.0.0.1:1/oauth/callback", pkce: PKCE(verifier: "v"), state: "s")
        }
        #expect(stub.requests.isEmpty)
        // An issuer that is not the server chosen is refused before the
        // browser opens.
        let honest = try client()
        await #expect(throws: ServerMismatch.self) { try await honest.signIn(server: origin) }
        #expect(honest.metadata.mismatch(server: try #require(URL(string: "https://ada.example.com:443"))) == nil)
    }

    @Test func theCallbackChecksStateAndIssuer() throws {
        let strict = try client()
        let iss = "https://ada.example.com"
        #expect(try strict.callback(["code": "c1", "state": "s", "iss": iss], state: "s") == "c1")
        #expect(throws: OAuthRefusal(code: "invalid_state", text: "The sign-in came back for another request. Try again.")) {
            try strict.callback(["code": "c1", "state": "other", "iss": iss], state: "s")
        }
        #expect(throws: OAuthRefusal.self) { try strict.callback(["code": "c1", "state": "s", "iss": "https://evil.example.com"], state: "s") }
        // Advertised, so required.
        do {
            _ = try strict.callback(["code": "c1", "state": "s"], state: "s")
            Issue.record("a redirect without iss was accepted from a server that sends one")
        } catch let refusal as OAuthRefusal {
            #expect(refusal.code == "invalid_issuer")
        }
        // Not advertised: absent is fine, a wrong one still is not.
        let lax = try client(iss: false)
        #expect(try lax.callback(["code": "c2", "state": "s"], state: "s") == "c2")
        #expect(throws: OAuthRefusal.self) { try lax.callback(["code": "c2", "state": "s", "iss": "https://evil.example.com"], state: "s") }
        do {
            _ = try strict.callback(["error": "access_denied", "state": "s", "iss": iss], state: "s")
            Issue.record("a denial read as a code")
        } catch let refusal as OAuthRefusal {
            #expect(refusal.denied)
            #expect(refusal.errorDescription == "Access was not granted.")
        }
        #expect(throws: OAuthRefusal.self) { try strict.callback(["state": "s", "iss": iss], state: "s") }
    }

    @Test func theTokenAnswerListsWhatWasSetUp() throws {
        let json = #"""
        {"access_token":"sub_app_x","token_type":"Bearer","scope":"write:providers.substrate.reamde.dev/apple/* webhook:applehealth-webhook",
         "expires_in":2592000,"repository":"ada.example.com",
         "authorization_details":[
           {"type":"package","package":"providers.substrate.reamde.dev/apple","version":3},
           {"type":"account","kind":"providers.substrate.reamde.dev/applehealth/account","id":"substrate-sync-1a2b3c4d"},
           {"type":"webhook","trigger":"applehealth-webhook","url":"https://ada.example.com/webhooks/ada.example.com/applehealth-webhook"}]}
        """#
        let token = try JSONDecoder().decode(TokenResponse.self, from: Data(json.utf8))
        #expect(token.scopes == ["write:providers.substrate.reamde.dev/apple/*", "webhook:applehealth-webhook"])
        #expect(token.authorizationDetails.count == 3)
        #expect(token.authorizationDetails[0].version == 3)
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        let session = Session(server: try #require(URL(string: "https://ada.example.com")), token: token,
                              clientID: "https://sync.substrate.example.com/client.json", revocation: nil, now: now)
        #expect(session.isApp)
        #expect(session.expiresAt == now.addingTimeInterval(2_592_000))
        #expect(session.issuedAt == now)
        #expect(session.webhooks["applehealth-webhook"]?.absoluteString == "https://ada.example.com/webhooks/ada.example.com/applehealth-webhook")
        #expect(session.grants?.delivers("applehealth-webhook") == true)
        #expect(session.tokenID == nil)

        let forever = try JSONDecoder().decode(TokenResponse.self, from: Data(#"{"access_token":"t","scope":"read:*","repository":"r.example.com"}"#.utf8))
        #expect(forever.expiresIn == nil)
        #expect(forever.authorizationDetails.isEmpty)
    }

    @Test func theExchangeIsSentOnceAndAFailureSaysWhy() async throws {
        let stub = StubTransport()
        stub.on("POST", "/oauth/token", status: 503, headers: ["Retry-After": "1"], json: #"{"error":"temporarily_unavailable"}"#)
        var oauth = try client()
        oauth.transport = stub
        do {
            _ = try await oauth.exchange(code: "c", redirect: "http://127.0.0.1:1/oauth/callback", pkce: PKCE(verifier: "v"))
            Issue.record("a refused exchange answered a token")
        } catch let refusal as OAuthRefusal {
            #expect(refusal.code == "temporarily_unavailable")
        }
        #expect(stub.sent("POST", "/oauth/token").count == 1)
        let body = String(decoding: stub.sent("POST", "/oauth/token")[0].httpBody ?? Data(), as: UTF8.self)
        #expect(body.contains("grant_type=authorization_code&code=c&client_id=https%3A%2F%2Fsync.substrate.example.com%2Fclient.json"))
        #expect(body.contains("code_verifier=v"))
    }

    /// The session's lifetime starts when the token came back, however long
    /// the owner took to approve.
    @Test func aSessionsLifetimeIsReadAfterTheExchange() async throws {
        final class Clock: @unchecked Sendable {
            var now = Date(timeIntervalSince1970: 1_800_000_000)
        }
        let clock = Clock()
        final class Slow: HTTPTransport, @unchecked Sendable {
            let clock: Clock
            init(_ clock: Clock) { self.clock = clock }
            func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
                clock.now = clock.now.addingTimeInterval(900)
                let body = #"{"access_token":"t","scope":"read:*","expires_in":3600,"repository":"ada.example.com"}"#
                return (Data(body.utf8), HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: [:])!)
            }
        }
        var oauth = try client()
        oauth.transport = Slow(clock)
        let session = try await oauth.session(exchanging: "c", redirect: "http://127.0.0.1:1/oauth/callback",
                                              pkce: PKCE(verifier: "v"), server: try #require(URL(string: "https://ada.example.com")),
                                              now: { clock.now })
        #expect(session.issuedAt == Date(timeIntervalSince1970: 1_800_000_900))
        #expect(session.expiresAt == Date(timeIntervalSince1970: 1_800_004_500))
        #expect(session.requestedScopes == syncManifest().scopes)
    }
}

/// Revoking is one thing for both kinds of session, and it is done only once
/// the server says so.
@Suite struct Revocation {
    let server = URL(string: "https://ada.example.com")!

    func app(clientID: String? = "http://127.0.0.1/substrate-sync", revocation: URL? = nil) -> Session {
        Session(server: server, repository: "ada.example.com", secret: "sub_app_x", clientID: clientID,
                scopes: ["webhook:applehealth-webhook"], revocation: revocation)
    }

    @Test func anAppSessionIsRevokedWhereAndAsItWasApproved() async throws {
        let stub = StubTransport()
        stub.on("POST", "/oauth/revoke", json: "")
        try await app().revoke(transport: stub)
        let sent = stub.sent("POST", "/oauth/revoke")
        #expect(sent.count == 1)
        #expect(sent.first?.url?.absoluteString == "https://ada.example.com/oauth/revoke")
        #expect(String(decoding: sent.first?.httpBody ?? Data(), as: UTF8.self)
            == "token=sub_app_x&client_id=http%3A%2F%2F127.0.0.1%2Fsubstrate-sync")
        // A session that never kept its client id sends none: the token is
        // the credential, and a guessed id would revoke nothing.
        try await app(clientID: nil).revoke(transport: stub)
        #expect(String(decoding: stub.sent("POST", "/oauth/revoke").last?.httpBody ?? Data(), as: UTF8.self) == "token=sub_app_x")
        // A revocation endpoint off the server's origin is never sent the
        // token.
        try await app(revocation: URL(string: "https://evil.example.com/oauth/revoke")).revoke(transport: stub)
        #expect(stub.requests.allSatisfy { $0.url?.host() == "ada.example.com" })
    }

    @Test(arguments: [429, 503, 400, 302])
    func anythingButA200LeavesTheTokenLive(status: Int) async {
        let busy = StubTransport()
        busy.on("POST", "/oauth/revoke", status: status, json: "")
        await #expect(throws: (any Error).self) { try await app().revoke(transport: busy) }
    }

    @Test func aPasswordSessionIsRevokedThroughItsOwnToken() async throws {
        let stub = StubTransport()
        stub.on("DELETE", "/tokens/tok_1", status: 204, json: "")
        let password = Session(server: server, repository: "ada.example.com", secret: "sub_password", tokenID: "tok_1")
        try await password.revoke(transport: stub)
        #expect(stub.sent("DELETE", "/tokens/tok_1").first?.value(forHTTPHeaderField: "Authorization") == "Bearer sub_password")
        #expect(stub.sent("POST", "/oauth/revoke").isEmpty)
        // One the server no longer accepts has nothing left to revoke.
        let gone = StubTransport()
        gone.on("DELETE", "/tokens/tok_1", status: 401, json: #"{"error":{"code":"unauthorized","message":"invalid token"}}"#)
        try await password.revoke(transport: gone)
    }

    @Test func aSignOutKeepsTheTokenUntilTheServerRevokesIt() async throws {
        let store = SessionStore(service: "dev.reamde.substrate.kit.tests", account: "test-\(UUID().uuidString)")
        defer {
            for old in store.pendingRevocations() { store.revoked(old) }
            store.clear()
        }
        let session = app()
        try store.save(session)
        #expect(store.signOut(session))
        #expect(store.load() == nil)
        let busy = StubTransport()
        busy.on("POST", "/oauth/revoke", status: 503, json: "")
        #expect(await store.revokePending(current: nil, transport: busy) == [session])
        #expect(store.pendingRevocations() == [session])
        let ok = StubTransport()
        ok.on("POST", "/oauth/revoke", json: "")
        #expect(await store.revokePending(current: nil, transport: ok) == [])
        #expect(store.pendingRevocations().isEmpty)
        // The session in use is never revoked, only forgotten from the list.
        try store.keepForRevocation(session)
        #expect(await store.revokePending(current: session, transport: busy) == [])
        #expect(store.pendingRevocations().isEmpty)
    }
}

@Suite struct Sessions {
    @Test func syncsSessionStillDecodes() throws {
        let old = #"{"server":"https://ada.example.com","repository":"ada.example.com","secret":"sub_x","tokenID":"tok_1"}"#
        let session = try JSONDecoder().decode(Session.self, from: Data(old.utf8))
        #expect(session.version == 1)
        #expect(!session.isApp)
        #expect(session.scopes == nil)
        #expect(session.grants == nil)
        #expect(session.tokenID == "tok_1")
        #expect(session.expiresAt == nil)
        #expect(session.webhooks.isEmpty)
    }

    @Test func chatsSessionStillDecodes() throws {
        // As Chat's JSONEncoder wrote it: the expiry in seconds since 2001.
        let chat = #"""
        {"server":"https://ada.example.com","repository":"ada.example.com","secret":"sub_app_x","tokenID":"",
         "grants":{"scopes":["agents:*","decide:*","read:*"]},"expiresAt":815000000,
         "revocation":"https://ada.example.com/oauth/revoke"}
        """#
        let session = try JSONDecoder().decode(Session.self, from: Data(chat.utf8))
        #expect(session.isApp)
        #expect(session.scopes == ["agents:*", "decide:*", "read:*"])
        #expect(session.tokenID == nil)
        #expect(session.expiresAt == Date(timeIntervalSinceReferenceDate: 815_000_000))
        #expect(session.revocation?.absoluteString == "https://ada.example.com/oauth/revoke")
        #expect(session.identity == "https://ada.example.com|ada.example.com")
    }

    @Test func aSessionRoundTripsAndStaysReadableByEarlierBuilds() throws {
        let session = Session(server: try #require(URL(string: "https://ada.example.com")), repository: "ada.example.com",
                              secret: "sub_app_x", clientID: "https://sync.substrate.example.com/client.json",
                              scopes: ["webhook:applehealth-webhook"], expiresAt: Date(timeIntervalSinceReferenceDate: 815_000_000),
                              issuedAt: Date(timeIntervalSinceReferenceDate: 812_408_000),
                              webhooks: ["applehealth-webhook": try #require(URL(string: "https://ada.example.com/webhooks/ada.example.com/applehealth-webhook"))],
                              details: [.webhook(trigger: "applehealth-webhook")])
        let data = try JSONEncoder().encode(session)
        #expect(try JSONDecoder().decode(Session.self, from: data) == session)
        let object = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        #expect(object["version"] as? Int == 2)
        #expect(object["tokenID"] as? String == "")
        #expect((object["grants"] as? [String: Any])?["scopes"] as? [String] == ["webhook:applehealth-webhook"])
        #expect(object["expiresAt"] as? Double == 815_000_000)
        #expect(object["issuedAt"] as? Double == 812_408_000)
        var asked = session
        asked.requestedScopes = ["read:*"]
        #expect(try JSONDecoder().decode(Session.self, from: JSONEncoder().encode(asked)).requestedScopes == ["read:*"])
        // An RFC 3339 stamp reads too.
        let stamped = #"{"version":2,"server":"https://a.example.com","repository":"a.example.com","secret":"s","expiresAt":"2026-12-01T10:00:00.123456789Z"}"#
        #expect(try JSONDecoder().decode(Session.self, from: Data(stamped.utf8)).expiresAt == Timestamp.parse("2026-12-01T10:00:00.123Z"))
    }

    @Test func theStoreAsksTheKeychainForOneItem() {
        let store = SessionStore(service: "dev.reamde.substrate.sync", accessGroup: "TEAMID.dev.reamde.substrate.sync")
        let query = store.query(account: "session")
        #expect(query[kSecAttrService as String] as? String == "dev.reamde.substrate.sync")
        #expect(query[kSecAttrAccessGroup as String] as? String == "TEAMID.dev.reamde.substrate.sync")
        #expect(query[kSecAttrAccount as String] as? String == "session")
        #expect(SessionStore.Accessibility.afterFirstUnlockThisDeviceOnly.attribute == kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly)
    }
}

@Suite(.timeLimit(.minutes(1))) struct Loopback {
    @Test func theReceiverHandsOverTheRedirectsQueryAndNamesTheApp() async throws {
        let receiver = try await LoopbackReceiver.start(appName: "Substrate Sync")
        defer { receiver.stop() }
        #expect(receiver.port > 0)
        #expect(receiver.redirect.hasSuffix("/oauth/callback"))
        let url = try #require(URL(string: receiver.redirect + "?code=abc&state=xyz&iss=https%3A%2F%2Fada.example.com"))
        async let page = URLSession.shared.data(from: url)
        let query = try await receiver.next()
        #expect(query["code"] == "abc")
        #expect(query["iss"] == "https://ada.example.com")
        let (body, response) = try await page
        #expect((response as? HTTPURLResponse)?.statusCode == 200)
        #expect(String(decoding: body, as: UTF8.self).contains("Signed in. You can go back to Substrate Sync."))
    }
}
