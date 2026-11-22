import Foundation

// What an open row, a meeting and the night hold: one type per card the
// design guide draws.

// MARK: A routine

/// A routine row, open: its items (names and doses, shown only inside the
/// app), one primary button, and Skip.
public struct RoutineCard: Codable, Equatable, Hashable, Sendable {
    public struct Item: Codable, Equatable, Hashable, Sendable, Identifiable {
        /// The occurrence's id.
        public var id: String
        public var name: String
        /// "200 mg", "1 capsule"; nil for a routine that is not a dose.
        public var detail: String?
        /// "Taken at 07:33.", "Skipped."; nil while it is open.
        public var logged: String?

        public init(id: String, name: String, detail: String? = nil, logged: String? = nil) {
            self.id = id
            self.name = name
            self.detail = detail
            self.logged = logged
        }
    }

    public var items: [Item]
    /// "Taken" for medication, "Done" otherwise: logs every open item.
    public var done: ButtonPresentation?
    /// "Skip": logs them as skipped.
    public var skip: ButtonPresentation?
    /// "Taken at 07:33."
    public var said: String?
    /// One pill and one sentence when nothing has been logged for long.
    public var stale: StaleNote?

    public init(items: [Item], done: ButtonPresentation?, skip: ButtonPresentation?, said: String? = nil, stale: StaleNote? = nil) {
        self.items = items
        self.done = done
        self.skip = skip
        self.said = said
        self.stale = stale
    }
}

// MARK: What yesterday left

/// What was not logged yesterday: each routine row with its option list.
/// It acts on a tap: it has no Save. A task yesterday left is on the plan
/// page (`PlanPage.Left`).
public struct YesterdayCard: Codable, Equatable, Hashable, Sendable {
    public struct Item: Codable, Equatable, Hashable, Sendable, Identifiable {
        public var id: String
        public var glyph: Glyph
        public var title: String
        /// What it was and when it was due, in one sentence: the glyph
        /// already says whether it is a routine or a task.
        public var text: String?
        public var options: OptionList
        /// "Chosen at 08:19."
        public var chosen: String?
        /// The message the chosen option needs, with its own Send.
        public var ready: ReadyMessage?

        public init(
            id: String, glyph: Glyph, title: String, text: String? = nil, options: OptionList, chosen: String? = nil,
            ready: ReadyMessage? = nil
        ) {
            self.id = id
            self.glyph = glyph
            self.title = title
            self.text = text
            self.options = options
            self.chosen = chosen
            self.ready = ready
        }
    }

    public var items: [Item]
    /// "From a routine.", under a card that holds one thing.
    public var note: String?

    public init(items: [Item], note: String? = nil) {
        self.items = items
        self.note = note
    }
}

// MARK: The night

/// The night: what happened since the day before ended, by project. Folded
/// it is `line`; it opens in place. When `leads` it is the current item and
/// is drawn open, with `headline` as its reason.
public struct NightPresentation: Codable, Equatable, Sendable {
    public struct Group: Codable, Equatable, Sendable, Identifiable {
        public var id: String
        /// "Yours to decide", "On your priorities", "The rest".
        public var label: String
        public var items: [NightItem]

        public init(id: String, label: String, items: [NightItem]) {
            self.id = id
            self.label = label
            self.items = items
        }
    }

    /// One line for waking: whether anything needs the owner. Once read:
    /// "The night, read at 08:27."
    public var line: String
    public var unread: Bool
    /// The night is the current item.
    public var leads: Bool
    /// "Read the night" when it leads, "The night" otherwise.
    public var title: String
    public var headline: String?
    /// What was checked, in one sentence, and a source that has not synced.
    public var coverage: String?
    public var stale: StaleNote?
    public var groups: [Group]
    /// "Three had nothing: Hiring, Tally and Home."
    public var quiet: String?
    /// "Got it, all read", until it is read.
    public var gotIt: ButtonPresentation?
    /// "Read at 08:27."
    public var read: String?
    /// Nothing happened: said in place of the groups.
    public var empty: String?

