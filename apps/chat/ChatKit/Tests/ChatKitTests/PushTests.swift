import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

/// A registration this install holds, in a Keychain of its own, with a
/// publisher, a relay and a server's push routes that agree with it: a
/// check of it finds nothing to do and keeps it.
struct HeldPush {
    static let tokenID = "71ac09be44"
    static let device = "/api/v1/substrate.reamde.dev/core/device/71ac09be44"
    static let client = "https://apps.example.com/apps/chat/client.json"

    let storage = MemoryStorage()
    let transport = StubTransport()
    let registrar: PushRegistrar

    init(current: String = "gC", pending: String? = nil) {
        let sessions = SessionStore(storage: storage)
        registrar = PushRegistrar(
            sessions: sessions, device: ScriptedPush(), transport: transport, buildRelay: "https://push.example.com", clientID: Self.client)
        let session = Session(
            server: origin, repository: repository, secret: "secret", tokenID: Self.tokenID, clientID: Self.client, scopes: ["full"])
        func made(_ generation: String) -> PushState.Registration {
            .init(
                generation: generation, relay: "https://push.example.com", publicKey: "key", deviceToken: String(repeating: "ab", count: 32),
                environment: .development, notifies: ["replies", "requests"], registeredAt: Date())
        }
        try? storage.write(JSONEncoder().encode(session), account: sessions.account)
        try? storage.write(
            JSONEncoder().encode(PushState(tokenID: Self.tokenID, current: made(current), pending: pending.map(made))), account: registrar.account)
        transport.on(
            "GET", "/.well-known/substrate/authority.json",
            json: #"{"apps":[{"client_id":"\#(Self.client)","name":"Chat","push":{"relay":"https://push.example.com"}}]}"#)
        transport.on("GET", "/api/v1/push/key", json: #"{"publicKey":"key","relay":"https://push.example.com"}"#)
        transport.on(
            "GET", Self.device,
            json: #"{"id":"71ac09be44","kind":"substrate.reamde.dev/core/device","version":1,"properties":{"pushKey":"v1.k"}}"#)
    }
}

/// What a push from the relay hands the app, flattened.
private func pushed(_ event: String, thread: String, generation: String = "gC") -> [String: String] {
    [PushPayload.Key.event: event, PushPayload.Key.thread: thread, PushPayload.Key.generation: generation, PushPayload.Key.eventID: "e-\(thread)"]
}

@Suite("Pushes from the publisher's relay", .timeLimit(.minutes(10)))
struct PushTests {
    @Test func aTappedPushOpensItsChatOnlyForTheCurrentOrThePendingGeneration() async throws {
        let held = HeldPush(current: "gC", pending: "gP")
        let rig = ChatRig(push: held.registrar)
        try await rig.signIn()
        rig.notifications.respond(NotificationResponse(id: "a", category: nil, action: .open, userInfo: pushed("reply", thread: "t1")))
        try await rig.harness.until { await $0.state.route == .thread("t1") }
        rig.notifications.respond(
            NotificationResponse(id: "b", category: nil, action: .open, userInfo: pushed("request", thread: "t2", generation: "gP")))
        try await rig.harness.until { await $0.state.route == .thread("t2") }
        // Meant for an earlier registration: it opens no chat, the list.
        rig.notifications.respond(
            NotificationResponse(id: "c", category: nil, action: .open, userInfo: pushed("reply", thread: "t3", generation: "gOld")))
        try await rig.harness.until { await $0.state.route == .list }
        // A local notification names its chat at the top, as ever.
        rig.notifications.respond(NotificationResponse(id: "d", category: nil, action: .open, userInfo: [ChatNotifications.threadKey: "t4"]))
        try await rig.harness.until { await $0.state.route == .thread("t4") }
    }

    @Test func aTappedPushOpensTheListWithNoRegistrationHeld() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        rig.notifications.respond(NotificationResponse(id: "a", category: nil, action: .open, userInfo: pushed("reply", thread: "t1")))
        try await rig.harness.until { await $0.state.route == .list }
        // A window takes it: no chat is open in it.
        await rig.harness.send(.sceneAppeared(scene: "s", restoring: nil))
        _ = try await rig.newChat()
        await rig.harness.send(.claimRoute(scene: "s"))
        let state = await rig.harness.state
        #expect(state.route == nil && state.scenes["s"]?.current == nil)
    }

    @Test func whileARegistrationIsHeldNoReplyIsNotifiedByTheApp() async throws {
        let server = MarkServer()
        server.push(badge: 1)
        server.chat("t1", readThrough: "t1-m2")
        let held = HeldPush()
        let rig = ChatRig(marks: server, push: held.registrar)
        try await rig.signIn(following: server)
        try await rig.harness.until { await $0.state.push?.held == true }
        server.settle("t1")
        try await rig.harness.until(advancing: 1) { await $0.state.thread("t1")?.lastMessage == "t1-m3" }
        await rig.harness.advance(by: 10)
        await rig.harness.settle()
        #expect(rig.scheduled.isEmpty)
        // The registration was checked, and kept: nothing needed doing.
        #expect(await rig.harness.state.push?.standing == .registered)
        #expect(held.transport.sent("POST", "/v1/register").isEmpty)

        // Without one, the same reply is the app's to say.
        let plain = MarkServer()
        plain.chat("t1", readThrough: "t1-m2")
        let other = ChatRig(marks: plain)
        try await other.signIn(following: plain)
        plain.settle("t1")
        try await other.harness.until(advancing: 1) { _ in !other.scheduled.isEmpty }
    }

    @Test func whileARegistrationIsHeldNoCardIsNotifiedByTheApp() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .ios)
        var state = signedInState()
        state.baselined = true
        let question = Record(
            id: "q1", kind: Kinds.interaction,
            properties: [
                "state": "pending", "thread": ["ref": .string("\(Kinds.thread)/t1")],
                "questions": [["id": "a", "prompt": "Which one?", "options": [["value": "x"], ["value": "y"]]]],
            ], version: 2)
        let cards = ChatAppStep.Cards(interactions: [question], requests: [], approvals: [])
        let local = try app.apply(.init(.cardsRead(cards)), to: state)
        #expect(local.asks.contains { if case .notifyCard = $0 { true } else { false } })
        state.push = PushFacts(standing: .registered, held: true)
        let held = try app.apply(.init(.cardsRead(cards)), to: state)
        #expect(!held.asks.contains { if case .notifyCard = $0 { true } else { false } })
    }

