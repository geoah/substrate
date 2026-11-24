import Foundation

/// The pane: the order of the day, from top to bottom. The strip, then
/// `items` in the order they are drawn, then the field. A view draws each
/// item and nothing between them: there are no headers.
public struct PanePresentation: Codable, Equatable, Sendable {
    public var strip: StripPresentation
    public var items: [PaneItem]
    public var ground: Ground
    public var composer: ComposerPresentation
    /// The answer to a question typed into the field, in place over it.
    public var answer: AnswerCard?
    /// The id of the item that leads: the one drawn in full.
    public var lead: String?
    /// Said once when nothing is in the repository to draw a day from.
    public var empty: String?

    public init(
        strip: StripPresentation, items: [PaneItem], ground: Ground, composer: ComposerPresentation = ComposerPresentation(),
        answer: AnswerCard? = nil, lead: String? = nil, empty: String? = nil
    ) {
        self.strip = strip
        self.items = items
        self.ground = ground
        self.composer = composer
        self.answer = answer
        self.lead = lead
        self.empty = empty
    }
}

/// One thing in the timeline, in the order of the day.
public enum PaneItem: Codable, Equatable, Sendable, Identifiable {
    /// Always first: one line with the moon glyph, which opens in place.
    case night(NightPresentation)
    /// What is done, folded into one line with a count.
    case earlier(EarlierPresentation)
    /// What came in since the last look, directly above the item that leads.
    case happening(HappeningPresentation)
    /// A row of a free stretch: a task, a routine, the suggested tasks, the
    /// plan, what was not logged yesterday, something urgent, the wrap. The
    /// one whose `state` is `.now` is drawn in full; the rest are lines
    /// that open in place, or open a page where `trailing` says so. The
    /// saved plan's row stands under what is done, and never leads.
    case row(Row)
    /// A meeting on its own line; the one that leads is drawn in full. Two
    /// that share time are two lines tied to each other (`MeetingRow.tie`).
    case meeting(MeetingRow)
    /// What waits while a meeting leads: "The vendor check waits until you
    /// are out." It opens in place to the rows it folds (`ParkedLine.rows`).
    case parked(ParkedLine)
    /// The assistant asks where something flagged High goes when it will
    /// not be finished.
    case unfinished(UnfinishedCard)
    /// One quiet line: "Nothing is left today.", "Still open from earlier: …".
    case note(NoteLine)

    public var id: String {
        switch self {
        case .night: "night"
        case .earlier: "earlier"
        case .happening: "happening"
        case let .row(row): "row:" + row.id
        case let .meeting(meeting): "meeting:" + meeting.id
        case let .parked(line): "parked:" + line.id
        case let .unfinished(card): "unfinished:" + card.id
        case let .note(note): "note:" + note.id
        }
    }
}

public struct ParkedLine: Codable, Equatable, Sendable, Identifiable {
    public var id: String
    /// The things that wait, in bold: "The vendor check and Lin's score".
    public var what: String
    /// "waits until you are out." / "wait until you are out."
    public var rest: String
    /// The rows the line folds, in the order of the day. The line opens in
    /// place and they are drawn under it as rows of the day are, so a
    /// routine can still be logged and a task ticked before the meeting.
    /// Empty where it names one thing that is not a row of its own.
    public var rows: [Row]

    public init(id: String, what: String, rest: String, rows: [Row] = []) {
        self.id = id
        self.what = what
        self.rest = rest
        self.rows = rows
    }
}

public struct NoteLine: Codable, Equatable, Hashable, Sendable, Identifiable {
    public var id: String
    public var text: String
    /// A way back to what the line names: "Open".
    public var button: ButtonPresentation?

    public init(id: String, text: String, button: ButtonPresentation? = nil) {
        self.id = id
        self.text = text
        self.button = button
    }
}

// MARK: The strip

