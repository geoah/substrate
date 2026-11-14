import Foundation

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
            switch index {
            case 0: "The week ahead"
            case 1: "Planning the offsite"
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
