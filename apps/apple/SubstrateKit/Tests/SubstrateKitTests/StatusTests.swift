import Foundation
import Testing
@testable import SubstrateKit

/// A transport that holds every request until it is released.
final class HeldTransport: HTTPTransport, @unchecked Sendable {
    let inner: StubTransport
    private let lock = NSLock()
    private var waiting: [CheckedContinuation<Void, Never>] = []
    private var open = false

    init(_ inner: StubTransport) {
        self.inner = inner
    }

    func waitUntilHeld() async throws {
        for _ in 0 ..< 1000 {
            if lock.withLock({ !waiting.isEmpty }) { return }
            try await Task.sleep(for: .milliseconds(5))
        }
        Issue.record("no request was held")
    }

    func release() {
        let waiters = lock.withLock {
            open = true
            defer { waiting = [] }
            return waiting
        }
        for waiter in waiters { waiter.resume() }
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        await withCheckedContinuation { continuation in
            let go = lock.withLock {
                if open { return true }
                waiting.append(continuation)
                return false
            }
            if go { continuation.resume() }
        }
        return try await inner.data(for: request)
    }
}

final class TransportBox: @unchecked Sendable {
    var transport: any HTTPTransport
    init(_ transport: any HTTPTransport) { self.transport = transport }
}

@Suite struct Status {
    static let now = Date(timeIntervalSince1970: 1_800_000_000)
    let server = URL(string: "https://ada.example.com")!

    func appSession(expiresAt: Date? = nil, webhook: Bool = true) -> Session {
        var scopes = syncManifest().scopes
        if webhook { scopes.append("webhook:applehealth-webhook") }
        return Session(server: server, repository: "ada.example.com", secret: "sub_app_x",
                       clientID: syncManifest().clientID, scopes: scopes, expiresAt: expiresAt)
    }

    func run(_ stub: StubTransport, session: Session, manifest: AppManifest = syncManifest()) async -> AppStatus {
        var client = SubstrateClient(session, transport: stub)
        client.pause = { _ in }
        return await AppStatusCheck(manifest: manifest, session: session, client: client, now: { Self.now }).run()
    }

    func server(version: String = "v0.114.0", details: [String] = AuthorizationDetail.Types.all, metadata: Bool = true,
                check: String? = nil, checkStatus: Int = 200) -> StubTransport
    {
        let stub = StubTransport()
        stub.on("GET", "/.well-known/substrate/server.json", json: discoveryJSON(version: version))
        if metadata {
            stub.on("GET", "/.well-known/oauth-authorization-server", json: metadataJSON(details: details))
        } else {
            stub.on("GET", "/.well-known/oauth-authorization-server", status: 404, json: "404 page not found")
        }
        if let check { stub.on("POST", "/api/v1/authorizations/check", status: checkStatus, json: check) }
        return stub
    }