    public init(
        line: String, unread: Bool, leads: Bool = false, title: String = "The night", headline: String? = nil, coverage: String? = nil,
        stale: StaleNote? = nil, groups: [Group] = [], quiet: String? = nil, gotIt: ButtonPresentation? = nil, read: String? = nil,
        empty: String? = nil
    ) {
        self.line = line
        self.unread = unread
        self.leads = leads
        self.title = title
        self.headline = headline
        self.coverage = coverage
        self.stale = stale
        self.groups = groups
        self.quiet = quiet
        self.gotIt = gotIt
        self.read = read
        self.empty = empty
    }
}

/// One project's night: its tile and name, how much is new, its latest
/// line, and the lines under it once opened.
public struct NightItem: Codable, Equatable, Sendable, Identifiable {
    public var id: String
    public var tile: Tile?
    public var name: String
    public var stripe: Stripe
    /// "4 new", or "read 08:27" once seen.
    public var count: String
    /// "Yours?" while its one decision is open.
    public var pill: Pill?
    public var text: String
    public var lines: [TimelineLine]
    /// The one thing it leaves to decide.
    public var decision: NightDecision?
    /// "Open the project".
    public var open: ButtonPresentation?
    /// "Got it", until this project is seen.
    public var gotIt: ButtonPresentation?
    public var seen: Bool

    public init(
        id: String, tile: Tile?, name: String, stripe: Stripe = .none, count: String, pill: Pill? = nil, text: String,
        lines: [TimelineLine] = [], decision: NightDecision? = nil, open: ButtonPresentation? = nil, gotIt: ButtonPresentation? = nil,
        seen: Bool = false
    ) {
        self.id = id
        self.tile = tile
        self.name = name
        self.stripe = stripe
        self.count = count
        self.pill = pill
        self.text = text
        self.lines = lines
        self.decision = decision
        self.open = open
        self.gotIt = gotIt
        self.seen = seen
    }
}

public struct NightDecision: Codable, Equatable, Hashable, Sendable {
    /// "Nobody has these in the sources checked. Are they yours?"
    public var question: String
    public var options: OptionList
    public var chosen: String?
    public var ready: ReadyMessage?

    public init(question: String, options: OptionList, chosen: String? = nil, ready: ReadyMessage? = nil) {
        self.question = question
        self.options = options
        self.chosen = chosen
        self.ready = ready
    }
}

/// One line of a timeline: a time, who (in bold), what happened, what was
/// said, and where it came from.
public struct TimelineLine: Codable, Equatable, Hashable, Sendable, Identifiable {
    public var id: String
    public var time: String
    public var who: String?
    public var text: String
    public var quote: String?
    public var source: Pill?
    /// The meeting or the task the line names, where it opens one.
    public var opens: TodayRoute?

    public init(id: String, time: String, who: String? = nil, text: String, quote: String? = nil, source: Pill? = nil, opens: TodayRoute? = nil) {
        self.id = id
        self.time = time
        self.who = who
        self.text = text
        self.quote = quote
        self.source = source
        self.opens = opens
    }
}

// MARK: Earlier today

/// What is done today, folded into one line with a count.
public struct EarlierPresentation: Codable, Equatable, Hashable, Sendable {
    public struct Line: Codable, Equatable, Hashable, Sendable, Identifiable {
        public var id: String
        public var time: String
        public var text: String
        /// "Open": the thing it names, where its options can be reached again.
        public var button: ButtonPresentation?

        public init(id: String, time: String, text: String, button: ButtonPresentation? = nil) {
            self.id = id
            self.time = time
            self.text = text
            self.button = button
        }
    }

    /// "Earlier today".
    public var title: String
    /// "5 done".
    public var count: String
    public var lines: [Line]

    public init(title: String = "Earlier today", count: String, lines: [Line]) {
        self.title = title
        self.count = count
        self.lines = lines
    }
}

// MARK: What is happening

