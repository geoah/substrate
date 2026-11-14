import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

private func context(_ change: (inout FailurePresenter.Context) -> Void = { _ in }) -> FailurePresenter.Context {
    var context = FailurePresenter.Context()
    context.agent = "Moss"
    context.agentID = "substrate.reamde.dev/assistant/moss"
    context.provider = "Anthropic"
    context.providerID = "anthropic"
    context.host = "s.example.com"
    context.providerPage = "https://s.example.com/data/substrate.reamde.dev/llm/provider/anthropic"
    context.agentPage = "https://s.example.com/agents/substrate.reamde.dev%2Fassistant%2Fmoss"
    context.thread = "t1"
    change(&context)
    return context
}

private func run(_ cause: String?, status: String = "error", said: String? = nil, retryAfter: Int? = nil, at: Date? = nil) -> Failure {
    Failure(.reply, .run(status: status, cause: cause), said: said, retryAfter: retryAfter, at: at)
}

private let providerPage = FailureAction.open("https://s.example.com/data/substrate.reamde.dev/llm/provider/anthropic")
private let agentPage = FailureAction.open("https://s.example.com/agents/substrate.reamde.dev%2Fassistant%2Fmoss")

@Suite("A failure's sentence and its one button", .timeLimit(.minutes(10)))
struct FailureSentences {
    /// The table of the plan's 8c, a row per cause of the closed set.
    static let causes: [RunCause: (sentence: String, note: String?, button: String, action: FailureAction)] = [
        .providerKeyMissing: ("Anthropic has no API key yet.", nil, "Add Key", providerPage),
        .providerKeyRefused: ("Anthropic refused the API key.", nil, "Replace Key", providerPage),
        .providerRateLimited: ("Anthropic is limiting requests.", nil, "Try Again", .resume),
        .providerBilling: ("The Anthropic account is out of credit.", nil, "Try Again", .resume),
        .providerOverloaded: ("Anthropic is having trouble.", nil, "Try Again", .resume),
        .providerUnavailable: ("Anthropic is having trouble.", nil, "Try Again", .resume),
        .modelUnavailable: ("Anthropic does not offer this model to your key.", nil, "Open in Console", agentPage),
        .contextTooLong: ("This chat is too long for the model.", nil, "New Chat", .newChat(agent: "substrate.reamde.dev/assistant/moss")),
        .allowanceSpent: ("Today's allowance is used up.", "It resets at midnight UTC.", "Add Your Own Key", providerPage),
        .interrupted: ("The server restarted while Moss was working.", nil, "Try Again", .resume),
        .writeFailed: ("The server couldn't save what Moss was doing.", nil, "Try Again", .resume),
        .internal: ("Moss ran into a problem and didn't finish.", nil, "Try Again", .resume),
        .turnLimit: ("Moss ran out of room before finishing.", nil, "Continue", .resume),
        .toolCallLimit: ("Moss ran out of room before finishing.", nil, "Continue", .resume),
        .deadline: ("Moss ran out of room before finishing.", nil, "Continue", .resume),
        .owner: ("Moss was stopped before it finished.", nil, "Continue", .resume),
        .shutdown: ("The server restarted while Moss was working.", nil, "Try Again", .resume),
    ]

    @Test func everyCauseHasItsSentenceAndItsButton() {
        #expect(Set(Self.causes.keys) == Set(RunCause.allCases))
        for cause in RunCause.allCases {
            let expected = Self.causes[cause]!
            let card = FailurePresenter.present(run(cause.rawValue), context())
            #expect(card.sentence == expected.sentence, "\(cause)")
            #expect(card.note == expected.note, "\(cause)")
            #expect(card.button?.title == expected.button, "\(cause)")
            #expect(card.button?.action == expected.action, "\(cause)")
            #expect(card.button?.external == (expected.action == providerPage || expected.action == agentPage), "\(cause)")
        }
    }

    @Test func aCauseANewerServerSendsGetsTheSentenceForItsStatus() {
        // Never the matching over its text: the server named a cause.
        let unknown = FailurePresenter.present(run("provider_on_holiday", said: "401 Unauthorized"), context())
        #expect(unknown.sentence == "Moss couldn't finish this reply." && unknown.button?.title == "Try Again" && unknown.tone == .bad)
        let limit = FailurePresenter.present(run("a_new_budget", status: "overbudget"), context())
        #expect(limit.sentence == "Moss ran out of room before finishing." && limit.button?.title == "Continue")
        let stopped = FailurePresenter.present(run("a_new_stop", status: "stopped"), context())
        #expect(stopped.sentence == "Moss was stopped before it finished." && stopped.button?.title == "Continue" && stopped.tone == .quiet)
        // A server that names none is read for one.
        #expect(FailurePresenter.present(run(nil, said: "401 Unauthorized"), context()).sentence == "Anthropic refused the API key.")
    }

