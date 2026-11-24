import Foundation

/// A page: a bar with only the back button (and at most one icon button at
/// the right), and under it, once, the icon of the thing and its title.
/// Nothing opens a panel inside a panel.
public enum PagePresentation: Codable, Equatable, Sendable {
    case priorities(PrioritiesPage)
    case projects(ProjectsPage)
    case project(ProjectPage)
    case person(PersonPage)
    case meeting(MeetingPage)
    case task(TaskPage)
    case plan(PlanPage)
    case suggested(SuggestedPage)
    /// The thing the route names is not in the repository any more.
    case missing(MissingPage)
}

public struct MissingPage: Codable, Equatable, Hashable, Sendable {
    public var title: String
    public var line: String

    public init(title: String = "Not here", line: String = "This is no longer in your records.") {
        self.title = title
        self.line = line
    }
}

// MARK: Priorities

/// The priorities as a page. Only a project is a priority. A flag opens its
/// three options under the entry, and a change applies at once, so what the
/// assistant suggests has the pill and is not selected.
public struct PrioritiesPage: Codable, Equatable, Sendable {
    /// "Priorities".
    public var title: String
    /// "Yours to set, as many as you like, each with a flag. …"
    public var note: String
    /// "Moss suggests adding": projects not on the list, outlined, each
    /// with its three options open.
    public var suggestedTitle: String?
    public var suggested: [PriorityEntry]
    /// "Your priorities".
    public var entriesTitle: String
    public var entries: [PriorityEntry]
    /// "Add a project": square rows, one for each project not on the list.
    public var addTitle: String
    public var add: OptionList
    /// "Every project is on your list."
    public var addEmpty: String?

    public init(
        title: String = "Priorities", note: String, suggestedTitle: String? = nil, suggested: [PriorityEntry] = [],
        entriesTitle: String = "Your priorities", entries: [PriorityEntry], addTitle: String = "Add a project", add: OptionList,
        addEmpty: String? = nil
    ) {
        self.title = title
        self.note = note
        self.suggestedTitle = suggestedTitle
        self.suggested = suggested
        self.entriesTitle = entriesTitle
        self.entries = entries
        self.addTitle = addTitle
        self.add = add
        self.addEmpty = addEmpty
    }
}

/// One priority on its page: tile, name, how today stands, its flag control
/// and Remove. No project is on the page twice: a flag the assistant
/// suggests for a project already listed is its own entry's open list.
public struct PriorityEntry: Codable, Equatable, Hashable, Sendable, Identifiable {
    public var id: String
    public var tile: Tile
    public var name: String
    public var line: String?
    public var flag: FlagControl
    /// "Remove", drawn as a button.
    public var remove: ButtonPresentation?
    public var opens: TodayRoute?

    public init(id: String, tile: Tile, name: String, line: String? = nil, flag: FlagControl, remove: ButtonPresentation? = nil, opens: TodayRoute? = nil) {
        self.id = id
        self.tile = tile
        self.name = name
        self.line = line
        self.flag = flag
        self.remove = remove
        self.opens = opens
    }
}

// MARK: All projects

/// Every project, in one list: the ones the owner flagged first, in flag
/// order, then the rest by name. Personal or work, with no areas.
public struct ProjectsPage: Codable, Equatable, Sendable {
    /// "All projects".
    public var title: String
    /// The edit icon button at the right of the bar: the priorities page.
    public var edit: ButtonPresentation
    /// "Your priorities".
    public var prioritiesTitle: String
    public var priorities: [ProjectLine]
    /// "Projects with no flag".
    public var othersTitle: String
    public var others: [ProjectLine]
    public var empty: String?

    public init(
        title: String = "All projects",
        edit: ButtonPresentation = ButtonPresentation("Edit priorities", glyph: .edit, iconOnly: true, role: .quiet, opens: .priorities),
        prioritiesTitle: String = "Your priorities", priorities: [ProjectLine], othersTitle: String = "Projects with no flag",
        others: [ProjectLine], empty: String? = nil
    ) {
        self.title = title
        self.edit = edit
        self.prioritiesTitle = prioritiesTitle
        self.priorities = priorities
        self.othersTitle = othersTitle
        self.others = others
        self.empty = empty
    }
}

