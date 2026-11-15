/// What stands under the last reply of a chat: Copy; Try Again or Continue
/// where the run can go on and its card does not already offer that; and
/// Share and Select Text once the reply is whole. Older replies keep Copy,
/// Select Text and Share behind a long press.
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
    /// The last reply is the last turn of the assistant's that has words,
    /// with nothing of the person's after it: a message sent since makes
    /// the reply before it an older one, and a call that only ran tools
    /// leaves the words before it the last said. Where the run's card names
    /// a Try Again beside its button and no reply has words, it stands
    /// under the assistant's last turn alone.
    public init?(under turns: [TurnPresentation], failures: ScreenFailures) {
        var said: (id: String, reply: AssistantTurn)?
        var last: String?
        scan: for turn in turns.reversed() {
            switch turn.body {
            case .user, .trigger:
                break scan
            case let .assistant(reply):
                if last == nil { last = turn.id }
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
        } else if let last, let resume {
            self.init(turn: last, copies: false, whole: false, resume: resume)
        } else {
            return nil
        }
    }
}

public extension ConversationScreen {
    /// What stands under the chat's last reply.
    var replyActions: ReplyActions? { ReplyActions(under: turns, failures: failures) }
}