    @Test func everyRefusalReasonHasItsSentenceAndItsButton() {
        let expected: [RefusalReason: (String, String, FailureAction)] = [
            .threadBusy: ("Moss is still answering in this chat.", "Send Again", .send),
            .turnCap: ("Too many replies are being written at once.", "Send Again", .send),
            .spendCap: ("Today's spending limit is reached.", "Open in Console", agentPage),
            .providerKeyMissing: ("Anthropic has no API key yet.", "Add Key", providerPage),
            .quota: ("Today's allowance is used up.", "Add Your Own Key", providerPage),
            .keyRefused: ("Anthropic refused the API key.", "Replace Key", providerPage),
        ]
        #expect(Set(expected.keys) == Set(RefusalReason.allCases))
        for reason in RefusalReason.allCases {
            let failure = Failure(.send, .http(status: 409, code: "conflict", reason: reason.rawValue), said: "the server's words")
            let strip = FailurePresenter.present(failure, context())
            #expect(strip.sentence == expected[reason]!.0, "\(reason)")
            #expect(strip.button?.title == expected[reason]!.1, "\(reason)")
            #expect(strip.button?.action == expected[reason]!.2, "\(reason)")
        }
        // A reason this build does not know acts on the status alone.
        let unknown = FailurePresenter.present(Failure(.send, .http(status: 409, code: "conflict", reason: "a_new_reason"), said: "words"), context())
        #expect(unknown.sentence == "Your message wasn't sent." && unknown.note == "Your message is in the box below.")
        #expect(unknown.button?.title == "Send Again")
    }

    @Test func aReplyCutShortSaysHow() {
        let expected: [CutBy: (String, String)] = [
            .length: ("The reply stopped at the model's limit.", "Continue"),
            .filter: ("Anthropic withheld the rest of this reply.", "Continue"),
            .stop: ("Moss was stopped before it finished.", "Continue"),
            .failure: ("Moss couldn't finish this reply.", "Try Again"),
        ]
        #expect(Set(expected.keys) == Set(CutBy.allCases))
        for cut in CutBy.allCases {
            let card = FailurePresenter.present(Failure(.reply, .cut(cut.rawValue)), context())
            #expect(card.sentence == expected[cut]!.0 && card.button?.title == expected[cut]!.1 && card.button?.action == .resume, "\(cut)")
        }
        // An older turn's card says what happened and offers nothing.
        let older = FailurePresenter.present(Failure(.reply, .cut("length")), context { $0.acts = false })
        #expect(older.sentence == "The reply stopped at the model's limit." && older.button == nil)
        let unanswered = FailurePresenter.present(Failure(.reply, .unanswered), context { $0.acts = false })
        #expect(unanswered.sentence == "Moss didn't answer this message." && unanswered.button == nil && unanswered.tone == .quiet)
    }

    /// What each attempt says when it did not get through, and the one
    /// button that does it again; nil where its place has its own controls.
    static let attempts: [Failure.Attempt: (sentence: String, button: String?, action: FailureAction?)] = [
        .send: ("You're offline.", "Send Again", .send),
        .resume: ("Couldn't ask Moss to carry on.", "Try Again", .resume),
        .reply: ("Moss couldn't finish this reply.", "Try Again", .resume),
        .stop: ("Couldn't stop Moss. The reply is still being written.", "Try Again", .stop),
        .attach: ("Couldn't open the reply Moss is writing.", "Try Again", .reopen),
        .reload: ("Couldn't load the end of this chat.", "Try Again", .reload),
        .rows: ("Couldn't load this chat's messages.", "Try Again", .reloadRows(thread: "t1")),
        .poll: ("Couldn't check on this chat.", nil, nil),
        .rename: ("Couldn't rename this chat.", "Try Again", .rename(thread: "t1", subject: "Plants")),
        .card: ("Couldn't load this.", nil, nil),
        .answer: ("That didn't go through.", nil, nil),
        .decide: ("That didn't go through.", nil, nil),
        .addKey: ("The key wasn't saved.", nil, nil),
        .refresh: ("Couldn't refresh your chats.", "Try Again", .refresh),
        .feed: ("Live updates stopped.", "Try Again", .refresh),
        .guidance: ("Couldn't read them.", nil, nil),
        .guidanceChange: ("That didn't go through.", nil, nil),
    ]

    @Test func everyAttemptHasItsSentenceAndItsButton() {
        #expect(Set(Self.attempts.keys) == Set(Failure.Attempt.allCases))
        for attempt in Failure.Attempt.allCases {
            let expected = Self.attempts[attempt]!
            let made = FailurePresenter.present(Failure(attempt, .offline), context { $0.subject = "Plants" })
            #expect(made.sentence == expected.sentence, "\(attempt)")
            #expect(made.button?.title == expected.button, "\(attempt)")
            #expect(made.button?.action == expected.action, "\(attempt)")
            // The note is why, but for a send, whose sentence it is.
            #expect(made.note == (attempt == .send ? "Your message is in the box below." : "You're offline."), "\(attempt)")
        }
    }

