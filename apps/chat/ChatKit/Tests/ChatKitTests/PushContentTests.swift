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

private let later = Date().addingTimeInterval(60)

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
        let content = await ChatPush.content(push("reply", eventID: "e-reply"), accepted: true, client: client, until: later)
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
        let content = await ChatPush.content(push("failed", eventID: "e-failed"), accepted: true, client: client, until: later)
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
        #expect(await ChatPush.content(push("failed", eventID: "e-failed"), accepted: true, client: again, until: later) == .fallback(dropsBadge: false))
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
        let one = await ChatPush.content(push("request", eventID: "e-1"), accepted: true, client: client, until: later)
        let two = await ChatPush.content(push("request", eventID: "e-2"), accepted: true, client: client, until: later)
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
        #expect(await ChatPush.content(push("request", eventID: "e-1"), accepted: true, client: client, until: later) == .fallback(dropsBadge: false))
    }

    @Test func aPushForAnotherRegistrationFetchesNothingAndDropsItsBadge() async {
        let (server, client) = server([])
        let content = await ChatPush.content(push("reply", eventID: "e-reply", generation: "gOld"), accepted: false, client: client, until: later)
        #expect(content == .fallback(dropsBadge: true))
        #expect(server.requests.isEmpty)
    }

    @Test func anUnreachableServerOrNoTimeLeftIsTheFallback() async throws {
        let (_, down) = server([.init(method: "GET", path: "/api/v1/push/events/e-reply", answer: .fail(.cannotConnectToHost))])
        #expect(await ChatPush.content(push("reply", eventID: "e-reply"), accepted: true, client: down, until: later) == .fallback(dropsBadge: false))
        let (server, client) = server([])
        #expect(await ChatPush.content(push("reply", eventID: "e-reply"), accepted: true, client: client, until: Date()) == .fallback(dropsBadge: false))
        #expect(server.requests.isEmpty)
        #expect(await ChatPush.content(push("reply", eventID: "e-reply"), accepted: true, client: nil, until: later) == .fallback(dropsBadge: false))
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
        #expect(await ChatPush.takeBack(push("read", eventID: "e-read"), accepted: true, client: client, notifications: notifications))
        #expect(notifications.standing.map(\.id).sorted() == ["chat|\(threadID)|reply", "other"])
        #expect(notifications.calls.contains(.badge(2)))
        // One meant for another registration removes nothing.
        let untouched = world()
        #expect(!(await ChatPush.takeBack(push("read", eventID: "e-read", generation: "gOld"), accepted: false, client: client, notifications: untouched)))
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
        #expect(await rig.harness.state.acting.isEmpty)
    }
}
