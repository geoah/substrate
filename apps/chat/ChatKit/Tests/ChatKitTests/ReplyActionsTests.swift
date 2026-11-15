import Foundation
import SubstrateCore
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

/// What stands under the last reply of a chat, and that it and the turn's
/// card never offer the same thing: one place says whether a run can be
/// tried again, and each of the two draws its part of the answer.
@Suite("What stands under a reply", .timeLimit(.minutes(10)))
struct ReplyActionsTests {
    private static func context(_ change: (inout FailurePresenter.Context) -> Void = { _ in }) -> FailurePresenter.Context {
        var context = FailurePresenter.Context()
        context.agent = "Moss"
        context.agentID = "substrate.reamde.dev/assistant/moss"
        context.provider = "Anthropic"
        context.providerID = "anthropic"
        context.providerPage = "https://s.example.com/data/substrate.reamde.dev/llm/provider/anthropic"
        context.agentPage = "https://s.example.com/agents/substrate.reamde.dev%2Fassistant%2Fmoss"
        change(&context)
        return context
    }

    private static func run(_ cause: String?, status: String = "error") -> Failure {
        Failure(.reply, .run(status: status, cause: cause))
    }

    private static func turns(_ made: [Turn], live: String? = nil) -> [TurnPresentation] {
        TurnPresenter.present(made, agent: nil, liveKey: live, titles: [:])
    }

    private static let asked = Turn(id: "m1", role: .user, content: "How are the plants?")
    private static let answered = Turn(id: "m2", role: .assistant, content: "They want water.")

    // MARK: The one place that says whether a run goes on

    /// For each cause: whether the card's button runs the thread again, or
    /// a Try Again stands beside another button, or neither does.
    enum Offer: Equatable { case card, beside, none }

    static let offers: [RunCause: Offer] = [
        .providerKeyRefused: .beside,
        .providerRateLimited: .card,
        .providerBilling: .card,
        .providerOverloaded: .card,
        .providerUnavailable: .card,
        // Asking again asks the same thing: a new chat is the way on.
        .providerRejected: .none,
        .modelUnavailable: .beside,
        .contextTooLong: .none,
        .allowanceSpent: .beside,
        .interrupted: .card,
        .writeFailed: .card,
        .internal: .card,
        .turnLimit: .card,
        .toolCallLimit: .card,
        .deadline: .card,
        .requested: .card,
        .shutdown: .card,
    ]

