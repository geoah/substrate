/// What a conversation holds of its failures, by where each is drawn: the
/// turn's at the turn, the chat's above the composer. Values, not words:
/// the list's presenter draws them with what it knows of the thread.
struct ConversationFailures: Codable, Equatable, Sendable {
    /// The turn's failure, as the stream said it or as a Try Again met it.
    var turn: Failure?
    /// Try Again or Continue is out and the server has said nothing yet.
    var turnBusy = false
    /// A send that started no turn, a stop that failed, a stream that will
    /// not open, a reload that did not land.
    var chat: Failure?
    var chatBusy = false
    /// A send of unknown delivery is being read for.
    var checking = false
    /// A stream dropped and is being opened again. A state for the
    /// composer's status line, never drawn as a failure.
    var reconnecting = false
}

extension Conversation {
    func failures(_ state: State) -> ConversationFailures {
        var out = ConversationFailures(turn: state.turnFailure, turnBusy: state.resuming)
        if let failure = state.chatFailure {
            out.chat = failure
            out.chatBusy = failure.attempt == .stop && state.stopping
        } else if let failure = state.reloadFailed {
            out.chat = failure
        } else if let failure = state.reloading, state.phase == .handingOver {
            out.chat = failure
            out.chatBusy = true
        }
        out.checking = state.checking
        out.reconnecting = state.reconnecting
        return out
    }
}
