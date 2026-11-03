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
        var failedBefore: String?
        /// The newest turn held before the send, for a delivery check; nil
        /// while the rows are unread or stale.
        var baseline: Int?
        var sentAt: Date
        var named = false
        var done = false
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
    var draft: String
    /// Moved when the engine itself rewrites the draft (a send clears it, a
    /// refusal gives it back), so the field takes the new text.
    var draftRevision = 0
    var phase = Phase.idle
    var overlay = LiveOverlay.empty
    var seq = 0
    var stopping = false
    /// "<Agent> couldn't answer: …"
    var error: String?
    var outcomes: [Outcome] = []
    /// The handover read failed or outlasted its wait: the overlay stays.
    var reloadFailed: String?
    var deleted = false
    /// Moved when a run settles, for the view's haptic and announcement.
    var settles = 0
    var listenOnOpen = false
    var run: Run?
    /// The last failure an attach saw, shown if the turn ended on it.
    var lastError: String?
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
    var checking: Bool { outcomes.contains { $0.kind == .checking } }
}
