import Foundation
import SubstrateCore

/// What the owner has picked above a Save and not saved: nothing here has
/// taken effect.
public struct Drafts: Codable, Equatable, Sendable {
    /// The pick for each suggested task, by its id.
    public var suggested: [String: SuggestedAnswer] = [:]
    /// The groups of the suggested tasks' page that draw all they hold,
    /// by the list's id or `none`: a group draws six until it is asked for
    /// the rest, and its Save answers only what it draws.
    public var suggestedShown: Set<String> = []
    /// The plan page's draft; nil while it shows what the assistant suggests.
    public var plan: PlanDraft?
    /// Where each unfinished thing goes, on the wrap.
    public var carry: [String: String] = [:]
    /// The flag each task is ordered by while its flag list is open: the
    /// one it had when the list was opened. Emptied when the list closes.
    public var held: [String: Flag] = [:]
    /// The tasks whose held flag was none.
    public var heldNone: Set<String> = []

    public init() {}

    /// What an earlier build kept decodes: a part it did not have is empty.
    public init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        suggested = try values.decodeIfPresent([String: SuggestedAnswer].self, forKey: .suggested) ?? [:]
        suggestedShown = try values.decodeIfPresent(Set<String>.self, forKey: .suggestedShown) ?? []
        plan = try values.decodeIfPresent(PlanDraft.self, forKey: .plan)
        carry = try values.decodeIfPresent([String: String].self, forKey: .carry) ?? [:]
        held = try values.decodeIfPresent([String: Flag].self, forKey: .held) ?? [:]
        heldNone = try values.decodeIfPresent(Set<String>.self, forKey: .heldNone) ?? []
    }
}

public struct PlanDraft: Codable, Equatable, Sendable {
    public var picked: [String: Bool] = [:]
    public var flags: [String: Flag] = [:]
    public var overlaps: [String: String] = [:]
    public var sends: [String: Bool] = [:]
    /// Where each task yesterday left goes, by its id: `today`, `tomorrow`
    /// or `drop`. None is chosen for the owner.
    public var left: [String: String] = [:]

    public init() {}

    public init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        picked = try values.decodeIfPresent([String: Bool].self, forKey: .picked) ?? [:]
        flags = try values.decodeIfPresent([String: Flag].self, forKey: .flags) ?? [:]
        overlaps = try values.decodeIfPresent([String: String].self, forKey: .overlaps) ?? [:]
        sends = try values.decodeIfPresent([String: Bool].self, forKey: .sends) ?? [:]
        left = try values.decodeIfPresent([String: String].self, forKey: .left) ?? [:]
    }
}

/// What the device keeps of the day that no record holds: when the owner
/// first looked, where the night started, when the suggested tasks were
/// saved, and what the owner chose on something urgent. Kept in the
/// repository's folder, so it is there after a relaunch, and started
/// afresh each day.
public struct DayNotes: Codable, Equatable, Sendable {
    public var day: CivilDay
    /// The owner's last look of the day before: where the night starts.
    public var nightFrom: Date?
    /// The first look of the day: where the night ends.
    public var wokeAt: Date?
    /// The latest look: what the next day's night starts from.
    public var lastLook: Date?
    public var suggestedSavedAt: Date?
    /// How the suggested tasks were saved: made tasks, already done, dismissed.
    public var suggestedSaved: [Int] = [0, 0, 0]
    /// The suggested tasks answered "Already done" today: finished, but not
    /// something done today. Optional, so the notes an earlier build kept
    /// still read.
    public var suggestedDone: Set<String>?
    /// When each task was marked done here today, by its id: the server
    /// stamps `completedAt` only once the write reaches it, and the task is
    /// a row of what is done from the moment it is marked. Optional, as above.
    public var doneAt: [String: Date]?
    /// Urgent things the owner took now, and the ones left where they are.
    public var urgentTaken: [String: Date] = [:]
    public var urgentLeft: [String: Date] = [:]
    /// The tasks typed into the field today.
    public var added: [String] = []
    /// Each thing yesterday left that was answered, and the option chosen.
    public var yesterday: [String: String] = [:]
    public var yesterdayAt: [String: Date] = [:]
    /// The night's decisions, by project.
    public var night: [String: String] = [:]
    /// Messages sent from the pane, by id, and when.
    public var sent: [String: Date] = [:]
    /// The answers saved on the wrap.
    public var carried: [String: String] = [:]

