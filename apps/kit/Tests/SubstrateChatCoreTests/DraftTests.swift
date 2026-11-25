import Foundation
import SubstrateCore
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import SubstrateChatCore

/// A composer's field as the app's view keeps it: its own text, and what it
/// tells its conversation of it (`DraftReports`). The view does these same
/// steps, in this order.
private struct Field {
    var text = ""
    var reports = DraftReports()

    /// A key: the text changes, and nothing is told.
    mutating func type(_ more: String, at now: Date = Date(timeIntervalSince1970: 0)) {
        text += more
        reports.typed(text, at: now)
    }

    /// The tree's composer arrived or changed: the field takes what moved,
    /// and answers what it then has to say.
    mutating func look(_ composer: ComposerPresentation?) -> Conversation.Command? {
        guard let composer else { return nil }
        if let next = reports.take(composer, holding: text) { text = next }
        let said = reports.say()
        reports.typed(text, at: Date(timeIntervalSince1970: 0))
        return said
    }

    /// The field leaves the screen.
    mutating func leave() -> Conversation.Command { reports.leave(text) }
}

@Suite("A draft is the field's while it is typed", .timeLimit(.minutes(10)))
struct DraftTests {
    let start = Date(timeIntervalSince1970: 1000)

    private func composer(_ rig: ChatRig, _ key: ChildKey) async -> ComposerPresentation? {
        await rig.conversation(key)?.composer
    }

    /// Tells the conversation what the field has to say, if anything.
    private func tell(_ rig: ChatRig, _ key: ChildKey, _ command: Conversation.Command?) async throws {
        if let command { try await rig.send(command, to: key) }
    }

    // MARK: When the field speaks

    @Test func aKeyTellsNothingAndAPauseTellsTheWholeText() {
        var field = Field()
        field.type("Hel", at: start)
        field.type("lo", at: start.addingTimeInterval(0.2))
        #expect(field.reports.due == start.addingTimeInterval(0.2 + DraftReports.pause))
        let early = field.reports.paused(at: start.addingTimeInterval(0.6))
        let due = field.reports.paused(at: start.addingTimeInterval(0.7))
        #expect(early == nil && due == .draft("Hello"))
        // Said: nothing is due, and nothing is said twice.
        let again = field.reports.say()
        #expect(field.reports.due == nil && again == nil)
    }

    @Test func typingWithoutAPauseIsNeverMoreThanTwoSecondsAhead() {
        var field = Field()
        var told: [Conversation.Command] = []
        for step in 0 ..< 20 {
            let now = start.addingTimeInterval(Double(step) * 0.3)
            if let command = field.reports.paused(at: now) { told.append(command) }
            field.type("x", at: now)
        }
        // No pause of half a second in six seconds of keys: told every two.
        #expect(told == [.draft(String(repeating: "x", count: 7)), .draft(String(repeating: "x", count: 14))])
    }

    @Test func backAtWhatWasSaidThereIsNothingToSay() {
        var field = Field()
        field.type("a", at: start)
        let said = field.reports.say()
        #expect(said == .draft("a"))
        field.type("b", at: start)
        field.text = "a"
        field.reports.typed("a", at: start)
        let nothing = field.reports.say()
        #expect(field.reports.due == nil && nothing == nil)
    }

    // MARK: The tree

