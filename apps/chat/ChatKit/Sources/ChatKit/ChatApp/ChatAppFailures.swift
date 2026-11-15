import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateRuntime

/// A conversation's failures as its screen draws them: a card at each turn
/// that has one, and one strip above the composer.
public struct ScreenFailures: Codable, Equatable, Sendable {
    /// By the turn's id: the card drawn under that turn.
    public var turns: [String: FailurePresentation] = [:]
    public var strip: FailurePresentation?
    /// The sentence of the failure that stands now, for the haptic and for
    /// VoiceOver, which hears it when it changes in a chat on screen.
    public var spoken: String?
    /// A stream dropped and is being opened again: a state, not a failure.
    public var reconnecting = false
}

extension ChatThread {
    /// How the thread's last run ended, where it did not finish: its
    /// `status`, the `cause` and `retryAfter` a server that names causes
    /// keeps beside it, and its `reason`, the server's own words.
    var runFailure: Failure? {
        guard ["error", "overbudget", "stopped"].contains(status) else { return nil }
        return Failure(
            .reply, .run(status: status, cause: record["cause"]?.string), said: reason, retryAfter: record["retryAfter"]?.int,
            at: record["finishedAt"]?.string.flatMap(Timestamp.parse))
    }
}

extension ChatApp {
    /// What the words of a failure in one chat need: who it is with, on
    /// which provider, and where the console keeps both.
    func failureContext(_ state: State, agent: String?, thread: String?) -> FailurePresenter.Context {
        let provider = state.provider(of: agent, thread: thread)
        var context = FailurePresenter.Context()
        context.agent = agent.map(agentName) ?? "The assistant"
        context.agentID = agent
        context.provider = provider?.name
        context.providerID = provider?.id
        context.host = state.session.map { ServerAddress.display($0.server) } ?? "the server"
        context.providerPage = provider.flatMap { state.session?.console("data/\(Kinds.provider)/\(segment($0.id))") }
        context.agentPage = agent.flatMap { state.session?.console("agents/\(segment($0))") }
        context.keySheet = provider.map(state.lacksKey) ?? false
        context.thread = thread
        return context
    }

    /// The failures one conversation's screen draws. The turn's: what the
    /// thread's row says of its last run, or what this conversation saw of
    /// it, at the newest turn; and an older turn that was cut short or went
    /// unanswered, at that turn, with no button. The chat's, above the
    /// composer: a pending message the device could not save, then the
    /// conversation's own, then a rename, then a read of its rows that
    /// failed.
    ///
    /// - Parameter all: the chat's turns as one list, the rows' and what the
    ///   stream has said since (`TurnFold`). Only a stored row says how it
    ///   was cut, or went unanswered.
    func failures(_ state: State, live: ConversationLive, row: ChatThread?, all: [Turn], turns: [TurnPresentation]) -> ScreenFailures {
        var out = ScreenFailures(reconnecting: live.failures.reconnecting)
        var context = failureContext(state, agent: live.agent, thread: live.thread)
        context.blocked = live.blocked
        let stored = all.filter { $0.row != nil }

        // Older turns first: the newest turn's card is the run's, below.
        var older = context
        older.acts = false
        for (index, turn) in stored.enumerated() where index < stored.count - 1 {
            if turn.role == .assistant, let cut = turn.cutBy, cut != CutBy.stop.rawValue {
                out.turns[turn.id] = FailurePresenter.present(Failure(.reply, .cut(cut)), older)
            } else if turn.role == .user, stored[index + 1].role == .user, deliveryNotice(turn) == nil,
                turn.content != Conversation.continueMessage, !Self.readLater(stored, after: index, queues: live.queues)
            {
                // Continue's own words, from before the continue route, are
                // no question of the person's that went unanswered.
                out.turns[turn.id] = FailurePresenter.present(Failure(.reply, .unanswered), older)
            }
        }

        let working = live.phase != .idle
        let resuming = live.failures.turnBusy
        // A message that stands unsent is the newest turn, and its card is
        // its own: sending it again is what goes on from here, so the run
        // before it offers nothing.
        if let id = live.unsent, let failure = live.failures.message {
            var unsent = context
            unsent.blocked = live.resendBlocked
            var card = FailurePresenter.present(failure, unsent, dismiss: .edit)
            if card.button?.action == .edit { card.dismiss = nil }
            out.turns[id] = card
            out.spoken = card.spoken
        } else if !live.deleted, !working || resuming, let last = turns.last, last.id != live.unsent {
            context.busy = resuming
            context.kept = all.last.map { $0.role == .assistant && !$0.content.isEmpty } ?? false
            context.unread = live.unread
            if let failure = Self.turnFailure(row: row, live: live.failures.turn) {
                out.turns[last.id] = FailurePresenter.present(failure, context)
                out.spoken = out.turns[last.id]?.spoken
            } else if let turn = all.last, turn.role == .assistant, turn.cutBy == CutBy.length.rawValue || turn.cutBy == CutBy.filter.rawValue {
                out.turns[last.id] = FailurePresenter.present(Failure(.reply, .cut(turn.cutBy ?? "")), context)
            } else if live.queues, Self.unanswered(row, all) {
                // Sent while a run was going, and that run ended without
                // reading it: no turn was started for it, or none could be.
                out.turns[last.id] = FailurePresenter.present(Failure(.reply, .unanswered), context)
            }
        }

        context.busy = live.failures.chatBusy
        if live.failures.checking {
            out.strip = FailurePresentation(id: "checking", tone: .quiet, sentence: "Checking whether your message arrived…", working: true)
        } else if let failure = live.failures.keep {
            // Before the chat's other failures: this one is lost with the
            // app, and the person can only keep the app open.
            context.stands = live.unsent != nil
            out.strip = FailurePresenter.present(failure, context)
        } else if let failure = live.failures.chat {
            let dismissible = failure.attempt == .send || failure.attempt == .attach
            out.strip = FailurePresenter.present(failure, context, dismiss: dismissible ? .dismiss : nil)
        } else if let rename = state.renameFailure, rename.thread == live.thread {
            context.subject = rename.subject
            out.strip = FailurePresenter.present(rename.failure, context, dismiss: .dismissRename)
        } else if let thread = live.thread, let failure = state.rowsFailed[thread], !working {
            out.strip = FailurePresenter.present(failure, context)
        }
        if let strip = out.strip, !strip.working { out.spoken = strip.spoken }
        return out
    }

