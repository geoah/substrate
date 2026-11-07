import SubstrateCore

/// One turn of the transcript, by what it shows. Stable ids across the
/// live-to-stored handover, so a view redraws only what moved.
public struct TurnPresentation: Codable, Hashable, Sendable, Identifiable {
    public enum Body: Codable, Hashable, Sendable {
        case user(UserTurn)
        /// A triggered thread's opening: "When <record> changed".
        case trigger(String)
        case assistant(AssistantTurn)
        case summary(SummaryTurn)
        /// A suggestion the owner decided: the line, then its card.
        case decision(OutcomeLine, suggestion: String)
        /// Questions the owner answered or dismissed.
        case answer(OutcomeLine)
        /// A call that ran in the background ended.
        case jobEnded(OutcomeLine)
        case system(String)
    }

    public var id: String
    public var body: Body
}

public struct UserTurn: Codable, Hashable, Sendable {
    public var text: String
    /// Not yet confirmed by the server.
    public var pending: Bool
    /// The records the message is about.
    public var about: [RecordReference]
    public var aboutLabel: String
}

public struct AssistantTurn: Codable, Hashable, Sendable {
    /// The stream is writing it.
    public var live: Bool
    public var reasoning: ReasoningPresentation?
    /// Markdown.
    public var text: String?
    /// The `kind/id` paths the reply links that name no record: their links
    /// read as their text alone.
    public var missing: [String]
    /// The stream has said nothing of it yet.
    public var typing: Bool
    /// The records a settled reply links, as pills under it.
    public var linked: [RecordReference]
    public var linkedLabel: String
    /// The web pages a finished reply links, in the order it cites them.
    public var sources: SourceList?
    public var tools: [ToolRowPresentation]
}

/// "Thinking…" while the model reasons, then "Thought for 6 s".
public struct ReasoningPresentation: Codable, Hashable, Sendable {
    public var label: String
    public var thinking: Bool
    public var text: String
}

public struct SummaryTurn: Codable, Hashable, Sendable {
    public var label: String
    public var content: String
}

/// What the owner did with a card, in the tool rows' shape.
public struct OutcomeLine: Codable, Hashable, Sendable {
    public var text: String
    public var done: Bool
}

/// One tool call in the console's words, opening to what it found, changed
/// or failed on, and what it left: a card or the changes it wrote.
public struct ToolRowPresentation: Codable, Hashable, Sendable, Identifiable {
    public enum Status: String, Codable, Hashable, Sendable {
        case running, done, failed
        /// It answered with a handle, and its work runs on in the background.
        case working
        /// The owner's policy held its write for review: the change waits
        /// on them in the suggestion under it.
        case held
    }

    public enum Attachment: Codable, Hashable, Sendable {
        case suggestion(String)
        case questions(String)
        case change(ChangePanelPresentation)
    }

    public var id: String
    public var status: Status
    /// Said beside a status the mark alone does not: "Working in the
    /// background", "Waiting for you".
    public var statusLabel: String?
    /// The line, naming the one record the call worked on by the heading
    /// its answer or the chat's lookups carried, else by its kind.
    public var summary: String
    /// That record, where nothing here carried its heading: the view reads
    /// it, and says the line with it.
    public var unnamed: UnnamedSubject?
    public var details: [ToolDetailPresentation]
    /// The tool, its arguments and its output, under Details.
    public var raw: [RawLine]
    /// The pages a result of a known shape found or read, under the line
    /// whether or not it is open.
    public var sources: SourceList?
    /// The records a call found or changed, under the line whether or not
    /// it is open: several, or a function's own changes.
    public var chips: [RecordChips]
    /// A sub-agent's own conversation, which its call ran in.
    public var thread: ThreadLink?
    public var attachment: Attachment?
}

/// The one record a tool line names, read for its heading.
public struct UnnamedSubject: Codable, Hashable, Sendable {
    public var record: RecordReference
    /// The line around the heading: these words, the heading quoted, then
    /// `after`.
    public var before: String
    public var after: String

