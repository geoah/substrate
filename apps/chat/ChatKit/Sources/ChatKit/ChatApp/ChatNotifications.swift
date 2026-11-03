import SubstrateCore
import SubstrateDevice

/// What the app notifies, in its words: there is no push channel, so it
/// says what it observed itself. Every notification carries its thread,
/// and tapping it opens that chat.
enum ChatNotifications {
    static let threadKey = "thread"

    /// A reply settled in a chat that is not on screen.
    static func reply(thread: ChatThread, agent: String, title: String) -> LocalNotification {
        LocalNotification(
            id: "reply.\(thread.id)", title: agent, subtitle: title,
            body: thread.status == "error" ? "Couldn't answer\(thread.reason.map { ": \($0)" } ?? ".")" : thread.preview ?? "Answered.",
            group: thread.id, payload: [threadKey: thread.id], urgency: .active)
    }

    /// A question or a suggestion is waiting on the owner.
    static func attention(record: Record, thread: String?, agent: String, title: String) -> LocalNotification {
        let body: String
        if record.kind == Kinds.request {
            body = "Suggested a change for you to apply or dismiss."
        } else {
            let n = record["questions"]?.array?.count ?? 1
            body = n == 1 ? "Has a question for you." : "Has \(n) questions for you."
        }
        return LocalNotification(id: "card.\(record.id)", title: agent, subtitle: title, body: body, group: thread,
                                 payload: thread.map { [threadKey: $0] } ?? [:], urgency: .timeSensitive)
    }
}