    /// Whether a later reply read the message at `index`. A server that
    /// takes a message into a running turn (`agents` 6) has every call read
    /// what was sent before it, several messages together: one followed by
    /// another and then by a reply was answered with it. Before that
    /// revision a message no reply follows directly went unanswered.
    static func readLater(_ stored: [Turn], after index: Int, queues: Bool) -> Bool {
        queues && stored[(index + 1)...].contains { $0.role == .assistant }
    }

    /// Whether the chat ends on a message of the person's that nothing
    /// answered and nothing is answering: the thread settled as finished,
    /// and its newest row is theirs. A message sent while a turn ran comes
    /// to this when that turn ended without reading it and the turn it
    /// should have started did not start; Continue has it read.
    static func unanswered(_ row: ChatThread?, _ all: [Turn]) -> Bool {
        guard row?.status == "ok", let last = all.last, last.row != nil, last.role == .user else { return false }
        return deliveryNotice(last) == nil && last.content != Conversation.continueMessage
    }

    /// The failure drawn at the newest turn. A Try Again that failed stands
    /// in the place of the failure it was pressed on, while the thread
    /// still says its run did not finish. Otherwise the thread's row says
    /// how its last run ended, unless only what this conversation saw
    /// names a cause.
    static func turnFailure(row: ChatThread?, live: Failure?) -> Failure? {
        let stored = row?.runFailure
        if let live, live.attempt == .resume { return row == nil || stored != nil ? live : nil }
        if let stored, stored.cause != nil { return stored }
        if let live, live.cause != nil, row == nil || stored != nil { return live }
        return stored ?? (row == nil ? live : nil)
    }

    /// The list's one failure, at its top: a refresh, the change feed, or a
    /// rename of a chat no window shows.
    func listFailure(_ state: State) -> FailurePresentation? {
        guard state.online else { return nil }
        var context = failureContext(state, agent: nil, thread: nil)
        context.busy = state.refreshing
        if let failure = state.listFailure { return FailurePresenter.present(failure, context) }
        if let rename = state.renameFailure, !state.visibleThreads.contains(rename.thread) {
            context.busy = false
            context.thread = rename.thread
            context.subject = rename.subject
            return FailurePresenter.present(rename.failure, context, dismiss: .dismissRename)
        }
        return nil
    }

    /// A failed reply's notification says what its card says.
    func notificationWords(_ state: State, thread: ChatThread) -> String? {
        guard thread.status == "error", let failure = thread.runFailure else { return nil }
        return FailurePresenter.present(failure, failureContext(state, agent: thread.agentID, thread: thread.id)).spoken
    }

    /// A failure in one line, for a card, a sheet or a row of Settings.
    func failureLine(_ state: State, _ failure: Failure?) -> String? {
        failure.map { FailurePresenter.line($0, failureContext(state, agent: nil, thread: nil)) }
    }
}
