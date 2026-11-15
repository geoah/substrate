import Foundation
import SubstrateCore
import SubstrateDevice

/// A repository's chats as a server would answer for them, made up and the
/// same every time: what a measurement streams into and a gallery draws.
/// Nothing here is a person's; the names are `example.com`'s.
public enum ChatFixtures {
    public static let origin = URL(string: "https://substrate.example.com")!
    public static let repository = "ada.example.com"
    public static let agent = "substrate.reamde.dev/assistant/moss"
    /// The chat a 16 KB reply is streamed into: 200 turns.
    public static let long = "t-long"
    /// The chat at the window's edge: 500 rows.
    public static let full = "t-full"

    // MARK: Replies

    /// A reply with every block the transcript draws: headings, a paragraph
    /// with emphasis, code and a link, a nested list, a task list, an
    /// ordered list, a table, a fenced block, a quote and a rule.
    public static let reply = """
        ## Where the week stands

        You have **three things due** before Friday, and one of them, the *Q4 budget*, is \
        blocked on numbers from `finance/actuals`. The plan is in \
        [the planning notes](https://example.com/notes/planning), last changed yesterday.

        ### What is due

        - **Q4 budget**: waiting on actuals
          - the travel line is still an estimate
          - headcount is final
        - Offsite agenda: two sessions have no owner
        - Reading list for the reading group

        ### What I would do first

        1. Ask for the actuals today, since everything else waits on them.
        2. Give the two open sessions an owner.
        3. Send the reading list as it is.

        - [x] Draft the budget summary
        - [ ] Confirm the venue

        | Item | Owner | Due | State |
        | :--- | :---: | ---: | --- |
        | Q4 budget | Ada | Thu | blocked |
        | Offsite agenda | Grace | Fri | open |
        | Reading list | Ada | Fri | ready |

        The query that found them:

        ```swift
        let due = try await client.list(
            ListQuery(kinds: ["~/tasks/task"], properties: ["due": ["lte": friday]], first: 50))
        for task in due.records where task["done"]?.bool != true {
            print(task.id, task["title"]?.string ?? "")
        }
        ```

        > Nothing here is overdue yet. The budget becomes so on Friday morning \
        if the actuals have not arrived.

        ---

        Say the word and I will draft the message asking for the actuals.
        """

    /// A reply at least `bytes` long: `reply`, again and again under
    /// numbered headings, so every block kind recurs through its length.
    public static func reply(bytes: Int) -> String {
        var out = reply
        var part = 2
        while out.utf8.count < bytes {
            out += "\n\n# Part \(part)\n\n" + reply
            part += 1
        }
        return out
    }

    /// A short answer, for a turn above the one measured: a few sentences
    /// and a list, different for each `index`.
    public static func answer(_ index: Int) -> String {
        let topics = ["the budget", "the offsite", "the reading list", "the hiring plan", "the roadmap", "the retrospective"]
        let topic = topics[index % topics.count]
        return """
            Here is where **\(topic)** stands as of turn \(index).

            - The first point about \(topic), with `a/path` in it.
            - The second, which runs a little longer so the line wraps on a phone and a list item takes two lines.
            - The third.

            Ask me to go deeper on any of them.
            """
    }

    // MARK: Records, as JSON