    @Test func everyReasonIsSaidInWordsAPersonUses() {
        let reasons: [(Failure.Why, String)] = [
            (.offline, "You're offline."), (.unreachable, "Couldn't reach s.example.com."),
            (.timeout, "The server took too long to answer."), (.streamEnded, "The connection dropped."),
            (.notTheAPI(status: 502), "The server answered with something Substrate Chat can't read."),
            (.sessionEnded, "Your sign-in ended."), (.needsApproval, "Substrate Chat needs your approval for that."),
            (.ended, "The server ended it."),
            (.http(status: 500, code: "internal", reason: nil), "The server ran into a problem."),
            (.http(status: 429, code: "rate_limited", reason: nil), "The server is busy. Try again in a moment."),
            (.http(status: 409, code: "conflict", reason: nil), "It changed in the meantime."),
            (.http(status: 404, code: "not_found", reason: nil), "It is no longer there."),
            (.http(status: 403, code: "forbidden", reason: nil), "You're not allowed to do that."),
            (.http(status: 422, code: "validation", reason: nil), "The server refused it."),
        ]
        for (why, words) in reasons {
            #expect(FailurePresenter.present(Failure(.reload, why), context()).note == words, "\(why)")
        }
    }

    @Test func theServersWordsAreUnderDetailsAndTheAppsOwnAreNeverCalledTheServers() {
        let server = FailurePresenter.present(run("internal", said: "substrate: validation failed: agent …: llm: …"), context())
        #expect(server.details == FailureDetails(title: "Details", label: "The server said", text: "substrate: validation failed: agent …: llm: …"))
        #expect(!server.sentence.contains("substrate:"))
        // What the owner's screenshot showed under "The server said".
        let dropped = Failure(.attach, StreamBroke(answered: true, underlying: URLError(.networkConnectionLost)), server: origin)
        #expect(dropped.why == .streamEnded && dropped.said == nil)
        let device = FailurePresenter.present(dropped, context())
        #expect(device.details?.label == "This device reported")
        #expect(device.details?.text == "The connection to s.example.com dropped.")
        #expect(FailurePresenter.present(Failure(.reload, .timeout), context()).details == nil)
    }

    @Test func theKeySheetIsTheAppsOwnWhereItHasOne() {
        let sheet = FailurePresenter.present(run("provider_key_missing"), context { $0.keySheet = true })
        #expect(sheet.button == FailureButton(title: "Add Key", action: .addKey(provider: "anthropic")))
        // A refused key has a key: no sheet adds one, so the console's page.
        let refused = FailurePresenter.present(run("provider_key_refused"), context { $0.keySheet = true })
        #expect(refused.button == FailureButton(title: "Replace Key", action: providerPage, external: true))
    }

    @Test func whatMossWroteIsSaidKeptOnlyWhereItIs() {
        #expect(FailurePresenter.present(run("provider_overloaded"), context()).note == nil)
        #expect(FailurePresenter.present(run("provider_overloaded"), context { $0.kept = true }).note == "What Moss wrote so far is kept.")
    }

