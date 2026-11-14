import Foundation
import SubstrateCore

/// One chat's run: the overlay a stream draws over the parent's rows, and
/// every way a send, an attach and a handover can end (the console's
/// `Conversation`, web/console/src/components/agent/conversation.tsx, with
/// the exits DESIGN.md's tables list).
public struct ConversationState: Codable, Equatable, Sendable {
    enum Phase: String, Codable, Sendable {
        case idle
        /// A send or an attach is streaming.
        case streaming
        /// The run settled; its rows are being read.
        case handingOver
        /// A turn runs that this server process does not stream.
        case background
    }

    /// A send's run: what it needs to end in any of its ways.
    struct Send: Codable, Equatable, Sendable {
        var message: String
        var agent: String
        /// The thread it went to; nil for a new chat.
        var sentTo: String?
        /// The overlay a failed handover left, still the only view of that run.
        var kept: [Turn]
        var failedBefore: Failure?
        /// The newest turn held before the send, for a delivery check; nil
        /// while the rows are unread or stale.
        var baseline: Int?
        var sentAt: Date
        var named = false
        var done = false
        /// The person typed it; false for the words Continue sends to a
        /// server without the continue route, which are never given back.
        var typed = true
        /// A continue: the thread run again with no message.
        var resumes = false
        /// The server answered with an event.
        var started = false
        /// Nobody pressed anything: a rate limit lifted. Such a run never
        /// writes a message in the person's name.
        var automatic = false
    }

    /// An attach's run.
    struct Attach: Codable, Equatable, Sendable {
        var attempt: Int
        /// The `turn` ordinals the rows read before it hold: its replay of
        /// them is dropped.
        var held: Set<Int> = []
        var idle: String?
        var done = false
    }

    enum Run: Codable, Equatable, Sendable {
        case send(Send)
        case attach(Attach)
    }

    /// What an applied event asks the runtime to do, made fresh by each.
    enum Ask: Codable, Equatable, Sendable {
        case chat(agent: String, thread: String?, message: String)
        case resume(thread: String)
        /// Wakes the engine when a rate limit lifts.
        case countdown(Date)
        case attach(thread: String)
        case wait(attempt: Int, seconds: Double)
        case handover(thread: String)
        case pollEvery
        case poll(thread: String)
        case stop(thread: String)
        case stopWait
        case check(Check)
        case advance
        /// Ends what runs under this key: a new run's start, so nothing an
        /// older one asked for lands.
        case cancel(String)
    }

    /// A send whose delivery is unknown, to read for.
    struct Check: Codable, Equatable, Sendable {
        var outcome: UUID
        var thread: String?
        var agent: String
        var message: String
        var baseline: Int?
        var sentAt: Date
    }

    /// Bound by the `thread` event, or from the start for a chat the list holds.
    var thread: String?
    /// Who a new chat goes to; a bound chat's thread names its agent.
    var agent: String?
    /// What the composer's field holds, as far as it has said: what a send
    /// sends and the disk keeps. The field owns the text while it is typed.
    var draft: String
    /// The draft as the tree carries it: what a field starts from. Typing
    /// never moves it; the engine's own rewrites do, with the revision.
    var carried = ""
    /// Moved when the engine itself rewrites the draft (a send clears it, a
    /// refusal gives it back, dictation lands, an opener is picked, a chat
    /// shown again takes up what was typed into it, a field left the
    /// screen), so the field takes the new text.
    var draftRevision = 0
    /// The text the rewrites since the field last spoke stand in place of:
    /// what a field that had said everything held before them. A field
    /// holding more than that typed it meanwhile, and keeps it.
    var draftReplaced = ""
    /// The same for the last rewrite alone: what it stands in place of, for
    /// a field that took the one before it.
    var draftBefore = ""
    /// The engine rewrote the draft since the field last spoke.
    var rewritten = false
    var phase = Phase.idle
    var overlay = LiveOverlay.empty
    var seq = 0
    var stopping = false
    /// The turn's failure, as the stream said it or as a Try Again met it:
    /// drawn at the turn until the next run answers.
    var turnFailure: Failure?
    /// A failure of the chat as a whole, drawn above the composer: a send
    /// that started no turn, a stop that failed, a stream that will not
    /// open. A second one replaces the first.
    var chatFailure: Failure?
    /// A send whose delivery is being read for.
    var lost: Lost?
    /// The handover read failed or outlasted its wait: the overlay stays.
    var reloadFailed: Failure?
    /// The reload failure whose Try Again is out: drawn working in its place.
    var reloading: Failure?
    /// The rate limit's end the engine waits on, and whether it already
    /// tried again on its own for this failure: once, never in a loop.
    var armed: Date?
    var autoRetried = false
    /// The server told this conversation it does not serve the continue
    /// route: the list remembers it for the session.
    var continueUnserved = false
    var deleted = false
    /// Moved when a run settles, for the view's haptic and announcement.
    var settles = 0
    var listenOnOpen = false
    var run: Run?
    /// The last failure a stream saw, shown if the turn ended on it. While
    /// it is set and the run streams, the app is reconnecting, which is a
    /// state and never drawn as a failure.
    var lastFailure: Failure?
    var polling = false
    var pollAgain = false
    /// The failure the last event met, for the parent to tell the link.
    var failure: Failure?
    var asks: [Ask] = []

    /// The conversation owns its thread's rows: a run is under way, or its
    /// overlay waits for a handover read that failed. No other read may
    /// publish under it, or the overlay and the rows would both show a turn.
    var ownsTranscript: Bool { phase != .idle || reloadFailed != nil }
    var active: Bool { phase != .idle }
    var streaming: Bool { phase == .streaming }
    var checking: Bool { lost != nil }
    /// A continue is out and the server has said nothing yet: the failure
    /// whose button was pressed stays drawn, working.
    var resuming: Bool {
        if case let .send(run)? = run, run.resumes, !run.started, phase == .streaming { return true }
        return false
    }
    /// A stream dropped and is being opened again.
    var reconnecting: Bool { phase == .streaming && lastFailure != nil }
    /// What keeps the conversation after its window left: a failure above
    /// the composer still to be read, or a check under way.
    var held: Int { (chatFailure != nil ? 1 : 0) + (lost != nil ? 1 : 0) }

    /// A send that ended with no answer either way, while it is read for.
    struct Lost: Codable, Equatable, Sendable {
        var id: UUID
        var message: String
        var typed: Bool
        var failure: Failure
    }
}