/// What came in since the last look, directly above the current item.
/// Closed it is one line; open, a group for each project in flag order.
public struct HappeningPresentation: Codable, Equatable, Sendable {
    /// The closed line: the latest thing, and how many more.
    public var line: String
    /// The latest thing's time, in bold before the line.
    public var time: String?
    /// The latest thing's project.
    public var tile: Tile?
    /// The urgent pill, when something marked urgent waits here.
    public var pill: Pill?
    /// Nothing is new: the line is drawn quiet.
    public var quiet: Bool
    /// The urgent thing that led earlier and is settled, said once on top.
    public var settled: HappeningItem?
    /// "Cannot wait", "Earlier, now settled": the label over `settled`.
    public var settledLabel: String?
    public var groups: [HappeningGroup]
    /// Projects with no flag, as one count that opens.
    public var rest: HappeningRest?
    /// "Got it": marks what is shown as read.
    public var gotIt: ButtonPresentation?
    /// "Nothing new since 11:05.", in place of the button.
    public var nothingNew: String?

    public init(
        line: String, time: String? = nil, tile: Tile? = nil, pill: Pill? = nil, quiet: Bool = false, settled: HappeningItem? = nil,
        settledLabel: String? = nil, groups: [HappeningGroup] = [], rest: HappeningRest? = nil, gotIt: ButtonPresentation? = nil,
        nothingNew: String? = nil
    ) {
        self.line = line
        self.time = time
        self.tile = tile
        self.pill = pill
        self.quiet = quiet
        self.settled = settled
        self.settledLabel = settledLabel
        self.groups = groups
        self.rest = rest
        self.gotIt = gotIt
        self.nothingNew = nothingNew
    }
}

/// A project's group. Its header is a row that opens the project's page:
/// stripe, tile and name.
public struct HappeningGroup: Codable, Equatable, Sendable, Identifiable {
    public var id: String
    public var tile: Tile?
    public var name: String
    public var stripe: Stripe
    public var opens: TodayRoute?
    public var items: [HappeningItem]

    public init(id: String, tile: Tile?, name: String, stripe: Stripe = .none, opens: TodayRoute?, items: [HappeningItem]) {
        self.id = id
        self.tile = tile
        self.name = name
        self.stripe = stripe
        self.opens = opens
        self.items = items
    }
}

public struct HappeningItem: Codable, Equatable, Sendable, Identifiable {
    public var id: String
    public var time: String
    public var text: String
    /// Where it came from, and what it answers: "Slack. It answers what you
    /// asked at 09:20."
    public var source: String?
    /// New since the last look.
    public var isNew: Bool
    /// The urgent pill, on something marked urgent that waits here.
    public var pill: Pill?
    /// Something marked urgent and held back: who marked it and its options.
    public var marked: MarkedBlock?

    public init(
        id: String, time: String, text: String, source: String? = nil, isNew: Bool = false, pill: Pill? = nil, marked: MarkedBlock? = nil
    ) {
        self.id = id
        self.time = time
        self.text = text
        self.source = source
        self.isNew = isNew
        self.pill = pill
        self.marked = marked
    }
}

public struct HappeningRest: Codable, Equatable, Sendable {
    /// "Projects with no flag".
    public var title: String
    /// "Three more things, one new".
    public var line: String
    public var groups: [HappeningGroup]

    public init(title: String = "Projects with no flag", line: String, groups: [HappeningGroup]) {
        self.title = title
        self.line = line
        self.groups = groups
    }
}

/// Something marked urgent that waits: who marked it, and its options. A
/// tap takes effect at once, so the assistant's option has the pill and is
/// not selected.
public struct MarkedBlock: Codable, Equatable, Hashable, Sendable, Identifiable {
    /// The task's id.
    public var id: String
    /// "A rule on Wei's message marked it urgent at 16:22."
    public var by: String?
    public var options: OptionList
    /// "Chosen at 16:40."
    public var chosen: String?

    public init(id: String, by: String?, options: OptionList, chosen: String? = nil) {
        self.id = id
        self.by = by
        self.options = options
        self.chosen = chosen
    }
}

// MARK: Suggested tasks

/// One suggested task: where it came from and its options. Above a Save
/// the assistant's option is selected and marked Suggested, and nothing
/// takes effect until the Save is pressed.
public struct SuggestedItem: Codable, Equatable, Sendable, Identifiable {
    /// How far it has faded: 0 fresh, 1 fades tomorrow, 2 fading, 3 gone
    /// tonight. A view dims by level and says `label` from level 1.
    public struct Fade: Codable, Equatable, Hashable, Sendable {
        public var level: Int
        public var label: String