    @Test func anErrorBecomesWhatWasAttemptedAndWhy() {
        func why(_ error: any Error) -> Failure.Why { Failure(.send, error, server: origin).why }
        #expect(why(URLError(.notConnectedToInternet)) == .offline)
        #expect(why(URLError(.cannotConnectToHost)) == .unreachable)
        #expect(why(URLError(.timedOut)) == .timeout)
        #expect(why(URLError(.networkConnectionLost)) == .streamEnded)
        #expect(why(SessionExpired(message: nil)) == .sessionEnded)
        #expect(why(InsufficientScope(message: nil)) == .needsApproval)
        #expect(why(TransportError(status: 502, body: Data("<html>".utf8))) == .notTheAPI(status: 502))
        #expect(why(ServerError(status: 409, code: "conflict", message: "busy")) == .http(status: 409, code: "conflict", reason: nil))
        #expect(why(StreamBroke(answered: false, underlying: CancellationError())) == .unreachable)
        #expect(why(StreamBroke(answered: true, underlying: CancellationError())) == .streamEnded)
        let failed = Failure(.send, TurnFailed(text: "429", cause: "provider_rate_limited", retryAfter: 20), server: origin)
        #expect(failed.why == .run(status: "error", cause: "provider_rate_limited") && failed.retryAfter == 20 && failed.said == "429")
        // The envelope's reason rides the refusal of an agent stream.
        let body = Data(#"{"error":{"code":"guard","reason":"spend_cap","message":"the cap is reached"}}"#.utf8)
        let refusal = ChatRefusal.reading(ServerError(status: 403, envelope: body)!, body: body, retryAfter: " 30 ")
        let spend = Failure(.send, refusal, server: origin)
        #expect(spend.why == .http(status: 403, code: "guard", reason: "spend_cap") && spend.retryAfter == 30 && spend.said == "the cap is reached")
        #expect(spend.refusal() == 403 && !spend.neverSent)
        #expect(Failure(.send, .offline).neverSent && Failure(.send, .unreachable).neverSent && !Failure(.send, .timeout).neverSent)
    }

    @Test func aStreamCarriesTheCauseWhereTheServerSendsOne() throws {
        let event = try #require(AgentEvent.parse(#"{"kind":"error","error":"429","cause":"provider_rate_limited","retryAfter":20}"#))
        #expect(event.cause == "provider_rate_limited" && event.retryAfter == 20 && event.failure == "429")
        let older = try #require(AgentEvent.parse(#"{"kind":"error","error":"429"}"#))
        #expect(older.cause == nil && older.retryAfter == nil)
        let done = try #require(
            AgentEvent.parse(#"{"kind":"done","result":{"thread":"t1","status":"error","reason":"x","cause":"provider_billing","retryAfter":3}}"#))
        #expect(done.result?.cause == "provider_billing" && done.result?.retryAfter == 3)
    }
}

@Suite("Where a failure is drawn", .timeLimit(.minutes(10)))
struct FailurePlaces {
    @Test func everyAttemptHasOnePlace() {
        let places: [Failure.Attempt: Failure.Place] = [
            .send: .chat, .resume: .turn, .reply: .turn, .stop: .chat, .attach: .chat, .reload: .chat, .rows: .chat, .poll: .nowhere,
            .rename: .chat, .card: .inline, .answer: .inline, .decide: .inline, .addKey: .inline, .refresh: .list, .feed: .list,
            .guidance: .inline, .guidanceChange: .inline,
        ]
        #expect(Set(places.keys) == Set(Failure.Attempt.allCases))
        for attempt in Failure.Attempt.allCases { #expect(Failure(attempt, .offline).place == places[attempt], "\(attempt)") }
    }

    @Test func aFailedStopSaysTheStopFailedAndNeverThatTheReplyDid() async throws {
        let refusal = #"{"error":{"code":"internal","message":"the stop could not be written"}}"#
        let rig = ChatRig(
            baseRoutes() + [
                .init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 2)),
                .init(method: "POST", path: "/stop", answer: .json(500, refusal)),
            ])
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.thread == "t1" }
        try await rig.send(.stop, to: key)
        try await rig.harness.until { _ in await rig.strip(key) != nil }
        let strip = try #require(await rig.strip(key))
        #expect(strip.sentence == "Couldn't stop Helper. The reply is still being written.")
        #expect(strip.note == "The server ran into a problem.")
        #expect(strip.button == FailureButton(title: "Try Again", action: .stop))
        #expect(strip.details == FailureDetails(title: "Details", label: "The server said", text: "the stop could not be written"))
        // The reply is still being written, and nothing says it failed.
        let screen = try #require(await rig.conversation(key))
        #expect(screen.streaming && screen.failures.turns.isEmpty)
        #expect(await rig.live(key)?.turnFailure == nil)

        // Try Again is the stop again, and shows that it is working.
        rig.server.hold("/stop")
        try await rig.send(.stop, to: key)
        try await rig.harness.until { _ in await rig.strip(key)?.button?.busy == true }
        #expect(await rig.strip(key)?.button?.enabled(at: Date()) == false)
        try await rig.send(.stop, to: key)
        await rig.harness.settle()
        #expect(rig.server.count("/stop") == 2)
        rig.server.letGo("/stop")

        // Once the turn ends, the stop is nothing to try again.
        rig.server.release()
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        #expect(await rig.strip(key) == nil)
    }