/// The strip on top of the day: the projects the owner has flagged, in flag
/// order, in two columns on a phone. Its two actions are buttons.
public struct StripPresentation: Codable, Equatable, Sendable {
    /// "Priorities".
    public var title: String
    /// "Thursday 19 November", and "Thu 19 Nov" for a narrow phone.
    public var date: String
    public var dateShort: String
    public var entries: [StripEntry]
    /// "All projects", which opens the list of projects.
    public var allProjects: ButtonPresentation
    /// The edit icon button, which opens the priorities page.
    public var edit: ButtonPresentation
    /// Said in place of the entries when no project is flagged.
    public var empty: String?

    public init(
        title: String = "Priorities", date: String, dateShort: String, entries: [StripEntry],
        allProjects: ButtonPresentation = ButtonPresentation("All projects", role: .quiet, opens: .projects),
        edit: ButtonPresentation = ButtonPresentation("Edit priorities", glyph: .edit, iconOnly: true, role: .quiet, opens: .priorities),
        empty: String? = nil
    ) {
        self.title = title
        self.date = date
        self.dateShort = dateShort
        self.entries = entries
        self.allProjects = allProjects
        self.edit = edit
        self.empty = empty
    }
}

/// One priority in the strip: its stripe, its tile, its name, and under the
/// name how today stands on it. Nothing sits on the tile.
public struct StripEntry: Codable, Equatable, Hashable, Sendable, Identifiable {
    public var id: String
    public var tile: Tile
    public var name: String
    /// How today stands: "1 of 2 done". Absent when `suggestion` is said.
    public var line: String?
    /// What the assistant suggests, in its hue with the sparkles glyph: "to
    /// Low" on the project's own entry, "add as High" on an entry of its own
    /// (then the stripe is hollow and the tile outlined).
    public var suggestion: String?
    public var stripe: Stripe
    /// The project's page, or the priorities page for a suggested addition.
    public var opens: TodayRoute

    public init(id: String, tile: Tile, name: String, line: String?, suggestion: String? = nil, stripe: Stripe, opens: TodayRoute) {
        self.id = id
        self.tile = tile
        self.name = name
        self.line = line
        self.suggestion = suggestion
        self.stripe = stripe
        self.opens = opens
    }
}

// MARK: Rows

/// One row of the day. Closed it is a line: its glyph, its title, one line,
/// its chips, and what ends it (`trailing`). Open, the same head with
/// `body` under it. The row that leads (`state == .now`) is drawn open and
/// larger, with no close button.
public struct Row: Codable, Equatable, Sendable, Identifiable {
    public enum State: String, Codable, Hashable, Sendable {
        /// The current item, drawn in full.
        case now
        /// After it in the same stretch.
        case next
        /// In a stretch that has not started.
        case later
        /// Open, from a stretch that has ended.
        case carried
        /// A meeting is about to start: it waits until the owner is out.
        case parked
        /// Finished today, done or skipped: a closed row of what is done
        /// ("Earlier today"), crossed out, whose filled tick takes it back.
        case done
    }

    /// What ends a closed row.
    public enum Trailing: Codable, Equatable, Hashable, Sendable {
        /// A task or a routine: the tick icon button. Open, it finishes the
        /// thing (a view counts down first, `Finishing`); filled, on a row
        /// that is done, it takes that back at once. A task's flag is not
        /// here: it is changed from the opened row.
        case tick(ButtonPresentation)
        /// It opens a page: a right chevron, and a tap on the row opens it.
        case page(TodayRoute)
        /// Everything else opens in place: a down chevron.
        case opens
    }

    public var id: String
    public var glyph: Glyph
    public var title: String
    /// Why it matters now, or what it holds.
    public var line: String?
    /// The opening words of what the open row shows in full under its
    /// head (a task's description): a closed row's line where it has no
    /// other, and not drawn on an open one, which would say them twice.
    public var opening: String?
    public var stripe: Stripe
    public var state: State
    public var chips: Chips
    public var trailing: Trailing
    public var body: RowBody

    public init(
        id: String, glyph: Glyph, title: String, line: String? = nil, opening: String? = nil, stripe: Stripe = .none, state: State,
        chips: Chips = .none, trailing: Trailing = .opens, body: RowBody
    ) {
        self.id = id
        self.glyph = glyph
        self.title = title
        self.line = line
        self.opening = opening
        self.stripe = stripe
        self.state = state
        self.chips = chips
        self.trailing = trailing
        self.body = body
    }
}

