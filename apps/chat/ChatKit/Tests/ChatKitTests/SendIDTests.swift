import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

/// The `messageId` a chat request carried, or nil.
private func messageID(_ body: String?) -> String? {
    body.flatMap(JSONValue.parse)?["messageId"]?.string
}

/// The newest-first page the transcript read answers once the server holds
/// the message under `id`, with its reply.
private func rows(user id: String, text: String = "Hi") -> String {
    #"{"head":10,"generation":"g","records":[{"id":"m2","kind":"substrate.reamde.dev/llm/message","version":1,"createdAt":"2026-10-31T10:00:04Z","updatedAt":"2026-10-31T10:00:04Z","properties":{"role":"assistant","content":"Hello","turn":1,"thread":{"ref":"substrate.reamde.dev/llm/thread/t1"}}},{"id":"\#(id)","kind":"substrate.reamde.dev/llm/message","version":1,"createdAt":"2026-10-31T10:00:01Z","updatedAt":"2026-10-31T10:00:01Z","properties":{"role":"user","content":"\#(text)","turn":0,"thread":{"ref":"substrate.reamde.dev/llm/thread/t1"}}}]}"#
}

/// The message's own record, as a read by its id answers.
private func messageJSON(_ id: String, text: String = "Hi") -> String {
    #"{"id":"\#(id)","kind":"substrate.reamde.dev/llm/message","version":1,"createdAt":"2026-10-31T10:00:01Z","updatedAt":"2026-10-31T10:00:01Z","properties":{"role":"user","content":"\#(text)","turn":0,"thread":{"ref":"substrate.reamde.dev/llm/thread/t1"}}}"#
}

private let gateway = ChatServer.Answer.json(502, "<html>bad gateway</html>")