    @Test func theBadgeIsTheServersWhereItListsPush() async throws {
        let server = MarkServer()
        server.push(badge: 7)
        server.chat("t1")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        try await rig.harness.until { _ in rig.badge == 7 }
        #expect(server.count("/api/v1/push/badge") > 0)
    }

    @Test func whereTheServerDoesNotPushTheBadgeCountsChatsAlone() {
        var state = ChatAppState()
        state.preferences.showAutomated = true
        func row(_ id: String, mode: String) -> Record {
            Record(
                id: id, kind: Kinds.thread, properties: ["mode": .string(mode), "status": "ok", "finishedAt": "2026-10-31T10:00:05Z"],
                version: 3)
        }
        state.chats = [row("c1", mode: "chat")]
        state.automated = [row("s1", mode: "schedule"), row("m1", mode: "manual")]
        #expect(state.threads.count == 3)
        #expect(state.threads.allSatisfy(state.unread))
        #expect(state.unreadCount == 1)
    }

    @Test func whatWaitsIsReadOverEveryConversationMode() async throws {
        let server = MarkServer(records: 9)
        server.chat("t1")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        let waiting = try #require(server.requests.first { $0.contains("waitingOnPerson") })
        for mode in ["chat", "manual", "call", "record", "schedule", "webhook"] {
            #expect(waiting.contains("\"\(mode)\""), "\(mode)")
        }
        #expect(await rig.harness.state.preferences.showAutomated == false)
    }

