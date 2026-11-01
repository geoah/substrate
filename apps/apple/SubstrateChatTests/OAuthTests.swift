import Foundation
import Testing
import SubstrateKit
@testable import SubstrateChat

private let origin = URL(string: "https://s.example.com")!
private let agentVerbs = ["read:*", "write:*", "agents:*", "decide:*"]

/// A server that answers each path from a table, keeping every request.
private final class Answers: HTTPTransport, @unchecked Sendable {
    private let routes: [String: (Int, String)]
    private let lock = NSLock()
    private var seen: [URLRequest] = []

    init(_ routes: [String: (Int, String)]) {
        self.routes = routes
    }

    var requests: [URLRequest] { lock.withLock { seen } }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        lock.withLock { seen.append(request) }
        let path = request.url?.path ?? ""
        let (status, body) = routes[path] ?? (404, #"{"error":{"code":"not_found","message":"no route"}}"#)
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: "HTTP/1.1",
                                       headerFields: ["Content-Type": "application/json"])!
        return (Data(body.utf8), response)
    }
}

private func metadataJSON(scopes: [String]) -> String {
    let list = scopes.map { "\"\($0)\"" }.joined(separator: ",")
    return #"{"issuer":"https://s.example.com","authorization_endpoint":"https://s.example.com/oauth/authorize","token_endpoint":"https://s.example.com/oauth/token","revocation_endpoint":"https://s.example.com/oauth/revoke","authorization_response_iss_parameter_supported":true,"scopes_supported":["#
        + list + "]}"
}

/// Substrate Chat signs in through the kit's flow under its own manifest:
/// the request it sends, the door each server takes, and the sessions
/// earlier builds kept.
@Suite struct OAuthFlow {
    private let metadata = AuthorizationServerMetadata(issuer: origin, scopesSupported: agentVerbs,
                                                       authorizationDetailsTypesSupported: AuthorizationDetail.Types.all)

    @Test func theChallengeIsRFC7636sOwnVector() throws {
        let pkce = PKCE(verifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
        #expect(pkce.challenge == "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM")
        let fresh = try PKCE().verifier
        #expect(fresh.count >= 43)
        #expect(fresh.allSatisfy { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" })
    }

    @Test func aLocalBuildIsALoopbackClient() {
        #expect(ChatManifest.manifest.clientID == ChatManifest.localClientID)
        #expect(ChatManifest.manifest.details.isEmpty)
        #expect(ChatManifest.manifest.redirectPath == "/oauth/callback")
    }

    @Test func theAuthorizeURLCarriesChatsRequestAndNoDetails() throws {
        let client = OAuthClient(manifest: ChatManifest.manifest, metadata: metadata)
        let url = try client.authorizeURL(redirect: "http://127.0.0.1:5000/oauth/callback", pkce: PKCE(verifier: "v"), state: "st")
        let items = Dictionary(uniqueKeysWithValues: (URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? [])
            .map { ($0.name, $0.value ?? "") })
        #expect(items["response_type"] == "code")
        #expect(items["client_id"] == ChatManifest.manifest.clientID)
        #expect(items["code_challenge"] == PKCE(verifier: "v").challenge)
        #expect(items["code_challenge_method"] == "S256")
        #expect(items["redirect_uri"] == "http://127.0.0.1:5000/oauth/callback")
        #expect(items["scope"] == "agents:* decide:* read:substrate.reamde.dev/llm/provider read:*")
        #expect(items["state"] == "st")
        // Chat sets nothing up, even on a server that would.
        #expect(items["authorization_details"] == nil)
        #expect(url.absoluteString.contains("scope=agents%3A%2A%20decide%3A%2A"))
    }

    @Test func theCallbackIsCheckedBeforeItsCodeIsUsed() throws {
        let client = OAuthClient(manifest: ChatManifest.manifest, metadata: metadata)
        let iss = metadata.issuer
        #expect(try client.callback(["code": "c1", "state": "s", "iss": iss], state: "s") == "c1")
        #expect(throws: OAuthRefusal.self) { try client.callback(["code": "c1", "state": "other", "iss": iss], state: "s") }
        #expect(throws: OAuthRefusal.self) {
            try client.callback(["code": "c1", "state": "s", "iss": "https://evil.example.com"], state: "s")
        }
        // The server says it names itself, so a redirect that does not is refused.
        #expect(throws: OAuthRefusal.self) { try client.callback(["code": "c1", "state": "s"], state: "s") }
        do {
            _ = try client.callback(["error": "access_denied", "state": "s", "iss": iss], state: "s")
            Issue.record("a refusal read as a code")
        } catch let refusal as OAuthRefusal {
            #expect(refusal.denied)
        }
    }