private func refusal(_ status: Int, code: String, reason: String? = nil, _ message: String) -> ChatServer.Answer {
    let reason = reason.map { #""reason":"\#($0)","# } ?? ""
    return .json(status, #"{"error":{"code":"\#(code)",\#(reason)"message":"\#(message)"}}"#)
}

/// The one chat in the list, as its read answers.
private let oneThread = #"{"head":10,"generation":"g","records":[\#(threadJSON(version: 3))]}"#

extension ChatRig {
    /// A new chat with "Hi" sent from it.
    fileprivate func sendHi(_ text: String = "Hi") async throws -> ChildKey {
        let key = try await newChat()
        try await send(.draft(text), to: key)
        try await send(.send, to: key)
        return key
    }

    /// The ids the `/chat` requests carried, in order.
    fileprivate var sentIDs: [String?] { server.bodies("POST", "/chat").map(messageID) }

    /// The waits the conversations asked for before sending a message
    /// again, in seconds and in order.
    fileprivate var repeatWaits: [Double] {
        harness.trace.compactMap { entry in
            guard case let .effect(_, effect) = entry, case let .repeatWait(_, seconds)? = (effect as? Conversation.Effect)?.ask else { return nil }
            return seconds
        }
    }

    /// What `drafts.json` holds, as text.
    fileprivate var draftsFile: String {
        (try? String(contentsOf: root.appendingPathComponent("drafts.json"), encoding: .utf8)) ?? ""
    }

    /// What `unsent.json` holds, as text.
    fileprivate var unsentFile: String {
        (try? String(contentsOf: root.appendingPathComponent("unsent.json"), encoding: .utf8)) ?? ""
    }
}

@Suite("A send that carries its own id", .timeLimit(.minutes(10)))
struct SendsUnderAnID {
    @Test func theBubbleIsTheStoredRowFromTheStart() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 1))])
        try await rig.signIn(agents: 3)
        let key = try await rig.sendHi()
        try await rig.harness.until { _ in await rig.live(key)?.thread == "t1" }
        let id = try #require(rig.sentIDs.first ?? nil)
        // A lowercase UUID, minted by the app: a legal record id.
        #expect(id == id.lowercased() && UUID(uuidString: id) != nil)
        // The bubble carries the id its row will have.
        #expect(await rig.conversation(key)?.turns.first?.id == id)
        #expect(await rig.live(key)?.pending == nil)
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, rows(user: id)))
        rig.server.release()
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        // The stored message is the same row: the handover replaced nothing.
        #expect(await rig.conversation(key)?.turns.map(\.id) == [id, "m2"])
        #expect(await rig.turns(key) == ["Hi", "Hello"])
        #expect(rig.server.count("/chat") == 1)
        #expect(await rig.harness.state.unsent.isEmpty)
    }

    @Test func belowTheRevisionNoIdIsSentAndTheDeliveryCheckStands() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: gateway)])
        try await rig.signIn(agents: 2)
        let key = try await rig.sendHi()
        try await rig.harness.until { _ in await rig.live(key)?.checking == true }
        // An older server refuses a body field it does not know.
        #expect(rig.sentIDs == [nil])
        #expect(rig.server.body("POST", "/chat")?.contains("messageId") == false)
        #expect(await rig.strip(key)?.sentence == "Checking whether your message arrived…")
        try await rig.harness.until(advancing: 2) { _ in await rig.live(key)?.chatFailure?.unconfirmed == true }
        #expect(await rig.strip(key)?.sentence == "Your message may not have arrived.")
        #expect(await rig.conversation(key)?.composer.draft == "Hi")
        // Nothing is sent again on its own, and nothing is kept as pending.
        #expect(rig.server.count("/chat") == 1)
        #expect(await rig.live(key)?.pending == nil)
        #expect(await rig.harness.state.unsent.isEmpty)
    }

    @Test func theDeviceKeepsTheMessageBeforeItsRequestLeaves() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream))])
        let seen = Noted()
        rig.server.onRequest { request in
            if request.contains("/chat") { seen.note(rig.unsentFile) }
        }
        try await rig.signIn(agents: 3)
        let key = try await rig.sendHi("Water the basil")
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        let id = try #require(rig.sentIDs.first ?? nil)
        // When the request reached the server, the folder already held the
        // message and the id to send it again under.
        let held = try #require(seen.notes.first)
        #expect(held.contains(id) && held.contains("Water the basil"))
        // And once the server took it, the folder let it go.
        try await rig.harness.until(advancing: 1) { _ in !rig.unsentFile.contains(id) }
    }

    @Test func aRequestThatRefusesTheFieldIsSentWithoutItAndTheNextOnesToo() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream))])
        // The document says revision 3; the server behind it is older.
        rig.server.route("POST", "/chat", once: true, refusal(400, code: "bad_request", #"unknown field \"messageId\""#))
        try await rig.signIn(agents: 3)
        let key = try await rig.sendHi()
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        let ids = rig.sentIDs
        #expect(ids.count == 2 && ids[0] != nil && ids[1] == nil)
        #expect(await rig.harness.state.sendsIDs == false)
        #expect(await rig.strip(key) == nil)
        #expect(await rig.live(key)?.pending == nil)
        // The next message goes without one, until the document is read again.
        try await rig.send(.draft("And the mint"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in rig.server.count("/chat") == 3 }
        #expect(rig.sentIDs.last == .some(nil))
    }
}

@Suite("A send whose answer was lost", .timeLimit(.minutes(10)))
struct LostAnswers {
    @Test func isSentAgainUnderItsIdAndArrivesOnce() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 1))])
        rig.server.route("POST", "/chat", once: true, gateway)
        try await rig.signIn(agents: 3)
        let key = try await rig.sendHi()
        // The answer was a gateway's page: whether the turn was admitted is
        // not known. The bubble stays, nothing is read for, nothing is
        // drawn as a failure, and the text is not handed back.
        try await rig.harness.until { _ in await rig.live(key)?.reconnecting == true }
        let waiting = try #require(await rig.conversation(key))
        #expect(waiting.failures.strip == nil && waiting.failures.turns.isEmpty && waiting.failures.reconnecting)
        #expect(waiting.turns.count == 1 && waiting.composer.draft == "" && waiting.composer.blocked == "Waiting for the answer")
        #expect(await rig.live(key)?.checking == false)
        let id = try #require(rig.sentIDs.first ?? nil)
        // A second later it goes again, as it was.
        try await rig.harness.until(advancing: 2) { _ in await rig.live(key)?.thread == "t1" }
        #expect(rig.sentIDs == [id, id])
        let bodies = rig.server.bodies("POST", "/chat")
        #expect(bodies[0] == bodies[1])
        #expect(await rig.live(key)?.pending == nil && rig.server.count("/events") == 0)
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, rows(user: id)))
        rig.server.release()
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        #expect(await rig.turns(key) == ["Hi", "Hello"])
        #expect(await rig.conversation(key)?.turns.map(\.id) == [id, "m2"])
        #expect(await rig.conversation(key)?.failures == ScreenFailures())
        // The first message of a new chat: one thread, one conversation,
        // one chat in the list.
        let state = await rig.harness.state
        #expect(state.chats.map(\.id) == ["t1"] && state.conversations.count == 1 && state.key(of: "t1") == key)
        #expect(bodies.allSatisfy { $0.contains(#""thread":"""#) })
    }

    @Test func isRepeatedThreeTimesAtMostAndThenStandsWithSendAgain() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: gateway)])
        try await rig.signIn(agents: 3)
        let key = try await rig.sendHi("Water the basil")
        try await rig.harness.until(advancing: 10) { _ in await rig.live(key)?.standing == true }
        // One second, three and eight after each answer that did not come.
        #expect(rig.repeatWaits == [1, 3, 8])
        let id = try #require(rig.sentIDs.first ?? nil)
        #expect(rig.sentIDs == [id, id, id, id])
        // The message stands where it was sent, with its card; the composer
        // is the person's again but for sending, and nothing is in it.
        let screen = try #require(await rig.conversation(key))
        #expect(screen.turns.map(\.id) == [id])
        guard case let .user(bubble) = screen.turns[0].body else {
            Issue.record("the unsent message is not a bubble")
            return
        }
        #expect(bubble.text == "Water the basil" && bubble.pending)
        let card = try #require(screen.failures.turns[id])
        #expect(card.sentence == "No answer came back for your message.")
        #expect(card.note == "The server answered with something Substrate Chat can't read.")
        #expect(card.button == FailureButton(title: "Send Again", action: .sendAgain))
        #expect(card.dismiss == .edit)
        #expect(screen.failures.strip == nil && screen.failures.turns.count == 1)
        #expect(screen.composer.draft == "" && screen.composer.blocked == "Send or edit the message above first")
        #expect(!screen.streaming && !screen.typing)
        // Nothing more goes on its own, however long it stands: no wait
        // is left to end.
        await rig.harness.advance(by: 600)
        #expect(rig.repeatWaits.count == 3 && rig.server.count("/chat") == 4)
        #expect(await rig.live(key)?.standing == true)
        // The window may leave: the conversation stays while its message
        // stands, and New Chat is the way back to a chat with no thread.
        await rig.harness.send(.back(scene: "s"))
        #expect(await rig.harness.state.conversations[key] != nil)
        #expect(try await rig.newChat() == key)
        #expect(await rig.harness.state.conversations.count == 1)

        // Send Again: the same request, under the same id.
        rig.server.route("POST", "/chat", .stream(replyStream, holdAfter: 1))
        try await rig.send(.sendAgain, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.thread == "t1" }
        #expect(rig.sentIDs.count == 5 && rig.sentIDs.allSatisfy { $0 == id })
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, rows(user: id, text: "Water the basil")))
        rig.server.release()
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        #expect(await rig.turns(key) == ["Water the basil", "Hello"])
        #expect(await rig.harness.state.unsent.isEmpty)
    }

    @Test func aNewChatIsTheThreadItsLostFirstCopyOpenedOnceThatThreadIsRead() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .fail(.timedOut))])
        try await rig.signIn(agents: 3)
        let key = try await rig.sendHi()
        try await rig.harness.until { _ in await rig.live(key)?.reconnecting == true }
        let id = try #require(rig.sentIDs.first ?? nil)
        // The first copy landed after all and opened a thread. The list
        // reads it, and the person opens it while the new chat still asks.
        rig.server.route("GET", "/api/v1/records", query: "llm/thread", .json(200, oneThread))
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, rows(user: id)))
        await rig.harness.send(.refresh)
        try await rig.harness.until { _ in await rig.harness.state.chats.map(\.id) == ["t1"] }
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let opened = try #require(await rig.harness.state.scenes["s"]?.current)
        #expect(opened != key)
        // Its rows hold the new chat's message: the new chat is that
        // thread, and gives way to the conversation already on it. One
        // chat, one conversation, the message once, nothing left pending.
        try await rig.harness.until { _ in await rig.harness.state.conversations[key] == nil }
        let state = await rig.harness.state
        #expect(state.conversations.count == 1 && state.key(of: "t1") == opened && state.unsent.isEmpty)
        try await rig.harness.until { _ in await rig.turns(opened) == ["Hi", "Hello"] }
        #expect(await rig.conversation(opened)?.turns.map(\.id) == [id, "m2"])
        // And no copy asks any more.
        let asked = rig.server.count("/chat")
        await rig.harness.advance(by: 30)
        #expect(rig.server.count("/chat") == asked)
    }

    @Test func aNewChatBindsToThatThreadWhereNoConversationHasIt() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .fail(.timedOut))])
        try await rig.signIn(agents: 3)
        let key = try await rig.sendHi()
        try await rig.harness.until { _ in await rig.live(key)?.reconnecting == true }
        let id = try #require(rig.sentIDs.first ?? nil)
        // Its repeats run out and it stands. The list then reads the thread
        // ahead, as it does for recent chats once nothing streams.
        try await rig.harness.until(advancing: 10) { _ in await rig.live(key)?.standing == true }
        rig.server.route("GET", "/api/v1/records", query: "llm/thread", .json(200, oneThread))
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, rows(user: id)))
        await rig.harness.send(.refresh)
        try await rig.harness.until(advancing: 1) { _ in
            let live = await rig.live(key)
            return live?.thread == "t1" && live?.phase == .idle
        }
        let state = await rig.harness.state
        #expect(state.conversations.count == 1 && state.key(of: "t1") == key && state.unsent.isEmpty)
        #expect(await rig.conversation(key)?.turns.map(\.id) == [id, "m2"])
        #expect(await rig.live(key)?.pending == nil)
    }

    @Test func aMessageThatLandedStopsStandingOnceItsRowsAreRead() async throws {
        let rig = ChatRig(baseRoutes(threads: oneThread) + [.init(method: "POST", path: "/chat", answer: gateway)])
        try await rig.signIn(agents: 3)
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        try await rig.send(.draft("And the tomatoes?"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in rig.server.count("/chat") == 1 }
        let id = try #require(rig.sentIDs.first ?? nil)
        // The first copy landed and its turn ran, and every answer was
        // lost. The list hears the thread moved while the conversation
        // holds it, when no read may publish.
        let moved = #"{"head":11,"generation":"g","records":[\#(threadJSON(version: 4))]}"#
        let held =
            #"{"head":11,"generation":"g","records":[{"id":"m4","kind":"substrate.reamde.dev/llm/message","version":1,"createdAt":"2026-10-31T10:01:04Z","updatedAt":"2026-10-31T10:01:04Z","properties":{"role":"assistant","content":"Every second day.","turn":3,"thread":{"ref":"substrate.reamde.dev/llm/thread/t1"}}},{"id":"\#(id)","kind":"substrate.reamde.dev/llm/message","version":1,"createdAt":"2026-10-31T10:01:01Z","updatedAt":"2026-10-31T10:01:01Z","properties":{"role":"user","content":"And the tomatoes?","turn":2,"thread":{"ref":"substrate.reamde.dev/llm/thread/t1"}}}]}"#
        rig.server.route("GET", "/api/v1/records", query: "llm/thread", .json(200, moved))
        rig.server.route("GET", "/llm/thread/t1", .json(200, threadJSON(version: 4)))
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, held))
        await rig.harness.send(.refresh)
        try await rig.harness.until { _ in await rig.harness.state.thread("t1")?.record.version == 4 }
        // Once the app's own repeats are spent the thread is the list's
        // again: its rows are read, they hold the message, and it is the
        // stored row with no card and nothing to press.
        try await rig.harness.until(advancing: 10) { _ in
            let live = await rig.live(key)
            return live?.pending == nil && live?.phase == .idle && rig.server.count("/chat") == 4
        }
        try await rig.harness.until { _ in await rig.turns(key) == ["And the tomatoes?", "Every second day."] }
        let screen = try #require(await rig.conversation(key))
        #expect(screen.turns.map(\.id) == [id, "m4"] && screen.failures == ScreenFailures())
        #expect(await rig.harness.state.unsent.isEmpty)
    }

    @Test func noCopyGoesOnItsOwnOnceTheWindowHasPassed() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .fail(.timedOut))])
        try await rig.signIn(agents: 3)
        let key = try await rig.sendHi()
        try await rig.harness.until { _ in await rig.live(key)?.reconnecting == true }
        // The app slept: when its wait ends, the press is long ago. The
        // wait ends with this move of the clock, or began after it and is
        // stepped to: late either way.
        await rig.harness.advance(by: Conversation.repeatWindow + 60)
        try await rig.harness.until(advancing: 2) { _ in await rig.live(key)?.standing == true }
        #expect(rig.server.count("/chat") == 1)
        let card = try #require(await rig.card(key))
        #expect(card.sentence == "No answer came back for your message." && card.note == "The server took too long to answer.")
        #expect(card.button?.action == .sendAgain)
    }

    @Test func aStreamThatEndsBeforeItsThreadEventIsRepeatedToo() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream))])
        rig.server.route("POST", "/chat", once: true, .stream([]))
        try await rig.signIn(agents: 3)
        let key = try await rig.sendHi()
        try await rig.harness.until(advancing: 2) { _ in await rig.live(key)?.settles == 1 }
        let ids = rig.sentIDs
        #expect(ids.count == 2 && ids[0] != nil && ids[0] == ids[1])
    }

    @Test func aRepeatTheServerAlreadyAnsweredReadsTheThreadAndRunsNothing() async throws {
        // The first copy landed and its turn finished; its answer was lost.
        let rig = ChatRig(
            baseRoutes() + [
                .init(method: "POST", path: "/chat", answer: .stream([#"{"kind":"thread","thread":"t1"}"#, #"{"kind":"idle","thread":"t1","status":"ok"}"#]))
            ])
        rig.server.route("POST", "/chat", once: true, .fail(.networkConnectionLost))
        try await rig.signIn(agents: 3)
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in rig.server.count("/chat") == 1 }
        let id = try #require(rig.sentIDs.first ?? nil)
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, rows(user: id)))
        try await rig.harness.until(advancing: 2) { _ in await rig.live(key)?.settles == 1 }
        // The `thread` event and one `idle` line: the rows are read, with
        // no attach in between.
        #expect(rig.server.count("/chat") == 2 && rig.server.count("/events") == 0)
        #expect(await rig.turns(key) == ["Hi", "Hello"])
        #expect(await rig.conversation(key)?.turns.map(\.id) == [id, "m2"])
    }

    @Test func comingBackToTheFrontSendsAnUnansweredMessageAgainAndNeverDropsIt() async throws {
        let rig = ChatRig(baseRoutes(threads: oneThread) + [.init(method: "POST", path: "/chat", answer: .stream(replyStream))])
        try await rig.signIn(agents: 3)
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        // The request is out and nothing comes back: the app was suspended.
        rig.server.hold("/chat")
        try await rig.send(.draft("And the tomatoes?"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in rig.server.count("/chat") == 1 }
        await rig.harness.send(.phase(.background))
        await rig.harness.send(.phase(.active))
        try await rig.harness.until { _ in rig.server.count("/chat") == 2 }
        // Never an attach: that would end the send and what was typed.
        #expect(rig.server.count("/events") == 0)
        let ids = rig.sentIDs
        #expect(ids[0] != nil && ids[0] == ids[1])
        #expect(await rig.turns(key).last == "And the tomatoes?")
    }

    @Test func belowTheRevisionComingBackToTheFrontLeavesAnUnansweredSendAlone() async throws {
        let rig = ChatRig(baseRoutes(threads: oneThread) + [.init(method: "POST", path: "/chat", answer: .stream(replyStream))])
        try await rig.signIn(agents: 2)
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        rig.server.hold("/chat")
        try await rig.send(.draft("And the tomatoes?"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in rig.server.count("/chat") == 1 }
        await rig.harness.send(.phase(.background))
        await rig.harness.send(.phase(.active))
        try await rig.harness.until { _ in await rig.harness.state.refreshing == false }
        await rig.harness.settle()
        // The send is still the run, its bubble still on screen: an attach
        // here would have settled an idle thread and dropped the message.
        #expect(rig.server.count("/events") == 0 && rig.server.count("/chat") == 1)
        #expect(await rig.turns(key).last == "And the tomatoes?")
        rig.server.letGo("/chat")
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
    }

    @Test func stopOnAnUnansweredSendLeavesTheMessageStandingAndNeverDropsIt() async throws {
        let rig = ChatRig(baseRoutes(threads: oneThread) + [.init(method: "POST", path: "/chat", answer: .stream(replyStream))])
        try await rig.signIn(agents: 3)
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        rig.server.hold("/chat")
        try await rig.send(.draft("And the tomatoes?"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in rig.server.count("/chat") == 1 }
        let id = try #require(rig.sentIDs.first ?? nil)
        try await rig.send(.stop, to: key)
        // The message stands, under its id, with Send Again: no attach took
        // its place and dropped it.
        let screen = try #require(await rig.conversation(key))
        #expect(screen.turns.last?.id == id && !screen.streaming && rig.server.count("/events") == 0)
        let card = try #require(screen.failures.turns[id])
        #expect(card.sentence == "No answer came back for your message." && card.note == "It was stopped before the server answered.")
        #expect(card.button?.action == .sendAgain && card.dismiss == .edit)
        // A turn a copy may have started is stopped where its thread is known.
        try await rig.harness.until { _ in rig.server.count("/stop") == 1 }
    }

    @Test func aSignInThatEndedGivesTheTextBack() async throws {
        for revision in [2, 3] {
            let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: refusal(401, code: "unauthorized", "the token was revoked"))])
            try await rig.signIn(agents: revision)
            let key = try await rig.sendHi("Water the basil")
            try await rig.harness.until { _ in await rig.live(key)?.phase == .idle && rig.server.count("/chat") == 1 }
            try await rig.harness.until { _ in await rig.live(key)?.draft == "Water the basil" }
            // The signed-out screen says the rest: no strip, no card, and
            // nothing kept as pending under an id nothing was written for.
            let live = try #require(await rig.live(key))
            #expect(live.chatFailure == nil && live.pending == nil && live.overlay.turns.isEmpty, "revision \(revision)")
        }
    }
}