        public init(level: Int, label: String) {
            self.level = level
            self.label = label
        }
    }

    /// The assistant objects, once, in place, when the owner dismisses
    /// something it thinks matters.
    public struct Objection: Codable, Equatable, Hashable, Sendable {
        /// "Moss objects, once".
        public var heading: String
        public var line: String
        public var note: String?
        public var ready: ReadyMessage?

        public init(heading: String = "Moss objects, once", line: String, note: String? = nil, ready: ReadyMessage? = nil) {
            self.heading = heading
            self.line = line
            self.note = note
            self.ready = ready
        }
    }

    /// The field "Other" shows while it is the pick: what is typed there is
    /// the view's until the Save, which sends it with `saveSuggested`.
    public struct Other: Codable, Equatable, Hashable, Sendable {
        /// "What is wrong, or what to do instead".
        public var placeholder: String
        /// "Kept for whoever suggests tasks, and sent to Moss with the task."
        public var note: String

        public init(placeholder: String, note: String) {
            self.placeholder = placeholder
            self.note = note
        }
    }

    /// The owner answered in words: the suggestion is with the assistant
    /// and is not asked again. It is drawn with what was said and the
    /// exchange that followed, and no options.
    public struct Told: Codable, Equatable, Hashable, Sendable {
        /// "You told Moss".
        public var heading: String
        /// The owner's words.
        public var words: String
        /// The exchange about it, while this session holds one.
        public var answer: AnswerCard?

        public init(heading: String = "You told Moss", words: String, answer: AnswerCard? = nil) {
            self.heading = heading
            self.words = words
            self.answer = answer
        }
    }

    public var id: String
    public var title: String
    /// "Heard on Wednesday at 21:37, from a message, with Kenji.", from
    /// when it was said; "Suggested …" where the record only says when the
    /// suggestion was written.
    public var origin: String
    /// What was said, as Markdown: its opening words (`Words.excerpt`) in a
    /// card or a row, all of it on the task's own page.
    public var quote: String?
    public var fade: Fade
    public var chips: Chips
    public var stripe: Stripe
    public var options: OptionList
    /// Set while "Other" is the pick.
    public var other: Other?
    public var objection: Objection?
    /// Its own Save, where it is drawn alone: it arrived during the day, or
    /// it is on a project's or a person's page. Nil inside a group, whose
    /// Save is the group's.
    public var save: SaveRow?
    /// Whether Save would change it.
    public var pending: Bool
    /// Set where the owner answered it in words: nothing else is drawn.
    public var told: Told?

    public init(
        id: String, title: String, origin: String, quote: String? = nil, fade: Fade, chips: Chips = .none, stripe: Stripe = .none,
        options: OptionList, other: Other? = nil, objection: Objection? = nil, save: SaveRow? = nil, pending: Bool, told: Told? = nil
    ) {
        self.id = id
        self.title = title
        self.origin = origin
        self.quote = quote
        self.fade = fade
        self.chips = chips
        self.stripe = stripe
        self.options = options
        self.other = other
        self.objection = objection
        self.save = save
        self.pending = pending
        self.told = told
    }
}

// MARK: A row that opens a page, and an overlap

/// What a row that opens a page holds where it leads the day: "Plan the
/// day", "Today's plan", the suggested tasks. One line and one button that
/// opens the page. As a line of the day the row itself opens the page.
public struct PageRowCard: Codable, Equatable, Hashable, Sendable {
    /// "It opens as a page, and nothing changes until you save."
    public var line: String
    /// "Open the plan".
    public var open: ButtonPresentation
    /// "Plan saved at 08:44."
    public var saved: String?

    public init(line: String, open: ButtonPresentation, saved: String? = nil) {
        self.line = line
        self.open = open
        self.saved = saved
    }
}

/// An overlap's options, wherever they are drawn. They stay; the chosen one
/// is filled; a message is sent separately.
public struct OverlapCard: Codable, Equatable, Hashable, Sendable, Identifiable {
    public var id: String
    /// "The 15:00 overlap".
    public var name: String
    /// What overlaps, in one sentence.
    public var text: String
    /// "Chosen" or "To choose".
    public var pill: Pill
    public var options: OptionList
    /// "Chosen at 08:44. No message is needed."
    public var chosen: String?
    public var ready: ReadyMessage?