    /// The line, naming the record by the heading read for it.
    public func line(titled title: String) -> String {
        ToolLine(words: "", before: before, after: after).said(title: title)
    }
}

/// Records one call worked on, as pills under its line: the first few, then
/// "and N more", which shows the rest it holds. "Saved [a] [b]" for a
/// function's own changes.
public struct RecordChips: Codable, Hashable, Sendable {
    /// How many pills show before "and N more".
    public static let first = 3

    /// Words before the pills, for a call whose line says something else.
    public var lead: String?
    /// What the list is, for a screen reader: "Records found", "Saved records".
    public var label: String
    public var records: [RecordChip]
    /// How many there were, where more came back than the list holds.
    public var total: Int

    /// "and 2 more", past the `shown` pills; nil when none are left.
    public func more(shown: Int) -> String? {
        total > shown ? "and \(total - shown) more" : nil
    }
}

/// A record as its pill, by the heading the call's answer or the chat's
/// lookups carried, else read.
public struct RecordChip: Codable, Hashable, Sendable {
    public var record: RecordReference
    public var title: String?
}

/// A thread to open in the app: "Open its conversation".
public struct ThreadLink: Codable, Hashable, Sendable {
    public var thread: String
    public var label: String
}

/// Web pages as pills: what a search found, the page a call read, the pages
/// a reply cites. Each opens outside the app.
public struct SourceList: Codable, Hashable, Sendable {
    /// Shown above the pills: "Sources" under a reply.
    public var heading: String?
    /// What the list is to a screen reader: "Results", "Sources".
    public var label: String
    public var pills: [SourcePill]
    /// Said in place of the pills when there are none: "No results."
    public var empty: String?
}

/// One web page: its number where a list cites it, the site, the page's
/// title, and what a search said of it.
public struct SourcePill: Codable, Hashable, Sendable, Identifiable {
    public var id: String { "\(index ?? 0):\(url)" }
    public var index: Int?
    /// An `http` or `https` address, and nothing else.
    public var url: String
    /// The site: the address's host without `www.`.
    public var host: String
    /// The page's title, where it says more than the host.
    public var title: String?
    /// A search hit's snippet.
    public var snippet: String?
}

public struct RawLine: Codable, Hashable, Sendable {
    public var label: String
    public var value: String
}

public enum ToolDetailPresentation: Codable, Hashable, Sendable {
    case failed(String)
    case found(String, [RecordReference])
    /// The one record a call's line names, for a call that changed it:
    /// "Changed [Garden]".
    case changed([RecordChips])
    case wrote(String)
    case reply(heading: String, text: String)
    case output(String)
}

/// What a change wrote: the record, then each value it set.
public struct ChangePanelPresentation: Codable, Hashable, Sendable {
    public enum Tint: String, Codable, Hashable, Sendable { case plain, applied, suggested }

    public var heading: ChangeHeading
    public var note: String?
    public var values: [ValueLine]
    /// "and 2 more"
    public var more: String?
    /// Struck through: a suggestion not taken.
    public var struck: Bool
    public var tint: Tint
}

/// "Changed [Groceries]", "Suggests adding a task".
public struct ChangeHeading: Codable, Hashable, Sendable {
    public enum Symbol: String, Codable, Hashable, Sendable { case create, edit, delete }
    public enum Badge: String, Codable, Hashable, Sendable { case applied, dismissed }

    public var symbol: Symbol
    public var verb: String
    /// The record as its pill, where there is one to open.
    public var record: RecordReference?
    public var recordTitle: String?
    /// The record in words, where there is no pill.
    public var words: String
    public var badge: Badge?
    public var badgeLabel: String?
}

public struct ValueLine: Codable, Hashable, Sendable, Identifiable {
    public var id: String { key }
    public var key: String
    public var label: String
    public var value: JSONValue
    /// A patch's value before, struck through.
    public var before: JSONValue?
}