    @Test func everyCauseOffersTheRunAgainInOnePlaceOrInNone() {
        #expect(Set(Self.offers.keys) == Set(RunCause.allCases))
        for cause in RunCause.allCases {
            let card = FailurePresenter.present(Self.run(cause.rawValue), Self.context())
            let offer: Offer = card.button?.action == .resume ? .card : card.resume != nil ? .beside : .none
            #expect(offer == Self.offers[cause], "\(cause)")
            // Never both, whatever the cause.
            #expect(!(card.button?.action == .resume && card.resume != nil), "\(cause)")
            if let beside = card.resume { #expect(beside.action == .resume && beside.title == "Try Again", "\(cause)") }
        }
    }

    @Test func whereTheCardHasNoPageToOpenItsButtonIsTheRunAgainAndNothingStandsBesideIt() {
        // No session's console to open: the card falls back to Try Again,
        // and that is then the one place it is offered.
        let bare = Self.context {
            $0.providerPage = nil
            $0.agentPage = nil
        }
        for cause in [RunCause.providerKeyRefused, .modelUnavailable, .allowanceSpent] {
            let card = FailurePresenter.present(Self.run(cause.rawValue), bare)
            #expect(card.button?.action == .resume && card.resume == nil, "\(cause)")
        }
    }

    @Test func aStatusACutAndARefusalEachOfferItOnce() {
        for status in ["error", "overbudget", "stopped"] {
            let card = FailurePresenter.present(Self.run("a_new_cause", status: status), Self.context())
            #expect(card.button?.action == .resume && card.resume == nil, "\(status)")
        }
        for cut in CutBy.allCases {
            let card = FailurePresenter.present(Failure(.reply, .cut(cut.rawValue)), Self.context())
            #expect(card.button?.action == .resume && card.resume == nil, "\(cut)")
        }
        // A Try Again the server refused for the day's spending, or for a
        // key that is not there: its card leads to where that is put
        // right, and the run again stands beside it.
        for reason in [RefusalReason.spendCap, .providerKeyMissing] {
            let card = FailurePresenter.present(Failure(.resume, .http(status: 403, code: "guard", reason: reason.rawValue)), Self.context())
            #expect(card.button?.action != .resume && card.resume?.action == .resume, "\(reason)")
        }
        // Only a run is run again: a send that was refused is sent again,
        // from the composer, and nothing stands under a reply for it.
        for reason in RefusalReason.allCases {
            let strip = FailurePresenter.present(Failure(.send, .http(status: 409, code: "conflict", reason: reason.rawValue)), Self.context())
            #expect(strip.resume == nil, "\(reason)")
        }
        #expect(FailurePresenter.present(Failure(.message, .timeout), Self.context()).resume == nil)
    }

    @Test func anOlderTurnsCardOffersNothingAndWhatIsBesideItFollowsItsBusyAndItsWait() {
        let older = Self.context { $0.acts = false }
        let card = FailurePresenter.present(Self.run(RunCause.providerKeyRefused.rawValue), older)
        #expect(card.button == nil && card.resume == nil)
        let busy = FailurePresenter.present(Self.run(RunCause.providerKeyRefused.rawValue), Self.context { $0.busy = true })
        #expect(busy.resume?.busy == true && busy.resume?.enabled(at: Date()) == false)
        let blocked = FailurePresenter.present(Self.run(RunCause.modelUnavailable.rawValue), Self.context { $0.blocked = "Offline" })
        #expect(blocked.resume?.blocked == "Offline")
    }

    // MARK: Where it stands

    @Test func aSettledReplyOffersCopyAndShareAndNothingToRunAgain() throws {
        let actions = try #require(ReplyActions(under: Self.turns([Self.asked, Self.answered]), failures: ScreenFailures()))
        #expect(actions == ReplyActions(turn: "m2", copies: true, whole: true))
        #expect(actions.copyLabel == "Copy" && actions.copiedLabel == "Copied" && actions.shareLabel == "Share" && actions.selectLabel == "Select Text")
    }

    @Test func aReplyBeingWrittenOffersCopyAndNotShare() throws {
        var writing = Self.answered
        writing.id = "turn-1"
        let actions = try #require(ReplyActions(under: Self.turns([Self.asked, writing], live: "turn-1"), failures: ScreenFailures()))
        #expect(actions == ReplyActions(turn: "turn-1", copies: true, whole: false))
    }

    @Test func nothingStandsUnderAChatWithNoReplyOrWithAMessageAfterIt() {
        #expect(ReplyActions(under: [], failures: ScreenFailures()) == nil)
        #expect(ReplyActions(under: Self.turns([Self.asked]), failures: ScreenFailures()) == nil)
        // A message sent since: the reply before it is an older one.
        let again = Turn(id: "m3", role: .user, content: "And the basil?")
        #expect(ReplyActions(under: Self.turns([Self.asked, Self.answered, again]), failures: ScreenFailures()) == nil)
        // A reply that has said nothing yet has nothing to copy.
        let thinking = Turn(id: "turn-1", role: .assistant, content: "", reasoning: "Let me think")
        #expect(ReplyActions(under: Self.turns([Self.asked, thinking], live: "turn-1"), failures: ScreenFailures()) == nil)
    }

    @Test func aCallThatOnlyRanToolsLeavesTheWordsBeforeItTheLastSaid() throws {
        // The row does not go and come back while a run's second call
        // thinks and looks things up: it moves when that call has words.
        let looked = Turn(id: "turn-3", role: .assistant, content: "", tools: [ToolCall(id: "c1", name: "query", callable: nil, arguments: "{}")])
        var turns = Self.turns([Self.asked, Self.answered, looked], live: "turn-3")
        #expect(ReplyActions(under: turns, failures: ScreenFailures())?.turn == "m2")
        let said = Turn(id: "turn-3", role: .assistant, content: "Found it.", tools: looked.tools)
        turns = Self.turns([Self.asked, Self.answered, said], live: "turn-3")
        #expect(ReplyActions(under: turns, failures: ScreenFailures()) == ReplyActions(turn: "turn-3", copies: true, whole: false))
        // What the person did with a card is no message of theirs.
        let decided = Turn(id: "m4", role: .system, content: "note")
        #expect(ReplyActions(under: Self.turns([Self.asked, Self.answered, decided]), failures: ScreenFailures())?.turn == "m2")
    }

    @Test func tryAgainStandsUnderTheReplyOnlyWhereTheCardNamesItBesideItsButton() throws {
        let turns = Self.turns([Self.asked, Self.answered])
        func failures(_ cause: RunCause) -> ScreenFailures {
            ScreenFailures(turns: ["m2": FailurePresenter.present(Self.run(cause.rawValue), Self.context())])
        }
        // The card's own button runs it again: nothing more under the reply.
        var actions = try #require(ReplyActions(under: turns, failures: failures(.providerOverloaded)))
        #expect(actions.resume == nil && failures(.providerOverloaded).turns["m2"]?.button?.title == "Try Again")
        // The card leads to the key: Try Again is under the reply.
        actions = try #require(ReplyActions(under: turns, failures: failures(.providerKeyRefused)))
        #expect(actions.resume?.title == "Try Again" && failures(.providerKeyRefused).turns["m2"]?.button?.title == "Replace Key")
        // A new chat is the way on: nowhere.
        actions = try #require(ReplyActions(under: turns, failures: failures(.contextTooLong)))
        #expect(actions.resume == nil)
        // Every cause, at once: the two never offer the same.
        for cause in RunCause.allCases {
            let made = failures(cause)
            let under = ReplyActions(under: turns, failures: made)
            #expect(!(under?.resume != nil && made.turns["m2"]?.button?.action == .resume), "\(cause)")
        }
    }

    @Test func aRunThatFailedBeforeItSaidAnythingHasItsTryAgainUnderItsLastTurnAlone() throws {
        let looked = Turn(id: "m2", role: .assistant, content: "", tools: [ToolCall(id: "c1", name: "query", callable: nil, arguments: "{}")])
        let turns = Self.turns([Self.asked, looked])
        let failures = ScreenFailures(turns: ["m2": FailurePresenter.present(Self.run(RunCause.providerKeyRefused.rawValue), Self.context())])
        #expect(ReplyActions(under: turns, failures: failures) == ReplyActions(turn: "m2", copies: false, whole: false, resume: failures.turns["m2"]?.resume))
        // With nothing to run again there is nothing to stand there.
        #expect(ReplyActions(under: turns, failures: ScreenFailures()) == nil)
        // A card at a message of the person's has no reply to stand under.
        let unanswered = ScreenFailures(turns: ["m1": FailurePresenter.present(Self.run(RunCause.providerKeyRefused.rawValue), Self.context())])
        #expect(ReplyActions(under: Self.turns([Self.asked]), failures: unanswered) == nil)
    }

    // MARK: On the runtime

    @Test func theScreenOffersWhatItsTurnsAndItsCardsSay() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream))])
        try await rig.signIn()
        let key = try await rig.newChat()
        #expect(await rig.conversation(key)?.replyActions == nil)
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until(advancing: 1) { _ in await rig.live(key)?.phase == .idle }
        await rig.harness.settle()
        let screen = try #require(await rig.conversation(key))
        let actions = try #require(screen.replyActions)
        // Under the reply as it is drawn, whole, with nothing to run again.
        #expect(screen.turns.last?.id == actions.turn && actions.copies && actions.whole && actions.resume == nil)
        #expect(screen.failures.turns.isEmpty)
    }

    @Test func theChoiceToWrapCodeIsThePersonsAndIsKeptOnTheDevice() async throws {
        let rig = ChatRig(baseRoutes())
        try await rig.signIn()
        let key = try await rig.newChat()
        #expect(await rig.conversation(key)?.wrapsCode == false)
        await rig.harness.send(.settings(.toggle(row: .app("wrapCode"), on: true)))
        await rig.harness.settle()
        #expect(await rig.conversation(key)?.wrapsCode == true)
        // In the device's own store, beside the person's other choices.
        let defaults = try #require(UserDefaults(suiteName: "wrap-\(UUID().uuidString)"))
        let store = DefaultsPreferenceStore(defaults: defaults)
        var preferences = store.load("a")
        #expect(!preferences.wrapCode)
        preferences.wrapCode = true
        store.save(preferences, "a")
        #expect(store.load("a").wrapCode && !store.load("b").wrapCode)
    }
}
