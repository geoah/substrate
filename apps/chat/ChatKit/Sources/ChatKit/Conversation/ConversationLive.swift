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
    var turns: [Turn]
    /// An attach's replay lost events: the overlay may miss text.
    var incomplete: Bool
    /// The assistant turn the stream is writing.
    var liveKey: String?
    /// A send is out and nothing has come back yet.
    var awaitingFirstEvent: Bool
    var phase: ConversationState.Phase
    var stopping: Bool
    var failures: ConversationFailures
    var deleted: Bool
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
            thread: state.thread, agent: agent(state), name: name(state), draft: state.carried, draftRevision: state.draftRevision,
            draftReplaced: state.draftReplaced, draftBefore: state.draftBefore,
            turns: state.overlay.turns, incomplete: state.overlay.incomplete,
            liveKey: streaming ? state.overlay.turns.last(where: { $0.role == .assistant })?.id : nil,
            awaitingFirstEvent: streaming && state.overlay.turns.last?.role == .user,
            phase: state.phase, stopping: state.stopping, failures: failures(state),
            deleted: state.deleted, listenOnOpen: state.listenOnOpen, blocked: blockedReason(state),
            keyless: keyless, online: online)
    }
}