    public init(id: String, name: String, text: String, pill: Pill, options: OptionList, chosen: String? = nil, ready: ReadyMessage? = nil) {
        self.id = id
        self.name = name
        self.text = text
        self.pill = pill
        self.options = options
        self.chosen = chosen
        self.ready = ready
    }
}

/// An overlap in the timeline: one block around its meetings.
public struct OverlapBlock: Codable, Equatable, Sendable, Identifiable {
    public var id: String
    public var card: OverlapCard
    public var meetings: [MeetingRow]

    public init(id: String, card: OverlapCard, meetings: [MeetingRow]) {
        self.id = id
        self.card = card
        self.meetings = meetings
    }
}

// MARK: Meetings

/// A meeting as one line: its time, title and who, whether the owner goes,
/// the task that goes with it or its purpose, its project. Only the next
/// fixed point says how long until it. It opens in place to `card`; the
/// meeting that leads is drawn open.
public struct MeetingRow: Codable, Equatable, Sendable, Identifiable {
    public var id: String
    /// "14:20".
    public var start: String
    /// "14:50".
    public var end: String
    public var title: String
    /// Up to three of who is in it.
    public var people: [Badge]
    /// "Going.", "Asked to move, no answer yet.": only inside an overlap
    /// that is chosen.
    public var attendance: String?
    /// "Your task: the clash fix. A task, not done.", or its purpose.
    public var line: String?
    /// "in 2 h 40", on the next fixed point alone.
    public var until: String?
    /// Under the title of the meeting in full: "to 14:40, Google Meet, 2 people".
    public var detail: String?
    /// On now.
    public var live: Bool
    /// The meeting is, or is about to be, the current item.
    public var leads: Bool
    /// Moved or skipped: drawn struck through, with nothing under it.
    public var off: Bool
    public var project: ProjectChip?
    public var card: MeetingCard

    public init(
        id: String, start: String, end: String, title: String, people: [Badge] = [], attendance: String? = nil, line: String? = nil,
        until: String? = nil, live: Bool = false, leads: Bool = false, off: Bool = false, project: ProjectChip? = nil, card: MeetingCard
    ) {
        detail = (["to \(end)"] + [card.meta].compactMap { $0 }).joined(separator: ", ")
        self.id = id
        self.start = start
        self.end = end
        self.title = title
        self.people = people
        self.attendance = attendance
        self.line = line
        self.until = until
        self.live = live
        self.leads = leads
        self.off = off
        self.project = project
        self.card = card
    }
}

/// A meeting, in full: what to know before it.
public struct MeetingCard: Codable, Equatable, Sendable {
    public struct Said: Codable, Equatable, Hashable, Sendable, Identifiable {
        public var id: String
        public var badge: Badge
        public var name: String
        public var text: String
        public var time: String?

        public init(id: String, badge: Badge, name: String, text: String, time: String? = nil) {
            self.id = id
            self.badge = badge
            self.name = name
            self.text = text
            self.time = time
        }
    }

    public struct Reading: Codable, Equatable, Hashable, Sendable, Identifiable {
        public var id: String
        public var title: String
        /// "doc", "PR", "thread".
        public var kind: String?
        public var url: String?

        public init(id: String, title: String, kind: String? = nil, url: String? = nil) {
            self.id = id
            self.title = title
            self.kind = kind
            self.url = url
        }
    }

    /// Where it is and how many are in it: "Google Meet, 12 people".
    public var meta: String?
    public var chips: Chips
    public var purpose: String?
    public var overlap: OverlapCard?
    /// "Your tasks with Tomas", "Your tasks with the room".
    public var tasksTitle: String?
    public var tasks: [TaskLine]
    public var said: [Said]
    /// "Since you last met".
    public var since: String?
    /// "To raise": square tick rows the view keeps to itself.
    public var raise: [String]
    /// "To read first".
    public var readFirst: [Reading]
    /// "Join", when it is on or about to start.
    public var join: ButtonPresentation?
    public var after: String?
    public var moss: MossBox?
    /// The label over each part.
    public var labels = MeetingLabels()

