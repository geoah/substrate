import Foundation
import Testing
import SubstrateKit
@testable import SubstrateChat

@Suite struct OAuthFlow {
    @Test func theChallengeIsRFC7636sOwnVector() {
        let pkce = OAuth.PKCE(verifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
        #expect(pkce.challenge == "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM")
    }

    @Test func aFreshVerifierIsLongEnoughAndURLSafe() {
        let v = OAuth.PKCE().verifier
        #expect(v.count >= 43)
        #expect(v.allSatisfy { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" })
    }

    @Test func theCallbackIsCheckedBeforeItsCodeIsUsed() throws {
        let iss = "https://s.example.com"
        #expect(try OAuth.callback(["code": "c1", "state": "s", "iss": iss], state: "s", issuer: iss) == "c1")
        #expect(throws: OAuth.Refusal.self) { try OAuth.callback(["code": "c1", "state": "other"], state: "s", issuer: iss) }
        #expect(throws: OAuth.Refusal.self) {
            try OAuth.callback(["code": "c1", "state": "s", "iss": "https://evil.example.com"], state: "s", issuer: iss)
        }
        do {
            _ = try OAuth.callback(["error": "access_denied", "state": "s"], state: "s", issuer: iss)
            Issue.record("a refusal read as a code")
        } catch let refusal as OAuth.Refusal {
            #expect(refusal.code == "access_denied")
        }
    }

    @Test func theAuthorizeURLCarriesEveryParameterEncoded() throws {
        let meta = try JSONDecoder().decode(OAuth.Metadata.self, from: Data(#"""
            {"issuer":"https://s.example.com","authorization_endpoint":"https://s.example.com/oauth/authorize",
             "token_endpoint":"https://s.example.com/oauth/token","revocation_endpoint":"https://s.example.com/oauth/revoke"}
            """#.utf8))
        let url = try #require(OAuth.authorizeURL(meta, redirect: "http://127.0.0.1:5000/oauth/callback",
                                                   pkce: OAuth.PKCE(verifier: "v"), state: "st"))
        let items = Dictionary(uniqueKeysWithValues: (URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? [])
            .map { ($0.name, $0.value ?? "") })
        #expect(items["response_type"] == "code")
        #expect(items["code_challenge_method"] == "S256")
        #expect(items["redirect_uri"] == "http://127.0.0.1:5000/oauth/callback")
        #expect(items["scope"] == "agents:* decide:* read:substrate.reamde.dev/llm/provider read:*")
        #expect(items["state"] == "st")
        #expect(url.absoluteString.contains("scope=agents%3A%2A%20decide%3A%2A"))
    }

    @Test func grantsSayWhatTheTokenMayDo() {
        let all = Grants(scopes: ["agents:*", "decide:*", "read:substrate.reamde.dev/llm/provider", "read:*"])
        #expect(all.chat && all.decide && all.providers)
        let chatOnly = Grants(scopes: ["agents:x.example.com/llm/helper"])
        #expect(chatOnly.chat && !chatOnly.decide && !chatOnly.providers)
        #expect(chatOnly.words == ["Talk to Helper"])
        #expect(all.words == ["Talk to every agent", "Apply or dismiss what every agent suggests", "Read providers", "Read your records"])
    }

    @Test func aSessionWrittenBeforeOAuthStillDecodes() throws {
        let old = #"{"server":"https://s.example.com","repository":"r.example.com","secret":"x","tokenID":"t"}"#
        let session = try JSONDecoder().decode(Session.self, from: Data(old.utf8))
        #expect(session.grants == nil)
        #expect(session.expiresAt == nil)
    }

    @Test(.timeLimit(.minutes(1))) func theLoopbackReceiverHandsOverTheRedirectsQuery() async throws {
        let receiver = try await LoopbackReceiver.start()
        defer { receiver.stop() }
        #expect(receiver.port > 0)
        let url = try #require(URL(string: receiver.redirect + "?code=abc&state=xyz&iss=https%3A%2F%2Fs.example.com"))
        async let page = URLSession.shared.data(from: url)
        let query = try await receiver.next()
        #expect(query["code"] == "abc")
        #expect(query["state"] == "xyz")
        #expect(query["iss"] == "https://s.example.com")
        let (body, response) = try await page
        #expect((response as? HTTPURLResponse)?.statusCode == 200)
        #expect(String(decoding: body, as: UTF8.self).contains("Signed in"))
    }
}

@Suite struct GrantReach {
    @Test func aReadScopeReachesItsKindsAndAGlobStopsAtTheInternalPackages() {
        let all = Grants(scopes: ["agents:*", "read:*"])
        #expect(all.reads("a.example.com/people/person"))
        #expect(!all.reads("substrate.reamde.dev/core/kind"))
        #expect(!all.reads("substrate.reamde.dev/llm/provider"))
        let some = Grants(scopes: ["read:a.example.com/tasks/*", "write:substrate.reamde.dev/core/kind"])
        #expect(some.reads("a.example.com/tasks/task"))
        #expect(!some.reads("a.example.com/people/person"))
        #expect(some.reads("substrate.reamde.dev/core/kind"))
        #expect(!Grants(scopes: ["agents:*"]).reads("a.example.com/people/person"))
    }
}