    public init(day: CivilDay) {
        self.day = day
    }
}

/// One exchange with the assistant, as the root keeps it.
public struct Exchange: Codable, Equatable, Sendable, Identifiable {
    public var id: String
    public var subject: MossSubject
    public var question: String
    public var text: String
    public var state: AnswerCard.State
    public var thread: String?
    public var at: Date

    public init(id: String, subject: MossSubject, question: String, text: String = "", state: AnswerCard.State = .waiting, thread: String? = nil, at: Date) {
        self.id = id
        self.subject = subject
        self.question = question
        self.text = text
        self.state = state
        self.thread = thread
        self.at = at
    }
}

/// Everything the engine reads, as one value: the records, the instant, the
/// calendar in the owner's time zone, the drafts and the day's notes. The
/// engine is a pure function of it.
public struct Day: Sendable {
    public var now: Date { didSet { bounds = Bounds(now, calendar) } }
    public var calendar: Calendar { didSet { bounds = Bounds(now, calendar) } }
    /// Where yesterday, today and tomorrow start, worked out once for the
    /// instant: the engine asks for them hundreds of times a pane, and the
    /// calendar is slow to answer.
    private var bounds: Bounds
    public var lists: [ListItem] = []
    public var tasks: [TaskItem] = []
    public var advice: [String: Advice] = [:]
    /// The owner's latest correction of each record, by the record's path.
    public var corrections: [String: Correction] = [:]
    public var series: [String: SeriesItem] = [:]
    /// Each medication's strength in words ("200 mg"), by its path, where
    /// its record gives one.
    public var strengths: [String: String] = [:]
    /// The assistant's standing instructions, oldest first: the order it
    /// reads them in.
    public var instructions: [Instruction] = []
    public var occurrences: [Occurrence] = []
    public var logs: [String: LogItem] = [:]
    public var meetings: [Meeting] = []
    public var digests: [Digest] = []
    /// Each project's review mark, by the list's id or `unfiled`.
    public var marks: [String: Date] = [:]
    public var plan: PlanRecord?
    public var preference = Preference()
    public var suggestions: [Suggestion] = []
    public var dossiers: [String: Dossier] = [:]
    public var returns: [String: ReturnPoint] = [:]
    public var people: [String: Person] = [:]
    public var stale: [StaleSource] = []
    public var drafts = Drafts()
    public var notes: DayNotes
    public var exchanges: [Exchange] = []
    /// False once the assistant could not be reached and until it answers.
    public var reachable = true
    /// The records' versions, for the writes: by `kind/id`.
    public var versions: [String: Int] = [:]
    /// Whether the repository holds each part: a part it lacks stays empty.
    public var discovery = Discovery()
    /// The server the records are on: a record's own page is in its console.
    public var server: URL?
    /// The page at its source of each record read for one, by its path.
    public var sources: [String: SourcePage] = [:]

    public init(now: Date, calendar: Calendar) {
        self.now = now
        self.calendar = calendar
        bounds = Bounds(now, calendar)
        notes = DayNotes(day: CivilDay(now, in: calendar))
    }

    private struct Bounds: Sendable {
        var yesterday: Date
        var today: Date
        var tomorrow: Date