    public init(
        meta: String? = nil, chips: Chips = .none, purpose: String? = nil, overlap: OverlapCard? = nil, tasksTitle: String? = nil,
        tasks: [TaskLine] = [], said: [Said] = [], since: String? = nil, raise: [String] = [], readFirst: [Reading] = [],
        join: ButtonPresentation? = nil, after: String? = nil, moss: MossBox? = nil
    ) {
        self.meta = meta
        self.chips = chips
        self.purpose = purpose
        self.overlap = overlap
        self.tasksTitle = tasksTitle
        self.tasks = tasks
        self.said = said
        self.since = since
        self.raise = raise
        self.readFirst = readFirst
        self.join = join
        self.after = after
        self.moss = moss
    }
}

// MARK: Tasks

/// A task as one line of a list on a page: it opens the task's page.
public struct TaskLine: Codable, Equatable, Hashable, Sendable, Identifiable {
    public var id: String
    /// `.task`, or `.done` once finished.
    public var glyph: Glyph
    public var title: String
    /// "Today.", "Friday.", "Done at 11:52.", "A task, not done."
    public var line: String?
    /// The urgent pill, or Suggested on one the assistant suggests for today.
    public var pill: Pill?
    public var stripe: Stripe
    public var opens: TodayRoute?

    public init(id: String, glyph: Glyph = .task, title: String, line: String? = nil, pill: Pill? = nil, stripe: Stripe = .none, opens: TodayRoute?) {
        self.id = id
        self.glyph = glyph
        self.title = title
        self.line = line
        self.pill = pill
        self.stripe = stripe
        self.opens = opens
    }
}

/// A task, open: in the day as the current item, or as its own page.
public struct TaskCard: Codable, Equatable, Sendable, Identifiable {
    public struct Left: Codable, Equatable, Hashable, Sendable {
        /// "Where you left it, 13:55".
        public var heading: String
        public var text: String

        public init(heading: String, text: String) {
            self.heading = heading
            self.text = text
        }
    }

    public var id: String
    public var chips: Chips
    /// What was said, where the task came from words: "“I will go over
    /// these tomorrow”, Tue 20:26". Its opening words in the day, all of
    /// it on the task's own page.
    public var quote: String?
    public var left: Left?
    /// "Put off three times.", from the third time.
    public var deferred: String?
    /// "Marked urgent, and waiting", with who marked it and its options.
    public var markedTitle: String?
    public var marked: MarkedBlock?
    /// "Flag", "Project" (for one the owner typed), "When".
    public var doors: [Door]
    /// "Done".
    public var done: ButtonPresentation?
    /// "Done at 11:52."
    public var said: String?
    public var moss: MossBox?

    public init(
        id: String, chips: Chips = .none, quote: String? = nil, left: Left? = nil, deferred: String? = nil, markedTitle: String? = nil,
        marked: MarkedBlock? = nil, doors: [Door] = [], done: ButtonPresentation? = nil, said: String? = nil, moss: MossBox? = nil
    ) {
        self.id = id
        self.chips = chips
        self.quote = quote
        self.left = left
        self.deferred = deferred
        self.markedTitle = markedTitle
        self.marked = marked
        self.doors = doors
        self.done = done
        self.said = said
        self.moss = moss
    }
}

// MARK: What cannot wait

/// The urgent card. Under its title: the urgent pill with who set it and
/// when, the text, who else says so, what it took the place of, then its
/// options with none selected.
public struct UrgentCard: Codable, Equatable, Sendable, Identifiable {
    public struct Voice: Codable, Equatable, Hashable, Sendable {
        public var who: String
        public var says: String

        public init(who: String, says: String) {
            self.who = who
            self.says = says
        }
    }

    /// The task's id.
    public var id: String
    /// After the pill: "The on-call paged you at 11:38."
    public var setBy: String
    public var text: String?
    /// Who else says it cannot wait, and why.
    public var voices: [Voice]
    /// "Moss cannot be reached, so nobody has checked this."
    public var unchecked: String?
    /// "It takes the place of the vendor check, which waits where you left it."
    public var took: String?
    public var options: OptionList
    public var chosen: String?
    public var ready: ReadyMessage?
    public var project: ProjectChip?
    /// "Done", on a task that was marked urgent.
    public var done: ButtonPresentation?
    public var said: String?
    public var moss: MossBox?