    /// The setup rows of Sync's request, each with `actions[i]`.
    func rows(_ actions: [String], held: Int = 3, shipped: Int = 3) -> String {
        let facts = [
            #""package":{"id":"providers.substrate.reamde.dev/apple","required":3,"held":\#(held),"shipped":\#(shipped)}"#,
            #""package":{"id":"providers.substrate.reamde.dev/applehealth","required":5,"held":5,"shipped":5}"#,
            #""account":{"kind":"providers.substrate.reamde.dev/applehealth/account","id":"substrate-sync-1a2b3c4d"}"#,
            #""webhook":{"trigger":"applehealth-webhook"}"#,
        ]
        let types = ["package", "package", "account", "webhook"]
        return "[" + zip(zip(types, actions), facts).map { #"{"type":"\#($0.0.0)","action":"\#($0.0.1)",\#($0.1)}"# }
            .joined(separator: ",") + "]"
    }

    @Test func aGrownRequestNeedsTheOwnersApproval() async throws {
        let stub = server(check: #"""
        {"scopes":{"missing":["write:providers.substrate.reamde.dev/apple/place"]},
         "setup":\#(rows(["upgrade", "none", "none", "none"], held: 2))}
        """#)
        let status = await run(stub, session: appSession())
        #expect(status.issues == [.needsApproval(changes: [
            Change(kind: .scope, words: "See, add, change and delete your Apple places"),
            Change(kind: .upgrade, words: "Update Apple", detail: "Version 2 to 3"),
        ])])
        // The body names its details `details`, as the API's other bodies
        // are camelCase.
        let body = try #require(stub.sent("POST", "/api/v1/authorizations/check").first?.httpBody)
        let sentBody = try #require(try JSONSerialization.jsonObject(with: body) as? [String: Any])
        #expect((sentBody["details"] as? [Any])?.count == 4)
        #expect(sentBody["authorization_details"] == nil)
        #expect(status.serverVersion == "v0.114.0")
        let sent = try #require(stub.sent("POST", "/api/v1/authorizations/check").first)
        #expect(sent.value(forHTTPHeaderField: "Authorization") == "Bearer sub_app_x")
    }

    @Test func aMissingAccountAndAnOffWebhookAreNamedInTheConsolesWords() async {
        let stub = server(check: #"{"scopes":{"missing":[]},"setup":\#(rows(["none", "install", "create", "enable"]))}"#)
        let status = await run(stub, session: appSession())
        #expect(status.changes == [
            Change(kind: .install, words: "Install Apple Health", detail: "Version 5"),
            Change(kind: .account, words: "Create the Apple Health account “Ada's iPhone”", detail: "Timezone: Europe/London"),
            Change(kind: .webhook, words: "Turn on the Apple Health webhook"),
        ])
    }

    @Test func rowsTheTokenCannotSeeAreNeverAReasonToAsk() async {
        let stub = server(check: #"{"scopes":{"missing":[]},"setup":\#(rows(["unknown", "unknown", "unknown", "unknown"]))}"#)
        let status = await run(stub, session: appSession())
        #expect(status.isClear)
    }

    @Test func aWebhookTheTokenDoesNotDeliverToIsAsked() async {
        let stub = server(check: #"{"scopes":{"missing":[]},"setup":\#(rows(["none", "none", "none", "unknown"]))}"#)
        let status = await run(stub, session: appSession(webhook: false))
        #expect(status.changes == [Change(kind: .scope, words: "Send data to your repository through the Apple Health webhook, for any Apple Health account, and see every delivery to it that could not be filed, whoever sent it")])
    }

    /// The package a webhook delivers to is the server's once a check names
    /// it, and the app's own word for it only until then; nothing is guessed
    /// from the trigger's name.
    @Test func aWebhookIsNamedForThePackageTheServerSaysItReaches() async {
        var manifest = syncManifest()
        manifest.details[3] = .webhook(trigger: "health-export")
        #expect(manifest.scopeWords("webhook:health-export")
            == "Send data to your repository through the health-export webhook, and see every delivery to it that could not be filed, whoever sent it")
        let stub = server(check: #"""
        {"scopes":{"missing":[]},"setup":[{"type":"package","action":"none"},{"type":"package","action":"none"},
          {"type":"account","action":"none"},
          {"type":"webhook","action":"enable","webhook":{"trigger":"health-export","package":"providers.substrate.reamde.dev/applehealth"}}]}
        """#)
        let status = await run(stub, session: appSession(webhook: false), manifest: manifest)
        #expect(status.changes.map(\.words) == [
            "Send data to your repository through the Apple Health webhook, for any Apple Health account, and see every delivery to it that could not be filed, whoever sent it",
            "Turn on the Apple Health webhook",
        ])
    }

    @Test func aServerTooOldIsSaidAndAnAppSessionIsNotCheckedAgainstIt() async {
        let stub = server(version: "v0.113.0", details: [])
        let status = await run(stub, session: appSession())
        #expect(status.issues == [.serverTooOld(running: "v0.113.0", needed: "0.114.0", missing: [
            "install what an app needs when you approve it", "create an app's account when you approve it",
            "turn on an app's webhook when you approve it",
        ])])
        #expect(stub.sent("POST", "/api/v1/authorizations/check").isEmpty)
    }

    /// A package the catalog ships too old is a server update, worded by
    /// what the row would do, with the server's sentence under it.
    @Test func aPackageTheServerShipsTooOldIsAServerUpdate() async {
        let block = AuthorizationSetup.Block(reason: "server-update", problem: Blocks.serverUpdate(shipped: 2, need: 3))
        let stub = server(check: #"""
        {"scopes":{"missing":[]},"setup":[{"type":"package","action":"upgrade",
          "blocked":{"reason":"server-update","problem":"\#(block.problem)"},
          "package":{"id":"providers.substrate.reamde.dev/apple","required":3,"held":2,"shipped":2}}]}
        """#)
        let status = await run(stub, session: appSession())
        #expect(status.issues == [.serverTooOld(running: "v0.114.0", needed: nil, missing: [], blocked: [
            Change(kind: .upgrade, words: "Update Apple to version 3 or later", detail: block.problem, block: block),
        ])])
        #expect(status.issues.waitsForUpdate)
        // A package the catalog does not ship at all is a block like any
        // other, whatever its versions say: the reason decides.
        let unshipped = server(check: #"""
        {"scopes":{"missing":[]},"setup":[{"type":"package","action":"install",
          "blocked":{"reason":"not-shipped","problem":"\#(Blocks.notShipped)"},
          "package":{"id":"providers.substrate.reamde.dev/apple","required":3}}]}
        """#)
        let blocked = await run(unshipped, session: appSession())
        #expect(blocked.changes == [Change(kind: .install, words: "Install Apple", detail: Blocks.notShipped,
                                           block: AuthorizationSetup.Block(reason: "not-shipped", problem: Blocks.notShipped))])
        #expect(blocked.issues.fix == nil)
    }

    /// A package the catalog ships at the version the app needs, whose
    /// requirement it ships too old: the row says what the app asked for,
    /// and the server's sentence that it is a package it needs first, never
    /// that this package is too old.
    @Test func aBlockedPrerequisiteIsSaidInTheServersWords() async {
        let block = AuthorizationSetup.Block(reason: "server-update", problem: Blocks.serverUpdateFirst(need: 2, shipped: 1))
        let stub = server(check: #"""
        {"scopes":{"missing":[]},"setup":[
          {"type":"package","action":"none","package":{"id":"providers.substrate.reamde.dev/apple","required":3,"held":3,"shipped":3}},
          {"type":"package","action":"install","blocked":{"reason":"server-update","problem":"\#(block.problem)"},
           "package":{"id":"providers.substrate.reamde.dev/applehealth","required":5,"shipped":5}}]}
        """#)
        let status = await run(stub, session: appSession())
        let change = Change(kind: .install, words: "Install Apple Health version 5 or later", detail: block.problem, block: block)
        #expect(status.issues == [.serverTooOld(running: "v0.114.0", needed: nil, missing: [], blocked: [change])])
        #expect(status.changes.isEmpty)
        #expect(status.approvalBlocked)
    }

    /// An update names the version the server will install, and the one the
    /// app needs where that is another.
    @Test func anUpdateNamesTheVersionItLandsAt() async {
        let stub = server(check: #"{"scopes":{"missing":[]},"setup":\#(rows(["upgrade", "none", "none", "none"], held: 2, shipped: 4))}"#)
        #expect(await run(stub, session: appSession()).changes
            == [Change(kind: .upgrade, words: "Update Apple", detail: "Version 2 to 4 (needs 3 or later)")])
    }

    @Test func accessEndingSoonOrEndedIsSaid() async {
        let soon = Self.now.addingTimeInterval(3 * 86400)
        let stub = server(check: #"{"scopes":{"missing":[]},"setup":[],"expiresAt":"\#(Timestamp.format(soon))"}"#)
        #expect(await run(stub, session: appSession()).issues == [.accessEndsSoon(soon)])
        // Far off is nothing to say.
        let later = server(check: #"{"scopes":{"missing":[]},"setup":[],"expiresAt":"2030-01-01T00:00:00Z"}"#)
        #expect(await run(later, session: appSession()).isClear)
        // Ended here already: the server is not asked.
        let ended = StubTransport()
        #expect(await run(ended, session: appSession(expiresAt: Self.now.addingTimeInterval(-1))).issues == [.accessEnded])
        #expect(ended.requests.isEmpty)
        // Ended on the server: revoked.
        let revoked = server(check: #"{"error":{"code":"unauthorized","message":"token revoked"}}"#, checkStatus: 401)
        #expect(await run(revoked, session: appSession()).issues == [.accessEnded])
    }

    /// The server offers access for a week: it is not ending from the moment
    /// it was approved, or a renewal would mint another that is.
    @Test func accessApprovedForAWeekEndsSoonOnlyNearItsEnd() async {
        let week: TimeInterval = 7 * 86400
        func status(approved: Date, lifetime: TimeInterval) async -> AppStatus {
            let ends = approved.addingTimeInterval(lifetime)
            var session = appSession(expiresAt: ends)
            session.issuedAt = approved
            let stub = server(check: #"{"scopes":{"missing":[]},"setup":[],"expiresAt":"\#(Timestamp.format(ends))"}"#)
            return await run(stub, session: session)
        }
        #expect(await status(approved: Self.now, lifetime: week).isClear)
        #expect(await status(approved: Self.now.addingTimeInterval(-5 * 86400), lifetime: week).isClear)
        let late = Self.now.addingTimeInterval(-6 * 86400)
        #expect(await status(approved: late, lifetime: week).issues == [.accessEndsSoon(late.addingTimeInterval(week))])
        // A month's access hears of it a week ahead.
        let month: TimeInterval = 30 * 86400
        #expect(await status(approved: Self.now.addingTimeInterval(-22 * 86400), lifetime: month).isClear)
        let monthLate = Self.now.addingTimeInterval(-24 * 86400)
        #expect(await status(approved: monthLate, lifetime: month).issues == [.accessEndsSoon(monthLate.addingTimeInterval(month))])
    }

    /// A blocked row makes the approval page refuse: the server's sentence
    /// says why, and its fix, where it names one, is where the owner goes.
    @Test func aBlockedRowSaysWhyAndWhereItIsFixed() async {
        let quarantined = server(check: #"""
        {"scopes":{"missing":[]},"setup":[
          {"type":"package","action":"install","package":{"id":"providers.substrate.reamde.dev/apple","required":3,"shipped":3}},
          {"type":"package","action":"none","blocked":{"reason":"quarantined","problem":"\#(Blocks.quarantined)",
            "fix":"/catalog/providers.substrate.reamde.dev%2Fapplehealth"},
           "package":{"id":"providers.substrate.reamde.dev/applehealth","required":5,"held":5,"shipped":5}}]}
        """#)
        let blocked = await run(quarantined, session: appSession())
        #expect(blocked.changes.map(\.words) == ["Install Apple", "Apple Health is installed"])
        #expect(blocked.changes.last?.detail == Blocks.quarantined)
        #expect(blocked.approvalBlocked)
        #expect(blocked.issues.fix == "/catalog/providers.substrate.reamde.dev%2Fapplehealth")
        #expect(!blocked.issues.waitsForUpdate)
        let tooOld = server(check: #"""
        {"scopes":{"missing":["write:providers.substrate.reamde.dev/apple/place"]},"setup":[{"type":"package","action":"upgrade",
          "blocked":{"reason":"server-update","problem":"\#(Blocks.serverUpdate(shipped: 2, need: 3))"},
          "package":{"id":"providers.substrate.reamde.dev/apple","required":3,"held":2,"shipped":2}}]}
        """#)
        let waiting = await run(tooOld, session: appSession())
        #expect(waiting.approvalBlocked)
        #expect(waiting.issues.waitsForUpdate)
        let open = server(check: #"{"scopes":{"missing":[]},"setup":\#(rows(["none", "install", "none", "none"]))}"#)
        let approvable = await run(open, session: appSession())
        #expect(!approvable.changes.isEmpty)
        #expect(!approvable.approvalBlocked)
        #expect(ServerAddress.page(server, path: "/catalog/x")?.absoluteString == "https://ada.example.com/catalog/x")
        for path in ["https://evil.example.com/x", "//evil.example.com/x", "catalog/x", "/\\evil.example.com"] {
            #expect(ServerAddress.page(server, path: path) == nil, "\(path)")
        }
    }

    /// A scope the owner left out of the last approval is never asked about
    /// again; a review offers it back. A scope a later build adds is asked.
    @Test func aScopeTheOwnerLeftOutIsOfferedBackNotAsked() async throws {
        let place = "write:providers.substrate.reamde.dev/apple/place"
        var manifest = syncManifest()
        manifest.scopes.append(place)
        var session = appSession()
        session.scopes = ["write:providers.substrate.reamde.dev/apple/*", "webhook:applehealth-webhook"]
        session.requestedScopes = syncManifest().scopes
        #expect(manifest.leftOut(by: session) == [
            "read:providers.substrate.reamde.dev/applehealth/account", "read:providers.substrate.reamde.dev/applehealth/export",
        ])
        #expect(manifest.check(for: session).scope == "write:providers.substrate.reamde.dev/apple/* \(place)")
        let stub = server(check: #"{"scopes":{"missing":["\#(place)"]},"setup":\#(rows(["none", "none", "none", "none"]))}"#)
        let status = await run(stub, session: session, manifest: manifest)
        #expect(status.changes == [Change(kind: .scope, words: "See, add, change and delete your Apple places")])
        let review = manifest.review(status, session: session)
        #expect(review.filter(\.leftOut).map(\.words) == ["See your Apple Health accounts", "See your Apple Health deliveries"])
        #expect(ReviewHeading(changes: review, firstSignIn: false) == .moreAccess)
        #expect(ReviewHeading(changes: review.filter(\.leftOut), firstSignIn: false) == .renew)
        // `~` is the repository's authority, as the token stores it.
        var home = manifest
        home.scopes = ["read:~/notes/*"]
        var held = session
        held.scopes = ["read:ada.example.com/notes/*"]
        held.requestedScopes = ["read:~/notes/*"]
        #expect(home.leftOut(by: held).isEmpty)
        // A session that never kept its request left nothing out.
        held.requestedScopes = nil
        held.scopes = []
        #expect(home.leftOut(by: held).isEmpty)
    }

    /// The approval page words a kind by the label it declares, and so does
    /// the app wherever its manifest carries it.
    @Test func aKindIsWordedByItsDeclaredLabel() async {
        var bare = syncManifest()
        bare.kindLabels = [:]
        #expect(bare.scopeWords("read:providers.substrate.reamde.dev/applehealth/export") == "See your Apple Health exports")
        let manifest = syncManifest()
        #expect(manifest.scopeWords("read:providers.substrate.reamde.dev/applehealth/export") == "See your Apple Health deliveries")
        #expect(manifest.scopeWords("read:providers.substrate.reamde.dev/applehealth/account") == "See your Apple Health accounts")
        let stub = server(check: #"{"scopes":{"missing":["read:providers.substrate.reamde.dev/applehealth/export"]},"setup":\#(rows(["none", "none", "create", "none"]))}"#)
        #expect(await run(stub, session: appSession()).changes == [
            Change(kind: .scope, words: "See your Apple Health deliveries"),
            Change(kind: .account, words: "Create the Apple Health account “Ada's iPhone”", detail: "Timezone: Europe/London"),
        ])
    }

    /// The check came with set up on approval: a server whose metadata lists
    /// no detail types has none to ask, and approved the scopes as asked.
    @Test func aServerWithoutSetUpIsNotChecked() async {
        let stub = server(version: "dev", details: [])
        var manifest = syncManifest()
        manifest.details = []
        #expect(await run(stub, session: appSession(), manifest: manifest).isClear)
        #expect(stub.sent("POST", "/api/v1/authorizations/check").isEmpty)
    }

    /// A check runs at most once an interval unless forced, by the model's
    /// own clock.
    @MainActor @Test func theStatusIsCheckedAtMostOnceAnInterval() async {
        final class Clock { var now = Date(timeIntervalSince1970: 1_800_000_000) }
        let clock = Clock()
        let stub = server(check: #"{"scopes":{"missing":[]},"setup":[]}"#)
        let session = appSession()
        let model = AppStatusModel(interval: 300, now: { clock.now }) {
            var client = SubstrateClient(session, transport: stub)
            client.pause = { _ in }
            return AppStatusCheck(manifest: syncManifest(), session: session, client: client, now: { Self.now })
        }
        let checks = { stub.sent("POST", "/api/v1/authorizations/check").count }
        await model.refresh()
        #expect(checks() == 1)
        clock.now = clock.now.addingTimeInterval(299)
        await model.refresh()
        #expect(checks() == 1)
        await model.refresh(force: true)
        #expect(checks() == 2)
        clock.now = clock.now.addingTimeInterval(300)
        await model.refresh()
        #expect(checks() == 3)
    }

    /// A check started under one session never stands for the next: a reset
    /// drops its answer, and a forced refresh is never dropped for it.
    @MainActor @Test func aCheckFromBeforeAResetNeverStandsForTheNextSession() async throws {
        let before = HeldTransport(server(check: #"{"scopes":{"missing":["write:providers.substrate.reamde.dev/apple/place"]},"setup":[]}"#))
        let after = server(check: #"{"scopes":{"missing":[]},"setup":[]}"#)
        let current = TransportBox(before)
        let session = appSession()
        let model = AppStatusModel {
            var client = SubstrateClient(session, transport: current.transport)
            client.pause = { _ in }
            return AppStatusCheck(manifest: syncManifest(), session: session, client: client, now: { Self.now })
        }
        let first = Task { await model.refresh() }
        try await before.waitUntilHeld()
        model.reset()
        current.transport = after
        await model.refresh(force: true)
        #expect(model.status?.isClear == true)
        before.release()
        await first.value
        #expect(model.status?.isClear == true)
        #expect(!model.checking)

        // Forced while one runs, without a reset: the forced answer stands.
        let slow = HeldTransport(server(check: #"{"scopes":{"missing":["write:providers.substrate.reamde.dev/apple/place"]},"setup":[]}"#))
        current.transport = slow
        let second = Task { await model.refresh(force: true) }
        try await slow.waitUntilHeld()
        current.transport = after
        await model.refresh(force: true)
        slow.release()
        await second.value
        #expect(model.status?.isClear == true)
    }

    @Test func aPasswordSessionMovesOnceTheServerCan() async {
        let legacy = Session(server: server, repository: "ada.example.com", secret: "sub_x", tokenID: "tok_1")
        #expect(await run(server(), session: legacy).issues == [.legacyAccess])
        // On a server Sync falls back for, the password session is the
        // right one, and nothing is said.
        #expect(await run(server(version: "v0.112.0", metadata: false), session: legacy).isClear)
        // An app that never falls back says the server is too old.
        var strict = syncManifest()
        strict.passwordFallback = .never
        let status = await run(server(version: "v0.112.0", metadata: false), session: legacy, manifest: strict)
        #expect(status.issues.first?.id == "serverTooOld")
    }

    @Test func anUnreachableServerIsSaidBesideWhatTheSessionKnows() async {
        let stub = StubTransport()
        stub.fail("GET", "/.well-known/substrate/server.json", with: URLError(.cannotConnectToHost))
        let soon = Self.now.addingTimeInterval(86400)
        let status = await run(stub, session: appSession(expiresAt: soon))
        #expect(status.issues == [
            .unreachable(message: "Ada.example.com isn't answering."),
            .accessEndsSoon(soon),
        ])
    }

    @Test func aFirstSignInPlansEveryRow() throws {
        let changes = syncManifest().plannedChanges()
        #expect(changes.map(\.words) == [
            "See, add, change and delete your Apple records, including kinds added later",
            "See your Apple Health accounts",
            "See your Apple Health deliveries",
            "Send data to your repository through the Apple Health webhook, for any Apple Health account, and see every delivery to it that could not be filed, whoever sent it",
            "Install Apple",
            "Install Apple Health",
            "Create the Apple Health account “Ada's iPhone”",
            "Turn on the Apple Health webhook",
        ])
        #expect(ReviewHeading(changes: changes, firstSignIn: true) == .firstSignIn)
        #expect(ReviewHeading(changes: [changes[0]], firstSignIn: false) == .moreAccess)
        #expect(ReviewHeading(changes: [changes[4]], firstSignIn: false) == .serverSetup)
        #expect(ReviewHeading(changes: changes, firstSignIn: false) == .moreAccessAndServerSetup)
        #expect(ReviewHeading(changes: [], firstSignIn: false).title(appName: "Substrate Sync") == "Renew access")
        #expect(ReviewHeading.changeAccess.title(appName: "Substrate Sync") == "Change what Substrate Sync may do")
    }
}

/// What a device says of the tokens it stopped using: each server and
/// repository in its own words, never the session in use's.
@Suite struct Unrevoked {
    @Test func eachServerAndRepositoryIsSaidOnce() throws {
        let ada = try #require(URL(string: "https://ada.example.com"))
        let other = try #require(URL(string: "https://substrate.example.org"))
        let sentences = UnrevokedNotices.sentences(appName: "Substrate Sync", unrevoked: [
            Session(server: ada, repository: "ada.example.com", secret: "a", tokenID: "tok_1"),
            Session(server: other, repository: "grace.example.org", secret: "b", clientID: "https://sync.substrate.example.com/client.json",
                    scopes: ["read:*"]),
            Session(server: ada, repository: "ada.example.com", secret: "c", clientID: "https://sync.substrate.example.com/client.json",
                    scopes: ["read:*"]),
        ])
        #expect(sentences == [
            "Substrate Sync's earlier access still works on ada.example.com. It tries again each time it opens; "
                + "you can also revoke it yourself in ada.example.com's Settings, under Apps and under Signed in.",
            "Substrate Sync's earlier access to grace.example.org still works on substrate.example.org. It tries again each time it opens; "
                + "you can also revoke it yourself in substrate.example.org's Settings, under Apps.",
        ])
        #expect(UnrevokedNotices.sentences(appName: "Substrate Sync", unrevoked: []).isEmpty)
    }
}

/// Change Access names only the controls the approval page will show.
@MainActor @Suite struct ChangeAccess {
    let server = URL(string: "https://ada.example.com")!

    func sheet(_ manifest: AppManifest, clientID: String? = nil) -> ReviewChangesSheet {
        let session = Session(server: server, repository: "ada.example.com", secret: "sub_app_x", clientID: clientID ?? manifest.clientID,
                              scopes: manifest.scopes)
        return ReviewChangesSheet(changingAccessFor: manifest, session: session, changes: [], onContinue: {}, onCancel: {})
    }

    @Test func alreadyAllowedIsSaidOnlyWhereThePageListsIt() {
        let released = syncManifest()
        #expect(released.showsAlreadyAllowed(Session(server: server, repository: "ada.example.com", secret: "s",
                                                     clientID: released.clientID, scopes: ["read:*"])))
        #expect(sheet(released).intro == "You choose on ada.example.com: what Substrate Sync has now is listed there under "
            + "Already allowed, where you can uncheck it.")
        // A local build's page is always a first approval.
        var local = released
        local.clientID = "http://127.0.0.1/substrate-sync"
        #expect(local.isLocal)
        #expect(sheet(local).intro == "You choose on ada.example.com, where you can uncheck any access Substrate Sync asks for.")
        // How set up is refused is the footer's to say, once.
        #expect(ReviewChangesSections.choosing(host: "ada.example.com", setsUp: true).contains("to refuse it, deny"))
        // Approved for another client id, or a password session: nothing
        // is listed as already allowed.
        #expect(!released.showsAlreadyAllowed(Session(server: server, repository: "ada.example.com", secret: "s",
                                                      clientID: "https://other.example.com/client.json", scopes: ["read:*"])))
        #expect(!released.showsAlreadyAllowed(Session(server: server, repository: "ada.example.com", secret: "s", tokenID: "t")))
        // An approval that has ended folds nothing on the page.
        let ended = Session(server: server, repository: "ada.example.com", secret: "s", clientID: released.clientID,
                            scopes: ["read:*"], expiresAt: Date(timeIntervalSince1970: 1_000))
        #expect(!released.showsAlreadyAllowed(ended, at: Date(timeIntervalSince1970: 2_000)))
        #expect(released.showsAlreadyAllowed(ended, at: Date(timeIntervalSince1970: 500)))
        // A request that sets nothing up has no access that comes with it.
        var scopesOnly = released
        scopesOnly.details = []
        #expect(sheet(scopesOnly).intro
            == "You choose on ada.example.com: what Substrate Sync has now is listed there under Already allowed, where you can uncheck it.")
    }
}
