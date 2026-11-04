import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateRuntime

/// One chat, a child of the chat list (`ChatApp`), kept under the key the
/// list minted when it made it, never a thread's: a new chat has no thread
/// until its `thread` event names one, which the list then binds. It owns
/// the run: a send, an attach, the handover, Stop, the delivery check and
/// the background poll, each an effect under a key of its own, so starting
/// one cancels the one before and drops its late results (the run epochs of
/// DESIGN.md's rule 5); closing it moves its controller generation, which
/// drops the rest. The rows are the list's: what a run reads goes up to it,
/// and it hands this conversation what it must know of them back as part of
/// its value.
public struct Conversation: Controller, Equatable {
    public enum Command: Codable, Equatable, Sendable {
        /// A scene shows it: draw what is held, attach when the thread runs.
        case open
        case draft(String)
        case choose(agent: String)
        case send
        case stop
        case retryReload
        case dismiss(UUID)
        /// The composer started listening, as a shortcut asked.
        case listened
    }

    /// An event, opaque outside ChatKit.
    public struct Event: Codable, Equatable, Sendable {
        let step: Step
        init(_ step: Step) { self.step = step }
    }

    enum Step: Codable, Equatable, Sendable {
        case drafted(String)
        case chose(String)
        case listened
        case opened
        case sent(message: String, agent: String, at: Date, baseline: Int?)
        case named(String)
        case streamed(AgentEvent, at: Date)
        case streamEnded(Failure?, outcome: UUID)
        case attachRead(thread: Record?, rows: [Record])
        case attachReadFailed(Failure)
        case retryDue(attempt: Int)
        case handedOver(thread: Record?, rows: [Record])
        case handoverFailed(String, Failure?)
        case pollTick
        case polled(thread: Record?, rows: [Record])
        case pollFailed(Failure)
        case stopAsked
        case stopFailed(Failure)
        case stopWaited
        case checked(outcome: UUID, found: Found?, failure: Failure?)
        case dismissed(UUID)
        case retriedReload
        case threadMoved(ThreadFacts?)
        case nudgedPoll
        case resumed
    }

    /// A send of unknown delivery the check found: its thread, and what it
    /// read of it.
    struct Found: Codable, Equatable, Sendable {
        var thread: String
        var record: Record?
        var rows: [Record]
        var running: Bool
    }

    public typealias State = ConversationState

    /// An effect, opaque outside ChatKit.
    public struct Effect: Equatable, Sendable {
        let ask: State.Ask
    }

    // MARK: The value the list hands it

    let key: ChildKey
    /// What the list made it with; the state takes it from there.
    let seed: Seed
    /// The thread's row as the list holds it.
    let facts: ThreadFacts?
    let online: Bool
    /// The provider of its agent, by name, when it has no key.
    let keyless: String?
    /// The newest turn the list holds of the thread's rows; nil while they
    /// are unread.
    let newestTurn: Int?
    /// A scene in front shows it.
    let visible: Bool
    /// Moved by the list for each change of a message the watch reports
    /// while the thread runs in the background: read it now.
    let pollNudge: Int
    /// Moved when the app comes back to the front: a stream that died while
    /// it was away opens again.
    let resumeNudge: Int
    /// The continued processing the app asked for while this one streams.
    let continuing: String?

    struct Seed: Codable, Hashable, Sendable {
        var thread: String?
        var agent: String?
        var draft: String
        var listen: Bool
    }

    init(
        key: ChildKey, seed: Seed, facts: ThreadFacts?, online: Bool, keyless: String?, newestTurn: Int?, visible: Bool,
        pollNudge: Int, resumeNudge: Int, continuing: String?
    ) {
        self.key = key
        self.seed = seed
        self.facts = facts
        self.online = online
        self.keyless = keyless
        self.newestTurn = newestTurn
        self.visible = visible
        self.pollNudge = pollNudge
        self.resumeNudge = resumeNudge
        self.continuing = continuing
    }

    public var initialState: State {
        State(thread: seed.thread, agent: seed.agent, draft: seed.draft, listenOnOpen: seed.listen)
    }

    /// Who the chat is with: a bound chat's thread names it; a new chat
    /// holds the one chosen.
    func agent(_ state: State) -> String? { facts?.agent ?? state.agent }

    func name(_ state: State) -> String { agent(state).map(agentName) ?? "the assistant" }

    /// Why a send cannot go now; the text stays in the composer while it
    /// says so.
    func blockedReason(_ state: State) -> String? {
        if state.deleted { return "This chat was deleted" }
        if agent(state) == nil { return "Choose an assistant" }
        if state.checking { return "Checking whether your last message arrived" }
        switch state.phase {
        case .streaming, .handingOver: return "Waiting for the answer"
        case .background: return "\(name(state)) is working on this chat"
        case .idle: break
        }
        if !online { return "Offline" }
        if let keyless { return "Add an API key for \(keyless) first" }
        return nil
    }

    // MARK: Commands into events

    public func events(for command: Command, in context: Enrichment<Conversation>) -> [Event] {
        let state = context.state
        switch command {
        case .open: return [Event(.opened)]
        case let .draft(text): return text == state.draft ? [] : [Event(.drafted(text))]
        case let .choose(agent): return [Event(.chose(agent))]
        case .listened: return state.listenOnOpen ? [Event(.listened)] : []
        case .stop: return [Event(.stopAsked)]
        case .retryReload: return [Event(.retriedReload)]
        case let .dismiss(id): return [Event(.dismissed(id))]
        case .send:
            let message = state.draft.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !message.isEmpty, blockedReason(state) == nil, let agent = agent(state) else { return [] }
            // Where the transcript stood, for a delivery check: unknown while
            // its rows are unread, or stale because their re-read failed.
            let baseline = state.thread != nil && state.reloadFailed == nil ? newestTurn : nil
            return [Event(.sent(message: message, agent: agent, at: context.now, baseline: baseline))]
        }
    }

    /// Made while a scene shows it: as if it had just appeared there.
    public func event(atOpen replayed: Bool) -> Event? { visible ? Event(.opened) : nil }

    public func event(replacing previous: Conversation) -> Event? {
        if facts != previous.facts { return Event(.threadMoved(facts)) }
        if pollNudge != previous.pollNudge { return Event(.nudgedPoll) }
        if resumeNudge != previous.resumeNudge { return Event(.resumed) }
        if visible, !previous.visible { return Event(.opened) }
        return nil
    }

    /// Structure (a turn started, a tool card, a compaction) shows at once;
    /// text waits for the window, so a fast model does not redraw on every
    /// token.
    public func coalescing(of event: Event) -> Coalescing {
        if case let .streamed(streamed, _) = event.step, streamed.kind == .delta || streamed.kind == .reasoning {
            return .within(.milliseconds(50))
        }
        return .now
    }

    // MARK: Effects

    public func effects(of event: Event, on state: State, presented presentation: Presentation) -> [Effect] {
        state.asks.map(Effect.init)
    }

    public func key(of effect: Effect) -> String? {
        switch effect.ask {
        case .chat, .attach, .handover: "run"
        case .wait: "retry"
        case .pollEvery: "pollTimer"
        case .poll: "poll"
        case .stop: "stop"
        case .stopWait: "stopWait"
        case .check: "check"
        case .advance: nil
        case let .cancel(key): key
        }
    }
}