    public init(
        id: String, setBy: String, text: String? = nil, voices: [Voice] = [], unchecked: String? = nil, took: String? = nil,
        options: OptionList, chosen: String? = nil, ready: ReadyMessage? = nil, project: ProjectChip? = nil, done: ButtonPresentation? = nil,
        said: String? = nil, moss: MossBox? = nil
    ) {
        self.id = id
        self.setBy = setBy
        self.text = text
        self.voices = voices
        self.unchecked = unchecked
        self.took = took
        self.options = options
        self.chosen = chosen
        self.ready = ready
        self.project = project
        self.done = done
        self.said = said
        self.moss = moss
    }
}

/// The assistant, about something flagged High that will not be finished:
/// it says so and asks where it goes. The answer stays and can be changed.
public struct UnfinishedCard: Codable, Equatable, Hashable, Sendable, Identifiable {
    public var id: String
    /// "Moss, about something flagged High".
    public var heading: String
    /// "The vendor check will not be finished before Tomas at 14:20. Where
    /// does it go?"
    public var line: String
    public var left: String?
    public var options: OptionList
    public var chosen: String?
    public var project: ProjectChip?

    public init(
        id: String, heading: String = "Moss, about something flagged High", line: String, left: String? = nil, options: OptionList,
        chosen: String? = nil, project: ProjectChip? = nil
    ) {
        self.id = id
        self.heading = heading
        self.line = line
        self.left = left
        self.options = options
        self.chosen = chosen
        self.project = project
    }
}

// MARK: The end of the day

/// The wrap: a save card. Where each unfinished thing goes is a list with
/// the assistant's option selected, and one button saves them all and
/// closes the day.
public struct WrapCard: Codable, Equatable, Sendable {
    public struct Done: Codable, Equatable, Hashable, Sendable, Identifiable {
        public var id: String
        public var title: String
        /// "11:52", or "open".
        public var time: String
        public var done: Bool
        public var stripe: Stripe

        public init(id: String, title: String, time: String, done: Bool, stripe: Stripe = .none) {
            self.id = id
            self.title = title
            self.time = time
            self.done = done
            self.stripe = stripe
        }
    }

    public struct Carry: Codable, Equatable, Hashable, Sendable, Identifiable {
        public var id: String
        public var title: String
        public var why: String
        public var options: OptionList

        public init(id: String, title: String, why: String, options: OptionList) {
            self.id = id
            self.title = title
            self.why = why
            self.options = options
        }
    }

    public struct Titled: Codable, Equatable, Hashable, Sendable, Identifiable {
        public var id: String
        public var title: String
        public var line: String

        public init(id: String, title: String, line: String) {
            self.id = id
            self.title = title
            self.line = line
        }
    }

    /// "Done today": what was flagged High, done or still open.
    public var done: [Done]
    /// "Four others: the proxy update PR, Lin's score, …"
    public var others: String?
    /// "Suggested tasks: twelve made tasks and one dismissed."
    public var suggested: String?
    /// "Moved earlier today".
    public var moved: [Titled]
    /// "Carries over: where does each go?"
    public var carry: [Carry]
    /// "Three suggested tasks are still not answered.": how many, and
    /// nothing more about them. They are answered on their own page.
    public var unanswered: String?
    /// "Open the suggested tasks": the door to that page.
    public var answer: ButtonPresentation?
    /// "Dismissed today".
    public var dismissed: [Titled]
    /// "Tomorrow": what is first.
    public var tomorrow: String?
    public var save: SaveRow
    /// The label over each part.
    public var labels = WrapLabels()

    public init(
        done: [Done] = [], others: String? = nil, suggested: String? = nil, moved: [Titled] = [], carry: [Carry] = [],
        unanswered: String? = nil, answer: ButtonPresentation? = nil, dismissed: [Titled] = [], tomorrow: String? = nil, save: SaveRow
    ) {
        self.done = done
        self.others = others
        self.suggested = suggested
        self.moved = moved
        self.carry = carry
        self.unanswered = unanswered
        self.answer = answer
        self.dismissed = dismissed
        self.tomorrow = tomorrow
        self.save = save
    }
}
