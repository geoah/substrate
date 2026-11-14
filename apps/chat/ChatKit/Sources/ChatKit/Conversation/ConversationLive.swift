import SubstrateRuntime

/// What a conversation presents of its own: the run and the composer. The
/// list draws it with the rows it holds into one `ConversationScreen`.
public struct ConversationLive: Codable, Equatable, Sendable {
    var thread: String?
    var agent: String?
    var name: String
    var draft: String
    var draftRevision: Int
    var draftReplaced: String
    var draftBefore: String
    var draftGiven: [DraftGiven] = []
    /// What the run's stream has said that the rows may not hold yet, with
    /// the message the server has not taken after it, and the ids the rows
    /// it has read keep (`TurnFold`).
    var provisional: LiveRows
    var aliases: [String: String]
    /// An attach's replay lost events: a turn may miss text its row holds.
    var incomplete: Bool
    /// The assistant turn the stream is writing.
    var liveKey: String?
    /// A send is out and nothing has come back yet.
    var awaitingFirstEvent: Bool
    /// The run's stream is open: the server answered it.
    var connected: Bool
    /// Moved by every send.
    var sends: Int
    var phase: ConversationState.Phase
    var stopping: Bool
    var failures: ConversationFailures
    var deleted: Bool
    var listenOnOpen: Bool
    var blocked: String?
    /// The id of the message the server has not taken, where it stands
    /// unsent or is read for: the last of `turns`.
    var unsent: String?
    /// Why it cannot be sent again now.
    var resendBlocked: String?
    var keyless: String?
    var online: Bool
}

extension Conversation {
    public typealias Presentation = ConversationLive

    public func present(_ input: PresenterInput<Conversation>) -> ConversationLive {
        let state = input.state
        let streaming = state.phase == .streaming
        // A pending message no run is sending stands after everything
        // else, under the id its row will have.
        var provisional = state.provisional
        var unsent: String?
        if let pending = state.pending, !provisional.turns.contains(where: { $0.id == pending.id }) {
            provisional.turns.append(Turn(id: pending.id, role: .user, content: pending.message, pending: true))
            unsent = pending.id
        }
        return ConversationLive(
            thread: state.thread, agent: agent(state), name: name(state), draft: state.carried, draftRevision: state.draftRevision,
            draftReplaced: state.draftReplaced, draftBefore: state.draftBefore, draftGiven: state.given,
            provisional: provisional, aliases: state.aliases, incomplete: state.provisional.incomplete,
            liveKey: streaming ? state.provisional.writing?.id : nil,
            awaitingFirstEvent: streaming && state.provisional.turns.last?.role == .user,
            connected: state.connected, sends: state.sends,
            phase: state.phase, stopping: state.stopping, failures: failures(state),
            deleted: state.deleted, listenOnOpen: state.listenOnOpen, blocked: blockedReason(state),
            unsent: unsent, resendBlocked: unsent == nil ? nil : resendBlocked(state), keyless: keyless, online: online)
    }
}
