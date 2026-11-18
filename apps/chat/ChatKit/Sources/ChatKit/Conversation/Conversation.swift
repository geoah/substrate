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
        /// What the composer's field holds, told after a pause in the
        /// typing, before a send and when the field stops editing
        /// (`DraftReports`): kept for the send and for the disk, and not
        /// carried by the tree, so typing draws nothing again.
        case draft(String)
        /// The field left the screen holding this text: the tree carries it
        /// from here, so the field that comes after starts from it whichever
        /// of the two the conversation hears of first.
        case draftLeft(String)
        /// The draft written for the person: an opener they picked. The
        /// field takes it.
        case fill(String)
        /// What dictation heard, landing after the draft. The field takes it.
        case heard(String)
        case choose(agent: String)
        case send
        /// Send Again on a message that stands unsent: the same request
        /// once more, under the id it was first sent with.
        case sendAgain
        /// Edit on a message that stands unsent: its text goes back into
        /// the composer, and what is sent from there is a new message.
        case takeBack
        /// Try Again and Continue: runs the thread again with no new
        /// message, and leaves the draft alone. A server whose `agents`
        /// feature is older than the continue route gets `continueMessage`
        /// as the owner's own.
        case continueTurn
        case stop
        case retryReload
        /// Try Again on a stream that would not open.
        case reopen
        /// The failure above the composer was read.
        case dismissFailure
        /// The composer started listening, as a shortcut asked.
        case listened
        /// "Answer in your own words" on a question's card: the card leaves
        /// the transcript, the composer's field takes the cursor, and the
        /// message sent from it is the answer.
        case answerInWords(String)
        /// The choice taken back: the card stands in the transcript again.
        case showOptions
    }

    /// An event, opaque outside ChatKit.
    public struct Event: Codable, Equatable, Sendable {
        let step: Step
        init(_ step: Step) { self.step = step }
    }

    enum Step: Codable, Equatable, Sendable {
        case drafted(String)
        case rested(String)
        case filled(String)
        case heard(String)
        case chose(String)
        case listened
        case opened
        case sent(message: String, agent: String, at: Date, baseline: Int?, fromDraft: Bool, id: String?)
        case sentAgain(at: Date)
        case tookBack
        /// The wait before an unanswered message goes again ended.
        case repeatDue(id: String, at: Date)
        /// The read for a message by its id answered: the thread it is on,
        /// or nil where no record has the id.
        case found(id: String, thread: String?, at: Date)
        case findFailed(id: String, Failure, at: Date)
        /// The device keeps the pending message.
        case kept
        /// The thread's rows hold the pending message.
        case landed
        /// The folder kept a draft for the thread this chat turned out to be.
        case draftFound
        case continued(at: Date)
        /// A rate limit lifted.
        case countdownDue(Date)
        case reopened
        case failureDismissed
        case named(String)
        case streamed(AgentEvent, at: Date)
        case streamEnded(Failure?, outcome: UUID)
        case attachRead(thread: Record?, rows: [Record])
        case attachReadFailed(Failure)
        case retryDue(attempt: Int)
        case handedOver(thread: Record?, rows: [Record])
        case handoverFailed(Failure)
        case pollTick
        case polled(thread: Record?, rows: [Record])
        case pollFailed(Failure)
        case stopAsked
        case stopFailed(Failure)
        case stopWaited
        case checked(outcome: UUID, found: Found?, failure: Failure?)
        case retriedReload
        case threadMoved(ThreadFacts?)
        case nudgedPoll
        case resumed
        case ownWords(String)
        case optionsShown
        /// A reply typed into a notification is the list's to hand over:
        /// read from the value, as after every event.
        case replyAsked
    }

    /// A reply typed into a notification, for this conversation to send as
    /// a message under `id`.
    struct Reply: Codable, Hashable, Sendable {
        var id: String
        var text: String
        var at: Date
        /// The action's time is up: a copy still unanswered stops being
        /// sent, and the message stands in the chat.
        var expired = false
        /// The push it was typed into: every copy goes with the client of
        /// the session that accepts it, or nowhere.
        var binding: PushBinding?
    }

    /// What became of it: sent, or why not, with the text kept in the
    /// composer or on a message that stands.
    /// Questions answered in the person's own words, by a message the
    /// server now holds.
    struct WordsAnswer: Codable, Equatable, Sendable {
        var interaction: String
        /// The message's id, where it was sent under one.
        var message: String?
        /// The message named them, and the server resolved them with it:
        /// nothing is left to dismiss.
        var linked: Bool
    }

    struct ReplyOutcome: Codable, Equatable, Sendable {
        var id: String
        var failure: Failure?
        /// A copy may have reached the server: a failure is never told as
        /// nothing sent.
        var mayHaveLanded = false
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

    /// What Continue sends to a server without the continue route. Such a
    /// server resumes a stopped thread only when its owner writes again, so
    /// going on is a message like any other, and the transcript shows it.
    static let continueMessage = "Continue where you left off."

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
    /// The `turn` ordinals of the rows the list holds of the thread: a
    /// message sent into a turn that already runs is answered with that
    /// turn's stream from its start, and what it says again of a row drawn
    /// from the rows is dropped (`stale`).
    let rowTurns: Set<Int>
    /// The newest ordinal a reply's row carries among them, or -1: a
    /// message sent while a turn ran whose ordinal is below it was read.
    let rowReplied: Int
    /// A reply typed into a notification of this thread, to send.
    let reply: Reply?
    /// The push this conversation acts for, made for a notification's
    /// action and not yet opened by the person: every request it makes goes
    /// with the client of the session that accepts that push, or nowhere.
    let bound: PushBinding?
    /// A scene in front shows it.
    let visible: Bool
    /// Moved by the list for each change of a message the watch reports
    /// while the thread runs in the background: read it now.
    let pollNudge: Int
    /// Moved when the app comes back to the front: a stream that died while
    /// it was away opens again.
    let resumeNudge: Int
    /// The server serves the continue route: its `agents` feature is at
    /// the revision that has it.
    let continues: Bool
    /// The server takes a `messageId`: its `agents` feature is at the
    /// revision that has it. Every message the person sends then goes under
    /// an id of its own.
    let sendsIDs: Bool
    /// The id of the pending message the device's folder holds, as the
    /// list last heard from the write: a send waits for it.
    let kept: String?
    /// The device's words where the folder's last write did not take the
    /// pending message: it is kept only while the app runs, and the list
    /// tries the write again.
    let unsaved: String?
    /// The rows the list holds of the thread have the pending message.
    let landed: Bool
    /// The thread whose rows, as the list holds them, have a new chat's
    /// pending message: its first copy opened that thread, and its answer
    /// was lost.
    let landedOn: String?
    /// The draft the folder kept for the thread this chat turned out to be,
    /// written on it before: it goes into the composer under what is
    /// there, and neither is dropped.
    let draftFound: String?

    /// Two texts for one composer, neither dropped: the newer above the
    /// older, as Edit Message puts a message above what was typed since.
    static func above(_ newer: String, _ older: String) -> String {
        if newer.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return older }
        if older.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return newer }
        return newer + "\n\n" + older
    }

    /// How long after each unanswered copy the next goes on its own: three
    /// repeats, as the console makes them.
    static let repeatDelays: [Double] = [1, 3, 8]
    /// No copy goes on its own later than this after the person pressed:
    /// past it the message stands unsent, with Send Again.
    static let repeatWindow: TimeInterval = 120
    /// The `agents` feature's revision as the server's document says it; 0
    /// until it is read, when the chat expects nothing a revision added.
    let agents: Int

    /// The revision from which a stream's text says where it belongs, a
    /// chat's every run streams and a quiet stream beats every 10 seconds.
    static let positionsRevision = 5

    /// The revision from which a message sent under its id while a chat
    /// turn runs joins that turn (decision 0318). Below it the same send
    /// is refused as busy, so the composer holds it until the turn ends.
    static let queueRevision = 6

    /// The revision from which a message names the questions it answers
    /// in the person's own words (`repliesTo`, decision 0327). Below it the
    /// message is sent and the questions are then dismissed.
    static let wordsRevision = 7

    /// An answer in the person's own words rides its message: the
    /// server's document says it takes that, and the message goes under
    /// an id. Asked of the document, never found out by sending: a server
    /// below the revision refuses the field.
    var linksWords: Bool { agents >= Self.wordsRevision && sendsIDs }

    /// A message may be sent while a turn runs: the server's document says
    /// it takes one, the message goes under an id, and the thread is a
    /// chat's. Asked of the document, never found out by sending.
    var queues: Bool { agents >= Self.queueRevision && sendsIDs && (facts?.chat ?? true) }

    /// A turn the server streams runs on the thread, and a message sent
    /// now joins it. Not while a stop is out, whose answer says how the
    /// turn ended, and not while a continue has had no answer: it may be
    /// refused, and there is then no turn to join.
    func joins(_ state: State) -> Bool {
        guard queues, state.thread != nil, state.phase == .streaming, !state.stopping else { return false }
        switch state.run {
        case let .send(run)?: return run.named && (!run.resumes || run.started)
        case let .attach(run)?: return !run.probe
        case nil: return false
        }
    }

    /// How long a stream may say nothing before it is taken for dead and
    /// opened again: past two of the server's heartbeats, which come every
    /// 10 seconds from `positionsRevision` and every 30 before it.
    var quiet: TimeInterval { agents >= Self.positionsRevision ? 25 : 75 }

    /// A run of this thread that has no stream yet will have one: it is a
    /// chat's, and the server streams a chat's every run. Below the
    /// revision, and for an automated run at any, the rows are read
    /// instead.
    var streamsEveryRun: Bool { agents >= Self.positionsRevision && (facts?.chat ?? true) }

    struct Seed: Codable, Hashable, Sendable {
        var thread: String?
        var agent: String?
        var draft: String
        var listen: Bool
        /// A message the device kept from a run of the app that ended
        /// before the server took it.
        var unsent: UnsentMessage?
    }

    init(
        key: ChildKey, seed: Seed, facts: ThreadFacts?, online: Bool, keyless: String?, newestTurn: Int?, visible: Bool,
        pollNudge: Int, resumeNudge: Int, continues: Bool = false, agents: Int = 0, sendsIDs: Bool = false,
        kept: String? = nil, unsaved: String? = nil, landed: Bool = false, landedOn: String? = nil, draftFound: String? = nil,
        rowTurns: Set<Int> = [], rowReplied: Int = -1, reply: Reply? = nil, bound: PushBinding? = nil
    ) {
        self.key = key
        self.bound = bound
        self.seed = seed
        self.facts = facts
        self.online = online
        self.keyless = keyless
        self.newestTurn = newestTurn
        self.rowTurns = rowTurns
        self.rowReplied = rowReplied
        self.reply = reply
        self.visible = visible
        self.pollNudge = pollNudge
        self.resumeNudge = resumeNudge
        self.continues = continues
        self.sendsIDs = sendsIDs
        self.kept = kept
        self.unsaved = unsaved
        self.landed = landed
        self.landedOn = landedOn
        self.draftFound = draftFound
        self.agents = agents
    }

    public var initialState: State {
        var state = State(
            thread: seed.thread, agent: seed.unsent?.agent ?? seed.agent, draft: seed.draft, carried: seed.draft, listenOnOpen: seed.listen)
        if let unsent = seed.unsent {
            state.pending = unsent
            state.copies = 1
            state.restored = true
        }
        return state
    }

    /// Who the chat is with: a bound chat's thread names it; a new chat
    /// holds the one chosen.
    func agent(_ state: State) -> String? { facts?.agent ?? state.agent }

    func name(_ state: State) -> String { agent(state).map(agentName) ?? "the assistant" }

    /// Why a send cannot go now; the text stays in the composer while it
    /// says so. One message at a time waits on the server: the next goes
    /// once the one above is sent or taken back.
    func blockedReason(_ state: State) -> String? {
        if let reason = resendBlocked(state) { return reason }
        return state.pending == nil ? nil : "Send or edit the message above first"
    }

    /// Why the pending message cannot be sent again now.
    func resendBlocked(_ state: State) -> String? {
        if state.deleted { return "This chat was deleted" }
        if agent(state) == nil { return "Choose an assistant" }
        if state.checking { return "Checking whether your last message arrived" }
        switch state.phase {
        case .streaming where joins(state): break
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
        case let .draftLeft(text): return text == state.draft && text == state.carried ? [] : [Event(.rested(text))]
        case let .fill(text): return [Event(.filled(text))]
        case let .heard(words):
            return words.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? [] : [Event(.heard(words))]
        case let .choose(agent): return [Event(.chose(agent))]
        case .listened: return state.listenOnOpen ? [Event(.listened)] : []
        case let .answerInWords(interaction): return [Event(.ownWords(interaction))]
        case .showOptions: return state.answering == nil ? [] : [Event(.optionsShown)]
        case .stop: return [Event(.stopAsked)]
        case .retryReload: return [Event(.retriedReload)]
        case .reopen: return [Event(.reopened)]
        case .dismissFailure: return [Event(.failureDismissed)]
        case .sendAgain:
            guard state.standing, resendBlocked(state) == nil else { return [] }
            return [Event(.sentAgain(at: context.now))]
        case .takeBack: return state.standing ? [Event(.tookBack)] : []
        case .send:
            let message = state.draft.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !message.isEmpty, blockedReason(state) == nil else { return [] }
            // The id is minted here, once, when the person presses Send:
            // every copy of this message goes under it.
            return sent(message, fromDraft: true, state, at: context.now, id: sendsIDs ? context.mint().uuidString.lowercased() : nil)
        case .continueTurn:
            guard state.thread != nil, blockedReason(state) == nil else { return [] }
            if continues { return [Event(.continued(at: context.now))] }
            return sent(Self.continueMessage, fromDraft: false, state, at: context.now, id: nil)
        }
    }

    private func sent(_ message: String, fromDraft: Bool, _ state: State, at now: Date, id: String?) -> [Event] {
        guard blockedReason(state) == nil, let agent = agent(state) else { return [] }
        // Where the transcript stood, for a delivery check: unknown while its
        // rows are unread, or stale because their re-read failed.
        let baseline = state.thread != nil && state.reloadFailed == nil ? newestTurn : nil
        return [Event(.sent(message: message, agent: agent, at: now, baseline: baseline, fromDraft: fromDraft, id: id))]
    }

    /// Made while a scene shows it: as if it had just appeared there.
    public func event(atOpen replayed: Bool) -> Event? { visible ? Event(.opened) : reply == nil ? nil : Event(.replyAsked) }

    public func event(replacing previous: Conversation) -> Event? {
        if facts != previous.facts { return Event(.threadMoved(facts)) }
        if pollNudge != previous.pollNudge { return Event(.nudgedPoll) }
        if resumeNudge != previous.resumeNudge { return Event(.resumed) }
        if visible, !previous.visible { return Event(.opened) }
        // Each of the two is also read from the value by whatever event is
        // applied next, so one that loses its turn here to a change above
        // is not missed.
        if kept != nil, kept != previous.kept { return Event(.kept) }
        if landed, !previous.landed { return Event(.landed) }
        if landedOn != nil, landedOn != previous.landedOn { return Event(.landed) }
        if draftFound != nil, draftFound != previous.draftFound { return Event(.draftFound) }
        if reply != nil, reply != previous.reply { return Event(.replyAsked) }
        return nil
    }

    /// The window a reply's pieces share: however fast they arrive, its
    /// text is presented, and so parsed, once in each, 20 times a second at
    /// most. The presenter runs one at a time on the runtime's executor, so
    /// one parse runs, the pieces that arrive meanwhile wait and are parsed
    /// together, and a presentation never follows one made of newer text.
    static let textWindow = Duration.milliseconds(50)

    /// Structure (a turn started, a tool card, a compaction) shows at once;
    /// text waits for the window, so a fast model does not redraw on every
    /// token.
    public func coalescing(of event: Event) -> Coalescing {
        if case let .streamed(streamed, _) = event.step, streamed.kind == .delta || streamed.kind == .reasoning {
            return .within(Self.textWindow)
        }
        return .now
    }

    // MARK: Effects

    public func effects(of event: Event, on state: State, presented presentation: Presentation) -> [Effect] {
        state.asks.map(Effect.init)
    }

    public func key(of effect: Effect) -> String? {
        switch effect.ask {
        case .chat, .resume, .attach, .handover: "run"
        case .repeatWait: "repeat"
        case .find: "find"
        case .countdown: "countdown"
        case .wait: "retry"
        case .pollEvery: "pollTimer"
        case .poll: "poll"
        case .stop: "stop"
        case .stopWait: "stopWait"
        case .check: "check"
        case let .cancel(key): key
        }
    }
}