@Suite("A send the server refused", .timeLimit(.minutes(10)))
struct RefusedSends {
    @Test func theOnlyCopyRefusedGivesTheTextBackAndItsIdIsDoneWith() async throws {
        let rig = ChatRig(
            baseRoutes() + [
                .init(method: "POST", path: "/chat", answer: refusal(409, code: "conflict", reason: "thread_busy", "a turn is running on the thread"))
            ])
        try await rig.signIn(agents: 3)
        let key = try await rig.sendHi()
        try await rig.harness.until { _ in await rig.strip(key) != nil }
        // The server's own refusal of the one copy sent: nothing was
        // written under the id, so the text is the composer's again.
        let strip = try #require(await rig.strip(key))
        #expect(strip.sentence == "Helper is still answering in this chat." && strip.note == "Your message is in the box below.")
        #expect(strip.button == FailureButton(title: "Send Again", action: .send))
        let screen = try #require(await rig.conversation(key))
        #expect(screen.composer.draft == "Hi" && screen.turns.isEmpty && screen.composer.blocked == nil)
        #expect(await rig.live(key)?.pending == nil)
        #expect(await rig.harness.state.unsent.isEmpty)
        // Sent from the composer it is a new message, by the person's word.
        rig.server.route("POST", "/chat", .stream(replyStream))
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        let ids = rig.sentIDs
        #expect(ids.count == 2 && ids[0] != nil && ids[1] != nil && ids[0] != ids[1])
    }

