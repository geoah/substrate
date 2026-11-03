import Foundation
import SubstrateCore
import SubstrateDevice

/// What the app notifies, in its words: there is no push channel, so it
/// says what it observed itself. Every notification carries its thread,
/// and tapping it opens that chat.
public enum ChatNotifications {
    /// The thread a notification's `userInfo` names, for the app's delegate.
    public static let threadKey = "thread"

    /// A reply settled in a chat that is not on screen.
    static func reply(thread: ChatThread, agent: String, title: String) -> NotificationRequest {
        NotificationRequest(
            id: "reply.\(thread.id)", title: agent,
            body: thread.status == "error" ? "Couldn't answer\(thread.reason.map { ": \($0)" } ?? ".")" : thread.preview ?? "Answered.",
            fireAt: .distantPast, thread: thread.id, userInfo: [threadKey: thread.id], subtitle: title)
    }

    /// A question or a suggestion is waiting on the owner.
    static func attention(record: Record, thread: String?, agent: String, title: String) -> NotificationRequest {
        let body: String
        if record.kind == Kinds.request {
            body = "Suggested a change for you to apply or dismiss."
        } else {
            let n = record["questions"]?.array?.count ?? 1
            body = n == 1 ? "Has a question for you." : "Has \(n) questions for you."
        }
        return NotificationRequest(id: "card.\(record.id)", title: agent, body: body, fireAt: .distantPast, thread: thread,
                                   timeSensitive: true, userInfo: thread.map { [threadKey: $0] } ?? [:], subtitle: title)
    }

    /// Shown as soon as the system lets it: the request's instant is the
    /// moment it is sent, which the engine cannot read.
    static func now(_ request: NotificationRequest, at instant: Date) -> NotificationRequest {
        var shown = request
        shown.fireAt = instant.addingTimeInterval(1)
        return shown
    }
}
