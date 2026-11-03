import SubstrateRuntime

/// What a conversation presents of its own: the run and the composer. The
/// list draws it with the rows it holds into one `ConversationScreen`.
public struct ConversationLive: Codable, Equatable, Sendable {
    var thread: String?
    var agent: String?
    var name: String
    var draft: String
    var draftRevision: Int
    var turns: [Turn]
    /// An attach's replay lost events: the overlay may miss text.
    var incomplete: Bool
    /// The assistant turn the stream is writing.
    var liveKey: String?
    /// A send is out and nothing has come back yet.
    var awaitingFirstEvent: Bool
    var phase: ConversationState.Phase
    var stopping: Bool
    var error: String?
    var outcomes: [Outcome]
    var reloadFailed: String?
    var deleted: Bool
    var settles: Int
    var listenOnOpen: Bool
    var blocked: String?
    var keyless: String?
    var online: Bool
}

extension Conversation {
    public typealias Presentation = ConversationLive

    public func present(_ input: PresenterInput<Conversation>) -> ConversationLive {
        let state = input.state
        let streaming = state.phase == .streaming
        return ConversationLive(
            thread: state.thread, agent: agent(state), name: name(state), draft: state.draft, draftRevision: state.draftRevision,
            turns: state.overlay.turns, incomplete: state.overlay.incomplete,
            liveKey: streaming ? state.overlay.turns.last(where: { $0.role == .assistant })?.id : nil,
            awaitingFirstEvent: streaming && state.overlay.turns.last?.role == .user,
            phase: state.phase, stopping: state.stopping, error: state.error, outcomes: state.outcomes, reloadFailed: state.reloadFailed,
            deleted: state.deleted, settles: state.settles, listenOnOpen: state.listenOnOpen, blocked: blockedReason(state),
            keyless: keyless, online: online)
    }
}