    @Test func aRepeatRefusedKeepsItsIdAndStandsWithSendAgain() async throws {
        let reasons: [(String, Int, String, String)] = [
            ("thread_busy", 409, "conflict", "Helper is still answering in this chat."),
            ("turn_cap", 409, "conflict", "Too many replies are being written at once."),
            ("spend_cap", 403, "guard", "Today's spending limit is reached."),
            ("provider_key_missing", 422, "validation", "The provider has no API key yet."),
            ("quota", 403, "guard", "Today's allowance is used up."),
        ]
        for (reason, status, code, sentence) in reasons {
            let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: refusal(status, code: code, reason: reason, "refused"))])
            // The first copy's answer was lost: whether it landed is not
            // known, and a copy still on its way can land after this refusal.
            rig.server.route("POST", "/chat", once: true, gateway)
            try await rig.signIn(agents: 3)
            let key = try await rig.sendHi()
            try await rig.harness.until(advancing: 2) { _ in await rig.live(key)?.standing == true }
            let id = try #require(rig.sentIDs.first ?? nil)
            #expect(rig.sentIDs == [id, id], "\(reason)")
            let card = try #require(await rig.conversation(key)?.failures.turns[id], "\(reason)")
            #expect(card.sentence == sentence, "\(reason)")
            // Whatever refused it, the way on is the same message again.
            #expect(card.button == FailureButton(title: "Send Again", action: .sendAgain), "\(reason)")
            #expect(card.dismiss == .edit && card.details?.text == "refused", "\(reason)")
            #expect(await rig.conversation(key)?.composer.draft == "", "\(reason)")
            #expect(await rig.conversation(key)?.failures.strip == nil, "\(reason)")
            rig.server.route("POST", "/chat", .stream(replyStream))
            try await rig.send(.sendAgain, to: key)
            try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
            #expect(rig.sentIDs == [id, id, id], "\(reason)")
        }
    }

    @Test func aBusyThreadIsReadAndTheMessageItHoldsIsNoLongerPending() async throws {
        // The first copy landed and its turn runs; the repeat raced it.
        let rig = ChatRig(
            baseRoutes(threads: oneThread) + [
                .init(method: "POST", path: "/chat", answer: refusal(409, code: "conflict", reason: "thread_busy", "a turn is running on the thread")),
                .init(method: "GET", path: "/events", answer: .stream([#"{"kind":"idle","status":"ok"}"#])),
            ])
        rig.server.route("POST", "/chat", once: true, gateway)
        try await rig.signIn(agents: 3)
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        try await rig.send(.draft("And the tomatoes?"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in rig.server.count("/chat") == 1 }
        let id = try #require(rig.sentIDs.first ?? nil)
        let held =
            #"{"head":10,"generation":"g","records":[{"id":"\#(id)","kind":"substrate.reamde.dev/llm/message","version":1,"createdAt":"2026-10-31T10:01:01Z","updatedAt":"2026-10-31T10:01:01Z","properties":{"role":"user","content":"And the tomatoes?","turn":2,"thread":{"ref":"substrate.reamde.dev/llm/thread/t1"}}}]}"#
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, held))
        try await rig.harness.until(advancing: 2) { _ in await rig.live(key)?.settles == 1 }
        // The attach read the rows, and they hold the message: it is the
        // stored row, once, with no card.
        #expect(rig.server.count("/events") == 1)
        let screen = try #require(await rig.conversation(key))
        #expect(screen.turns.map(\.id) == [id] && screen.failures.turns.isEmpty && screen.failures.strip == nil)
        #expect(await rig.live(key)?.pending == nil)
        #expect(await rig.harness.state.unsent.isEmpty)
    }

    @Test func anIdThatNamesAnotherMessageIsSaidAndNeverAnsweredWithANewId() async throws {
        let taken = #"substrate: conflict: the messageId is taken: messageId \"x\" was sent with another text: send the message under a new id"#
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: refusal(409, code: "conflict", taken))])
        rig.server.route("POST", "/chat", once: true, gateway)
        try await rig.signIn(agents: 3)
        let key = try await rig.sendHi("Water the basil")
        try await rig.harness.until(advancing: 2) { _ in await rig.live(key)?.standing == true }
        let id = try #require(rig.sentIDs.first ?? nil)
        let card = try #require(await rig.conversation(key)?.failures.turns[id])
        #expect(card.sentence == "This message can't be sent again as it is." && card.tone == .bad)
        #expect(card.note == "Something went wrong in Substrate Chat. Edit the message to send it as a new one.")
        #expect(card.button == FailureButton(title: "Edit Message", action: .edit) && card.dismiss == nil)
        #expect(card.details?.label == "The server said" && card.details?.text.contains("the messageId is taken") == true)
        // Nothing was sent under another id, and nothing goes on its own.
        await rig.harness.advance(by: 60)
        #expect(rig.sentIDs == [id, id] && rig.repeatWaits == [1])
        // Edit Message: the text is the composer's, ahead of what was typed
        // since, and the message no longer stands.
        try await rig.send(.draft("and the mint"), to: key)
        try await rig.send(.takeBack, to: key)
        let screen = try #require(await rig.conversation(key))
        #expect(screen.composer.draft == "Water the basil\n\nand the mint" && screen.turns.isEmpty && screen.failures.turns.isEmpty)
        #expect(screen.composer.blocked == nil)
        #expect(await rig.harness.state.unsent.isEmpty)
        #expect(await rig.harness.state.drafts.values.contains("Water the basil\n\nand the mint"))
    }

    @Test func everyReasonARepeatCanMeetIsAnsweredBySendingAgain() {
        var context = FailurePresenter.Context()
        context.agent = "Moss"
        context.provider = "Anthropic"
        context.providerPage = "https://s.example.com/data/substrate.reamde.dev/llm/provider/anthropic"
        context.agentPage = "https://s.example.com/agents/moss"
        for reason in RefusalReason.allCases where reason != .nothingToContinue {
            let card = FailurePresenter.present(Failure(.message, .http(status: 409, code: "conflict", reason: reason.rawValue)), context)
            #expect(card.button == FailureButton(title: "Send Again", action: .sendAgain), "\(reason)")
            #expect(card.note != "Your message is in the box below.", "\(reason)")
        }
        // A conflict with no reason that does not name the field is a
        // refusal like any other: a server older than `turn_cap` answers a
        // full set of turns so.
        let cap = FailurePresenter.present(Failure(.message, .http(status: 409, code: "conflict", reason: nil), said: "4 chat turns are running"), context)
        #expect(cap.sentence == "The server turned your message down." && cap.button?.action == .sendAgain)
    }
}