/// A project as a row that opens its page.
public struct ProjectLine: Codable, Equatable, Hashable, Sendable, Identifiable {
    public var id: String
    public var tile: Tile
    public var name: String
    /// How today stands on it, or where its work stands.
    public var line: String?
    /// What is new on it, said once: "New at 12:02. The write-up is posted."
    public var news: String?
    public var pill: Pill?
    public var stripe: Stripe
    public var opens: TodayRoute

    public init(id: String, tile: Tile, name: String, line: String? = nil, news: String? = nil, pill: Pill? = nil, stripe: Stripe = .none) {
        self.id = id
        self.tile = tile
        self.name = name
        self.line = line
        self.news = news
        self.pill = pill
        self.stripe = stripe
        opens = .project(id)
    }
}

// MARK: A project

/// A project's page: its tasks, its suggested tasks and its timeline.
public struct ProjectPage: Codable, Equatable, Sendable, Identifiable {
    /// One day of the timeline. Its label says the day once, so no line
    /// under it does: "Coming up", "Today", "Overnight", "Yesterday", and a
    /// date for anything older, "Tuesday 17 November".
    public struct TimelineGroup: Codable, Equatable, Hashable, Sendable, Identifiable {
        /// The lines of one day that came from one place, the newest first.
        public struct Section: Codable, Equatable, Hashable, Sendable, Identifiable {
            public var id: String
            /// Where its lines came from, said once over them: "Slack",
            /// "Slack, #api-reliability", "Gmail". Nil over lines written
            /// from no record: a meeting ahead, a task you finished.
            public var source: String?
            public var lines: [TimelineLine]

            public init(id: String, source: String? = nil, lines: [TimelineLine]) {
                self.id = id
                self.source = source
                self.lines = lines
            }
        }

        public var id: String
        public var label: String
        /// By where the lines came from: what was written from no record
        /// first, then each place in order of its newest line.
        public var sections: [Section]

        public init(id: String, label: String, sections: [Section]) {
            self.id = id
            self.label = label
            self.sections = sections
        }

        /// Every line of the day, in the order drawn.
        public var lines: [TimelineLine] { sections.flatMap(\.lines) }
    }

    public var id: String
    public var tile: Tile
    public var name: String
    /// Its flag, where it is a priority; the control's list is open while
    /// the assistant suggests another.
    public var flag: FlagControl?
    /// A button that makes it a priority, where it is not one.
    public var makePriority: ButtonPresentation?
    /// What the list says of itself, as prose.
    public var summary: String?
    public var stale: StaleNote?
    /// Something urgent on it, as it stands.
    public var urgent: TaskLine?
    public var moss: MossBox?
    /// "Tasks".
    public var tasks: [TaskLine]
    public var tasksEmpty: String?
    public var done: [TaskLine]
    /// "Not today".
    public var notToday: [TaskLine]
    /// "Suggested": each with its own Save.
    public var suggested: [SuggestedItem]
    public var suggestedEmpty: String?
    /// "Timeline".
    public var timeline: [TimelineGroup]
    public var timelineEmpty: String?
    /// "Meetings today".
    public var meetings: [MeetingLine]
    /// The label over each part.
    public var labels = PageLabels()

    public init(
        id: String, tile: Tile, name: String, flag: FlagControl? = nil, makePriority: ButtonPresentation? = nil, summary: String? = nil,
        stale: StaleNote? = nil, urgent: TaskLine? = nil, moss: MossBox? = nil, tasks: [TaskLine] = [], tasksEmpty: String? = nil,
        done: [TaskLine] = [], notToday: [TaskLine] = [], suggested: [SuggestedItem] = [], suggestedEmpty: String? = nil,
        timeline: [TimelineGroup] = [], timelineEmpty: String? = nil, meetings: [MeetingLine] = []
    ) {
        self.id = id
        self.tile = tile
        self.name = name
        self.flag = flag
        self.makePriority = makePriority
        self.summary = summary
        self.stale = stale
        self.urgent = urgent
        self.moss = moss
        self.tasks = tasks
        self.tasksEmpty = tasksEmpty
        self.done = done
        self.notToday = notToday
        self.suggested = suggested
        self.suggestedEmpty = suggestedEmpty
        self.timeline = timeline
        self.timelineEmpty = timelineEmpty
        self.meetings = meetings
    }
}