    @Test func aReloadThatFailsIsTheStripAboveTheComposer() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream))])
        try await rig.signIn()
        let key = try await rig.newChat()
        rig.server.route("GET", "/llm/thread/t1", .json(502, "<html>bad gateway</html>"))
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.reloadFailed != nil }
        let strip = try #require(await rig.strip(key))
        #expect(strip.sentence == "Couldn't load the end of this chat.")
        #expect(strip.note == "The server answered with something Substrate Chat can't read.")
        #expect(strip.button == FailureButton(title: "Try Again", action: .reload))
        #expect(strip.details?.label == "This device reported")
        #expect(await rig.conversation(key)?.failures.turns.isEmpty == true)
        // The run stays shown, and Try Again lands it.
        #expect(await rig.turns(key) == ["Hi", "Hello"])
        rig.server.unroute()
        try await rig.send(.retryReload, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        #expect(await rig.strip(key) == nil)
        #expect(await rig.turns(key) == ["Hi", "Hello"])
    }

    @Test func aRefreshThatFailsIsTheStripAtTheTopOfTheListWithTryAgain() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        #expect(await rig.harness.presentation.list.failure == nil)
        rig.server.route("GET", "/api/v1/records", .json(500, #"{"error":{"code":"internal","message":"the list could not be read"}}"#))
        await rig.harness.send(.refresh)
        try await rig.harness.until { harness in await harness.presentation.list.failure != nil }
        let strip = try #require(await rig.harness.presentation.list.failure)
        #expect(strip.sentence == "Couldn't refresh your chats." && strip.note == "The server ran into a problem.")
        #expect(strip.button == FailureButton(title: "Try Again", action: .refresh))
        #expect(strip.details?.text == "the list could not be read")
        rig.server.unroute()
        await rig.harness.send(.refresh)
        try await rig.harness.until { harness in await harness.presentation.list.failure == nil }
    }

    @Test func theChangeFeedEndingIsDrawnAtTheTopOfTheList() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        await rig.harness.deliver(ChatApp.Event(.watchEnded("the feed is closed to this token", nil)))
        let strip = try #require(await rig.harness.presentation.list.failure)
        #expect(strip.sentence == "Live updates stopped." && strip.note == "The server ended it.")
        #expect(strip.button == FailureButton(title: "Try Again", action: .refresh))
        #expect(strip.details == FailureDetails(title: "Details", label: "The server said", text: "the feed is closed to this token"))
        // A feed that only dropped is a state, never a failure.
        await rig.harness.deliver(ChatApp.Event(.watchOpened(fresh: false)))
        #expect(await rig.harness.presentation.list.failure == nil)
        await rig.harness.deliver(ChatApp.Event(.watchInterrupted(true)))
        #expect(await rig.harness.presentation.list.failure == nil)
        #expect(await rig.harness.presentation.list.reconnecting)
        // A sign-in that ended is the link's to say, not the list's.
        await rig.harness.deliver(ChatApp.Event(.watchEnded(nil, Failure(.feed, .sessionEnded))))
        #expect(await rig.harness.presentation.list.failure == nil)
    }

    @Test func aRenameThatFailsIsSaidInTheChatItIsAboutOrElseInTheList() async throws {
        let threads = #"{"head":10,"generation":"g","records":[\#(threadJSON(version: 3))]}"#
        let rig = ChatRig(
            baseRoutes(threads: threads) + [.init(method: "PATCH", path: "/llm/thread/t1", answer: .fail(.timedOut))])
        try await rig.signIn()
        await rig.harness.send(.rename(thread: "t1", subject: "Plants"))
        try await rig.harness.until { harness in await harness.state.renameFailure != nil }
        // No window shows the chat: the list says it.
        let listed = try #require(await rig.harness.presentation.list.failure)
        #expect(listed.sentence == "Couldn't rename this chat." && listed.note == "The server took too long to answer.")
        #expect(listed.button == FailureButton(title: "Try Again", action: .rename(thread: "t1", subject: "Plants")))
        #expect(listed.dismiss == .dismissRename)

        // Shown in a window, it is the strip above that chat's composer, once.
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.strip(key) != nil }
        #expect(await rig.strip(key)?.sentence == "Couldn't rename this chat.")
        #expect(await rig.harness.presentation.list.failure == nil)
        await rig.harness.send(.dismissRename)
        #expect(await rig.strip(key) == nil)
    }

    @Test func aTurnsFailureIsACardAtItsTurnAlsoWhenItIsAnOlderTurn() throws {
        func row(_ id: String, _ role: String, _ content: String, turn: Int, cutBy: String? = nil) -> Record {
            var properties: [String: JSONValue] = ["role": .string(role), "content": .string(content), "turn": .number(JSONNumber(turn))]
            if let cutBy { properties["cutBy"] = .string(cutBy) }
            return Record(id: id, kind: Kinds.message, properties: properties)
        }
        let rows = [
            row("m1", "user", "First", turn: 0), row("m2", "user", "Again", turn: 1),
            row("m3", "assistant", "Half an ans", turn: 2, cutBy: "length"), row("m4", "user", "Go on", turn: 3),
            row("m5", "assistant", "Done", turn: 4),
        ]
        let stored = transcript(of: rows)
        #expect(stored.map(\.cutBy) == [nil, nil, "length", nil, nil])
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        let turns = TurnPresenter.present(stored, agent: nil, liveKey: nil, titles: [:])
        let live = ConversationLive(
            thread: "t1", agent: helper, name: "Helper", draft: "", draftRevision: 0, turns: [], incomplete: false, liveKey: nil,
            awaitingFirstEvent: false, phase: .idle, stopping: false, failures: ConversationFailures(), deleted: false, listenOnOpen: false,
            blocked: nil, keyless: nil, online: true)
        let failures = app.failures(ChatAppState(), live: live, row: nil, stored: stored, turns: turns)
        // The unanswered message and the reply cut short, each at its turn,
        // with no button: only the newest turn can be tried again.
        #expect(Set(failures.turns.keys) == ["m1", "m3"])
        #expect(failures.turns["m1"]?.sentence == "Helper didn't answer this message." && failures.turns["m1"]?.button == nil)
        #expect(failures.turns["m3"]?.sentence == "The reply stopped at the model's limit." && failures.turns["m3"]?.button == nil)
        #expect(failures.strip == nil)

        // The newest turn cut at the model's limit offers Continue.
        let cut = transcript(of: [row("m1", "user", "First", turn: 0), row("m2", "assistant", "Half", turn: 1, cutBy: "length")])
        let last = app.failures(
            ChatAppState(), live: live, row: nil, stored: cut, turns: TurnPresenter.present(cut, agent: nil, liveKey: nil, titles: [:]))
        #expect(last.turns["m2"]?.button == FailureButton(title: "Continue", action: .resume))
    }
}

