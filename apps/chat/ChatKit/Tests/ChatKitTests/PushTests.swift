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

    /// - Parameter named: the repository of the session the registrar
    ///   holds, where a test needs it to be another than the rig's.
    init(current: String = "gC", pending: String? = nil, named: String = repository) {
        let sessions = SessionStore(storage: storage)
        registrar = PushRegistrar(
            sessions: sessions, device: ScriptedPush(), transport: transport, buildRelay: "https://push.example.com", clientID: Self.client)
        let session = Session(
            server: origin, repository: named, secret: "secret", tokenID: Self.tokenID, clientID: Self.client, scopes: ["full"])
        func made(_ generation: String) -> PushState.Registration {
            .init(
                generation: generation, relay: "https://push.example.com", publicKey: "key", deviceToken: String(repeating: "ab", count: 32),
                environment: .development, notifies: ["replies", "requests"], registeredAt: Date())
        }
        try? storage.write(JSONEncoder().encode(session), account: sessions.account)
        try? storage.write(
            JSONEncoder().encode(PushState(tokenID: Self.tokenID, current: made(current), pending: pending.map { [made($0)] } ?? [])),
            account: registrar.account)
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

    /// The only unread chat is opened while the server is slow to take the
    /// mark: the server still counts it, and the badge never says so. Once
    /// the mark is answered the server is asked again.
    @Test func openingTheOnlyUnreadChatClearsTheBadgeBeforeTheServerTakesTheMark() async throws {
        let server = MarkServer()
        server.push(.counted)
        server.chat("t1")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        try await rig.harness.until { _ in rig.badge == 1 }
        server.holdMarks()
        await rig.harness.send(.sceneAppeared(scene: "s", restoring: nil))
        let key = try await rig.open("t1")
        let before = rig.notifications.calls.count
        try await rig.see(key)
        // Read here, and the server not told yet: the lower count shows.
        #expect(!(await rig.harness.state.marking.isEmpty))
        try await rig.harness.until(advancing: 0.1) { _ in rig.badge == 0 }
        let asked = server.count("/api/v1/push/badge")
        server.letGoMarks()
        // The mark's write is due a few seconds on, and then answered.
        for _ in 0 ..< 6 {
            await rig.harness.advance(by: 1)
            await rig.harness.settle()
        }
        let state = await rig.harness.state
        #expect(state.marking.isEmpty && state.markSending.isEmpty && server.writes.count == 1)
        #expect(server.count("/api/v1/push/badge") > asked)
        #expect(rig.badge == 0)
        // Never the server's stale 1 after the chat was opened.
        #expect(!rig.notifications.calls.dropFirst(before).contains(.badge(1)))
    }

    /// Another device reads what this one never paged in: only the server's
    /// count says so, and it is asked each time the app comes to the front.
    /// A read that fails keeps the last count; push off is the app's own.
    @Test func theServersBadgeIsAskedAtEveryReturnAndKeptThroughAFailedRead() async throws {
        let server = MarkServer()
        server.push(badge: 3)
        server.chat("t1")
        let rig = ChatRig(marks: server, platform: .ios)
        try await rig.signIn(following: server)
        try await rig.harness.until { _ in rig.badge == 3 }
        server.push(badge: 5)
        await rig.harness.send(.phase(.background))
        await rig.harness.send(.phase(.active))
        try await rig.harness.until(advancing: 0.1) { _ in rig.badge == 5 }

        server.push(.failing)
        let calls = rig.notifications.calls.count
        let asked = server.count("/api/v1/push/badge")
        await rig.harness.send(.phase(.background))
        await rig.harness.send(.phase(.active))
        try await rig.harness.until(advancing: 0.1) { _ in server.count("/api/v1/push/badge") > asked + 1 }
        await rig.harness.settle()
        #expect(!rig.notifications.calls.dropFirst(calls).contains { if case .badge = $0 { true } else { false } })

        server.push(.off)
        await rig.harness.send(.phase(.background))
        await rig.harness.send(.phase(.active))
        try await rig.harness.until(advancing: 0.1) { _ in rig.badge == 1 }
    }

    /// A chat archived on another device, which the loaded lists no longer
    /// hold, and whose `read` push this phone never ran for: its alert goes
    /// when the app comes to the front, its thread read by name.
    @Test func anAlertInAChatTheListsDoNotHoldIsReconciledByName() async throws {
        let server = MarkServer(records: 9)
        server.push(badge: 0)
        server.chat("t1", readThrough: "t1-m2")
        server.chat("t9", readThrough: "t9-m2")
        server.write("t9", archived: true)
        let held = HeldPush()
        let rig = ChatRig(marks: server, platform: .ios, push: held.registrar)
        try await rig.signIn(following: server)
        try await rig.harness.until { await $0.state.push?.held == true }
        #expect(await rig.harness.state.thread("t9") == nil)
        rig.notifications.deliver(DeliveredNotification(id: "r9", thread: "t9", userInfo: pushed("reply", thread: "t9")))
        await rig.harness.send(.phase(.background))
        await rig.harness.send(.phase(.active))
        try await rig.harness.until(advancing: 0.5) { _ in rig.notifications.standing.isEmpty }
        #expect(await rig.harness.state.pushDelivered == ["t9"])
    }

    /// A launch iOS made for a tap or a refresh may never come to the
    /// front: the alerts standing on the device are reconciled all the same,
    /// and only this sign-in's name a chat to the server.
    @Test func aColdLaunchReconcilesWhatThisSignInsPushesAlertedAndAskesOfNoOther() async throws {
        let server = MarkServer(records: 9)
        server.push(badge: 0)
        server.chat("t1", readThrough: "t1-m2")
        server.chat("t9", readThrough: "t9-m2")
        server.write("t9", archived: true)
        let held = HeldPush()
        let rig = ChatRig(marks: server, platform: .ios, push: held.registrar)
        rig.notifications.deliver(DeliveredNotification(id: "r9", thread: "t9", userInfo: pushed("reply", thread: "t9")))
        rig.notifications.deliver(DeliveredNotification(id: "x8", thread: "t8", userInfo: pushed("reply", thread: "t8", generation: "gOther")))
        try await rig.signIn(following: server)
        try await rig.harness.until(advancing: 0.5) { _ in rig.notifications.standing.map(\.id) == ["x8"] }
        #expect(await rig.harness.state.pushDelivered == ["t9"])
        #expect(!server.requests.contains { $0.contains("t8") })
    }

    @Test func aKeychainThatCannotBeReadLeavesWhatWasKnown() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .ios)
        var state = signedInState()
        state.push = PushFacts(standing: .registered, held: true)
        state = try app.apply(.init(.pushSynced(PushFacts(standing: .unreadable))), to: state)
        #expect(state.pushHeld && state.push?.standing == .registered)
    }

    @Test func aMarkWriteThatFailedAsksTheServersBadgeAgain() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .ios)
        var state = signedInState()
        state.pushRevision = 1
        state = try app.apply(.init(.markWriteFailed(thread: "t1", sent: MarkChange(readThrough: "m1"), .later, nil)), to: state)
        #expect(state.asks.contains { if case .serverBadge = $0 { true } else { false } })
    }

    /// The device keeps the chats' state on disk; Apple's token is in the
    /// registration's Keychain item alone, never there.
    @Test func noFileTheAppWritesHoldsApplesToken() async throws {
        let server = MarkServer()
        server.push(badge: 0)
        server.chat("t1", readThrough: "t1-m2")
        let held = HeldPush()
        let rig = ChatRig(marks: server, platform: .ios, push: held.registrar)
        try await rig.signIn(following: server)
        try await rig.harness.until { await $0.state.push?.deviceToken != nil }
        await rig.harness.send(.diagnostics(shown: true))
        await rig.harness.send(.phase(.background))
        await rig.harness.advance(by: 5)
        await rig.harness.settle()
        let token = String(repeating: "ab", count: 32)
        var files = 0
        let walker = FileManager.default.enumerator(at: rig.base, includingPropertiesForKeys: nil)
        while let url = walker?.nextObject() as? URL {
            guard let data = try? Data(contentsOf: url) else { continue }
            files += 1
            #expect(!String(decoding: data, as: UTF8.self).contains(token), "\(url.lastPathComponent)")
        }
        #expect(files > 0)
        #expect(await rig.harness.presentation.diagnostics?.text.contains(token) == false)
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
        #expect(off.asks.contains(.syncPush(listed: true, notifies: ["requests"], tokenID: nil)))
        let both = try app.apply(.init(.setting(row: "notifyAttention", toggle: false, choice: nil)), to: off)
        #expect(both.asks.contains(.syncPush(listed: true, notifies: [], tokenID: nil)))
        let denied = try app.apply(.init(.permissionRead(.off)), to: state)
        #expect(denied.asks.contains { if case .syncPush = $0 { true } else { false } })
        // Before the server's document says whether it pushes, nothing is asked.
        state.pushRevision = nil
        let early = try app.apply(.init(.setting(row: "notifyReplies", toggle: false, choice: nil)), to: state)
        #expect(!early.asks.contains { if case .syncPush = $0 { true } else { false } })
    }

    /// A sync is asked for the sign-in the app holds, by its token's id, as
    /// it is asked: however late it runs, it is for that sign-in.
    @Test func aSyncIsAskedForTheSignInTheAppHolds() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .ios)
        let state = signedInState()
        var link = LinkSnapshot(phase: .signedIn, server: origin, repository: repository, tokenID: "0b5e1a7c33")
        link.online = true
        let linked = try app.apply(.init(.linked(link)), to: state)
        #expect(linked.session?.tokenID == "0b5e1a7c33")
        let listed = try app.apply(.init(.featuresRead(agents: 3, records: 9, push: 1)), to: linked)
        #expect(listed.asks.contains(.syncPush(listed: true, notifies: ["replies", "requests"], tokenID: "0b5e1a7c33")))
    }

    @Test func theDocumentReadAtEveryRefreshChecksTheRegistration() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .ios)
        let state = signedInState()
        let listed = try app.apply(.init(.featuresRead(agents: 3, records: 9, push: 1)), to: state)
        #expect(listed.pushListed)
        #expect(listed.asks.contains(.syncPush(listed: true, notifies: ["replies", "requests"], tokenID: nil)))
        #expect(listed.asks.contains { if case .serverBadge = $0 { true } else { false } })
        let unlisted = try app.apply(.init(.featuresRead(agents: 3, records: 9)), to: state)
        #expect(unlisted.asks.contains(.syncPush(listed: false, notifies: ["replies", "requests"], tokenID: nil)))
        #expect(!unlisted.asks.contains { if case .serverBadge = $0 { true } else { false } })
    }

    @Test func aRequestsAlertGoesWithItsOwnCardThoughAnotherInTheChatWaits() async {
        let notifications = RecordedNotifications()
        for (id, card) in [("q1", "c1"), ("q2", "c2"), ("q0", nil)] as [(String, String?)] {
            var info = pushed("request", thread: "t1")
            if let card { info[ChatNotifications.cardKey] = card }
            notifications.deliver(DeliveredNotification(id: id, thread: "t1", userInfo: info))
        }
        await ChatApp.reconcile(notifications, read: [], settled: [], pending: ["t1": ["c2"]])
        #expect(notifications.standing.map(\.id).sorted() == ["q0", "q2"])
        // Cards of a chat whose cards were not read say nothing.
        await ChatApp.reconcile(notifications, read: [], settled: [], pending: [:])
        #expect(notifications.standing.count == 2)
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

    /// A pushed decision whose session is gone by the time it is sent is its
    /// own outcome: nothing is written, nothing is read with the session
    /// held now, no failure stands on the card, and the person is sent to
    /// the chat to decide.
    @Test func aPushedDecisionThatLostItsSessionSendsThePersonToTheChatToDecide() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .ios)
        let state = signedInState()
        var info = pushed("request", thread: "t1")
        info[ChatNotifications.threadKey] = "t1"
        info[ChatNotifications.cardKey] = "ap1"
        info[ChatNotifications.versionKey] = "4"
        info[ChatPush.sessionKey] = try #require(state.session?.identity)
        let response = NotificationResponse(id: "apns-1", category: ChatNotifications.approvalCategory, action: .named("approve"), userInfo: info)
        let acted = try app.apply(.init(.notificationActed(response, key: ChildKey("k"), message: "m", at: Date())), to: state)
        #expect(acted.asks.contains { if case .writeCard("ap1", _, _, 4, "gC"?) = $0 { true } else { false } })
        let unbound = try app.apply(.init(.cardWriteUnbound("ap1")), to: acted)
        #expect(unbound.acting.isEmpty)
        #expect(unbound.cards["ap1"]?.busy == nil && unbound.cards["ap1"]?.problem == nil)
        #expect(!unbound.asks.contains { if case .readCard = $0 { true } else { false } })
        #expect(unbound.asks.contains(.finishAction(response, cancel: nil, notify: ChatNotifications.refused())))
        #expect(ChatNotifications.refused().body == "Open the chat to decide.")
    }

    /// Only an accepted push's response names its session: one for another
    /// registration that came with the key anyway decides nothing.
    @Test func aSessionNamedByAPushThatWasNotAcceptedIsDropped() async throws {
        let held = HeldPush(current: "gC")
        let rig = ChatRig(push: held.registrar)
        try await rig.signIn()
        let identity = try #require(await rig.harness.state.session?.identity)
        var info = pushed("request", thread: "t1", generation: "gOld")
        info[ChatNotifications.threadKey] = "t1"
        info[ChatNotifications.cardKey] = "ap1"
        info[ChatNotifications.versionKey] = "4"
        info[ChatPush.sessionKey] = identity
        rig.notifications.respond(NotificationResponse(id: "apns-1", category: ChatNotifications.approvalCategory, action: .named("approve"), userInfo: info))
        try await rig.harness.until { _ in rig.notifications.calls.contains { if case .finished = $0 { true } else { false } } }
        await rig.harness.settle()
        #expect(!rig.server.requests.contains { $0.contains("approval/ap1") })
        #expect(held.transport.requests.allSatisfy { !($0.url?.path.contains("approval") ?? false) })
        #expect(rig.notifications.calls.contains { if case let .schedule(request) = $0 { request.id == "chat|refused" } else { false } })
    }
}
