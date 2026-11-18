import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

/// The server's and the relay's push fixtures, as ChatKit's copies hold
/// them (`lint:apps` holds each to its original).
private enum Fixture {
    static func text(_ folder: String, _ name: String) throws -> String {
        let url = try #require(Bundle.module.url(forResource: "Fixtures/\(folder)/\(name)", withExtension: "json"))
        return try String(contentsOf: url, encoding: .utf8)
    }

    static func object(_ folder: String, _ name: String) throws -> [String: Any] {
        try #require(try JSONSerialization.jsonObject(with: Data(text(folder, name).utf8)) as? [String: Any])
    }
}

private let threadID = "8f2c41d07a"

/// A push as iOS hands it to the extension, flattened.
private func push(_ event: String, eventID: String, generation: String = "gC") -> [String: String] {
    [PushPayload.Key.event: event, PushPayload.Key.thread: threadID, PushPayload.Key.generation: generation, PushPayload.Key.eventID: eventID]
}

private let agentRef = "\(Kinds.agent)/x.example.com/pkg/helper"

/// The thread every fixture names, as its row reads.
private func threadRow(status: String = "ok", version: Int = 7, cause: String? = nil) -> String {
    let cause = cause.map { #","cause":"\#($0)""# } ?? ""
    return
        #"{"id":"\#(threadID)","kind":"\#(Kinds.thread)","version":\#(version),"createdAt":"2026-11-18T10:00:00Z","updatedAt":"2026-11-18T10:00:05Z","properties":{"mode":"chat","status":"\#(status)"\#(cause),"subject":"Weekend plans","agent":{"ref":"\#(agentRef)"},"finishedAt":"2026-11-18T10:00:05Z","lastReply":"An older reply","reason":"upstream said: overloaded_error"}}"#
}

private func question(_ id: String, version: Int = 1) -> String {
    #"{"id":"\#(id)","kind":"\#(Kinds.interaction)","version":\#(version),"createdAt":"2026-11-18T10:00:00Z","updatedAt":"2026-11-18T10:00:00Z","properties":{"state":"pending","thread":{"ref":"\#(Kinds.thread)/\#(threadID)"},"questions":[{"id":"q","prompt":"Which one?","options":[{"value":"a","label":"A"},{"value":"b","label":"B"}]}]}}"#
}

private func server(_ routes: [ChatServer.Route]) -> (ChatServer, SubstrateClient) {
    let server = ChatServer(routes)
    return (server, SubstrateClient(server: origin, token: "secret", transport: server))
}

private var later: ContinuousClock.Instant { .now + .seconds(60) }

/// A check of a push's generation that answers `verdict`, with `client` as
/// the session's that accepted it.
private func accepting(_ verdict: PushVerdict, _ client: SubstrateClient?) -> ChatPush.Accepting {
    let acceptance = PushAcceptance(verdict, client: client)
    return { _ in acceptance }
}

/// Work that ignores its cancellation, as a stuck Keychain or a slow system
/// call may.
private func uncancellable(seconds: Double) async {
    await withCheckedContinuation { continuation in
        DispatchQueue.global().asyncAfter(deadline: .now() + seconds) { continuation.resume() }
    }
}

/// A rig whose server is also the one a push's bound client reaches, as in
/// the app, where both go to the session's server; signed in, the server's
/// document read, and a chat no list holds, `t9`, ready to be read by name.
private func boundRig() async throws -> (ChatRig, ChatServer, HeldPush) {
    let held = HeldPush()
    let server = ChatServer(baseRoutes())
    server.route("GET", "/.well-known/substrate/server.json", featuresRoute(agents: 3).answer)
    server.route("GET", "/llm/thread/t9", .json(200, threadRow().replacingOccurrences(of: threadID, with: "t9")))
    let registrar = PushRegistrar(
        sessions: held.registrar.sessions, device: ScriptedPush(), transport: server, buildRelay: "https://push.example.com",
        clientID: HeldPush.client)
    let rig = ChatRig(transport: server, push: registrar)
    try await rig.signIn()
    try await rig.harness.until { await $0.state.agentsRevision == 3 }
    return (rig, server, held)
}

/// A reply typed into the notification of a push for `t9`.
private func pushedReply() -> NotificationResponse {
    let info: [String: String] = [
        ChatNotifications.threadKey: "t9", PushPayload.Key.event: "reply", PushPayload.Key.thread: "t9", PushPayload.Key.generation: "gC",
        PushPayload.Key.eventID: "e",
    ]
    return NotificationResponse(id: "apns-r", category: ChatNotifications.replyCategory, action: .named("reply"), userInfo: info, text: "On my way")
}

/// Another sign-in takes the Keychain the push's registration reads.
private func switchSession(_ held: HeldPush) async throws {
    var other = try #require(await held.registrar.sessions.load())
    other.tokenID = "0b5e1a7c33"
    other.secret = "another"
    try await held.registrar.sessions.save(other)
}

private func finished(_ rig: ChatRig) -> Bool {
    rig.notifications.calls.contains { if case .finished = $0 { true } else { false } }
}

/// The notices the actions posted, in order.
private func notices(_ rig: ChatRig) -> [NotificationRequest] {
    rig.notifications.calls.compactMap { call in
        if case let .schedule(request) = call, request.id == "chat|refused" || request.id.hasSuffix("|failed") { request } else { nil }
    }
}

/// Whether `condition` holds within `seconds`: a wait that fails the test,
/// never one that hangs it.
private func eventually(within seconds: Double = 15, _ condition: @Sendable () -> Bool) async -> Bool {
    let until = ContinuousClock.now + .seconds(seconds)
    while !condition() {
        guard ContinuousClock.now < until else { return false }
        try? await Task.sleep(for: .milliseconds(10))
    }
    return true
}

/// Whether a test's slow work ran.
private final class Ran: @unchecked Sendable {
    private let lock = NSLock()
    private var ran = false
    func mark() { lock.withLock { ran = true } }
    var value: Bool { lock.withLock { ran } }
}

@Suite("What a pushed notification shows", .timeLimit(.minutes(10)))
struct PushContentTests {
    @Test func aReplyShowsItsMessageAsTheLocalNotificationWould() async throws {
        let (_, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-reply", answer: .json(200, try Fixture.text("pushserver", "event-reply"))),
            .init(
                method: "GET", path: "/llm/message/5b90e3c1f2",
                answer: .json(
                    200,
                    #"{"id":"5b90e3c1f2","kind":"\#(Kinds.message)","version":1,"properties":{"role":"assistant","content":"**Here** is the summary you asked for.","thread":{"ref":"\#(Kinds.thread)/\#(threadID)"}}}"#
                )),
            .init(method: "GET", path: "/llm/thread/\(threadID)", answer: .json(200, threadRow())),
        ])
        let content = await ChatPush.content(push("reply", eventID: "e-reply"), accepting: accepting(.accepted, client), until: later)
        guard case let .shown(request) = content else {
            Issue.record("\(content)")
            return
        }
        // The local notification of the same thread, but for its words.
        let row = ChatThread(record: try JSONDecoder().decode(Record.self, from: Data(threadRow().utf8)))
        var local = ChatNotifications.reply(thread: row, agent: agentName("x.example.com/pkg/helper"), title: "Weekend plans", replies: true)
        local.body = "Here is the summary you asked for."
        #expect(request == local)
        #expect(request.thread == threadID && request.category == ChatNotifications.replyCategory)
    }

    @Test func aFailureShowsWhatItsCardSaysAndNeverTheServersWords() async throws {
        let (_, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-failed", answer: .json(200, try Fixture.text("pushserver", "event-failed"))),
            .init(method: "GET", path: "/llm/thread/\(threadID)", answer: .json(200, threadRow(status: "error", version: 9, cause: "provider_overloaded"))),
        ])
        let content = await ChatPush.content(push("failed", eventID: "e-failed"), accepting: accepting(.accepted, client), until: later)
        guard case let .shown(request) = content else {
            Issue.record("\(content)")
            return
        }
        #expect(request.thread == threadID && request.id == ChatNotifications.replyID(threadID))
        let body = try #require(request.body)
        #expect(!body.isEmpty && !body.contains("overloaded_error") && !body.contains("An older reply"))
        // A thread that ran again since is no failure to show.
        let (_, again) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-failed", answer: .json(200, try Fixture.text("pushserver", "event-failed"))),
            .init(method: "GET", path: "/llm/thread/\(threadID)", answer: .json(200, threadRow(status: "ok", version: 12))),
        ])
        #expect(
            await ChatPush.content(push("failed", eventID: "e-failed"), accepting: accepting(.accepted, again), until: later) == .fallback(dropsBadge: false))
    }

    @Test func twoCardsOnOneThreadEachShowItsOwnWithTheActionsItsResolutionAllows() async throws {
        let first = "01h2xcejqtf2nbrexx3vqjhp42"
        let second = "01h2xcejqtf2nbrexx3vqjhp43"
        let (_, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-1", answer: .json(200, try Fixture.text("pushserver", "event-request-card-1"))),
            .init(method: "GET", path: "/api/v1/push/events/e-2", answer: .json(200, try Fixture.text("pushserver", "event-request-card-2"))),
            .init(method: "GET", path: "/llm/interaction/\(first)", answer: .json(200, question(first))),
            .init(method: "GET", path: "/llm/interaction/\(second)", answer: .json(200, question(second))),
            .init(method: "GET", path: "/llm/thread/\(threadID)", answer: .json(200, threadRow())),
        ])
        let one = await ChatPush.content(push("request", eventID: "e-1"), accepting: accepting(.accepted, client), until: later)
        let two = await ChatPush.content(push("request", eventID: "e-2"), accepting: accepting(.accepted, client), until: later)
        guard case let .shown(a) = one, case let .shown(b) = two else {
            Issue.record("\(one) \(two)")
            return
        }
        #expect(a.userInfo[ChatNotifications.cardKey] == first && b.userInfo[ChatNotifications.cardKey] == second)
        #expect(a.userInfo[ChatNotifications.versionKey] == "1" && a.thread == threadID && b.thread == threadID)
        // A question opens its chat, and only while it may be decided; its
        // options are a category the extension cannot rely on.
        #expect(a.category == ChatNotifications.openCategory)
        #expect(b.category == nil)
        #expect(a.userInfo[ChatNotifications.questionKey] == nil && a.userInfo["option.0"] == nil)
        #expect(a.body == "Which one?")
    }

    @Test func anApprovalOffersItsDecisionOnlyWhileItIsPending() throws {
        let approval = Record(
            id: "ap1", kind: Kinds.approval,
            properties: ["state": "pending", "tool": "gmail_send", "thread": ["ref": .string("\(Kinds.thread)/\(threadID)")], "arguments": ["to": "a"]],
            version: 4)
        let open = ChatPush.card(approval, thread: threadID, row: nil, actions: true)
        #expect(open.category == ChatNotifications.approvalCategory && open.userInfo[ChatNotifications.versionKey] == "4")
        #expect(ChatPush.card(approval, thread: threadID, row: nil, actions: false).category == nil)
        var decided = approval
        decided.properties["state"] = "approved"
        #expect(ChatPush.card(decided, thread: threadID, row: nil, actions: true).category == nil)
        // The local notification and the pushed one decide alike: unlocked.
        let category = try #require(ChatNotifications.fixed.first { $0.id == ChatNotifications.approvalCategory })
        #expect(category.actions.allSatisfy { $0.requiresUnlock })
    }

    @Test func aCardThatMovedSinceIsTheFallback() async throws {
        let first = "01h2xcejqtf2nbrexx3vqjhp42"
        let (_, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-1", answer: .json(200, try Fixture.text("pushserver", "event-request-card-1"))),
            .init(method: "GET", path: "/llm/interaction/\(first)", answer: .json(200, question(first, version: 2))),
        ])
        #expect(await ChatPush.content(push("request", eventID: "e-1"), accepting: accepting(.accepted, client), until: later) == .fallback(dropsBadge: false))
    }

    @Test func aPushForAnotherRegistrationFetchesNothingAndDropsItsBadge() async {
        let (server, client) = server([])
        let content = await ChatPush.content(push("reply", eventID: "e-reply", generation: "gOld"), accepting: accepting(.refused, client), until: later)
        #expect(content == .fallback(dropsBadge: true))
        #expect(server.requests.isEmpty)
    }

    @Test func anUnreachableServerOrNoTimeLeftIsTheFallback() async throws {
        let (_, down) = server([.init(method: "GET", path: "/api/v1/push/events/e-reply", answer: .fail(.cannotConnectToHost))])
        #expect(await ChatPush.content(push("reply", eventID: "e-reply"), accepting: accepting(.accepted, down), until: later) == .fallback(dropsBadge: false))
        let (server, client) = server([])
        #expect(await ChatPush.content(push("reply", eventID: "e-reply"), accepting: accepting(.accepted, client), until: .now) == .fallback(dropsBadge: false))
        #expect(server.requests.isEmpty)
        #expect(await ChatPush.content(push("reply", eventID: "e-reply"), accepting: accepting(.accepted, nil), until: later) == .fallback(dropsBadge: false))
    }

    @Test func eventIDsAreNamedByTheWiresFingerprint() throws {
        let vectors = try #require(try JSONSerialization.jsonObject(with: Data(Fixture.text("pushwire", "fingerprints").utf8)) as? [[String: String]])
        #expect(!vectors.isEmpty)
        for vector in vectors {
            #expect(PushFingerprint.of(eventID: try #require(vector["eventId"])) == vector["fingerprint"])
        }
    }

    @Test func aReadTakesBackExactlyWhatItsResolutionCovers() async throws {
        let vectors = try #require(try JSONSerialization.jsonObject(with: Data(Fixture.text("pushwire", "fingerprints").utf8)) as? [[String: String]])
        let covered = try #require(vectors.first?["eventId"])
        let other = try #require(vectors.dropFirst().first?["eventId"])
        func world() -> RecordedNotifications {
            let notifications = RecordedNotifications()
            for (id, eventID) in [("covered", covered), ("other", other)] {
                notifications.deliver(
                    DeliveredNotification(
                        id: id, thread: threadID,
                        userInfo: [
                            PushPayload.Key.event: "reply", PushPayload.Key.thread: threadID, PushPayload.Key.generation: "gC",
                            PushPayload.Key.eventID: eventID,
                        ]))
            }
            // The app's own notification of the same thread is its own to
            // take back.
            notifications.deliver(DeliveredNotification(id: "chat|\(threadID)|reply", thread: threadID, userInfo: ["thread": threadID]))
            return notifications
        }
        let (_, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-read", answer: .json(200, try Fixture.text("pushserver", "event-read"))),
            .init(method: "GET", path: "/api/v1/push/badge", answer: .json(200, #"{"badge":2}"#)),
        ])
        let notifications = world()
        #expect(await ChatPush.takeBack(push("read", eventID: "e-read"), accepting: accepting(.accepted, client), notifications: notifications, until: later))
        #expect(notifications.standing.map(\.id).sorted() == ["chat|\(threadID)|reply", "other"])
        #expect(notifications.calls.contains(.badge(2)))
        // One meant for another registration removes nothing.
        let untouched = world()
        #expect(
            !(await ChatPush.takeBack(
                push("read", eventID: "e-read", generation: "gOld"), accepting: accepting(.refused, client), notifications: untouched, until: later)))
        #expect(untouched.standing.count == 3)
    }

    @Test func anActionFromAPushForAnotherSignInIsRefusedBeforeAnythingIsWritten() async throws {
        let held = HeldPush(current: "gC")
        let rig = ChatRig(push: held.registrar)
        try await rig.signIn()
        let info: [String: String] = [
            ChatNotifications.threadKey: "t1", ChatNotifications.cardKey: "ap1", ChatNotifications.versionKey: "4",
            PushPayload.Key.event: "request", PushPayload.Key.thread: "t1", PushPayload.Key.generation: "gOld", PushPayload.Key.eventID: "e",
        ]
        rig.notifications.respond(NotificationResponse(id: "apns-1", category: ChatNotifications.approvalCategory, action: .named("approve"), userInfo: info))
        try await rig.harness.until { _ in rig.notifications.calls.contains { if case .finished = $0 { true } else { false } } }
        await rig.harness.settle()
        #expect(!rig.server.requests.contains { $0.contains("approval/ap1") })
        #expect(held.transport.requests.allSatisfy { !($0.url?.path.contains("approval") ?? false) })
        #expect(await rig.harness.state.acting.isEmpty)
        // Said: nothing was decided, and the chat is where to decide.
        #expect(rig.notifications.calls.contains { if case let .schedule(request) = $0 { request.id == "chat|refused" } else { false } })
    }

    /// A decision is bound to the session that accepted its push: accepted
    /// for a repository the app no longer holds, it writes nothing; for the
    /// one it holds, it is sent with that session's own client.
    @Test func aPushedDecisionIsSentOnlyWithTheSessionThatAcceptedIt() async throws {
        let info: [String: String] = [
            ChatNotifications.threadKey: "t1", ChatNotifications.cardKey: "ap1", ChatNotifications.versionKey: "4",
            PushPayload.Key.event: "request", PushPayload.Key.thread: "t1", PushPayload.Key.generation: "gC", PushPayload.Key.eventID: "e",
        ]
        let approval = "/api/v1/substrate.reamde.dev/llm/approval/ap1"
        // The registrar's session is another repository's than the one the
        // app's link holds now: a sign-in that changed after acceptance.
        let moved = HeldPush(named: "other.example.com")
        let rig = ChatRig(push: moved.registrar)
        try await rig.signIn()
        rig.notifications.respond(NotificationResponse(id: "apns-1", category: ChatNotifications.approvalCategory, action: .named("approve"), userInfo: info))
        try await rig.harness.until { _ in rig.notifications.calls.contains { if case .finished = $0 { true } else { false } } }
        await rig.harness.settle()
        #expect(moved.transport.sent("PATCH", approval).isEmpty && !rig.server.requests.contains { $0.contains("approval/ap1") })
        #expect(rig.notifications.calls.contains { if case let .schedule(request) = $0 { request.id == "chat|refused" } else { false } })

        // The same session: the write goes, with its client.
        let held = HeldPush()
        held.transport.on("PATCH", approval, json: #"{"id":"ap1","kind":"substrate.reamde.dev/llm/approval","version":5,"properties":{"state":"approved"}}"#)
        let same = ChatRig(push: held.registrar)
        try await same.signIn()
        same.notifications.respond(NotificationResponse(id: "apns-2", category: ChatNotifications.approvalCategory, action: .named("approve"), userInfo: info))
        try await same.harness.until { _ in !held.transport.sent("PATCH", approval).isEmpty }
        let body = try #require(held.transport.sent("PATCH", approval).first?.httpBody)
        #expect(String(decoding: body, as: UTF8.self).contains("\"ifVersion\":4"))
    }

    /// A reply typed into a push's notification goes with the session that
    /// accepted the push, or nowhere: the session changes after the push was
    /// accepted and before the reply leaves, and no server hears it; the
    /// person is told to open the chat to reply.
    @Test func aReplyFromAPushWhoseSessionChangedIsSentNowhere() async throws {
        let (rig, server, held) = try await boundRig()
        // A chat no list holds: its row is read by name, with the push's
        // session, and the reply waits for it.
        server.hold("/llm/thread/t9")
        rig.notifications.respond(pushedReply())
        try await rig.harness.until { _ in server.count("/llm/thread/t9") > 0 }
        // Another sign-in takes the Keychain before the reply leaves.
        try await switchSession(held)
        server.letGo("/llm/thread/t9")
        try await rig.harness.until { _ in finished(rig) }
        await rig.harness.settle()
        #expect(!server.requests.contains { $0.hasPrefix("POST") && $0.contains("/chat") })
        #expect(notices(rig).map(\.body) == ["Open the chat to reply."])
    }

    /// A sign-out while the last read is out: the push is asked about again
    /// just before it is shown, accepted no more, and the plain fallback is
    /// delivered, with no chat text and no badge.
    @Test func aSignOutDuringTheLastReadShowsNothingItRead() async throws {
        let (server, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-reply", answer: .json(200, try Fixture.text("pushserver", "event-reply"))),
            .init(
                method: "GET", path: "/llm/message/5b90e3c1f2",
                answer: .json(
                    200,
                    #"{"id":"5b90e3c1f2","kind":"\#(Kinds.message)","version":1,"properties":{"role":"assistant","content":"**Here** is the summary you asked for.","thread":{"ref":"\#(Kinds.thread)/\#(threadID)"}}}"#
                )),
            .init(method: "GET", path: "/llm/thread/\(threadID)", answer: .json(200, threadRow())),
        ])
        let signedOut = Ran()
        server.onRequest { request in
            if request.contains("/llm/thread/") { signedOut.mark() }
        }
        let checking: ChatPush.Accepting = { _ in
            signedOut.value ? PushAcceptance(.refused) : PushAcceptance(.accepted, client: client)
        }
        let content = await ChatPush.content(push("reply", eventID: "e-reply"), accepting: checking, until: later)
        #expect(signedOut.value)
        #expect(content == .fallback(dropsBadge: true))
    }

    /// The compatibility retry of a pushed reply, after `400 unknown field
    /// "messageId"`, is the push's too: the session changes while the 400
    /// is out, and the retry goes to no server.
    @Test func aPushedReplysCompatibilityRetryGoesNowhereOnceTheSessionChanged() async throws {
        let (rig, server, held) = try await boundRig()
        server.route("POST", "/chat", .json(400, #"{"error":{"code":"bad_request","message":"unknown field \"messageId\""}}"#))
        server.hold("/chat")
        rig.notifications.respond(pushedReply())
        try await rig.harness.until { _ in server.count("/chat") == 1 }
        try await switchSession(held)
        server.letGo("/chat")
        try await rig.harness.until { _ in finished(rig) }
        await rig.harness.settle()
        #expect(server.count("/chat") == 1)
        #expect(notices(rig).map(\.title) == ["Nothing was sent"])
    }

    /// A pushed reply that waits for its chat's row reads it with the push's
    /// session: the session changes before the read leaves, and no server
    /// is asked for the row; the person is told nothing of the chat.
    @Test func aPushedReplysRowReadGoesNowhereOnceTheSessionChanged() async throws {
        let held = HeldPush()
        let rig = ChatRig(push: held.registrar)
        rig.server.route("GET", "/.well-known/substrate/server.json", featuresRoute(agents: 3).answer)
        rig.server.hold("/.well-known/substrate/server.json")
        rig.server.route("GET", "/llm/thread/t9", .json(200, threadRow().replacingOccurrences(of: threadID, with: "t9")))
        try await rig.signIn()
        // Before the server's document is read the reply waits, and no row
        // is asked for yet.
        rig.notifications.respond(pushedReply())
        try await rig.harness.until { await $0.state.deferred.count == 1 }
        try await switchSession(held)
        rig.server.letGo("/.well-known/substrate/server.json")
        try await rig.harness.until(advancing: 30) { _ in finished(rig) }
        await rig.harness.settle()
        #expect(rig.server.count("/llm/thread/t9") == 0)
        #expect(held.transport.requests.allSatisfy { !($0.url?.path.hasSuffix("/llm/thread/t9") ?? false) })
        #expect(!rig.server.requests.contains { $0.hasPrefix("POST") && $0.contains("/chat") })
        #expect(notices(rig).map(\.title) == ["Nothing was sent"])
    }

    /// A copy refused because the session changed, after one that may have
    /// reached the server, says the reply may have been sent: never that
    /// nothing was.
    @Test func aLaterCopyRefusedForTheSessionKeepsTheUncertainWords() async throws {
        let (rig, server, held) = try await boundRig()
        // The first copy's answer is a gateway's: it may have been written.
        server.route("POST", "/chat", once: true, .json(502, #"{"error":{"code":"bad_gateway","message":"upstream"}}"#))
        rig.notifications.respond(pushedReply())
        try await rig.harness.until { _ in server.count("/chat") == 1 }
        try await switchSession(held)
        try await rig.harness.until(advancing: 5) { _ in finished(rig) }
        await rig.harness.settle()
        #expect(server.count("/chat") == 1)
        let said = notices(rig)
        #expect(said.map(\.title) == ["Your reply may have been sent"])
        #expect(said.map(\.body) == ["Open the chat to check."])
    }

    /// The badge a `read` sets is the accepting session's count: a sign-out
    /// while it is read sets none.
    @Test func aSignOutDuringTheBadgeReadSetsNoBadge() async throws {
        let notifications = RecordedNotifications()
        let (server, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-read", answer: .json(200, try Fixture.text("pushserver", "event-read"))),
            .init(method: "GET", path: "/api/v1/push/badge", answer: .json(200, #"{"badge":2}"#)),
        ])
        let signedOut = Ran()
        server.onRequest { request in
            if request.contains("/api/v1/push/badge") { signedOut.mark() }
        }
        let checking: ChatPush.Accepting = { _ in
            signedOut.value ? PushAcceptance(.refused) : PushAcceptance(.accepted, client: client, identity: "https://s.example.com|r.example.com")
        }
        let read = await ChatPush.takeBack(push("read", eventID: "e-read"), accepting: checking, notifications: notifications, until: later)
        #expect(signedOut.value && !read)
        #expect(!notifications.calls.contains { if case .badge = $0 { true } else { false } })
    }

    /// The read of a chat a delivered alert names, started under one
    /// session, sends nothing once another session is held: the session
    /// changes while the read's list is out, and the chat is never asked
    /// for by name.
    @Test func aDeliveredAlertsReadStartedUnderOneSessionAsksNothingUnderTheNext() async throws {
        let server = MarkServer(records: 9)
        server.push(badge: 0)
        server.chat("t1", readThrough: "t1-m2")
        server.chat("t9", readThrough: "t9-m2")
        server.write("t9", archived: true)
        let held = HeldPush()
        let rig = ChatRig(marks: server, platform: .ios, push: held.registrar)
        try await rig.signIn(following: server)
        try await rig.harness.until { await $0.state.push?.held == true }
        rig.notifications.deliver(
            DeliveredNotification(
                id: "r9", thread: "t9",
                userInfo: [
                    PushPayload.Key.event: "reply", PushPayload.Key.thread: "t9", PushPayload.Key.generation: "gC", PushPayload.Key.eventID: "e-t9",
                ]))
        // The lists the read asks first are held.
        server.hold("/api/v1/records")
        await rig.harness.send(.phase(.background))
        await rig.harness.send(.phase(.active))
        try await rig.harness.until { await $0.state.pushDelivered == ["t9"] }
        try await Task.sleep(for: .milliseconds(200))
        // Another sign-in before the chat would be read by name.
        await rig.runtime.linkChanged(
            LinkSnapshot(phase: .signedIn, server: origin, repository: "o.example.com", tokenID: "0b5e1a7c33", online: true))
        server.letGo("/api/v1/records")
        await rig.harness.settle()
        try await Task.sleep(for: .milliseconds(300))
        await rig.harness.settle()
        #expect(server.count("llm/thread/t9") == 0)
    }

    /// The read of whose hand ended a card asks with the client of the
    /// session it started under, or not at all: its first try is answered
    /// with an error, another sign-in then takes the Keychain before the
    /// app is told, and the next try is never sent, with that sign-in's
    /// token or any.
    @Test func anOwnershipReadStartedUnderOneSessionAsksNothingUnderTheNext() async throws {
        let held = HeldPush()
        let server = ChatServer(baseRoutes())
        server.route("GET", "/.well-known/substrate/server.json", featuresRoute(agents: 7).answer)
        for link in ["resolvedBy", "decidedBy"] {
            server.route("GET", "/api/v1/records", query: link, .json(500, #"{"error":{"code":"internal","message":"no"}}"#))
        }
        let sessions = held.registrar.sessions
        let rig = ChatRig(
            transport: server,
            source: .sessions {
                await sessions.load().map {
                    ClientSource.Vended(client: SubstrateClient(server: $0.server, token: $0.secret, transport: server), tokenID: $0.tokenID)
                }
            })
        try await rig.signIn()
        try await rig.harness.until { await $0.state.agentsRevision == 7 }
        let card = Record(
            id: "mine", kind: Kinds.interaction,
            properties: [
                "state": "answered", "thread": ["ref": .string("\(Kinds.thread)/tz")], "resolvedBy": ["ref": "substrate.reamde.dev/core/actor/a1"],
            ], version: 2)
        await rig.harness.deliver(ChatApp.Event(.cardRead("mine", card, target: nil)))
        // Only this card's chat, `tz`, which no list holds: Chat's own reads
        // of other chats ask about theirs.
        let own: @Sendable (String) -> Int = { link in server.requests.count { $0.contains(link) && $0.contains(#""thread":{"eq":"tz"}"#) } }
        // The first try reads each kind a hand can resolve, and none answers.
        try await rig.harness.until { _ in own("decidedBy") == 2 }
        try await switchSession(held)
        await rig.harness.advance(by: 5)
        await rig.harness.settle()
        try await Task.sleep(for: .milliseconds(200))
        #expect(own("resolvedBy") == 1)
        #expect(own("decidedBy") == 2)
    }

    /// A sign-out while the extension reads the event: nothing more is asked
    /// of the server, neither the message nor the thread, and what shows is
    /// the plain fallback with no badge.
    @Test func aSignOutDuringTheEventReadAsksNothingMoreAndShowsNoBadge() async throws {
        let (server, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-reply", answer: .json(200, try Fixture.text("pushserver", "event-reply"))),
            .init(
                method: "GET", path: "/llm/message/5b90e3c1f2",
                answer: .json(200, #"{"id":"5b90e3c1f2","kind":"\#(Kinds.message)","version":1,"properties":{"content":"Hi"}}"#)),
            .init(method: "GET", path: "/llm/thread/\(threadID)", answer: .json(200, threadRow())),
        ])
        let signedOut = Ran()
        server.onRequest { request in
            if request.contains("/api/v1/push/events/") { signedOut.mark() }
        }
        let checking: ChatPush.Accepting = { _ in
            signedOut.value ? PushAcceptance(.refused) : PushAcceptance(.accepted, client: client, identity: "https://s.example.com|r.example.com")
        }
        let content = await ChatPush.content(push("reply", eventID: "e-reply"), accepting: checking, until: later)
        #expect(content == .fallback(dropsBadge: true))
        #expect(server.count("/llm/message/") == 0 && server.count("/llm/thread/") == 0)
    }

    /// A `read` whose resolution covers no alert shown: a sign-out after the
    /// alerts were read, and the badge is never asked for.
    @Test func aSignOutAfterTheAlertsAreReadAsksNoBadge() async throws {
        let notifications = RecordedNotifications()
        let (server, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-read", answer: .json(200, try Fixture.text("pushserver", "event-read"))),
            .init(method: "GET", path: "/api/v1/push/badge", answer: .json(200, #"{"badge":2}"#)),
        ])
        let signedOut = Ran()
        notifications.onDelivered { signedOut.mark() }
        let checking: ChatPush.Accepting = { _ in
            signedOut.value ? PushAcceptance(.refused) : PushAcceptance(.accepted, client: client, identity: "https://s.example.com|r.example.com")
        }
        _ = await ChatPush.takeBack(push("read", eventID: "e-read"), accepting: checking, notifications: notifications, until: later)
        #expect(signedOut.value)
        #expect(server.count("/api/v1/push/badge") == 0)
        #expect(!notifications.calls.contains { if case .badge = $0 { true } else { false } })
    }

    /// A gateway's 502, then a refusal, then the push no longer accepted: the
    /// first copy may have landed, so the notice never says nothing was sent.
    @Test func aCopyThatMayHaveLandedKeepsTheUncertainWordsThroughALaterRefusal() async throws {
        let (rig, server, held) = try await boundRig()
        server.route("POST", "/chat", .json(422, #"{"error":{"code":"validation","message":"no"}}"#))
        server.route("POST", "/chat", once: true, .json(502, #"{"error":{"code":"bad_gateway","message":"upstream"}}"#))
        rig.notifications.respond(pushedReply())
        try await rig.harness.until { _ in server.count("/chat") == 1 }
        // The second copy is out when the push stops being accepted, and its
        // refusal is the last answer: no clock moves after it, so no third
        // copy is asked for.
        server.hold("/chat")
        try await rig.harness.until(advancing: 1) { _ in server.count("/chat") == 2 }
        try await switchSession(held)
        server.letGo("/chat")
        try await rig.harness.until { _ in finished(rig) }
        await rig.harness.settle()
        #expect(server.count("/chat") == 2)
        #expect(notices(rig).map(\.title) == ["Your reply may have been sent"])
    }

    @Test func aReadThatStartsInTimeAndRunsPastIsCutAtTheDeadline() async throws {
        let (server, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-reply", answer: .json(200, try Fixture.text("pushserver", "event-reply")))
        ])
        server.hold("/api/v1/push/events/e-reply")
        let deadline = ContinuousClock.now + .seconds(4)
        let reading = Task { await ChatPush.content(push("reply", eventID: "e-reply"), accepting: accepting(.accepted, client), until: deadline) }
        // The read is on the wire, in time, and the server never answers it.
        while server.count("/api/v1/push/events/e-reply") == 0 { try await Task.sleep(for: .milliseconds(10)) }
        #expect(ContinuousClock.now < deadline)
        let content = await reading.value
        #expect(content == .fallback(dropsBadge: false))
        // Cut at the deadline, not at the request's own timeout.
        #expect(ContinuousClock.now - deadline < .seconds(5))
    }

    /// The check and the requests are one session's, asked again before
    /// every request: another sign-in that lands between them has the push
    /// read by no one, never with its token, and shows nothing of it; a
    /// check after it accepts nothing.
    @Test func anotherSignInBetweenTheCheckAndTheReadHasThePushReadByNoOne() async throws {
        let held = HeldPush()
        held.transport.on("GET", "/api/v1/push/events/e-reply", json: try Fixture.text("pushserver", "event-reply"))
        let registrar = held.registrar
        let switching: ChatPush.Accepting = { generation in
            let acceptance = await registrar.acceptance(of: generation)
            let other = Session(
                server: URL(string: "https://elsewhere.example.com")!, repository: "o.example.com", secret: "another", tokenID: "0b5e1a7c33",
                clientID: HeldPush.client, scopes: ["full"])
            try? await registrar.sessions.save(other)
            return acceptance
        }
        let content = await ChatPush.content(push("reply", eventID: "e-reply"), accepting: switching, until: later)
        #expect(content == .fallback(dropsBadge: true))
        let sent = held.transport.requests
        #expect(held.transport.sent("GET", "/api/v1/push/events/e-reply").isEmpty)
        #expect(sent.allSatisfy { $0.url?.host == origin.host && $0.value(forHTTPHeaderField: "Authorization") == "Bearer secret" })
        let after = await ChatPush.content(push("reply", eventID: "e-reply"), accepting: { await registrar.acceptance(of: $0) }, until: later)
        #expect(after == .fallback(dropsBadge: true))
        #expect(held.transport.requests.count == sent.count)
    }

    /// The last check before a push shows is answered within a bound past
    /// the deadline, or not at all: one that never answers leaves nothing
    /// the extension read and no badge, soon after the deadline.
    @Test func aLastCheckThatDoesNotAnswerShowsNothingItRead() async throws {
        let (server, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-reply", answer: .json(200, try Fixture.text("pushserver", "event-reply"))),
            .init(
                method: "GET", path: "/llm/message/5b90e3c1f2",
                answer: .json(
                    200,
                    #"{"id":"5b90e3c1f2","kind":"\#(Kinds.message)","version":1,"properties":{"role":"assistant","content":"**Here** is the summary you asked for.","thread":{"ref":"\#(Kinds.thread)/\#(threadID)"}}}"#
                )),
            .init(method: "GET", path: "/llm/thread/\(threadID)", answer: .json(200, threadRow())),
        ])
        let identity = "https://s.example.com|r.example.com"
        let answering: ChatPush.Accepting = { _ in PushAcceptance(.accepted, client: client, identity: identity) }
        guard case .shown = await ChatPush.content(push("reply", eventID: "e-reply"), accepting: answering, until: later) else {
            Issue.record("a check that answers shows the reply")
            return
        }
        // The same push, where the Keychain stops answering once the thread
        // was read.
        let read = Ran()
        server.onRequest { if $0.contains("/llm/thread/") { read.mark() } }
        let stops: ChatPush.Accepting = { _ in
            if read.value { await uncancellable(seconds: 5) }
            return PushAcceptance(.accepted, client: client, identity: identity)
        }
        let started = ContinuousClock.now
        let content = await ChatPush.content(push("reply", eventID: "e-reply"), accepting: stops, until: .now + .seconds(1))
        #expect(read.value)
        #expect(content == .fallback(dropsBadge: true))
        #expect(ContinuousClock.now - started < .seconds(4))
    }

    /// The deadline starts before the Keychain is read: one slower than it
    /// is the fallback at the deadline, and nothing is asked of the server
    /// when it answers late.
    @Test func aKeychainSlowerThanTheDeadlineIsTheFallbackAtTheDeadline() async throws {
        let (server, client) = server([])
        let stuck: ChatPush.Accepting = { _ in
            await uncancellable(seconds: 5)
            return PushAcceptance(.accepted, client: client)
        }
        // Each answers by its deadline, the content once its last check is
        // given up on, well before the Keychain would.
        var started = ContinuousClock.now
        let content = await ChatPush.content(push("reply", eventID: "e-reply"), accepting: stuck, until: .now + .seconds(1))
        #expect(content == .fallback(dropsBadge: false))
        #expect(ContinuousClock.now - started < .seconds(4))
        started = ContinuousClock.now
        let read = await ChatPush.takeBack(
            push("read", eventID: "e-read"), accepting: stuck, notifications: RecordedNotifications(), until: .now + .seconds(1))
        #expect(!read)
        #expect(ContinuousClock.now - started < .seconds(4))
        try await Task.sleep(for: .seconds(5))
        #expect(server.requests.isEmpty)
    }

    /// A `read` whose system answers after the deadline takes nothing back
    /// and sets no badge: the answer was given at the deadline.
    @Test func aReadPastItsDeadlineTakesNothingBackWhenTheSystemAnswersLate() async throws {
        let vectors = try #require(try JSONSerialization.jsonObject(with: Data(Fixture.text("pushwire", "fingerprints").utf8)) as? [[String: String]])
        let covered = try #require(vectors.first?["eventId"])
        let notifications = RecordedNotifications()
        notifications.deliver(
            DeliveredNotification(
                id: "covered", thread: threadID,
                userInfo: [
                    PushPayload.Key.event: "reply", PushPayload.Key.thread: threadID, PushPayload.Key.generation: "gC", PushPayload.Key.eventID: covered,
                ]))
        let asked = Ran()
        let answered = Ran()
        notifications.onDelivered {
            asked.mark()
            await uncancellable(seconds: 6)
            answered.mark()
        }
        let (_, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-read", answer: .json(200, try Fixture.text("pushserver", "event-read"))),
            .init(method: "GET", path: "/api/v1/push/badge", answer: .json(200, #"{"badge":2}"#)),
        ])
        let deadline = ContinuousClock.now + .seconds(4)
        let read = await ChatPush.takeBack(
            push("read", eventID: "e-read"), accepting: accepting(.accepted, client), notifications: notifications, until: deadline)
        #expect(!read && asked.value)
        #expect(ContinuousClock.now - deadline < .seconds(2))
        // The system answers late; whatever would follow it has its moment.
        #expect(await eventually { answered.value })
        try await Task.sleep(for: .milliseconds(500))
        #expect(notifications.standing.map(\.id) == ["covered"])
        #expect(!notifications.calls.contains { if case .badge = $0 { true } else { false } })
    }

    /// A `read` whose event answers after the deadline asks nothing of the
    /// device: the answer was given, and the late result acts on nothing.
    @Test func aReadWhoseEventAnswersAfterTheDeadlineAsksNothingOfTheDevice() async throws {
        let vectors = try #require(try JSONSerialization.jsonObject(with: Data(Fixture.text("pushwire", "fingerprints").utf8)) as? [[String: String]])
        let covered = try #require(vectors.first?["eventId"])
        let notifications = RecordedNotifications()
        notifications.deliver(
            DeliveredNotification(
                id: "covered", thread: threadID,
                userInfo: [
                    PushPayload.Key.event: "reply", PushPayload.Key.thread: threadID, PushPayload.Key.generation: "gC", PushPayload.Key.eventID: covered,
                ]))
        let asked = Ran()
        notifications.onDelivered { asked.mark() }
        let (server, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-read", answer: .json(200, try Fixture.text("pushserver", "event-read"))),
            .init(method: "GET", path: "/api/v1/push/badge", answer: .json(200, #"{"badge":2}"#)),
        ])
        // Held until the read is cancelled at the deadline, then answered in
        // full: a result that comes after the fallback.
        server.hold("/api/v1/push/events/e-read")
        let read = await ChatPush.takeBack(
            push("read", eventID: "e-read"), accepting: accepting(.accepted, client), notifications: notifications, until: .now + .seconds(4))
        #expect(!read)
        #expect(await eventually { server.count("/api/v1/push/events/e-read") == 1 })
        try await Task.sleep(for: .seconds(1))
        #expect(!asked.value)
        #expect(notifications.standing.map(\.id) == ["covered"])
        #expect(!notifications.calls.contains { if case .badge = $0 { true } else { false } })
    }

    /// The extension's event read answers after the deadline: the record it
    /// names is never asked for, since every read checks the race first.
    @Test func aRecordReadThatWouldStartAfterTheDeadlineIsNeverSent() async throws {
        let (server, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-reply", answer: .json(200, try Fixture.text("pushserver", "event-reply"))),
            .init(method: "GET", path: "/llm/thread/\(threadID)", answer: .json(200, threadRow())),
        ])
        // Held until the read is cancelled at the deadline, then answered in
        // full: a result that comes after the fallback.
        server.hold("/api/v1/push/events/e-reply")
        let content = await ChatPush.content(push("reply", eventID: "e-reply"), accepting: accepting(.accepted, client), until: .now + .seconds(4))
        #expect(content == .fallback(dropsBadge: false))
        #expect(await eventually { server.count("/api/v1/push/events/e-reply") == 1 })
        try await Task.sleep(for: .seconds(1))
        #expect(server.count("/llm/message/5b90e3c1f2") == 0)
    }

    /// A cancelled read has no time left to ask in, whatever the clock says.
    @Test func noTimeIsLeftForACancelledRead() async {
        #expect(ChatPush.left(.now + .seconds(30)) != nil)
        let cancelled = Task {
            withUnsafeCurrentTask { $0?.cancel() }
            return ChatPush.left(.now + .seconds(30))
        }
        let left = await cancelled.value
        #expect(left == nil)
    }

    @Test func aKeychainThatCannotBeReadIsNoOtherSignIn() async {
        let (server, client) = server([])
        #expect(await ChatPush.content(push("reply", eventID: "e-reply"), accepting: accepting(.unknown, client), until: later) == .fallback(dropsBadge: false))
        #expect(server.requests.isEmpty)
    }

    @Test func aReplysPreviewIsCutBeforeItsMarkdownIsReadAndNeverATheThreadsLaterWords() async throws {
        let long = "**Bold** start. " + String(repeating: "word ", count: 2000)
        let cut = try #require(ChatPush.preview(long))
        #expect(cut.count <= ChatPush.previewLimit + 1 && cut.hasPrefix("Bold start.") && cut.hasSuffix("…"))
        // Cut between characters as read: a family is one, of five scalars.
        let family = "\u{1F468}\u{200D}\u{1F469}\u{200D}\u{1F467}"
        let faces = try #require(ChatPush.preview(String(repeating: family, count: ChatPush.previewLimit + 10)))
        #expect(faces == String(repeating: family, count: ChatPush.previewLimit) + "…")
        #expect(ChatPush.preview("Short and whole.") == "Short and whole.")
        #expect(ChatPush.preview("```\n\n```") == nil)
        let (_, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-reply", answer: .json(200, try Fixture.text("pushserver", "event-reply"))),
            .init(
                method: "GET", path: "/llm/message/5b90e3c1f2",
                answer: .json(200, #"{"id":"5b90e3c1f2","kind":"\#(Kinds.message)","version":1,"properties":{"role":"assistant","content":""}}"#)),
            .init(method: "GET", path: "/llm/thread/\(threadID)", answer: .json(200, threadRow())),
        ])
        let content = await ChatPush.content(push("reply", eventID: "e-reply"), accepting: accepting(.accepted, client), until: later)
        guard case let .shown(request) = content else {
            Issue.record("\(content)")
            return
        }
        #expect(request.body == "\(agentName("x.example.com/pkg/helper")) replied.")
    }

    @Test func aRunOverItsBudgetSaysSoAndNotItsOlderReply() async throws {
        let (_, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-failed", answer: .json(200, try Fixture.text("pushserver", "event-failed"))),
            .init(method: "GET", path: "/llm/thread/\(threadID)", answer: .json(200, threadRow(status: "overbudget", version: 9))),
        ])
        let content = await ChatPush.content(push("failed", eventID: "e-failed"), accepting: accepting(.accepted, client), until: later)
        guard case let .shown(request) = content else {
            Issue.record("\(content)")
            return
        }
        let body = try #require(request.body)
        #expect(!body.isEmpty && !body.contains("An older reply"))
    }

    @Test func anEventForAnotherThreadOrAnItemThatIsGoneIsTheFallback() async throws {
        let (_, other) = server([
            .init(
                method: "GET", path: "/api/v1/push/events/e-reply",
                answer: .json(200, try Fixture.text("pushserver", "event-reply").replacingOccurrences(of: threadID, with: "0000000000")))
        ])
        #expect(await ChatPush.content(push("reply", eventID: "e-reply"), accepting: accepting(.accepted, other), until: later) == .fallback(dropsBadge: false))
        let (_, gone) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-reply", answer: .json(200, try Fixture.text("pushserver", "event-reply"))),
            .init(method: "GET", path: "/llm/message/5b90e3c1f2", answer: .json(404, #"{"error":{"code":"not_found","message":"no"}}"#)),
        ])
        #expect(await ChatPush.content(push("reply", eventID: "e-reply"), accepting: accepting(.accepted, gone), until: later) == .fallback(dropsBadge: false))
    }

    @Test func aReadWithNoSessionOrForAnotherThreadTakesNothingBack() async throws {
        let vectors = try #require(try JSONSerialization.jsonObject(with: Data(Fixture.text("pushwire", "fingerprints").utf8)) as? [[String: String]])
        let covered = try #require(vectors.first?["eventId"])
        let notifications = RecordedNotifications()
        notifications.deliver(
            DeliveredNotification(
                id: "covered", thread: threadID,
                userInfo: [
                    PushPayload.Key.event: "reply", PushPayload.Key.thread: threadID, PushPayload.Key.generation: "gC", PushPayload.Key.eventID: covered,
                ]))
        #expect(!(await ChatPush.takeBack(push("read", eventID: "e-read"), accepting: accepting(.accepted, nil), notifications: notifications, until: later)))
        let (_, client) = server([
            .init(method: "GET", path: "/api/v1/push/events/e-read", answer: .json(200, try Fixture.text("pushserver", "event-read")))
        ])
        var elsewhere = push("read", eventID: "e-read")
        elsewhere[PushPayload.Key.thread] = "0000000000"
        #expect(!(await ChatPush.takeBack(elsewhere, accepting: accepting(.accepted, client), notifications: notifications, until: later)))
        #expect(notifications.standing.count == 1)
    }
}