@Suite("Try Again and Continue", .timeLimit(.minutes(10)))
struct FailureRetries {
    private func failedRoutes(_ reason: String = "llm: overloaded") -> [ChatServer.Route] {
        let failed = threadJSON(version: 3, status: "error")
            .replacingOccurrences(of: #""lastReply":"Hello""#, with: #""reason":"\#(reason)""#)
        let threads = #"{"head":10,"generation":"g","records":[\#(failed)]}"#
        return [.init(method: "GET", path: "/llm/thread/t1", answer: .json(200, failed))] + baseRoutes(threads: threads, status: "error")
    }

    private func open(_ rig: ChatRig) async throws -> ChildKey {
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        return key
    }

    @Test func tryAgainRunsTheThreadAgainAndWritesNoMessage() async throws {
        let rig = ChatRig(failedRoutes() + [.init(method: "POST", path: "/continue", answer: .stream(replyStream, holdAfter: 0))])
        let key = try await open(rig)
        #expect(await rig.card(key)?.button == FailureButton(title: "Try Again", action: .resume))
        try await rig.send(.continueTurn, to: key)
        // The button shows that it is working and cannot be pressed twice.
        try await rig.harness.until { _ in await rig.card(key)?.button?.busy == true }
        #expect(await rig.card(key)?.button?.enabled(at: Date()) == false)
        try await rig.send(.continueTurn, to: key)
        await rig.harness.settle()
        #expect(rig.server.count("/continue") == 1 && rig.server.count("/chat") == 0)
        #expect(rig.server.body("POST", "/continue") == "{}")
        // The server answers: the card goes, and the reply streams.
        rig.server.route("GET", "/llm/thread/t1", .json(200, threadJSON(version: 4)))
        rig.server.release()
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        #expect(await rig.card(key) == nil)
    }

    @Test func continueFallsBackOnAServerWithoutTheRouteAndRemembersIt() async throws {
        // What a server older than the route answers, word for word.
        let missing = #"{"error":{"code":"not_found","message":"no such API path: /api/v1/substrate.reamde.dev/llm/thread/t1/continue"}}"#
        let rig = ChatRig(
            failedRoutes() + [
                .init(method: "POST", path: "/continue", answer: .json(404, missing)),
                .init(method: "POST", path: "/chat", answer: .stream(replyStream)),
            ])
        let key = try await open(rig)
        try await rig.send(.draft("half a thought"), to: key)
        try await rig.send(.continueTurn, to: key)
        try await rig.harness.until { _ in rig.server.count("/chat") == 1 }
        // What Continue did before the route: the words as the person's own
        // message, and the half-typed one left alone.
        let body = try #require(JSONValue.parse(rig.server.body("POST", "/chat")))
        #expect(body["thread"]?.string == "t1" && body["message"]?.string == "Continue where you left off.")
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        #expect(await rig.live(key)?.draft == "half a thought")
        #expect(await rig.harness.state.continueUnserved)

        // The answer is remembered for the session: the next Continue does
        // not ask the route again.
        try await rig.send(.continueTurn, to: key)
        try await rig.harness.until { _ in rig.server.count("/chat") == 2 }
        #expect(rig.server.count("/continue") == 1)
        // A new session asks again.
        await rig.signOut()
        #expect(await rig.harness.state.continueUnserved == false)
    }

    @Test func aTryAgainThatFailsReplacesTheFailureInTheSamePlace() async throws {
        let rig = ChatRig(failedRoutes() + [.init(method: "POST", path: "/continue", answer: .fail(.notConnectedToInternet))])
        let key = try await open(rig)
        #expect(await rig.card(key)?.sentence == "Helper couldn't finish this reply.")
        try await rig.send(.continueTurn, to: key)
        try await rig.harness.until { _ in await rig.card(key)?.sentence == "Couldn't ask Helper to carry on." }
        let card = try #require(await rig.card(key))
        #expect(card.note == "You're offline." && card.button == FailureButton(title: "Try Again", action: .resume))
        let screen = try #require(await rig.conversation(key))
        #expect(screen.failures.turns.count == 1 && screen.failures.strip == nil)
        // Nothing was written and nothing is sent on its own.
        #expect(rig.server.count("/chat") == 0 && rig.server.count("/continue") == 1)
    }

    @Test func aBusyThreadIsAttachedToRatherThanCalledAFailure() async throws {
        let busy = #"{"error":{"code":"conflict","reason":"thread_busy","message":"a turn is already running on this thread"}}"#
        let rig = ChatRig(
            failedRoutes() + [
                .init(method: "POST", path: "/continue", answer: .json(409, busy)),
                .init(method: "GET", path: "/events", answer: .stream([#"{"kind":"idle","status":"running"}"#])),
            ])
        let key = try await open(rig)
        try await rig.send(.continueTurn, to: key)
        try await rig.harness.until { _ in rig.server.count("/events") == 1 }
        #expect(await rig.live(key)?.turnFailure == nil)
    }
}

@Suite("A rate limit counts down", .timeLimit(.minutes(10)))
struct FailureCountdown {
    @Test func theButtonCountsDownToTheLimitsEnd() {
        let at = Date(timeIntervalSince1970: 1_793_696_400)
        let card = FailurePresenter.present(run("provider_rate_limited", retryAfter: 20, at: at), context())
        #expect(card.sentence == "Anthropic is limiting requests. Try again in 20 seconds.")
        let button = try! #require(card.button)
        #expect(button.enablesAt == at.addingTimeInterval(20))
        #expect(button.title(at: at) == "Try Again in 20 s" && !button.enabled(at: at))
        #expect(button.title(at: at.addingTimeInterval(19.2)) == "Try Again in 1 s")
        #expect(button.title(at: at.addingTimeInterval(20)) == "Try Again" && button.enabled(at: at.addingTimeInterval(20)))
        // A long wait is said in minutes.
        let ninety = FailurePresenter.present(run("provider_rate_limited", retryAfter: 90, at: at), context())
        #expect(ninety.sentence == "Anthropic is limiting requests. Try again in 90 seconds." && ninety.button?.title(at: at) == "Try Again in 90 s")
        let long = FailurePresenter.present(run("provider_rate_limited", retryAfter: 300, at: at), context())
        #expect(long.sentence == "Anthropic is limiting requests. Try again in 5 minutes.")
        #expect(long.button?.title(at: at) == "Try Again in 5 min")
        // Without one, it is Try Again at once.
        let plain = FailurePresenter.present(run("provider_rate_limited"), context())
        #expect(plain.sentence == "Anthropic is limiting requests." && plain.button?.enabled(at: at) == true)
    }

    private func limited() -> ChatRig {
        let failed = threadJSON(version: 3, status: "error")
        let limit = [
            #"{"kind":"thread","thread":"t1"}"#,
            #"{"kind":"error","error":"status code: 429","cause":"provider_rate_limited","retryAfter":20}"#,
        ]
        return ChatRig(
            [.init(method: "GET", path: "/llm/thread/t1", answer: .json(200, failed))] + baseRoutes(status: "error") + [
                .init(method: "POST", path: "/chat", answer: .stream(limit)),
                .init(method: "GET", path: "/events", answer: .stream([#"{"kind":"idle","status":"error"}"#])),
                .init(method: "POST", path: "/continue", answer: .stream(limit)),
            ])
    }

    @Test func itTriesAgainOnceAtZeroWhileTheChatIsOnScreen() async throws {
        let rig = limited()
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.card(key) != nil }
        let card = try #require(await rig.card(key))
        #expect(card.sentence == "The provider is limiting requests. Try again in 20 seconds.")
        let sent = try #require(await rig.live(key)?.turnFailure?.at)
        #expect(card.button?.enablesAt == sent.addingTimeInterval(20))
        #expect(rig.server.count("/continue") == 0)

        // At zero it tries again by itself: once.
        try await rig.harness.until(advancing: 25) { _ in rig.server.count("/continue") == 1 }
        try await rig.harness.until { _ in await rig.live(key)?.phase == .idle && rig.server.count("/events") == 2 }
        #expect(await rig.live(key)?.autoRetried == true)
        // The limit it met again counts down too, and is the person's to
        // press: nothing loops.
        #expect(await rig.card(key)?.button?.enablesAt != nil)
        try await rig.harness.untilIdle(since: rig.harness.effectsStarted, advancing: 60)
        #expect(rig.server.count("/continue") == 1 && rig.server.count("/chat") == 1)
    }

    @Test func whatItTriesOnItsOwnNeverWritesAMessageInThePersonsName() async throws {
        // A server without the continue route: only a press sends the words.
        let missing = #"{"error":{"code":"not_found","message":"no such API path: /api/v1/substrate.reamde.dev/llm/thread/t1/continue"}}"#
        let rig = limited()
        rig.server.route("POST", "/continue", .json(404, missing))
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.card(key) != nil }
        try await rig.harness.until(advancing: 25) { _ in rig.server.count("/continue") == 1 }
        try await rig.harness.until { harness in await harness.state.continueUnserved }
        try await rig.harness.untilIdle(since: rig.harness.effectsStarted, advancing: 60)
        #expect(rig.server.count("/chat") == 1)
        #expect(await rig.card(key)?.button?.title == "Try Again")
        // The press does: what Continue did before the route.
        try await rig.send(.continueTurn, to: key)
        try await rig.harness.until { _ in rig.server.count("/chat") == 2 }
        #expect(rig.server.count("/continue") == 1)
    }

    @Test func itDoesNotTryAgainWhileNobodyLooksAtTheChat() async throws {
        let rig = limited()
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.card(key) != nil }
        await rig.harness.send(.sceneActive(scene: "s", active: false))
        try await rig.harness.untilIdle(since: rig.harness.effectsStarted, advancing: 60)
        #expect(rig.server.count("/continue") == 0)
        // The button is there when they come back.
        await rig.harness.send(.sceneActive(scene: "s", active: true))
        #expect(await rig.card(key)?.button?.title == "Try Again")
        #expect(rig.server.count("/continue") == 0)
    }
}

@Suite("Nothing typed is lost", .timeLimit(.minutes(10)))
struct FailureKeepsWhatWasTyped {
    @Test func aSendThatNeverLeftGivesTheTextBackAndIsSentAgainWithoutACheck() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .fail(.notConnectedToInternet))])
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Water the basil"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.strip(key) != nil }
        let strip = try #require(await rig.strip(key))
        #expect(strip.sentence == "You're offline." && strip.note == "Your message is in the box below.")
        #expect(strip.button == FailureButton(title: "Send Again", action: .send))
        #expect(strip.dismiss == .dismiss)
        #expect(await rig.conversation(key)?.composer.draft == "Water the basil")
        // The request never left: nothing is read for, and nothing waits.
        #expect(await rig.live(key)?.checking == false)
        // The list keeps the draft too, so it outlives the window.
        #expect(await rig.harness.state.drafts.values.contains("Water the basil"))

        // Send Again is the composer's Send, and it goes once the server answers.
        rig.server.route("POST", "/chat", .stream(replyStream))
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        #expect(await rig.strip(key) == nil)
        #expect(await rig.conversation(key)?.composer.draft == "")
        #expect(rig.server.count("/chat") == 2)
    }