    @Test func eachServerTakesTheDoorItCanServe() {
        func door(_ metadata: AuthorizationServerMetadata?, version: String = "dev") -> SignInDoor {
            let verdict = ChatManifest.requirement.verdict(version: version, metadata: metadata)
            return ChatManifest.manifest.door(for: ServerProbe.Result(origin: origin, discovery: Discovery(server: .init(version: version)),
                                                                      metadata: metadata, verdict: verdict))
        }
        #expect(door(metadata) == .app)
        #expect(door(metadata, version: "v0.114.0") == .app)
        // App authorization that cannot reach the agents would refuse the
        // request with an error page: the server is too old.
        #expect(door(AuthorizationServerMetadata(issuer: origin, scopesSupported: ["read:*", "write:*"])) == .tooOld)
        #expect(door(AuthorizationServerMetadata(issuer: origin, scopesSupported: ["read:*", "write:*", "agents:*"])) == .tooOld)
        // A release before the agent verbs is too old whatever it lists.
        #expect(door(metadata, version: "v0.113.0") == .tooOld)
        // None at all takes the password, as before.
        #expect(door(nil) == .password)
        #expect(door(nil, version: "v0.113.0") == .password)
    }

    @Test func aProbedServerWithoutTheAgentVerbsIsTooOldAndOneWithoutAppsTakesThePassword() async throws {
        let between = Answers([
            "/.well-known/substrate/server.json": (200, #"{"server":{"version":"v0.113.0-12-gabc1234"}}"#),
            "/.well-known/oauth-authorization-server": (200, metadataJSON(scopes: ["read:*", "write:*"])),
        ])
        let probe = try await ServerProbe(transport: between).probe(origin, requirement: ChatManifest.requirement, app: ChatManifest.appName)
        #expect(ChatManifest.manifest.door(for: probe) == .tooOld)
        guard case let .tooOld(running, needed, missing) = probe.verdict else {
            Issue.record("a server without the agent verbs passed")
            return
        }
        #expect(running == "v0.113.0-12-gabc1234")
        #expect(needed == "0.114.0")
        #expect(missing == ["let apps talk to your agents", "let apps apply what your agents suggest"])

        let before = Answers([
            "/.well-known/substrate/server.json": (200, #"{"server":{"version":"v0.113.0"},"registration":{"totpRequired":false}}"#),
        ])
        let old = try await ServerProbe(transport: before).probe(origin, requirement: ChatManifest.requirement, app: ChatManifest.appName)
        #expect(old.metadata == nil)
        #expect(ChatManifest.manifest.door(for: old) == .password)
        #expect(!old.discovery.registration.totpRequired)

        let current = Answers([
            "/.well-known/substrate/server.json": (200, #"{"server":{"version":"dev"}}"#),
            "/.well-known/oauth-authorization-server": (200, metadataJSON(scopes: agentVerbs)),
        ])
        let now = try await ServerProbe(transport: current).probe(origin, requirement: ChatManifest.requirement, app: ChatManifest.appName)
        #expect(ChatManifest.manifest.door(for: now) == .app)
    }

    @Test(.timeLimit(.minutes(1))) func theLoopbackReceiverHandsOverTheRedirectsQuery() async throws {
        let receiver = try await LoopbackReceiver.start(appName: ChatManifest.appName, path: ChatManifest.manifest.redirectPath)
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
        #expect(String(decoding: body, as: UTF8.self).contains("Signed in. You can go back to Substrate Chat."))
    }
}

/// The session as every build before the kit kept it, field for field.
private struct LegacySession: Codable {
    struct Grants: Codable { var scopes: [String] }
    var server: URL
    var repository: String
    var secret: String
    var tokenID: String
    var grants: Grants?
    var expiresAt: Date?
    var revocation: URL?
}

@Suite struct KeptSessions {
    private let ends = Date(timeIntervalSinceReferenceDate: 815_000_000)

    private var app: LegacySession {
        LegacySession(server: origin, repository: "r.example.com", secret: "x", tokenID: "",
                      grants: .init(scopes: ChatManifest.scopes), expiresAt: ends,
                      revocation: URL(string: "https://s.example.com/oauth/revoke"))
    }

    @Test func anAppSessionAnEarlierBuildKeptStillReads() throws {
        let read = try JSONDecoder().decode(Session.self, from: JSONEncoder().encode(app))
        #expect(read.isApp)
        #expect(read.scopes == ChatManifest.scopes)
        #expect(read.grants?.talks == true)
        #expect(read.expiresAt == ends)
        #expect(read.revocation == app.revocation)
        #expect(read.tokenID == nil)
        #expect(read.clientID == nil)
        #expect(read.identity == "https://s.example.com|r.example.com")
        // It names no client id; Sign Out revokes under this build's, as the
        // build that kept it did.
        #expect(AppModel.revocable(read).clientID == ChatManifest.manifest.clientID)

        // What this build keeps, an earlier one still reads: a rollback keeps
        // the sign-in.
        let back = try JSONDecoder().decode(LegacySession.self, from: JSONEncoder().encode(read))
        #expect(back.grants?.scopes == ChatManifest.scopes)
        #expect(back.expiresAt == ends)
        #expect(back.tokenID == "")
    }

    @Test func aPasswordSessionAnEarlierBuildKeptStillReads() throws {
        let old = #"{"server":"https://s.example.com","repository":"r.example.com","secret":"x","tokenID":"t"}"#
        let session = try JSONDecoder().decode(Session.self, from: Data(old.utf8))
        #expect(!session.isApp)
        #expect(session.grants == nil)
        #expect(session.tokenID == "t")
        #expect(session.expiresAt == nil)
        #expect(AppModel.revocable(session).clientID == nil)
    }

    @Test func theKeychainItemIsWhereEveryBuildKeptIt() throws {
        let store = SessionStore.chat
        #expect(store.service == "dev.reamde.substrate.chat")
        #expect(store.account == "session")
        #expect(store.accessGroup == nil)
        #expect(store.accessibility == .afterFirstUnlockThisDeviceOnly)
        #expect(!store.dataProtection)

        // The bytes an earlier build wrote, read back through the kit's store
        // under the same service.
        let scratch = SessionStore(service: store.service, account: "test-\(UUID().uuidString)")
        defer { scratch.clear() }
        try scratch.write(JSONEncoder().encode(app), account: scratch.account)
        guard case let .found(session) = scratch.read() else {
            Issue.record("the kept session did not read")
            return
        }
        #expect(session.scopes == ChatManifest.scopes)
        #expect(session.expiresAt == ends)
    }
}

@Suite struct GrantReach {
    @Test func aReadScopeReachesItsKindsAndAGlobStopsAtTheInternalPackages() {
        let all = Grants(["agents:*", "read:*"])
        #expect(all.reads("a.example.com/people/person"))
        #expect(!all.reads("substrate.reamde.dev/core/kind"))
        #expect(!all.reads("substrate.reamde.dev/llm/provider"))
        let some = Grants(["read:a.example.com/tasks/*", "write:substrate.reamde.dev/core/kind"])
        #expect(some.reads("a.example.com/tasks/task"))
        #expect(!some.reads("a.example.com/people/person"))
        #expect(some.reads("substrate.reamde.dev/core/kind"))
        #expect(!Grants(["agents:*"]).reads("a.example.com/people/person"))
    }

    @MainActor @Test func theStoreSaysWhatTheTokenMayDo() {
        func store(_ scopes: [String]?) -> ChatStore {
            let id = UUID().uuidString.lowercased()
            return ChatStore(session: Session(server: URL(string: "https://\(id).example.com")!, repository: "\(id).example.com",
                                              secret: "x", clientID: scopes == nil ? nil : ChatManifest.localClientID,
                                              scopes: scopes))
        }
        let all = store(ChatManifest.scopes)
        defer { all.close(forget: true) }
        #expect(all.canChat && all.canDecide && all.canReadProviders)
        #expect(!all.canWriteKeys)

        let chatOnly = store(["agents:x.example.com/llm/helper"])
        defer { chatOnly.close(forget: true) }
        #expect(chatOnly.canChat && !chatOnly.canDecide && !chatOnly.canReadProviders)

        // A glob never reaches the provider rows: llm is internal.
        let globs = store(["agents:*", "read:*", "read:substrate.reamde.dev/*"])
        defer { globs.close(forget: true) }
        #expect(!globs.canReadProviders)

        let password = store(nil)
        defer { password.close(forget: true) }
        #expect(password.canChat && password.canDecide && password.canReadProviders && password.canWriteKeys)
    }

    @Test func aKindOutOfReachIsSaidSoWithWhereToAllowIt() async {
        let api = API(server: origin)
        let app = ChatRecordReader(api: api, server: origin, grants: Grants(["agents:*"]))
        await #expect(throws: RecordReadError.self) {
            _ = try await app.record(RecordReference(kind: "a.example.com/people/person", id: "ada"))
        }
        #expect(app.deniedHint == "Substrate Chat wasn't approved to read these. Allow it with Settings → Change Access…")
        #expect(ChatRecordReader(api: api, server: origin, grants: nil).deniedHint == nil)
    }

    @Test func theApprovedAccessReadsAsTheApprovalPageSaysIt() {
        let words = Grants(ChatManifest.scopes).words
        #expect(words.count == 4)
        #expect(words[0].hasPrefix("Talk to all your agents, including agents added later; read every chat"))
        #expect(words[1].hasPrefix("Apply or dismiss the changes your agents suggest"))
        #expect(words[2].hasPrefix("See your"))
        #expect(words[3] == "See all your records, including kinds added later")
        #expect(ChatManifest.manifest.scopeWords("read:*") == words[3])
    }
}

@Suite struct StillSetUp {
    private func session(_ scopes: [String]) -> Session {
        Session(server: origin, repository: "r.example.com", secret: "x", clientID: ChatManifest.localClientID, scopes: scopes)
    }

    @Test func aScopeTheOwnerLeftOutIsNeverAskedAboutAgain() {
        let kept = session(["agents:*", "decide:*"])
        // The last approval asked for all four, and the owner kept two.
        #expect(ChatManifest.checked(session: kept, requested: ChatManifest.scopes).scopes == ["agents:*", "decide:*"])
        // A session from before the request was kept was asked for the same.
        #expect(ChatManifest.checked(session: kept, requested: nil).scopes == ["agents:*", "decide:*"])
        // A scope the last approval never asked for is asked about.
        #expect(ChatManifest.checked(session: kept, requested: ["agents:*", "decide:*"]).scopes == ChatManifest.scopes)
        let password = Session(server: origin, repository: "r.example.com", secret: "x", tokenID: "t")
        #expect(ChatManifest.checked(session: password, requested: nil).scopes == ChatManifest.scopes)
    }

    @Test func theCheckAsksForWhatANewerBuildNeedsAndTheBannerSaysIt() async throws {
        let kept = session(["agents:*", "decide:*"])
        let server = Answers([
            "/.well-known/substrate/server.json": (200, #"{"server":{"version":"dev"}}"#),
            "/.well-known/oauth-authorization-server": (200, metadataJSON(scopes: agentVerbs)),
            "/api/v1/authorizations/check": (200, #"{"scopes":{"missing":["read:*"]},"setup":[]}"#),
        ])
        // The provider rows were asked for and left out; `read:*` is new.
        let manifest = ChatManifest.checked(session: kept, requested: ["agents:*", "decide:*", "read:\(Kinds.provider)"])
        let status = await AppStatusCheck(manifest: manifest, session: kept,
                                          client: SubstrateClient(kept, transport: server)).run()
        #expect(status.issues == [.needsApproval(changes: [Change(kind: .scope, words: ScopeWords.sentence("read:*"))])])
        let check = try #require(server.requests.first { $0.url?.path == "/api/v1/authorizations/check" })
        let body = try JSONDecoder().decode(AuthorizationCheck.self, from: try #require(check.httpBody))
        #expect(body.scope == "agents:* decide:* read:*")
        #expect(body.details.isEmpty)
    }
}