/// A meeting as a row that opens its page.
public struct MeetingLine: Codable, Equatable, Hashable, Sendable, Identifiable {
    public var id: String
    public var title: String
    /// "14:20 to 14:50, on now."
    public var line: String
    public var opens: TodayRoute

    public init(id: String, title: String, line: String) {
        self.id = id
        self.title = title
        self.line = line
        opens = .meeting(id)
    }
}

// MARK: A person

/// A person's page: the tasks that involve the person, the suggestions,
/// and when the owner sees the person. A person has no flag.
public struct PersonPage: Codable, Equatable, Sendable, Identifiable {
    public var id: String
    public var badge: Badge
    public var name: String
    /// "You see Tomas at 14:20."
    public var sees: String?
    /// "Tasks with Tomas".
    public var tasksTitle: String
    public var tasks: [TaskLine]
    public var tasksEmpty: String?
    public var suggested: [SuggestedItem]
    public var meetings: [MeetingLine]
    public var moss: MossBox?
    /// The label over each part.
    public var labels = PageLabels()

    public init(
        id: String, badge: Badge, name: String, sees: String? = nil, tasksTitle: String, tasks: [TaskLine] = [], tasksEmpty: String? = nil,
        suggested: [SuggestedItem] = [], meetings: [MeetingLine] = [], moss: MossBox? = nil
    ) {
        self.id = id
        self.badge = badge
        self.name = name
        self.sees = sees
        self.tasksTitle = tasksTitle
        self.tasks = tasks
        self.tasksEmpty = tasksEmpty
        self.suggested = suggested
        self.meetings = meetings
        self.moss = moss
    }
}

// MARK: A meeting and a task

public struct MeetingPage: Codable, Equatable, Sendable, Identifiable {
    public var id: String
    /// Above the title, because it is information: "14:20 to 14:50, in 2 h 40".
    public var eyebrow: String
    public var title: String
    public var card: MeetingCard

    public init(id: String, eyebrow: String, title: String, card: MeetingCard) {
        self.id = id
        self.eyebrow = eyebrow
        self.title = title
        self.card = card
    }
}

public struct TaskPage: Codable, Equatable, Sendable, Identifiable {
    public var id: String
    /// "Done at 11:52", when it is; "Suggested" on one not answered yet.
    public var eyebrow: String?
    /// `.task`, or `.done`.
    public var glyph: Glyph
    public var title: String
    /// Why it matters, or the reason of its flag.
    public var line: String?
    public var stripe: Stripe
    public var card: TaskCard
    /// A suggested task not answered yet: its three options and its Save,
    /// in place of the card's doors.
    public var suggestion: SuggestedItem?

    public init(
        id: String, eyebrow: String? = nil, glyph: Glyph = .task, title: String, line: String? = nil, stripe: Stripe = .none, card: TaskCard,
        suggestion: SuggestedItem? = nil
    ) {
        self.id = id
        self.eyebrow = eyebrow
        self.glyph = glyph
        self.title = title
        self.line = line
        self.stripe = stripe
        self.card = card
        self.suggestion = suggestion
    }
}

// MARK: The suggested tasks