    public static func agents() -> String {
        page([
            record(
                id: agent, kind: Kinds.agent, version: 1, at: 0,
                properties: #"{"description":"Your assistant.","provider":{"ref":"\#(Kinds.provider)/anthropic"},"model":"claude-fable-5-1"}"#)
        ])
    }

    public static func providers() -> String {
        page([record(id: "anthropic", kind: Kinds.provider, version: 1, at: 0, properties: #"{"wire":"anthropic","apiKey":"set"}"#)])
    }

    /// The chat list, newest first: the two measured chats, then others up
    /// to `count`, each a settled chat with its last reply's start.
    public static func threads(count: Int = 100) -> String {
        page(([long, full] + (2 ..< max(count, 2)).map { "t-\($0)" }).map { thread($0) })
    }

    /// One chat's row: its subject and its age follow from its id, so the
    /// list and a read of the one thread say the same.
    public static func thread(_ id: String, version: Int = 3, status: String = "ok") -> String {
        let index = id == long ? 0 : id == full ? 1 : Int(id.dropFirst(2)) ?? 2
        let subject =
            switch (id, index) {
            case ("t-ask", _): "Dentist check-up"
            case ("t-approve", _): "Lunch with Sam"
            case ("t-live", _): "The balcony"
            case (_, 0): "The week ahead"
            case (_, 1): "Planning the offsite"
            default: "Chat \(index) about \(index % 2 == 0 ? "tasks" : "notes")"
            }
        let age = index < 2 ? (index + 1) * 60 : index * 37 * 60
        return record(
            id: id, kind: Kinds.thread, version: version, at: -age,
            properties:
                #"{"subject":\#(quoted(subject)),"mode":"chat","status":"\#(status)","agent":{"ref":"\#(Kinds.agent)/\#(agent)"},"finishedAt":"\#(stamp(-age))","lastReply":"Here is where it stands."}"#
        )
    }

    /// A thread's rows as the transcript read answers: newest first, `rows`
    /// of them, a question and its answer in turn.
    public static func transcript(_ thread: String, rows: Int) -> String {
        var out: [String] = []
        for turn in 0 ..< rows {
            let user = turn % 2 == 0
            let content = user ? "What about item \(turn / 2 + 1)? Keep it short." : answer(turn / 2)
            out.append(
                record(
                    id: "\(thread)-m\(turn)", kind: Kinds.message, version: 1, at: -(rows - turn) * 30,
                    properties:
                        #"{"role":"\#(user ? "user" : "assistant")","content":\#(quoted(content)),"turn":\#(turn),"thread":{"ref":"\#(Kinds.thread)/\#(thread)"}}"#
                ))
        }
        return page(out.reversed())
    }

    /// The rows after a turn that sent `message` and got `reply`.
    public static func transcript(_ thread: String, rows: Int, message: String, reply: String) -> String {
        let before = transcript(thread, rows: rows)
        let sent = record(
            id: "\(thread)-m\(rows)", kind: Kinds.message, version: 1, at: 0,
            properties: #"{"role":"user","content":\#(quoted(message)),"turn":\#(rows),"thread":{"ref":"\#(Kinds.thread)/\#(thread)"}}"#)
        let answered = record(
            id: "\(thread)-m\(rows + 1)", kind: Kinds.message, version: 1, at: 0,
            properties: #"{"role":"assistant","content":\#(quoted(reply)),"turn":\#(rows + 1),"thread":{"ref":"\#(Kinds.thread)/\#(thread)"}}"#)
        // The page is newest first.
        let held = before.dropFirst(#"{"head":10,"generation":"g","records":["#.count)
        return #"{"head":10,"generation":"g","records":["# + answered + "," + sent + (rows > 0 ? "," : "") + held
    }

    // MARK: A stream

    /// A turn's stream as the server writes it: the thread, `text` in pieces
    /// of `piece` characters, then the result.
    public static func stream(thread: String, text: String, turn: Int, piece: Int = 12) -> [String] {
        var lines = [#"{"kind":"thread","thread":"\#(thread)"}"#]
        var rest = Substring(text)
        while !rest.isEmpty {
            let head = rest.prefix(piece)
            rest = rest.dropFirst(piece)
            lines.append(#"{"kind":"delta","text":\#(quoted(String(head))),"turn":\#(turn)}"#)
        }
        lines.append(#"{"kind":"done","result":{"reply":\#(quoted(String(text.prefix(80)))),"thread":"\#(thread)","status":"ok","turns":\#(turn + 1)}}"#)
        return lines
    }

    // MARK: A run, line by line

    /// One run as a server at `agents` 5 streams it, with the rows the
    /// thread holds as it goes: the model thinks, says it will look, calls a
    /// tool, and answers in a second reply. Each piece of text says where
    /// it belongs (`at`), so an attach that replays the lines from the first
    /// writes over what a client holds.
    public struct Run: Sendable {
        public var message: String
        public var lines: [String]
        /// The transcript's page once `after` lines of the stream are out.
        public var rows: [(after: Int, page: String)]
        /// The model's thinking, and its two replies, whole.
        public var thinking: String
        public var first: String
        public var second: String
    }

    public static func run(thread: String) -> Run {
        let message = "What needs doing on the balcony?"
        let thinking = [
            "The balcony list is short this week. ", "The basil was watered on Monday and wilts after three dry days, so it is due. ",
            "The mint has outgrown its pot: its roots show at the drain holes. ",
            "The tomatoes were fed last week and can wait. I should look the list up before I say so.",
        ]
        let first = "I'll check the list.\n\n"
        let second = [
            "Two things need doing:\n\n", "- **Water the basil**: it has gone three days without\n", "- Repot the mint, whose roots show\n\n",
            "The tomatoes can wait until the weekend. ", "They were fed last week, and their soil is still damp a finger deep.\n\n",
            "The rosemary and the thyme want nothing before spring: both prefer dry feet, ",
            "and a pot left out of the rain is care enough.\n\n", "If you only have ten minutes, water the basil and move the mint into the shade. ",
            "Repotting takes longer and is kinder done in the evening, when the plant has the night to settle.\n\n",
            "Say the word and I'll add both to your tasks.",
        ]
        var lines = [#"{"kind":"thread","thread":"\#(thread)"}"#]
        func pieces(_ kind: String, _ texts: [String], turn: Int) {
            var at = 0
            for text in texts {
                lines.append(#"{"kind":"\#(kind)","text":\#(quoted(text)),"turn":\#(turn),"at":\#(at)}"#)
                at += text.unicodeScalars.count
            }
        }
        pieces("reasoning", thinking, turn: 1)
        pieces("delta", [first], turn: 1)
        let call = #""id":"c1","tool":"query","callable":"substrate.reamde.dev/core/function/substrate.reamde.dev/core/query","turn":2"#
        lines.append(#"{"kind":"toolStarted",\#(call),"args":"{\"q\":\"balcony\"}"}"#)
        let found = lines.count
        lines.append(
            #"{"kind":"toolFinished",\#(call),"ok":true,"output":"{\"records\":[{\"kind\":\"~/tasks/task\",\"id\":\"basil\",\"title\":\"Water the basil\"},{\"kind\":\"~/tasks/task\",\"id\":\"mint\",\"title\":\"Repot the mint\"}],\"total\":2}"}"#
        )
        pieces("delta", second, turn: 3)
        lines.append(#"{"kind":"done","result":{"reply":"Two things need doing.","thread":"\#(thread)","status":"ok","turns":4}}"#)

        func row(_ ordinal: Int, _ properties: String) -> String {
            record(
                id: "\(thread)-m\(ordinal)", kind: Kinds.message, version: 1, at: 0,
                properties: #"{\#(properties),"turn":\#(ordinal),"thread":{"ref":"\#(Kinds.thread)/\#(thread)"}}"#)
        }
        let sent = row(0, #""role":"user","content":\#(quoted(message))"#)
        let asked = row(
            1,
            #""role":"assistant","content":\#(quoted(first)),"reasoning":\#(quoted(thinking.joined())),"reasoningMillis":4000,"toolCalls":[{"id":"c1","name":"query","callable":"substrate.reamde.dev/core/function/substrate.reamde.dev/core/query","arguments":"{\"q\":\"balcony\"}"}]"#
        )
        let answered = row(
            2,
            #""role":"tool","toolCallId":"c1","name":"query","ok":true,"content":"{\"records\":[{\"kind\":\"~/tasks/task\",\"id\":\"basil\",\"title\":\"Water the basil\"},{\"kind\":\"~/tasks/task\",\"id\":\"mint\",\"title\":\"Repot the mint\"}],\"total\":2}""#
        )
        let reply = row(3, #""role":"assistant","content":\#(quoted(second.joined()))"#)
        return Run(
            message: message, lines: lines,
            // Newest first, as the read answers: the message once the turn
            // is taken, the first reply's row once its call is out, the
            // call's once it is answered, the second reply's at the end.
            rows: [
                (1, page([sent])), (found, page([asked, sent])), (found + 1, page([answered, asked, sent])),
                (lines.count, page([reply, answered, asked, sent])),
            ],
            thinking: thinking.joined(), first: first, second: second.joined())
    }

    // MARK: Answering

    /// A chat whose reply asked the person something that waits on them:
    /// the rows while it waits and once it ended, and the card's record at
    /// each.
    public struct Asked: Sendable {
        public var thread: String
        public var card: CardReference
        /// The record's path under `/api/v1/`.
        public var path: String
        public var pending: String
        public var resolved: String
        /// The transcript's page while it waits, and once it ended.
        public var rows: String
        public var rowsResolved: String
    }

    private static func row(_ thread: String, _ ordinal: Int, _ properties: String) -> String {
        record(
            id: "\(thread)-m\(ordinal)", kind: Kinds.message, version: 1, at: -(60 - ordinal * 5),
            properties: #"{\#(properties),"turn":\#(ordinal),"thread":{"ref":"\#(Kinds.thread)/\#(thread)"}}"#)
    }

    /// The assistant asked one question of three options, and waits.
    public static func question(thread: String = "t-ask") -> Asked {
        let id = "01k9z3v8q4c0d5m2r7w1x6y8ab"
        let ask = #""id":"c1","name":"ask","callable":"\#(Kinds.function)/substrate.reamde.dev/core/ask""#
        let questions =
            #"[{"id":"day","prompt":"Which morning suits you for the dentist?","options":[{"value":"tue","label":"Tuesday at 9:00"},{"value":"wed","label":"Wednesday at 8:30"},{"value":"thu","label":"Thursday at 10:15","description":"The only slot with Dr. Okafor"}]}]"#
        let sent = row(thread, 0, #""role":"user","content":"Book me a check-up at the dentist next week.""#)
        let asked = row(
            thread, 1,
            #""role":"assistant","content":"Three mornings are free next week. One thing before I book it.","toolCalls":[{\#(ask),"arguments":"{}"}]"#)
        let landed = row(
            thread, 2,
            #""role":"tool","toolCallId":"c1","name":"ask","ok":true,"content":"{\"id\":\"\#(id)\"}","changes":[{"seq":7,"op":"put","kind":"\#(Kinds.interaction)","id":"\#(id)"}]"#
        )
        let said = row(
            thread, 3,
            #""role":"system","content":"{\"event\":\"interactionAnswered\",\"interaction\":\"\#(Kinds.interaction)/\#(id)\",\"answers\":[{\"question\":\"day\",\"selected\":[\"thu\"]}]}""#
        )
        let booked = row(thread, 4, #""role":"assistant","content":"Booked: Thursday at 10:15 with Dr. Okafor. It is in your calendar.""#)
        func card(_ state: String, _ more: String, version: Int) -> String {
            record(
                id: id, kind: Kinds.interaction, version: version, at: -30,
                properties: #"{"thread":{"ref":"\#(Kinds.thread)/\#(thread)"},"state":"\#(state)","questions":\#(questions)\#(more)}"#)
        }
        return Asked(
            thread: thread, card: .question(id), path: "\(Kinds.interaction)/\(id)", pending: card("pending", "", version: 1),
            resolved: card("answered", #","answers":[{"question":"day","selected":["thu"]}],"resolvedAt":"\#(stamp(-20))""#, version: 2),
            rows: page([landed, asked, sent]), rowsResolved: page([booked, said, landed, asked, sent]))
    }

    /// The assistant wants to send a mail, which waits for approval.
    public static func approval(thread: String = "t-approve") -> Asked {
        let id = "01h2xcejqtf2nbrexx3vqjhp42"
        let function = "\(Kinds.function)/providers.substrate.reamde.dev/google/sendmail"
        let arguments = #"{"to":["sam@example.com"],"subject":"Lunch on Friday","body":"Friday at 12:30 works for me. See you at the usual place."}"#
        let sent = row(thread, 0, #""role":"user","content":"Tell Sam that Friday works for lunch.""#)
        let asked = row(
            thread, 1,
            #""role":"assistant","content":"I've written it. It goes once you approve it.","toolCalls":[{"id":"c2","name":"sendmail","callable":"\#(function)","arguments":\#(quoted(arguments))}]"#
        )
        let landed = row(
            thread, 2,
            #""role":"tool","toolCallId":"c2","name":"sendmail","ok":true,"content":"{\"output\":{\"approval\":\"\#(id)\",\"status\":\"waiting for the owner's approval\"}}","changes":[{"seq":9,"op":"put","kind":"\#(Kinds.approval)","id":"\#(id)"}]"#
        )
        let said = row(
            thread, 3,
            #""role":"system","content":"{\"event\":\"approvalDecided\",\"approval\":\"\#(Kinds.approval)/\#(id)\",\"decision\":\"approved\",\"tool\":\"sendmail\",\"ok\":true}""#
        )
        let done = row(thread, 4, #""role":"assistant","content":"Sent. Sam has your reply.""#)
        func card(_ state: String, _ more: String, version: Int) -> String {
            record(
                id: id, kind: Kinds.approval, version: version, at: -30,
                properties:
                    #"{"thread":{"ref":"\#(Kinds.thread)/\#(thread)"},"state":"\#(state)","agent":{"ref":"\#(Kinds.agent)/\#(agent)"},"function":{"ref":"\#(function)"},"tool":"sendmail","arguments":\#(arguments)\#(more)}"#
            )
        }
        return Asked(
            thread: thread, card: .approval(id), path: "\(Kinds.approval)/\(id)", pending: card("pending", "", version: 1),
            resolved: card("done", #","decidedAt":"\#(stamp(-20))""#, version: 3),
            rows: page([landed, asked, sent]), rowsResolved: page([done, said, landed, asked, sent]))
    }

    /// A run a message joins while it is written (`agents` 6): the reply
    /// starts, the message is queued, the reply's tool runs, the next call
    /// takes the message and answers it. `stopped` ends the run before
    /// that, with the message unread. `$Q` in a line stands for the
    /// message's id, which is the sender's to mint.
    public struct Queued: Sendable {
        public var message: String
        public var joining: String
        public var lines: [String]
        /// How many lines are out when the message joins, once it is said
        /// queued, and once it is said taken.
        public var joins: Int
        public var queued: Int
        public var taken: Int
        public var rows: [(after: Int, page: String)]
    }

    public static func queued(thread: String, stopped: Bool = false, id: String = "$Q") -> Queued {
        let message = "What needs doing on the balcony?"
        let joining = "Only what I can do before Friday."
        var lines = [#"{"kind":"thread","thread":"\#(thread)"}"#]
        func pieces(_ texts: [String], turn: Int) {
            var at = 0
            for text in texts {
                lines.append(#"{"kind":"delta","text":\#(quoted(text)),"turn":\#(turn),"at":\#(at)}"#)
                at += text.unicodeScalars.count
            }
        }
        let first = ["I'll look at the list. ", "The basil and the mint both need a hand this week."]
        pieces(first, turn: 1)
        let joins = lines.count
        lines.append(#"{"kind":"messageQueued","id":"\#(id)","turn":2,"text":\#(quoted(joining))}"#)
        let queued = lines.count
        let call = #""id":"c1","tool":"query","callable":"\#(Kinds.function)/substrate.reamde.dev/core/query","turn":3"#
        var second: [String] = []
        if stopped {
            lines.append(#"{"kind":"done","result":{"reply":"","thread":"\#(thread)","status":"stopped","cause":"requested","turns":2}}"#)
        } else {
            lines.append(#"{"kind":"toolStarted",\#(call),"args":"{\"q\":\"balcony\"}"}"#)
            lines.append(
                #"{"kind":"toolFinished",\#(call),"ok":true,"output":"{\"records\":[{\"kind\":\"~/tasks/task\",\"id\":\"basil\",\"title\":\"Water the basil\"},{\"kind\":\"~/tasks/task\",\"id\":\"mint\",\"title\":\"Repot the mint\"}],\"total\":2}"}"#
            )
            lines.append(#"{"kind":"messageTaken","id":"\#(id)","turn":2}"#)
        }
        let taken = lines.count
        if !stopped {
            second = ["Before Friday, then: ", "water the basil today, ", "and leave the mint for the weekend."]
            pieces(second, turn: 4)
            lines.append(#"{"kind":"done","result":{"reply":"Before Friday, then.","thread":"\#(thread)","status":"ok","turns":5}}"#)
        }
        func row(_ ordinal: Int, id row: String? = nil, _ properties: String) -> String {
            record(
                id: row ?? "\(thread)-m\(ordinal)", kind: Kinds.message, version: 1, at: 0,
                properties: #"{\#(properties),"turn":\#(ordinal),"thread":{"ref":"\#(Kinds.thread)/\#(thread)"}}"#)
        }
        let sent = row(0, #""role":"user","content":\#(quoted(message))"#)
        let reply = row(
            1,
            stopped
                ? #""role":"assistant","content":\#(quoted(first.joined())),"cutBy":"stop""#
                : #""role":"assistant","content":\#(quoted(first.joined())),"toolCalls":[{"id":"c1","name":"query","callable":"\#(Kinds.function)/substrate.reamde.dev/core/query","arguments":"{\"q\":\"balcony\"}"}]"#
        )
        let joined = row(2, id: id, #""role":"user","content":\#(quoted(joining))"#)
        let found = row(
            3,
            #""role":"tool","toolCallId":"c1","name":"query","ok":true,"content":"{\"records\":[{\"kind\":\"~/tasks/task\",\"id\":\"basil\",\"title\":\"Water the basil\"},{\"kind\":\"~/tasks/task\",\"id\":\"mint\",\"title\":\"Repot the mint\"}],\"total\":2}""#
        )
        let answer = row(4, #""role":"assistant","content":\#(quoted(second.joined()))"#)
        return Queued(
            message: message, joining: joining, lines: lines, joins: joins, queued: queued, taken: taken,
            rows: [(1, page([sent])), (queued, page([joined, sent]))]
                + (stopped ? [(lines.count, page([joined, reply, sent]))] : [(lines.count, page([answer, found, joined, reply, sent]))]))
    }

    /// The notifications the app posts of those chats, with the categories
    /// their actions come from: a reply that takes text, a question that
    /// takes its options, and an approval that shows all of what would
    /// run. Made by what makes the app's own.
    public static func notifications() -> (categories: [NotificationCategory], requests: [NotificationRequest]) {
        func parsed(_ json: String) -> Record? { JSONValue.parse(json).flatMap { try? ValueCoding.decode(Record.self, from: $0) } }
        let asked = question()
        let sending = approval()
        guard let interaction = parsed(asked.pending), let call = parsed(sending.pending),
            let settled = parsed(thread("t-live")).map(ChatThread.init(record:))
        else { return ([], []) }
        var reply = settled.record
        reply.properties["lastReply"] = "Before Friday, then: water the basil today, and leave the mint for the weekend."
        return (
            ChatNotifications.categories(for: [interaction]),
            [
                ChatNotifications.reply(thread: ChatThread(record: reply), agent: "Moss", title: "The balcony", replies: true),
                ChatNotifications.attention(record: interaction, thread: asked.thread, agent: "Moss", title: "Dentist check-up"),
                ChatNotifications.attention(record: call, thread: sending.thread, agent: "Moss", title: "Lunch with Sam"),
            ]
        )
    }

    // MARK: JSON

    public static func page(_ records: [String]) -> String {
        #"{"head":10,"generation":"g","records":["# + records.joined(separator: ",") + "]}"
    }

    static func record(id: String, kind: String, version: Int, at seconds: Int, properties: String) -> String {
        #"{"id":\#(quoted(id)),"kind":"\#(kind)","version":\#(version),"createdAt":"\#(stamp(seconds))","updatedAt":"\#(stamp(seconds))","properties":\#(properties)}"#
    }

    /// When the fixture's rows were written: the instant the process first
    /// asked, so the list's stamps read "now" and "09:41" whenever it runs.
    public static let base = Date()

    static func stamp(_ seconds: Int) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.string(from: base.addingTimeInterval(TimeInterval(seconds)))
    }

    public static func quoted(_ text: String) -> String {
        var out = "\""
        for scalar in text.unicodeScalars {
            switch scalar {
            case "\"": out += "\\\""
            case "\\": out += "\\\\"
            case "\n": out += "\\n"
            case "\r": out += "\\r"
            case "\t": out += "\\t"
            default:
                if scalar.value < 0x20 {
                    out += String(format: "\\u%04x", scalar.value)
                } else {
                    out.unicodeScalars.append(scalar)
                }
            }
        }
        return out + "\""
    }
}