    @Test func whatWasTypedSinceStaysBehindTheTextGivenBack() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .json(502, "<html>bad gateway</html>"))])
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Water the basil"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.checking == true }
        // While it is read for, the strip says so and asks nothing.
        let checking = try #require(await rig.strip(key))
        #expect(checking.working && checking.button == nil && checking.sentence == "Checking whether your message arrived…")
        try await rig.send(.draft("and the mint"), to: key)
        try await rig.harness.until(advancing: 2) { _ in await rig.live(key)?.chatFailure?.unconfirmed == true }
        // Both are in the composer: the message first, then what was typed.
        #expect(await rig.conversation(key)?.composer.draft == "Water the basil\n\nand the mint")
        let strip = try #require(await rig.strip(key))
        #expect(strip.sentence == "Your message may not have arrived.")
        #expect(strip.note == "It's in the box below. Look for it in this chat before sending it again.")
        #expect(strip.button?.title == "Send Again")
        // Reading it takes the strip away and leaves the text.
        try await rig.send(.dismissFailure, to: key)
        #expect(await rig.strip(key) == nil)
        #expect(await rig.conversation(key)?.composer.draft == "Water the basil\n\nand the mint")
    }

    @Test func aFailedReplysNotificationSaysWhatItsCardSays() {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        let record = Record(
            id: "t1", kind: Kinds.thread,
            properties: [
                "status": "error", "reason": "substrate: validation failed: agent x: llm: error, status code: 429",
                "agent": ["ref": .string("\(Kinds.agent)/\(helper)")],
            ])
        let thread = ChatThread(record: record)
        let words = app.notificationWords(ChatAppState(), thread: thread)
        #expect(words == "The provider is limiting requests.")
        let request = ChatNotifications.reply(thread: thread, agent: "Helper", title: "Plants", failed: words)
        #expect(request.body == "The provider is limiting requests." && request.body?.contains("substrate:") != true)
        #expect(app.notificationWords(ChatAppState(), thread: ChatThread(record: Record(id: "t2", kind: Kinds.thread, properties: ["status": "ok"]))) == nil)
    }
}

@Suite("A failure's colours")
struct FailureColours {
    @Test func everyPairThatCarriesTextMeetsFourAndAHalfToOne() {
        for tone in [FailurePresentation.Tone.bad, .warn, .quiet] {
            for dark in [false, true] {
                for pair in FailurePalette.of(tone, dark: dark).textPairs {
                    let ratio = pair.text.contrast(with: pair.on)
                    #expect(ratio >= 4.5, "\(pair.name) of \(tone) in \(dark ? "dark" : "light") is \(ratio):1")
                }
            }
        }
        // The measure itself: black on white is 21:1.
        #expect(abs(FailurePalette.Shade(0x000000).contrast(with: FailurePalette.Shade(0xFFFFFF)) - 21) < 0.01)
    }
}