@Suite("A message pending when the app was killed", .timeLimit(.minutes(10)))
struct KilledMidSend {
    /// The app with "Water the basil" sent and no answer, as the device
    /// holds it when the process dies.
    private func killed() async throws -> (rig: ChatRig, id: String) {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream))])
        rig.server.hold("/chat")
        try await rig.signIn(agents: 3)
        _ = try await rig.sendHi("Water the basil")
        try await rig.harness.until { _ in rig.server.count("/chat") == 1 }
        let id = try #require(rig.sentIDs.first ?? nil)
        #expect(rig.unsentFile.contains(id))
        return (rig, id)
    }

    @Test func standsUnsentAtTheNextLaunchAndIsSentAgainUnderItsId() async throws {
        let (before, id) = try await killed()
        // No record has the id: the read by it answers 404.
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 1))], base: before.base)
        try await rig.signIn(agents: 3)
        let key = try await rig.newChat()
        try await rig.harness.until { _ in await rig.live(key)?.standing == true }
        // Never sent again without a press, and never dropped.
        #expect(rig.server.count("/chat") == 0 && rig.server.count("/llm/message/\(id)") == 1)
        let screen = try #require(await rig.conversation(key))
        #expect(screen.turns.map(\.id) == [id] && screen.composer.draft == "")
        let card = try #require(screen.failures.turns[id])
        #expect(card.sentence == "No answer came back for your message.")
        #expect(card.note == "Substrate Chat closed while it was being sent.")
        #expect(card.button == FailureButton(title: "Send Again", action: .sendAgain) && card.dismiss == .edit)
        try await rig.send(.sendAgain, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.thread == "t1" }
        #expect(rig.sentIDs == [id])
        #expect(rig.server.bodies("POST", "/chat") == before.server.bodies("POST", "/chat"))
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, rows(user: id, text: "Water the basil")))
        rig.server.release()
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        #expect(await rig.turns(key) == ["Water the basil", "Hello"])
        try await rig.harness.until(advancing: 1) { _ in !rig.unsentFile.contains(id) }
    }

    @Test func isTheStoredRowWhereTheServerHoldsItAndNothingIsSent() async throws {
        let (before, id) = try await killed()
        // The send landed before the kill: its record is there.
        let rig = ChatRig(
            baseRoutes() + [
                .init(method: "POST", path: "/chat", answer: .stream(replyStream)),
                .init(method: "GET", path: "/llm/message/\(id)", answer: .json(200, messageJSON(id, text: "Water the basil"))),
            ], base: before.base)
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, rows(user: id, text: "Water the basil")))
        try await rig.signIn(agents: 3)
        let key = try await rig.newChat()
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        // Bound to the thread the message opened: one chat, its rows read,
        // no card, and no request sent.
        #expect(rig.server.count("/chat") == 0)
        #expect(await rig.live(key)?.thread == "t1")
        #expect(await rig.conversation(key)?.turns.map(\.id) == [id, "m2"])
        #expect(await rig.conversation(key)?.failures == ScreenFailures())
        let state = await rig.harness.state
        #expect(state.unsent.isEmpty && state.conversations.count == 1 && state.key(of: "t1") == key)
    }

    @Test func aMessageKeptInBothFilesIsTheUnsentOneAndNoDraft() async throws {
        // The kill came between the two writes: unsent.json has the
        // message, drafts.json still has its text.
        let storage = ChatStorage(base: temporaryFolder("chat"))
        let lease = await storage.open(identity).value
        let message = UnsentMessage(id: "0195c1f2-7a3e-7b10-9c44-2f6d8e1a5b03", message: "Water the basil", agent: helper, thread: "t1", sentAt: Date())
        storage.save(drafts: ["t1": "Water the basil", "t2": "half a thought"], unsent: ["t1": message], lease, ticket: storage.draftsTicket())
        let app = ChatApp(storage: storage, preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        state.disk = nil
        state = try app.apply(
            .init(
                .diskOpened(
                    .init(lease: lease, state: nil, drafts: storage.drafts(lease), unsent: storage.unsent(lease), preferences: ChatPreferences(), stale: []))),
            to: state)
        #expect(state.unsent == ["t1": message] && state.drafts == ["t2": "half a thought"])
        #expect(state.keptUnsent == [message.id])
        // The other way: Edit Message had put it back, with what was typed
        // since behind it, and the kill came before it left the unsent
        // messages. It is the draft, once, and nothing stands to be sent.
        var back = signedInState()
        back.disk = nil
        back = try app.apply(
            .init(
                .diskOpened(
                    .init(
                        lease: lease, state: nil, drafts: ["t1": "Water the basil\n\nand the mint"], unsent: ["t1": message],
                        preferences: ChatPreferences(), stale: []))), to: back)
        #expect(back.unsent.isEmpty && back.drafts == ["t1": "Water the basil\n\nand the mint"])
    }

    @Test func aSentTextIsNoDraftOnDiskOnceItsRequestIsOut() async throws {
        // On any server: the draft's own save waits out a pause, and a kill
        // inside it would bring the sent text back as a draft.
        for revision in [2, 3] {
            let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 1))])
            try await rig.signIn(agents: revision)
            let key = try await rig.newChat()
            try await rig.send(.draft("Water the basil"), to: key)
            // Typed long enough ago to be saved.
            try await rig.harness.until(advancing: 1) { _ in rig.draftsFile.contains("Water the basil") }
            try await rig.send(.send, to: key)
            try await rig.harness.until { _ in rig.server.count("/chat") == 1 }
            // With no move of the clock: the write was not left to the pause.
            try await rig.harness.until { _ in !rig.draftsFile.contains("Water the basil") }
            rig.server.release()
        }
    }

    @Test func aKillBetweenTheTwoFilesLeavesTheTextInOneOfThemEitherWay() async throws {
        let base = temporaryFolder("chat")
        let storage = ChatStorage(base: base)
        let lease = await storage.open(identity).value
        let folder = base.appendingPathComponent(ChatStorage.hash(identity), isDirectory: true)
        let message = UnsentMessage(id: "a", message: "Water the basil", agent: helper, thread: "t1", sentAt: Date())
        // What the two files hold, read as a launch after a kill reads them.
        let held = { () -> Bool in
            let read = { (name: String) in (try? String(contentsOf: folder.appendingPathComponent(name), encoding: .utf8)) ?? "" }
            return read("drafts.json").contains("Water the basil") || read("unsent.json").contains("Water the basil")
        }
        let stops = Noted()
        let stop = { stops.note(held() ? "held" : "lost") }
        // Typed and saved as a draft; then sent: out of the drafts, into
        // the unsent messages.
        storage.save(drafts: ["t1": "Water the basil"], lease, ticket: storage.draftsTicket())
        storage.save(drafts: [:], unsent: ["t1": message], lease, ticket: storage.draftsTicket(), between: stop)
        #expect(storage.unsent(lease) == ["t1": message] && storage.drafts(lease).isEmpty)
        // Edit Message, or a refusal: out of the unsent messages, back into
        // the drafts.
        storage.save(drafts: ["t1": "Water the basil"], unsent: [:], lease, ticket: storage.draftsTicket(), between: stop)
        #expect(storage.unsent(lease).isEmpty && storage.drafts(lease) == ["t1": "Water the basil"])
        // One message sent as another is taken back, in one write.
        let other = UnsentMessage(id: "b", message: "And the mint", agent: helper, thread: "t2", sentAt: Date())
        storage.save(drafts: [:], unsent: ["t1": message], lease, ticket: storage.draftsTicket())
        storage.save(drafts: ["t1": "Water the basil"], unsent: ["t2": other], lease, ticket: storage.draftsTicket(), between: stop)
        #expect(storage.unsent(lease) == ["t2": other])
        // A stop after each file but the last, every time, and the text
        // was in one of the two at each.
        #expect(stops.notes.count == 4 && stops.notes.allSatisfy { $0 == "held" })
    }

    @Test func aNewChatGivesWayToAConversationOnItsThreadThatHoldsSomething() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        let new = ChildKey("new")
        let open = ChildKey("open")
        let theirs = UnsentMessage(id: "b", message: "And the mint", agent: helper, thread: "t1", sentAt: Date())
        state.conversations[new] = .init(seed: .init(thread: nil, agent: helper, draft: "", listen: false), agent: helper, unsent: "a")
        // The thread the new chat's first copy opened, opened from the list
        // meanwhile, with a message of its own waiting on the server.
        state.conversations[open] = .init(
            seed: .init(thread: "t1", agent: helper, draft: "", listen: false), thread: "t1", agent: helper, owns: true, active: true, unsent: "b")
        state.unsent = ["new:new": UnsentMessage(id: "a", message: "Hi", agent: helper, thread: nil, sentAt: Date()), "t1": theirs]
        state.scenes["s"] = .init(current: new)
        // The new chat's repeat is answered at last: its `thread` event.
        var report = ChatAppStep.Report(
            thread: "t1", agent: helper, owns: true, active: true, streaming: true, outcomes: 0, name: "Helper", settles: 0, named: "t1",
            read: nil, draft: nil, sent: false, failure: nil)
        let after = try app.apply(.init(.conversation(new, report)), to: state)
        // The conversation already there stays, with what it holds, and the
        // new chat gives way to it.
        #expect(after.conversations[new] == nil && after.conversations[open] != nil)
        #expect(after.unsent == ["t1": theirs] && after.scenes["s"]?.current == open)
        // A draft the folder keeps for the thread is something held too.
        state.conversations[open] = .init(seed: .init(thread: "t1", agent: helper, draft: "", listen: false), thread: "t1", agent: helper)
        state.unsent["t1"] = nil
        state.drafts = ["t1": "half a thought", "new:new": "typed while it was out"]
        report.unsent = nil
        let drafted = try app.apply(.init(.conversation(new, report)), to: state)
        #expect(drafted.conversations[new] == nil && drafted.conversations[open] != nil)
        #expect(drafted.drafts == ["t1": "half a thought", "new:new": "typed while it was out"])
        // Where the one on the thread holds nothing, it is the one that
        // goes, and the new chat's draft is the thread's.
        state.drafts["t1"] = nil
        let plain = try app.apply(.init(.conversation(new, report)), to: state)
        #expect(plain.conversations[open] == nil && plain.conversations[new]?.thread == "t1" && plain.unsent.isEmpty)
        #expect(plain.drafts == ["t1": "typed while it was out"])
    }

    @Test func aTextPutBackIntoTheComposerIsOnDiskAtOnce() async throws {
        // A refusal hands the text back. It was sent, so the folder's
        // draft of it is gone: until it is written again it is nowhere.
        for revision in [2, 3] {
            let rig = ChatRig(
                baseRoutes() + [
                    .init(method: "POST", path: "/chat", answer: refusal(409, code: "conflict", reason: "thread_busy", "a turn is running on the thread"))
                ])
            try await rig.signIn(agents: revision)
            let key = try await rig.sendHi("Water the basil")
            try await rig.harness.until { _ in await rig.strip(key) != nil }
            // With no move of the clock: not left to the draft's pause.
            // And out of the unsent messages after, never before.
            try await rig.harness.until { _ in rig.draftsFile.contains("Water the basil") && !rig.unsentFile.contains("Water the basil") }
        }
    }

    @Test func aMessageStandingInAChatThatIsGoneIsTheComposersAndTheLists() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        let key = ChildKey("k")
        let conversation = Conversation(
            key: key, seed: .init(thread: "t1", agent: helper, draft: "", listen: false), facts: nil, online: true, keyless: nil, newestTurn: nil,
            visible: true, pollNudge: 0, resumeNudge: 0, continuing: nil, sendsIDs: true)
        var state = conversation.initialState
        state.pending = UnsentMessage(id: "a", message: "Water the basil", agent: helper, thread: "t1", sentAt: Date())
        state.unsentFailure = Failure(.message, .timeout)
        state.draft = "and the mint"
        // The chat was deleted elsewhere: a read of it finds no thread.
        let event = Conversation.Event(.handedOver(thread: nil, rows: []))
        let gone = try conversation.apply(event, to: state)
        #expect(gone.deleted && gone.pending == nil && gone.draft == "Water the basil\n\nand the mint")
        // And the list keeps it, so it outlives the window and the launch.
        guard case let .conversation(_, report)? = app.event(fromChild: key, event, after: gone)?.step else {
            Issue.record("the list is not told")
            return
        }
        #expect(report.draft == "Water the basil\n\nand the mint" && report.gaveBack && report.unsent == nil)
    }

    @Test func aWriteAskedForEarlierNeverLandsOverALaterOne() async throws {
        let storage = ChatStorage(base: temporaryFolder("chat"))
        let lease = await storage.open(identity).value
        // The draft's coalesced save was asked for first and waits; the
        // send's write lands meanwhile, with the draft cleared.
        let waiting = storage.draftsTicket()
        let sent = storage.draftsTicket()
        let message = UnsentMessage(id: "a", message: "Hi", agent: helper, thread: "t1", sentAt: Date())
        storage.save(drafts: [:], unsent: ["t1": message], lease, ticket: sent)
        storage.save(drafts: ["t1": "Hi"], lease, ticket: waiting)
        #expect(storage.drafts(lease).isEmpty && storage.unsent(lease) == ["t1": message])
        // A later save of the drafts alone leaves the unsent messages.
        storage.save(drafts: ["t1": "more"], lease, ticket: storage.draftsTicket())
        #expect(storage.drafts(lease) == ["t1": "more"] && storage.unsent(lease) == ["t1": message])
    }
}

/// What a watcher noted, in order.
private final class Noted: @unchecked Sendable {
    private let lock = NSLock()
    private var kept: [String] = []

    func note(_ text: String) { lock.withLock { kept.append(text) } }
    var notes: [String] { lock.withLock { kept } }
}
