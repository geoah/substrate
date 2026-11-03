import Foundation
import SubstrateCore
import SubstrateDevice

/// What the app notifies, in its words: there is no push channel, so it
/// says what it observed itself. Every notification carries its thread,
/// and tapping it opens that chat. Each is shown at once, and in front too:
/// the engine never notifies a chat a window shows.
public enum ChatNotifications {
    /// The thread a notification's `userInfo` names, which a tap opens.
    public static let threadKey = "thread"

    /// What every notification of one thread's id starts with, so opening
    /// the chat takes back the ones still pending. `|` is outside the
    /// record id alphabet, so one thread's prefix never covers another's.
    static func prefix(_ thread: String) -> String { "chat|\(thread)|" }

    /// A reply settled in a chat that is not on screen.
    static func reply(thread: ChatThread, agent: String, title: String) -> NotificationRequest {
        NotificationRequest(
            id: prefix(thread.id) + "reply", title: agent,
            body: thread.status == "error" ? "Couldn't answer\(thread.reason.map { ": \($0)" } ?? ".")" : thread.preview ?? "Answered.",
            thread: thread.id, userInfo: [threadKey: thread.id], subtitle: title)
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
        return NotificationRequest(id: (thread.map(prefix) ?? "") + "card|\(record.id)", title: agent, body: body, thread: thread,
                                   timeSensitive: true, userInfo: thread.map { [threadKey: $0] } ?? [:], subtitle: title)
    }
}
