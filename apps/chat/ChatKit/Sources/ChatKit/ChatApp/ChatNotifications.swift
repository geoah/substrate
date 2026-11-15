import Foundation
import SubstrateCore
import SubstrateDevice

/// What the app notifies, in its words: there is no push channel, so it
/// says what it observed itself. Every notification carries its thread,
/// and tapping it opens that chat. Each is shown at once, and in front too:
/// the engine never notifies a chat a window shows.
///
/// A notification can be answered where it stands. A reply's takes a line
/// of text, which is sent as a message under an id of its own. An
/// approval's takes Approve and Decline, where the notification shows all
/// of what would run. A single question of three options or fewer takes
/// its options. Each is one write over what the notification showed: the
/// version of the record it was made from rides in its `userInfo`, so a
/// record that changed since is refused by the server, never decided
/// unseen.
public enum ChatNotifications {
    /// The thread a notification's `userInfo` names, which a tap opens.
    public static let threadKey = "thread"
    /// The record a card's notification is about, and the version it
    /// showed: an action writes over that version and no other.
    static let cardKey = "card"
    static let versionKey = "version"
    /// The one question a question's notification answers, and the stored
    /// value each of its actions selects (`option.0`, `option.1`, …).
    static let questionKey = "question"
    static func optionKey(_ index: Int) -> String { "option.\(index)" }
    /// What tells one notification of an id from the next under the same
    /// id: a reply's is the instant its thread settled.
    static let nonceKey = "nonce"

    /// What every notification of one thread's id starts with, so opening
    /// the chat takes back the ones still pending. `|` is outside the
    /// record id alphabet, so one thread's prefix never covers another's.
    static func prefix(_ thread: String) -> String { "chat|\(thread)|" }

    // MARK: Categories and actions

    /// A reply: answered with a line of text.
    static let replyCategory = "chat.reply"
    /// A call to approve whose notification shows all of it.
    static let approvalCategory = "chat.approval"
    /// One that can only be opened: an approval too long to show whole.
    static let openCategory = "chat.open"
    /// A single question of a few options: a category of its own for each,
    /// since an action's title is its category's.
    static func questionCategory(_ interaction: String) -> String { "chat.question.\(interaction)" }

    /// What an action's id asks for.
    enum Action: Equatable {
        case reply
        case approve
        case decline
        /// An option of the question, by its place.
        case option(Int)
        case open

        /// The action's id, as its category names it.
        var id: String {
            switch self {
            case .reply: "reply"
            case .approve: "approve"
            case .decline: "decline"
            case .open: "open"
            case let .option(index): "option.\(index)"
            }
        }

        init?(_ id: String) {
            switch id {
            case "reply": self = .reply
            case "approve": self = .approve
            case "decline": self = .decline
            case "open": self = .open
            default:
                guard id.hasPrefix("option."), let index = Int(id.dropFirst("option.".count)), index >= 0 else { return nil }
                self = .option(index)
            }
        }
    }

    /// How many options a question may have for a notification to offer
    /// them, and how long a label may be to be read whole on a button.
    static let optionLimit = 3
    static let labelLimit = 40
    /// How much of a call a notification shows whole. Past it, the call is
    /// read in the chat, and the notification only opens it.
    static let approvalLimit = 320

    /// The categories that never change: a reply's, an approval's, and the
    /// one that only opens.
    static let fixed: [NotificationCategory] = [
        NotificationCategory(
            id: replyCategory,
            actions: [NotificationAction(id: Action.reply.id, title: "Reply", input: NotificationTextInput(button: "Send", placeholder: "Message"))]),
        NotificationCategory(
            id: approvalCategory,
            actions: [NotificationAction(id: Action.approve.id, title: "Approve"), NotificationAction(id: Action.decline.id, title: "Decline")]),
        NotificationCategory(id: openCategory, actions: [NotificationAction(id: Action.open.id, title: "Open Chat", opensApp: true)]),
    ]

    /// Every category the app's notifications name now: the fixed ones, and
    /// one for each question that waits and can be answered from its
    /// notification, whose actions are that question's options.
    static func categories(for interactions: [Record]) -> [NotificationCategory] {
        fixed
            + interactions.sorted { $0.id < $1.id }.compactMap { record in
                guard let asked = answerable(record) else { return nil }
                return NotificationCategory(
                    id: questionCategory(record.id),
                    actions: asked.options.enumerated().map { NotificationAction(id: Action.option($0.offset).id, title: $0.element.label) })
            }
    }

    /// The one question a notification can answer: a single question that
    /// takes exactly one of three options or fewer, each with a label short
    /// enough to read on its button. Anything else is answered in the chat.
    static func answerable(_ interaction: Record) -> (question: String, prompt: String, options: [(value: String, label: String)])? {
        guard interaction["state"]?.string == "pending", let questions = interaction["questions"]?.array, questions.count == 1,
            let question = questions.first, question["multi"]?.bool != true, let id = question["id"]?.text,
            let prompt = question["prompt"]?.text
        else { return nil }
        let options = (question["options"]?.array ?? []).compactMap { option -> (value: String, label: String)? in
            guard let value = option["value"]?.text else { return nil }
            return (value, option["label"]?.text ?? value)
        }
        guard (1 ... optionLimit).contains(options.count), options.allSatisfy({ !$0.label.isEmpty && $0.label.count <= labelLimit }) else {
            return nil
        }
        return (id, prompt, options)
    }