    @Test func comingToTheFrontTakesBackWhatThePushesAlertedThatIsSeen() async throws {
        let server = MarkServer()
        server.push(badge: 0)
        server.chat("t1", readThrough: "t1-m2")
        server.chat("t2")
        server.chat("t3", readThrough: "t3-m2")
        server.chat("t4", readThrough: "t4-m2", waiting: 1)
        let held = HeldPush()
        let rig = ChatRig(marks: server, platform: .ios, push: held.registrar)
        try await rig.signIn(following: server)
        try await rig.harness.until { await $0.state.push?.held == true }
        // What the pushes put up while the app was away.
        for (id, event, thread) in [
            ("r1", "reply", "t1"), ("f1", "failed", "t1"), ("r2", "reply", "t2"), ("q3", "request", "t3"), ("q4", "request", "t4"),
        ] {
            rig.notifications.deliver(DeliveredNotification(id: id, thread: thread, userInfo: pushed(event, thread: thread)))
        }
        await rig.harness.send(.phase(.background))
        await rig.harness.send(.phase(.active))
        try await rig.harness.until(advancing: 1) { _ in rig.notifications.standing.map(\.id).sorted() == ["q4", "r2"] }
        // A chat read is a reply's and a failure's; a chat with nothing
        // waiting is a request's.
        let state = await rig.harness.state
        #expect(state.pushRead.contains("t1") && !state.pushRead.contains("t2"))
        #expect(state.pushSettled.contains("t3") && !state.pushSettled.contains("t4"))
    }

    @Test func aChatOnScreenIsQuietInFront() async throws {
        let server = MarkServer()
        server.chat("t1", readThrough: "t1-m2")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        await rig.harness.send(.sceneAppeared(scene: "s", restoring: nil))
        _ = try await rig.open("t1")
        try await rig.harness.until { _ in rig.notifications.calls.contains(.showing(["t1"])) }
    }

    @Test func turningASwitchOrThePermissionChecksTheRegistration() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .ios)
        var state = signedInState()
        state.pushRevision = 1
        state.permission = .on
        let off = try app.apply(.init(.setting(row: "notifyReplies", toggle: false, choice: nil)), to: state)
        #expect(off.asks.contains(.syncPush(listed: true, notifies: ["requests"])))
        let both = try app.apply(.init(.setting(row: "notifyAttention", toggle: false, choice: nil)), to: off)
        #expect(both.asks.contains(.syncPush(listed: true, notifies: [])))
        let denied = try app.apply(.init(.permissionRead(.off)), to: state)
        #expect(denied.asks.contains { if case .syncPush = $0 { true } else { false } })
        // Before the server's document says whether it pushes, nothing is asked.
        state.pushRevision = nil
        let early = try app.apply(.init(.setting(row: "notifyReplies", toggle: false, choice: nil)), to: state)
        #expect(!early.asks.contains { if case .syncPush = $0 { true } else { false } })
    }

    @Test func theDocumentReadAtEveryRefreshChecksTheRegistration() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .ios)
        let state = signedInState()
        let listed = try app.apply(.init(.featuresRead(agents: 3, records: 9, push: 1)), to: state)
        #expect(listed.pushListed)
        #expect(listed.asks.contains(.syncPush(listed: true, notifies: ["replies", "requests"])))
        #expect(listed.asks.contains { if case .serverBadge = $0 { true } else { false } })
        let unlisted = try app.apply(.init(.featuresRead(agents: 3, records: 9)), to: state)
        #expect(unlisted.asks.contains(.syncPush(listed: false, notifies: ["replies", "requests"])))
        #expect(!unlisted.asks.contains { if case .serverBadge = $0 { true } else { false } })
    }

    @Test func reconcilingTakesBackOnlyWhatTheReadsSay() async {
        let notifications = RecordedNotifications()
        for (id, event, thread) in [("r1", "reply", "t1"), ("r2", "reply", "t2"), ("q1", "request", "t1"), ("q2", "request", "t2"), ("x", "read", "t2")] {
            notifications.deliver(DeliveredNotification(id: id, thread: thread, userInfo: pushed(event, thread: thread)))
        }
        // A local one is the app's own, and its rules take it back.
        notifications.deliver(DeliveredNotification(id: "chat|t1|reply", thread: "t1", userInfo: [ChatNotifications.threadKey: "t1"]))
        await ChatApp.reconcile(notifications, read: ["t1"], settled: ["t2"])
        #expect(notifications.standing.map(\.id).sorted() == ["chat|t1|reply", "q1", "r2"])
    }
}