    @Test func whatIsTypedIsKeptForTheSendAndTheDiskAndTheTreeDoesNotCarryIt() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        let key = try await rig.newChat()
        let before = try #require(await composer(rig, key))
        let published = rig.harness.published
        try await rig.send(.draft("Half a thou"), to: key)
        // The conversation holds it, and so does the list, for the disk.
        #expect(await rig.live(key)?.draft == "Half a thou")
        #expect(await rig.harness.state.drafts["new:\(key.rawValue)"] == "Half a thou")
        // Nothing a view draws moved: the tree is the one before, and none
        // was published.
        #expect(await composer(rig, key) == before)
        #expect(rig.harness.published == published)
        try await rig.harness.until(advancing: 1) { _ in rig.storage.drafts(try! await rig.lease())["new:\(key.rawValue)"] == "Half a thou" }
    }

    // MARK: No key is lost

    @Test func aSendSendsEveryKeyAndKeepsWhatIsTypedAfterIt() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 1))])
        try await rig.signIn()
        let key = try await rig.newChat()
        var field = Field()
        try await tell(rig, key, field.look(await composer(rig, key)))
        // Typed and sent at once, with no pause for the conversation to
        // have heard of it.
        field.type("Hello", at: start)
        try await tell(rig, key, field.reports.tell(field.text))
        try await rig.send(.send, to: key)
        // More keys before the field has seen its text cleared.
        field.type(" again", at: start)
        try await rig.harness.until { _ in rig.server.count("/chat") == 1 }
        #expect(rig.server.body("POST", "/chat")?.contains(#""message":"Hello""#) == true)
        // The cleared draft arrives: the sent text goes, the keys after it stay.
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == " again")
        #expect(await rig.live(key)?.draft == " again")
        rig.server.release()
    }

    @Test func aPauseBetweenTheSendAndItsClearingBringsNothingBack() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 1))])
        try await rig.signIn()
        let key = try await rig.newChat()
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("Hello", at: start)
        try await tell(rig, key, field.reports.say())
        try await rig.send(.send, to: key)
        // The field was slow to look: it typed on, paused and told the whole
        // of what it still showed, the sent text included.
        field.type(" again", at: start)
        try await tell(rig, key, field.reports.say())
        #expect(await rig.live(key)?.draft == "Hello again")
        // Looking, it drops what was sent and says what is left.
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == " again")
        #expect(await rig.live(key)?.draft == " again")
        rig.server.release()
    }

    @Test func aRefusedSendGivesItsTextBackAndKeepsWhatWasTypedSince() async throws {
        let refusal = #"{"error":{"code":"conflict","message":"a turn is already running on this thread"}}"#
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .json(409, refusal))])
        try await rig.signIn()
        let key = try await rig.newChat()
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("Hello", at: start)
        try await tell(rig, key, field.reports.say())
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.chatFailure != nil }
        // The field saw neither the clearing nor the text given back until
        // now, and typed meanwhile: it holds both.
        field.type("!", at: start)
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == "Hello!")
        #expect(await rig.live(key)?.draft == "Hello!")
    }

    @Test func aRefusedSendComesBackAheadOfWhatWasToldSinceAndTheFieldKeepsItsNextKey() async throws {
        let refusal = #"{"error":{"code":"conflict","message":"a turn is already running on this thread"}}"#
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .json(409, refusal))])
        rig.server.hold("/chat")
        try await rig.signIn()
        let key = try await rig.newChat()
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("Water the basil", at: start)
        try await tell(rig, key, field.reports.say())
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.phase == .streaming }
        try await tell(rig, key, field.look(await composer(rig, key)))
        // Typed and told while the send is out, then a key the conversation
        // has not heard of when the refusal lands.
        field.type("and the mint", at: start)
        try await tell(rig, key, field.reports.say())
        field.type("!", at: start)
        rig.server.letGo("/chat")
        try await rig.harness.until { _ in await rig.live(key)?.chatFailure != nil }
        // The conversation put the message ahead of what it had heard; the
        // field takes that and keeps its key at the end.
        #expect(await composer(rig, key)?.draft == "Water the basil\n\nand the mint")
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == "Water the basil\n\nand the mint!")
        #expect(await rig.live(key)?.draft == "Water the basil\n\nand the mint!")
    }

    @Test func sendAgainSendsWhatTheFieldHoldsAndNotWhatTheConversationLastHeard() async throws {
        let refusal = #"{"error":{"code":"conflict","message":"a turn is already running on this thread"}}"#
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .json(409, refusal))])
        try await rig.signIn()
        let key = try await rig.newChat()
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("Water the basil", at: start)
        try await tell(rig, key, field.reports.say())
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.chatFailure != nil }
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == "Water the basil")
        #expect(await rig.conversation(key)?.failures.strip?.button?.action == .send)
        // More is typed, with no pause, and the strip's Send Again is
        // pressed: the field says what it holds first (`DraftField`), then
        // the send goes.
        field.type(" twice a week", at: start)
        #expect(await rig.live(key)?.draft == "Water the basil")
        rig.server.route("POST", "/chat", .stream(replyStream, holdAfter: 2))
        try await rig.send(field.reports.tell(field.text), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in rig.server.count("/chat") == 2 }
        let body = try #require(JSONValue.parse(rig.server.body("POST", "/chat")))
        #expect(body["message"]?.string == "Water the basil twice a week")
        // And the field is cleared of what was sent.
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == "")
        rig.server.release()
    }

    @Test func aNewLineFromReturnIsTheFieldsAndCommandReturnSendsItWithTheRest() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 2))])
        try await rig.signIn()
        let key = try await rig.newChat()
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("Water the basil", at: start)
        // Return on a Mac (`ComposerKeys`): the new line goes into the
        // field's own text, where the cursor is, and the conversation hears
        // nothing of it.
        #expect(ComposerKeys.onReturn([], on: .mac, composing: false) == .newline)
        let (text, cursor) = ComposerKeys.newline(in: field.text, replacing: nil)
        field.text = text
        field.reports.typed(text, at: start)
        #expect(cursor == text.count && text == "Water the basil\n")
        field.type("and the mint", at: start)
        #expect(await rig.live(key)?.draft == "")
        // Command-Return: what the field holds is said, then sent.
        #expect(ComposerKeys.onReturn([.command], on: .mac, composing: false) == .send)
        try await rig.send(field.reports.tell(field.text), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in rig.server.count("/chat") == 1 }
        let body = try #require(JSONValue.parse(rig.server.body("POST", "/chat")))
        #expect(body["message"]?.string == "Water the basil\nand the mint")
        // The field is cleared of what was sent, the new line with it.
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == "")
        rig.server.release()
    }

    @Test func aFieldThatSawItsTextClearedKeepsWhatItTypedBeforeARefusal() async throws {
        let refusal = #"{"error":{"code":"conflict","message":"a turn is already running on this thread"}}"#
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .json(409, refusal))])
        rig.server.hold("/chat")
        try await rig.signIn()
        let key = try await rig.newChat()
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("Hello", at: start)
        try await tell(rig, key, field.reports.say())
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.phase == .streaming }
        // Cleared, and seen cleared; then a key the conversation has not
        // heard of when the refusal lands.
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == "")
        field.type("P", at: start)
        rig.server.letGo("/chat")
        try await rig.harness.until { _ in await rig.live(key)?.chatFailure != nil }
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == "HelloP")
        #expect(await rig.live(key)?.draft == "HelloP")
    }

    @Test func leavingTheFrontKeepsEveryKeyForTheDisk() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("Not yet sai", at: start)
        // The app leaves the front before any pause: the field says it all.
        try await tell(rig, key, field.reports.say())
        await rig.harness.send(.phase(.inactive))
        await rig.harness.send(.phase(.background))
        #expect(await rig.harness.state.drafts["t1"] == "Not yet sai")
        // Written at once: the clock does not move, and the app may not be
        // running half a second from now.
        let lease = try await rig.lease()
        try await rig.harness.until { _ in rig.storage.drafts(lease)["t1"] == "Not yet sai" }
        // Back in front, the same field holds its text, once.
        await rig.harness.send(.phase(.active))
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == "Not yet sai")
    }

    @Test func whatAFieldSaysAfterTheAppLeftTheFrontIsWrittenAtOnceToo() async throws {
        let rig = ChatRig(platform: .ios)
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("Told late", at: start)
        // The app's own word of leaving arrives before the field's.
        await rig.harness.send(.phase(.inactive))
        await rig.harness.send(.phase(.background))
        try await tell(rig, key, field.reports.say())
        let lease = try await rig.lease()
        try await rig.harness.until { _ in rig.storage.drafts(lease)["t1"] == "Told late" }
    }

    @Test func whatAFieldSaysBetweenTheTwoWordsOfLeavingIsWrittenAtOnce() async throws {
        let rig = ChatRig(platform: .ios)
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("In between", at: start)
        // Inactive comes before background, and the field's word between
        // them: it must not wait for the second, which may never be heard.
        await rig.harness.send(.phase(.inactive))
        try await tell(rig, key, field.reports.say())
        let lease = try await rig.lease()
        try await rig.harness.until { _ in rig.storage.drafts(lease)["t1"] == "In between" }
        // In front again, a draft waits for more, as before.
        await rig.harness.send(.phase(.active))
        field.type(", and after", at: start)
        let mark = await rig.harness.effectsStarted
        try await tell(rig, key, field.reports.say())
        #expect(await rig.harness.state.drafts["t1"] == "In between, and after")
        #expect(rig.storage.drafts(lease)["t1"] == "In between")
        try await rig.harness.untilIdle(since: mark, advancing: 1)
        #expect(rig.storage.drafts(lease)["t1"] == "In between, and after")
    }

    @Test func anOlderWriteOfTheDraftsThatLandsAfterANewerIsDropped() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        let lease = try await rig.lease()
        // Asked for in this order, as two writes with no wait are; run in
        // the other.
        let older = rig.storage.draftsTicket()
        let newer = rig.storage.draftsTicket()
        rig.storage.save(drafts: ["t1": "the whole sentence"], lease, ticket: newer)
        rig.storage.save(drafts: ["t1": "the whole sen"], lease, ticket: older)
        #expect(rig.storage.drafts(lease)["t1"] == "the whole sentence")
        // A write asked for after both lands.
        rig.storage.save(drafts: [:], lease)
        #expect(rig.storage.drafts(lease).isEmpty)
    }

    @Test func twoWindowsOnOneChatEachSendWhatTheirOwnFieldShows() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 1))])
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "a"))
        await rig.harness.send(.show(thread: "t1", scene: "b"))
        let key = try #require(await rig.harness.state.scenes["a"]?.current)
        #expect(await rig.harness.state.scenes["b"]?.current == key)
        // Its thread is read: the chat knows who it is with.
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        var first = Field()
        var second = Field()
        _ = first.look(await composer(rig, key))
        _ = second.look(await composer(rig, key))
        // Each types and pauses: the conversation holds what it heard last.
        first.type("Alpha", at: start)
        try await tell(rig, key, first.reports.say())
        second.type("Beta", at: start)
        try await tell(rig, key, second.reports.say())
        #expect(await rig.live(key)?.draft == "Beta")
        // The first sends: what its own field shows goes.
        try await tell(rig, key, first.reports.tell(first.text))
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in rig.server.count("/chat") == 1 }
        #expect(rig.server.body("POST", "/chat")?.contains(#""message":"Alpha""#) == true)
        // Its field is cleared, and the other keeps what was typed into it.
        try await tell(rig, key, first.look(await composer(rig, key)))
        try await tell(rig, key, second.look(await composer(rig, key)))
        #expect(first.text == "" && second.text == "Beta")
        #expect(await rig.live(key)?.draft == "Beta")
        rig.server.release()
    }

    @Test func textTypedOverWhatWasSentAndAlreadyToldOutlivesTheClearing() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 1))])
        try await rig.signIn()
        let key = try await rig.newChat()
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("Hello", at: start)
        try await tell(rig, key, field.reports.tell(field.text))
        try await rig.send(.send, to: key)
        // Before the field sees its text cleared, all of it is replaced,
        // and a pause tells the conversation so.
        field.text = "Goodbye"
        field.reports.typed(field.text, at: start)
        try await tell(rig, key, field.reports.say())
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == "Goodbye")
        #expect(await rig.live(key)?.draft == "Goodbye")
        rig.server.release()
    }

    @Test func aChatShownAgainStartsFromWhatWasTypedIntoIt() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("To be continued", at: start)
        // Another window keeps the conversation while this one moves on.
        await rig.harness.send(.show(thread: "t1", scene: "other"))
        await rig.harness.send(.newChat(agent: helper, listen: false, scene: "s"))
        // The field leaves the screen: it says what it holds, to its
        // conversation and to the list.
        try await tell(rig, key, field.leave())
        await rig.harness.send(.keepDraft(key: key, thread: "t1", text: field.text))
        // Shown again, a new field starts from it, in the tree it mounts on.
        await rig.harness.send(.sceneGone(scene: "other"))
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        var next = Field()
        _ = next.look(await composer(rig, try #require(await rig.harness.state.scenes["s"]?.current)))
        #expect(next.text == "To be continued")
    }

    @Test func aChatWhoseConversationWentFirstKeepsItsDraftAllTheSame() async throws {
        let rig = ChatRig(platform: .ios)
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("On a phone", at: start)
        // Back to the list on a phone: the conversation is gone before the
        // field leaves the screen and speaks.
        await rig.harness.send(.back(scene: "s"))
        #expect(await rig.harness.state.conversations[key] == nil)
        try? await tell(rig, key, field.leave())
        await rig.harness.send(.keepDraft(key: key, thread: "t1", text: field.text))
        #expect(await rig.harness.state.drafts["t1"] == "On a phone")
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        var next = Field()
        _ = next.look(await composer(rig, try #require(await rig.harness.state.scenes["s"]?.current)))
        #expect(next.text == "On a phone")
    }

    @Test func aNewChatLeftBehindHandsItsDraftToTheNext() async throws {
        let rig = ChatRig(platform: .ios)
        try await rig.signIn()
        let key = try await rig.newChat()
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("An idea", at: start)
        await rig.harness.send(.back(scene: "s"))
        try? await tell(rig, key, field.leave())
        await rig.harness.send(.keepDraft(key: key, thread: nil, text: field.text))
        let next = try await rig.newChat()
        #expect(await composer(rig, next)?.draft == "An idea")
    }

    @Test func aFieldThatComesAfterStartsFromWhatTheOneBeforeHeldWhicheverIsHeardFirst() async throws {
        // The window's layout changed: the field is made again over the
        // same conversation, and the order its leaving and the new one's
        // opening reach the conversation in is not the view's to say.
        for leavesFirst in [true, false] {
            let rig = ChatRig()
            try await rig.signIn()
            await rig.harness.send(.show(thread: "t1", scene: "s"))
            let key = try #require(await rig.harness.state.scenes["s"]?.current)
            var field = Field()
            _ = field.look(await composer(rig, key))
            field.type("Mid-sentence", at: start)
            var next = Field()
            if leavesFirst { try await tell(rig, key, field.leave()) }
            _ = next.look(await composer(rig, key))
            try await rig.send(.open, to: key)
            // A key in the new field before it has seen anything more.
            next.type("!", at: start)
            if !leavesFirst { try await tell(rig, key, field.leave()) }
            try await tell(rig, key, next.look(await composer(rig, key)))
            #expect(next.text == "Mid-sentence!")
            #expect(await rig.live(key)?.draft == "Mid-sentence!")
        }
    }

    @Test func aFieldThatTypedOverAStaleTreeKeepsItsKeysAfterTheTextItTakesUp() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        // Told after a pause, so the tree never carried it; then the window
        // goes behind, and a field made later mounts on the tree as it was.
        try await rig.send(.draft("Earlier"), to: key)
        var field = Field()
        _ = field.look(await composer(rig, key))
        #expect(field.text == "")
        field.type(" and now", at: start)
        try await rig.send(.open, to: key)
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == "Earlier and now")
        #expect(await rig.live(key)?.draft == "Earlier and now")
    }

    @Test func whatAFieldToldThatLandedAfterARewriteIsSetRight() async throws {
        let refusal = #"{"error":{"code":"conflict","message":"a turn is already running on this thread"}}"#
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .json(409, refusal))])
        rig.server.hold("/chat")
        try await rig.signIn()
        let key = try await rig.newChat()
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("Hello", at: start)
        try await tell(rig, key, field.reports.say())
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.phase == .streaming }
        try await tell(rig, key, field.look(await composer(rig, key)))
        // A key, a pause, and the field's word is on its way when the
        // refusal gives the text back: it lands after, over the text.
        field.type("P", at: start)
        let late = field.reports.say()
        rig.server.letGo("/chat")
        try await rig.harness.until { _ in await rig.live(key)?.chatFailure != nil }
        try await tell(rig, key, late)
        #expect(await rig.live(key)?.draft == "P")
        // The field takes the text back with its key after it, and says so.
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == "HelloP")
        #expect(await rig.live(key)?.draft == "HelloP")
    }

    @Test func aFieldStillOnScreenNeverTakesItsOwnTextTwice() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("abc", at: start)
        try await tell(rig, key, field.reports.say())
        // The window goes behind and comes back: the conversation is shown
        // again, and carries what it holds to a field that already has it.
        await rig.harness.send(.sceneActive(scene: "s", active: false))
        await rig.harness.send(.sceneActive(scene: "s", active: true))
        #expect(await composer(rig, key)?.draft == "abc")
        field.type("d", at: start)
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == "abcd")
        #expect(await rig.live(key)?.draft == "abcd")
    }

    // MARK: What the engine writes

    @Test func whatDictationHeardLandsAfterTheDraftAndWhatWasTypedSince() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        let key = try await rig.newChat()
        var field = Field()
        _ = field.look(await composer(rig, key))
        field.type("Buy", at: start)
        try await tell(rig, key, field.reports.say())
        try await rig.send(.heard(" milk and eggs "), to: key)
        field.type("!", at: start)
        try await tell(rig, key, field.look(await composer(rig, key)))
        #expect(field.text == "Buy milk and eggs!")
        #expect(await rig.live(key)?.draft == "Buy milk and eggs!")
        // Nothing heard is no rewrite.
        let revision = await composer(rig, key)?.draftRevision
        try await rig.send(.heard("  "), to: key)
        #expect(await composer(rig, key)?.draftRevision == revision)
    }

    @Test func anOpenerPickedIsWrittenIntoTheField() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        let key = try await rig.newChat()
        var field = Field()
        _ = field.look(await composer(rig, key))
        try await rig.send(.fill("What can you help me with?"), to: key)
        let nothing = field.look(await composer(rig, key))
        #expect(nothing == nil)
        #expect(field.text == "What can you help me with?")
    }

    @Test func anEditInsideWhatWasReplacedStandsAsTyped() {
        var field = Field()
        var composer = ComposerPresentation(
            draft: "", draftRevision: 0, draftReplaced: "", draftBefore: "", placeholder: "", blocked: nil, blockedWhenEmpty: false,
            keyless: nil, listenOnOpen: false, canSend: true)
        _ = field.look(composer)
        field.type("Hello", at: start)
        let said = field.reports.say()
        #expect(said == .draft("Hello"))
        // Sent, and before the clearing is seen the text is edited at its
        // start: no rule says which keys came after, so all of it stays.
        field.text = "Oh, hello"
        field.reports.typed(field.text, at: start)
        composer.draftRevision = 1
        composer.draftReplaced = "Hello"
        composer.draftBefore = "Hello"
        let kept = field.look(composer)
        #expect(kept == .draft("Oh, hello"))
        #expect(field.text == "Oh, hello")
    }
}