/// The suggested tasks, as a page: grouped by project, the flagged projects
/// first in flag order, then the others by name, then what is on no
/// project. Each group is a save card of its own: its Save writes that
/// group's answers and no other's, and an answered group leaves the page.
public struct SuggestedPage: Codable, Equatable, Sendable {
    public struct Group: Codable, Equatable, Sendable, Identifiable {
        /// The list's id, or `none`.
        public var id: String
        public var tile: Tile?
        public var name: String
        /// "7 suggested".
        public var count: String
        public var stripe: Stripe
        public var opens: TodayRoute?
        /// The ones drawn: those the assistant advises making a task, then
        /// those with no advice, then those it advises done or dismissed,
        /// the newest first within each. Six, until the rest are asked for.
        public var items: [SuggestedItem]
        /// "Show the other 4": draws the rest of the group.
        public var more: ButtonPresentation?
        /// The ones the owner answered in words, while their exchange is here.
        public var told: [SuggestedItem]
        /// "Save 5 answers": the ones `items` holds. Nil where the group
        /// holds only what was answered in words.
        public var save: SaveRow?

        public init(
            id: String, tile: Tile?, name: String, count: String, stripe: Stripe = .none, opens: TodayRoute? = nil, items: [SuggestedItem],
            more: ButtonPresentation? = nil, told: [SuggestedItem] = [], save: SaveRow? = nil
        ) {
            self.id = id
            self.tile = tile
            self.name = name
            self.count = count
            self.stripe = stripe
            self.opens = opens
            self.items = items
            self.more = more
            self.told = told
            self.save = save
        }
    }

    public var title: String
    /// "Thirteen to answer, six on your priorities."
    public var line: String?
    /// "Not answered, a suggestion fades after a week and is gone after
    /// two. What was said stays in memory."
    public var rule: String
    public var groups: [Group]
    /// "Nothing suggested is waiting for an answer."
    public var empty: String?

    public init(title: String = "Suggested tasks", line: String? = nil, rule: String, groups: [Group], empty: String? = nil) {
        self.title = title
        self.line = line
        self.rule = rule
        self.groups = groups
        self.empty = empty
    }
}

// MARK: The plan

/// The plan, as a page: what yesterday left, then everything on the table
/// today, grouped by project. A draft until it is saved, and saving keeps
/// only what was ticked.
public struct PlanPage: Codable, Equatable, Sendable {
    /// One task yesterday left: due before today and still open. Its own
    /// list, with nothing selected until the owner chooses, and the plan's
    /// Save writes the choice.
    public struct Left: Codable, Equatable, Hashable, Sendable, Identifiable {
        public var id: String
        public var title: String
        /// "It was due yesterday.", and why it matters where a record says.
        public var line: String?
        public var chips: Chips
        public var stripe: Stripe
        /// Today, Tomorrow, Drop it.
        public var options: OptionList

        public init(id: String, title: String, line: String? = nil, chips: Chips = .none, stripe: Stripe = .none, options: OptionList) {
            self.id = id
            self.title = title
            self.line = line
            self.chips = chips
            self.stripe = stripe
            self.options = options
        }
    }

    /// One thing that could be for today: a square to tick, its reason and
    /// its flag. What the assistant suggests for today starts ticked and is
    /// marked Suggested. Nothing moves on a tick.
    public struct Item: Codable, Equatable, Hashable, Sendable, Identifiable {
        public var id: String
        public var title: String
        public var why: String?
        /// "This morning.", or "Otherwise: Friday morning."
        public var line: String?
        public var ticked: Bool
        /// The assistant suggests it for today.
        public var suggested: Bool
        public var flag: FlagControl
        public var toggle: TodayCommand

        public init(id: String, title: String, why: String? = nil, line: String? = nil, ticked: Bool, suggested: Bool, flag: FlagControl) {
            self.id = id
            self.title = title
            self.why = why
            self.line = line
            self.ticked = ticked
            self.suggested = suggested
            self.flag = flag
            toggle = .planPick(id)
        }
    }

    public struct Group: Codable, Equatable, Hashable, Sendable, Identifiable {
        public var id: String
        public var tile: Tile?
        public var name: String
        /// "2 today", "1 suggested", "nothing today".
        public var count: String
        public var stripe: Stripe
        public var opens: TodayRoute?
        /// "Meetings today: Lantern standup at 17:00."
        public var meetings: String?
        /// What the assistant suggests for today.
        public var items: [Item]
        /// "Not suggested for today (3), 1 ticked": the rest, folded.
        public var moreTitle: String?
        public var more: [Item]
        /// "Nothing of yours to pick here."
        public var empty: String?