/// What an open row holds under its head, by what the row is.
public enum RowBody: Codable, Equatable, Sendable {
    /// A routine, or a group of them at one time.
    case routine(RoutineCard)
    /// What was not logged yesterday: each routine with its option list,
    /// acting on a tap.
    case yesterday(YesterdayCard)
    /// The suggested tasks: one row that opens them as a page.
    case suggested(PageRowCard)
    /// One suggested task that arrived during the day, with its own Save.
    case heard(SuggestedItem)
    /// "Plan the day", "Today's plan": one row that opens the plan as a page.
    case plan(PageRowCard)
    /// An overlap the plan left open, back before it is needed.
    case overlap(OverlapCard)
    /// What cannot wait.
    case urgent(UrgentCard)
    /// A task.
    case task(TaskCard)
    /// The end of the day: a save card.
    case wrap(WrapCard)
}

// MARK: The field

/// "Add or ask", fixed at the bottom of the day and of every page. What is
/// typed is the view's until it is sent with `TodayCommand.addOrAsk`, and
/// all of it goes to the assistant.
public struct ComposerPresentation: Codable, Equatable, Hashable, Sendable {
    /// "Add or ask".
    public var placeholder: String
    /// Why the last add was not kept on this device, said beside the field.
    public var notKept: String?
    /// The row of the task the last add made, by its `RowPresentation.id`:
    /// a view scrolls to it.
    public var added: String?

    public init(placeholder: String = "Add or ask", notKept: String? = nil, added: String? = nil) {
        self.placeholder = placeholder
        self.notKept = notKept
        self.added = added
    }
}

/// One exchange with the assistant: what was asked, and its answer as it
/// arrives.
public struct AnswerCard: Codable, Equatable, Hashable, Sendable, Identifiable {
    public enum State: String, Codable, Hashable, Sendable {
        /// Sent, nothing back yet.
        case waiting
        /// The answer is arriving.
        case writing
        case done
        /// It could not be asked or answered; `text` says why, in place.
        case failed
    }

    public var id: String
    /// "You asked Moss", "You said".
    public var heading: String
    public var question: String
    /// The answer so far.
    public var text: String
    public var state: State
    /// What it was answered from, where the answer names records.
    public var sources: [String]
    /// "Close", once it is done or failed; "Stop" while it is written.
    public var button: ButtonPresentation?
    /// "Add as a task": what keeps the words where the assistant could not
    /// take them, on an exchange about the day that failed.
    public var keep: ButtonPresentation?

    public init(
        id: String, heading: String, question: String, text: String, state: State, sources: [String] = [], button: ButtonPresentation? = nil,
        keep: ButtonPresentation? = nil
    ) {
        self.id = id
        self.heading = heading
        self.question = question
        self.text = text
        self.state = state
        self.sources = sources
        self.button = button
        self.keep = keep
    }
}

/// The assistant's box, about one thing: what it can be asked here as
/// buttons, the exchanges so far, and a field with a send button. It
/// prepares and waits: a draft is shown and nothing leaves until Send.
public struct MossBox: Codable, Equatable, Hashable, Sendable {
    public var subject: MossSubject
    /// "Moss, about the vendor check".
    public var title: String
    /// What it can do here: each a secondary button that asks it.
    public var asks: [ButtonPresentation]
    public var exchanges: [AnswerCard]
    /// "Ask or tell Moss about the vendor check".
    public var placeholder: String
    /// Said in place of the field when it cannot be reached.
    public var unreachable: String?

    public init(
        subject: MossSubject, title: String, asks: [ButtonPresentation] = [], exchanges: [AnswerCard] = [], placeholder: String,
        unreachable: String? = nil
    ) {
        self.subject = subject
        self.title = title
        self.asks = asks
        self.exchanges = exchanges
        self.placeholder = placeholder
        self.unreachable = unreachable
    }
}