    /// What a call to approve would run with, as a notification shows it:
    /// each argument on a line of its own, whole. Nil where that is more
    /// than a notification shows, when it offers only to open the chat.
    static func shown(_ approval: Record) -> String? {
        let arguments = (approval["arguments"]?.object ?? [:]).filter { $0.value != .null && $0.value != .string("") }
        let lines = arguments.sorted { $0.key < $1.key }.map { "\(propertyLabel($0.key)): \(valueWords($0.value))" }
        let text = lines.joined(separator: "\n")
        return text.count <= approvalLimit ? text : nil
    }

    // MARK: Requests

    /// A reply settled in a chat that is not on screen. One that failed
    /// says what its card in the chat says (`failed`), never the server's
    /// own text.
    ///
    /// - Parameter replies: the server takes a message under an id of its
    ///   own, so a reply typed into the notification is sent again if its
    ///   answer is lost, and never twice. Without that the notification
    ///   only opens the chat.
    static func reply(thread: ChatThread, agent: String, title: String, failed: String? = nil, replies: Bool = false) -> NotificationRequest {
        NotificationRequest(
            id: replyID(thread.id), title: agent,
            body: thread.status == "error" ? failed ?? "\(agent) couldn't finish this reply." : thread.preview ?? "Answered.",
            thread: thread.id, category: replies ? replyCategory : nil,
            userInfo: [threadKey: thread.id, nonceKey: thread.record["finishedAt"]?.string ?? ""], subtitle: title)
    }

    /// How much of a reply a card's notification says above the card: one
    /// longer is read in the chat.
    static let saidLimit = 160

    /// The id of a thread's reply notification, and of a card's.
    static func replyID(_ thread: String) -> String { prefix(thread) + "reply" }
    static func cardID(_ card: String, thread: String?) -> String { (thread.map(prefix) ?? "") + "card|\(card)" }

    /// A question, a suggestion or a call to approve is waiting on the owner.
    ///
    /// - Parameter said: what the reply that asked said. It is the one
    ///   notification of that turn, so it carries the reply where it fits.
    static func attention(record: Record, thread: String?, agent: String, title: String, said: String? = nil) -> NotificationRequest {
        var body: String
        var category: String?
        var info: [String: String] = thread.map { [threadKey: $0] } ?? [:]
        info[cardKey] = record.id
        info[versionKey] = String(record.version)
        info[nonceKey] = String(record.version)
        if record.kind == Kinds.request {
            body = "Suggested a change for you to apply or dismiss."
        } else if record.kind == Kinds.approval {
            body = "Wants your approval to run \(record["tool"]?.text ?? "a tool")."
            // Approve and Decline only where the notification shows all of
            // what would run.
            if let shown = shown(record) {
                if !shown.isEmpty { body += "\n" + shown }
                category = approvalCategory
            } else {
                body += " Open the chat to read all of what it would run with."
                category = openCategory
            }
        } else if let asked = answerable(record) {
            body = asked.prompt
            category = questionCategory(record.id)
            info[questionKey] = asked.question
            for (index, option) in asked.options.enumerated() { info[optionKey(index)] = option.value }
        } else {
            let n = record["questions"]?.array?.count ?? 1
            body = n == 1 ? "Has a question for you." : "Has \(n) questions for you."
        }
        // An action answers only in a chat it can be read in afterwards.
        if thread == nil { category = nil }
        if let said = said?.trimmingCharacters(in: .whitespacesAndNewlines), !said.isEmpty, said.count <= saidLimit { body = said + "\n" + body }
        return NotificationRequest(
            id: cardID(record.id, thread: thread), title: agent, body: body, thread: thread, category: category,
            timeSensitive: true, userInfo: info, subtitle: title)
    }

    /// What an action from a notification could not do, said in the
    /// sentence the chat says it in. Opening it opens the chat, where what
    /// was typed is kept and the card still stands.
    static func failed(thread: String, agent: String, title: String, sentence: String, kept: String) -> NotificationRequest {
        NotificationRequest(
            id: prefix(thread) + "failed", title: agent, body: "\(sentence) \(kept)", thread: thread, timeSensitive: true,
            userInfo: [threadKey: thread], subtitle: title)
    }

    /// What tells one response from every other: its notification, what
    /// that notification was about when it was shown, the action and the
    /// text. The same response heard twice acts once.
    static func stamp(_ response: NotificationResponse) -> String {
        "\(response.id)|\(response.userInfo[nonceKey] ?? "")|\(response.action)|\(response.text ?? "")"
    }
}