        public init(
            id: String, tile: Tile?, name: String, count: String, stripe: Stripe = .none, opens: TodayRoute? = nil, meetings: String? = nil,
            items: [Item], moreTitle: String? = nil, more: [Item] = [], empty: String? = nil
        ) {
            self.id = id
            self.tile = tile
            self.name = name
            self.count = count
            self.stripe = stripe
            self.opens = opens
            self.meetings = meetings
            self.items = items
            self.moreTitle = moreTitle
            self.more = more
            self.empty = empty
        }
    }

    public struct Overlap: Codable, Equatable, Hashable, Sendable, Identifiable {
        public var id: String
        public var name: String
        public var text: String
        public var options: OptionList

        public init(id: String, name: String, text: String, options: OptionList) {
            self.id = id
            self.name = name
            self.text = text
            self.options = options
        }
    }

    /// "Today's plan": the page names its noun, and the row in the day says what to do.
    public var title: String
    /// The shape of the day in real times: "Free until Tomas at 14:20, then
    /// meetings until 21:30, with gaps at 16:30 and 20:30."
    public var opening: String?
    /// "What Moss suggests for today is ticked. Untick what you do not
    /// want. Nothing changes until you save."
    public var hint: String
    /// "Your priorities stand as you set them. They change on their own page."
    public var prioritiesLine: String
    /// "Left from yesterday": the first section, where anything was left.
    public var leftTitle: String?
    public var left: [Left]
    /// "Twelve suggested tasks wait for an answer.", and the door to their
    /// page: the plan does not list them a second time.
    public var suggested: String?
    public var suggestedOpen: ButtonPresentation?
    public var groups: [Group]
    /// "The four overlaps".
    public var overlapsTitle: String?
    public var overlaps: [Overlap]
    /// "What Moss may send when you save".
    public var sendsTitle: String?
    /// "Moss sends only what you tick. Nothing is ticked for you."
    public var sendsHint: String?
    /// Square rows, none ticked in advance.
    public var sends: OptionList
    /// What went with an earlier save: "To Karim, for the 15:00 overlap.
    /// Sent at 08:44."
    public var sent: [String]
    /// "Moss notes": a project flagged High with nothing ticked for today.
    public var note: String?
    /// The bar in place of the field: its line says exactly what Save does.
    public var barLine: String
    public var save: ButtonPresentation
    /// "Reset", while the draft differs from what the assistant suggests.
    public var reset: ButtonPresentation?
    /// "Plan saved at 08:44."
    public var saved: String?
    /// Nothing is on the table.
    public var empty: String?

    public init(
        title: String = "Today's plan", opening: String? = nil,
        hint: String = "What Moss suggests for today is ticked. Untick what you do not want. Nothing changes until you save.",
        prioritiesLine: String = "Your priorities stand as you set them. They change on their own page.", leftTitle: String? = nil,
        left: [Left] = [], suggested: String? = nil, suggestedOpen: ButtonPresentation? = nil, groups: [Group],
        overlapsTitle: String? = nil, overlaps: [Overlap] = [], sendsTitle: String? = nil, sendsHint: String? = nil,
        sends: OptionList = OptionList(.square, []), sent: [String] = [], note: String? = nil, barLine: String, save: ButtonPresentation,
        reset: ButtonPresentation? = nil, saved: String? = nil, empty: String? = nil
    ) {
        self.title = title
        self.opening = opening
        self.hint = hint
        self.prioritiesLine = prioritiesLine
        self.leftTitle = leftTitle
        self.left = left
        self.suggested = suggested
        self.suggestedOpen = suggestedOpen
        self.groups = groups
        self.overlapsTitle = overlapsTitle
        self.overlaps = overlaps
        self.sendsTitle = sendsTitle
        self.sendsHint = sendsHint
        self.sends = sends
        self.sent = sent
        self.note = note
        self.barLine = barLine
        self.save = save
        self.reset = reset
        self.saved = saved
        self.empty = empty
    }
}
