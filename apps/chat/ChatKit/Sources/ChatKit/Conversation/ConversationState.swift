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
        /// The id the message goes under (`messageId`), where the server
        /// takes one: the run's bubble carries it, and so will its row. Nil
        /// on a server that takes none, and for Continue's own words.
        var id: String?
        /// The request waits until the device keeps the message: it never
        /// leaves before a kill could lose what it sent.
        var unkept = false
        /// A repeat's whole answer after `thread`: the message's turn is no
        /// stream of this server process, and this is its thread's status.
        var idle: String?
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
        case chat(agent: String, thread: String?, message: String, id: String?)
        /// Wakes the engine to send an unanswered message again.
        case repeatWait(id: String, seconds: Double)
        /// Reads for the message a send wrote under its id.
        case find(id: String)
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
    /// A send whose delivery is being read for: only on a server that
    /// takes no `messageId`.
    var lost: Lost?
    /// The message sent under an id of its own, from the press until the
    /// server takes it: while a copy is out, while one waits to go again,
    /// and while it stands unsent at its place in the chat.
    var pending: UnsentMessage?
    /// The copies sent under the pending message's id, as far as this run
    /// of the app knows. One the device kept from a run before starts at
    /// one: what that run sent is not known.
    var copies = 0
    /// The copies sent since the person last pressed Send or Send Again:
    /// what bounds the repeats the app makes on its own.
    var tries = 0
    /// Why the pending message stands unsent, drawn at it with Send Again;
    /// nil while a copy is out, waits to go, or is read for.
    var unsentFailure: Failure?
    /// The pending message came from the device's folder and has not been
    /// read for yet.
    var restored = false
    /// The pending message is read for by its id: the app closed before
    /// its send was answered.
    var finding = false
    /// The server refused `messageId` as a field it does not know, after
    /// its document said it takes one: the list is told.
    var idsUnserved = false
    /// The last event put a message back into the composer (a refusal, a
    /// lost send, Edit Message, a chat that is gone): the list is told, so
    /// the folder holds the text at once and not after a draft's pause.
    var gaveBack = false
    /// The thread the last event found the pending message on, by a read
    /// and not by a run's `thread` event: the list is told, and where
    /// another conversation already has that thread, this one gives way.
    var adopted: String?
    /// The handover read failed or outlasted its wait: the overlay stays.
    var reloadFailed: Failure?
    /// The reload failure whose Try Again is out: drawn working in its place.
    var reloading: Failure?
    /// The rate limit's end the engine waits on, and whether it already
    /// tried again on its own for this failure: once, never in a loop.
    var armed: Date?
    var autoRetried = false
    /// The server answered that it does not serve the continue route its
    /// document said it has: the list is told.
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
    /// the composer still to be read, a check under way, or a message the
    /// server has not taken.
    var held: Int { (chatFailure != nil ? 1 : 0) + (lost != nil ? 1 : 0) + (pending != nil ? 1 : 0) }
    /// The pending message stands unsent: no copy is out and none waits.
    var standing: Bool { pending != nil && unsentFailure != nil }

    /// A send that ended with no answer either way, while it is read for.
    struct Lost: Codable, Equatable, Sendable {
        var id: UUID
        var message: String
        var typed: Bool
        var failure: Failure
    }
}

/// A message sent under an id of its own, as the conversation holds it
/// until the server takes it and as the device keeps it meanwhile
/// (`unsent.json`): what the same request is made from again. The id is
/// minted once, when the person presses Send, and never changes: a request
/// that repeats it writes nothing and runs nothing.
struct UnsentMessage: Codable, Hashable, Sendable {
    /// The `messageId`: the id of the message's own record.
    var id: String
    var message: String
    var agent: String
    /// The thread it goes to; nil for one that opens a chat.
    var thread: String?
    /// When the person pressed Send, or Send Again: no copy goes on its own
    /// later than `Conversation.repeatWindow` after it.
    var sentAt: Date
}
