/// What stands under the last reply of a chat: Copy; Try Again or Continue
/// where the run can go on and its card does not already offer that; and
/// Share and Select Text once the reply is whole. Older replies keep Copy,
/// Select Text and Share behind a long press. Where there is no reply to
/// stand under and the run can be tried again beside its card's button, it
/// is Try Again alone, under the chat's last turn.
///
/// It is read from the screen's own turns and failures, so it is decided in
/// one place with them: whether a run can be tried again, and under which
/// words, is `FailurePresenter`'s, which gives the card its one button and
/// names a Try Again beside it only where that button is another
/// (`FailurePresentation.resume`). The card and this never offer the same.
public struct ReplyActions: Codable, Hashable, Sendable {
    /// The turn it stands under, by the id the turn is drawn by.
    public var turn: String
    /// The reply has words: Copy takes them as they stand, part way through
    /// a reply still being written too.
    public var copies: Bool
    /// The reply is whole: it can be shared, and opened for selecting.
    public var whole: Bool
    /// Try Again or Continue: the thread run again with no new message.
    public var resume: FailureButton?
    public var copyLabel = "Copy"
    /// What Copy says, and VoiceOver is told, once it has copied.
    public var copiedLabel = "Copied"
    public var shareLabel = "Share"
    public var selectLabel = "Select Text"

    public init(turn: String, copies: Bool, whole: Bool, resume: FailureButton? = nil) {
        self.turn = turn
        self.copies = copies
        self.whole = whole
        self.resume = resume
    }

    /// The actions under a chat's last reply; nil where nothing is offered.
    ///
    /// The last reply is the last turn of the assistant's that has words.
    /// A message of the person's after it makes it an older one only where
    /// nothing answers that message and nothing is going to: the chat is
    /// not working, and nothing of the assistant's stands after the
    /// message. So the row stays where the last words are while a run goes
    /// on (a message that started it, one queued into it, one a call has
    /// just read) and through a call that only ran tools or left a card
    /// to wait, and the reply that has the next words takes it over in the
    /// same step: what stands above it never gets shorter under a reader.
    ///
    /// Where the run's card names a Try Again beside its button and there
    /// is no such reply, it stands alone under the chat's last turn, the
    /// card's own: the assistant's, or the person's message where the run
    /// ended before the assistant said or did anything.
    ///
    /// - Parameter working: a run of this chat goes on, streamed here or
    ///   in the background.
    public init?(under turns: [TurnPresentation], failures: ScreenFailures, working: Bool = false) {
        var said: (id: String, reply: AssistantTurn)?
        var answered = false
        scan: for turn in turns.reversed() {
            switch turn.body {
            case .user, .trigger:
                if working || answered { continue }
                break scan
            case let .assistant(reply):
                answered = true
                if reply.text != nil {
                    said = (turn.id, reply)
                    break scan
                }
            case .summary, .decision, .answer, .jobEnded, .approvalEnded, .system:
                continue
            }
        }
        let resume = turns.last.flatMap { failures.turns[$0.id]?.resume }
        if let said {
            self.init(turn: said.id, copies: true, whole: !said.reply.live, resume: resume)
        } else if let last = turns.last, let resume {
            self.init(turn: last.id, copies: false, whole: false, resume: resume)
        } else {
            return nil
        }
    }
}

public extension ConversationScreen {
    /// What stands under the chat's last reply.
    var replyActions: ReplyActions? {
        ReplyActions(under: turns, failures: failures, working: streaming || status.contains { $0.kind == .background })
    }
}