        init(_ now: Date, _ calendar: Calendar) {
            today = calendar.startOfDay(for: now)
            tomorrow = calendar.date(byAdding: .day, value: 1, to: today) ?? today.addingTimeInterval(86400)
            yesterday = calendar.date(byAdding: .day, value: -1, to: today) ?? today.addingTimeInterval(-86400)
        }
    }

    public var today: CivilDay { CivilDay(now, in: calendar) }
    public var startOfToday: Date { bounds.today }
    public var startOfTomorrow: Date { bounds.tomorrow }
    public var startOfYesterday: Date { bounds.yesterday }

    public func list(_ id: String?) -> ListItem? { id.flatMap { id in lists.first { $0.id == id } } }
    public func task(_ id: String) -> TaskItem? { tasks.first { $0.id == id } }
    public func person(_ path: String) -> Person? { people[path] }

    /// The buttons that open what something was made from: its page at the
    /// source, once that is read, and the record itself.
    public func links(for path: String?) -> [SourceLink] {
        SourcePages.links(for: path, page: path.flatMap { sources[$0] }, server: server)
    }

    /// A meeting by its id, or by the id another calendar gave it.
    public func meeting(_ id: String) -> Meeting? { meetings.first { $0.id == id } ?? meetings.first { $0.also.contains(id) } }

    /// What to know before a meeting, kept under any of its ids.
    public func dossier(_ meeting: Meeting) -> Dossier? { dossiers[meeting.id] ?? meeting.also.lazy.compactMap { dossiers[$0] }.first }

    /// The records of a linked read, decoded into the day.
    public mutating func take(_ records: some Sequence<Record>) {
        for record in records {
            versions["\(record.kind)/\(record.id)"] = record.version
            switch record.kind {
            case TodayKinds.list: if let item = ListItem(record) { lists.append(item) }
            case TodayKinds.task: if let item = TaskItem(record) { tasks.append(item) }
            case TodayKinds.advice: if let item = Advice(record) { advice[item.task] = item }
            case TodayKinds.correction:
                // The latest stands: one still on its way is the newest there is.
                if let item = Correction(record), corrections[item.subject].map({ item.isAfter($0.createdAt) }) ?? true { corrections[item.subject] = item }
            case TodayKinds.log: if let item = LogItem(record) { logs[item.id] = item }
            case TodayKinds.medication:
                if let amount = record["strengthAmount"]?.decimal {
                    strengths["\(record.kind)/\(record.id)"] = Words.amount(amount, record["strengthUnit"]?.text)
                }
            case TodayKinds.instruction: if let item = Instruction(record) { instructions.append(item) }
            case TodayKinds.digest: if let item = Digest(record) { digests.append(item) }
            case TodayKinds.reviewmark:
                if let through = record["through"]?.text.flatMap(Timestamp.parse) { marks[record["project"]?.referenceID ?? record.id] = through }
            case TodayKinds.plan:
                if let item = PlanRecord(record), item.id == PlanRecord.id(today) { plan = item }
            case TodayKinds.preference: if record.id == TodayKinds.preferenceID, let item = Preference(record) { preference = item }
            case TodayKinds.dossier: if let item = Dossier(record) { dossiers[item.id] = item }
            case TodayKinds.returnpoint: if let item = ReturnPoint(record) { returns[item.record] = item }
            case TodayKinds.patchRequest: if let item = Suggestion(record) { suggestions.append(item) }
            case TodayKinds.contact:
                let person = Person(record)
                people[person.path] = person
            default:
                if discovery.series.contains(record.kind) {
                    let item = SeriesItem(record)
                    series[item.path] = item
                }
            }
        }
        lists.sort { ($0.name.lowercased(), $0.id) < ($1.name.lowercased(), $1.id) }
        tasks.sort { ($0.createdAt, $0.id) < ($1.createdAt, $1.id) }
        digests.sort { $0.key < $1.key }
        suggestions.sort { $0.id < $1.id }
        instructions.sort { ($0.createdAt, $0.id) < ($1.createdAt, $1.id) }
    }
}
