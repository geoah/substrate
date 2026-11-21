import Foundation
import SubstrateCore

// The records the pane is drawn from, as values the engine reads. Each is
// decoded tolerantly: a property of an unexpected shape reads as absent.

/// Who set the priority a task has now. Absent reads as the owner.
public enum SetBy: String, Codable, Sendable {
    case owner, assistant, automation, api
}

/// A project: a `tasks/list`.
public struct ListItem: Equatable, Sendable {
    public var id: String
    public var name: String
    public var flag: Flag?
    public var active: Bool
    public var summary: String?
    public var version: Int

    public var path: String { "\(TodayKinds.list)/\(id)" }

    init?(_ record: Record) {
        guard record.kind == TodayKinds.list else { return nil }
        id = record.id
        name = record["name"]?.text ?? "Untitled list"
        flag = record["priority"]?.text.flatMap(Flag.init(rawValue:))
        active = (record["status"]?.text ?? "active") == "active"
        summary = record["description"]?.text
        version = record.version
    }
}

/// A task or a suggested task: a `tasks/task`.
public struct TaskItem: Equatable, Sendable {
    public enum Status: String, Sendable {
        case proposed, open, done, abandoned
    }

    public var id: String
    public var name: String
    public var details: String?
    /// The list's id.
    public var list: String?
    public var dueDay: CivilDay?
    public var dueAt: Date?
    /// High, Medium or Low; nil for none, and for urgent (see `urgent`).
    public var flag: Flag?
    public var urgent: Bool
    public var status: Status
    public var completedAt: Date?
    /// The path of the record it was made from.
    public var origin: String?
    /// The contacts' paths.
    public var people: [String]
    public var notBefore: Date?
    public var deferrals: Int
    public var setBy: SetBy
    public var reason: String?
    /// The flag urgent replaced.
    public var was: Flag?
    public var checkedAt: Date?
    public var heldAt: Date?
    public var heldReason: String?
    public var createdAt: Date
    public var updatedAt: Date
    public var version: Int

    public var path: String { "\(TodayKinds.task)/\(id)" }
    public var isOpen: Bool { status == .open }
    public var isProposed: Bool { status == .proposed }

    init?(_ record: Record) {
        guard record.kind == TodayKinds.task else { return nil }
        id = record.id
        name = record["name"]?.text ?? "Untitled task"
        details = record["description"]?.text
        list = record["list"]?.referenceID
        dueDay = record["dueDay"]?.text.flatMap(CivilDay.init)
        dueAt = record["dueAt"]?.text.flatMap(Timestamp.parse)
        let priority = record["priority"]?.text ?? "none"
        urgent = priority == "urgent"
        flag = Flag(rawValue: priority)
        status = record["status"]?.text.flatMap(Status.init(rawValue:)) ?? .open
        completedAt = record["completedAt"]?.text.flatMap(Timestamp.parse)
        origin = record["origin"]?.referencePath
        people = (record["people"]?.array ?? []).compactMap(\.referencePath)
        notBefore = record["notBefore"]?.text.flatMap(Timestamp.parse)
        deferrals = record["deferrals"]?.int ?? 0
        setBy = record["prioritySetBy"]?.text.flatMap(SetBy.init(rawValue:)) ?? .owner
        reason = record["priorityReason"]?.text
        was = record["priorityWas"]?.text.flatMap(Flag.init(rawValue:))
        checkedAt = record["urgentCheckedAt"]?.text.flatMap(Timestamp.parse)
        heldAt = record["urgentHeldAt"]?.text.flatMap(Timestamp.parse)
        heldReason = record["urgentHeldReason"]?.text
        createdAt = record.createdDate
        updatedAt = record.updatedDate
        version = record.version
    }

    /// The flag drawn and ordered by: its own, or while urgent the one it had.
    public var shownFlag: Flag? { urgent ? was : flag }
}

/// The assistant's answer to a suggested task: a `today/advice`.
public struct Advice: Equatable, Sendable {
    public var task: String
    public var answer: SuggestedAnswer
    public var reason: String?
    public var objection: String?
    public var objectedAt: Date?

    init?(_ record: Record) {
        guard record.kind == TodayKinds.advice, let answer = record["answer"]?.text.flatMap(SuggestedAnswer.init(rawValue:)) else { return nil }
        task = record["task"]?.referenceID ?? record.id
        self.answer = answer
        reason = record["reason"]?.text
        objection = record["objection"]?.text
        objectedAt = record["objectedAt"]?.text.flatMap(Timestamp.parse)
    }
}

/// Something that repeats and is ticked: a record of a kind that binds
/// `routines/series`.
public struct SeriesItem: Equatable, Sendable {
    public var kind: String
    public var id: String
    public var name: String
    public var window: String
    public var active: Bool
    public var group: String?
    /// "1 capsule", "2 tablets": only a dose has one.
    public var dose: String?
    public var isDose: Bool
    /// The medication a dose is of, by path.
    public var medication: String?

    public var path: String { "\(kind)/\(id)" }

    init(_ record: Record) {
        kind = record.kind
        id = record.id
        name = record["name"]?.text ?? record.title ?? "Untitled routine"
        window = record["window"]?.text ?? "exact"
        active = (record["status"]?.text ?? "active") == "active"
        group = record["group"]?.text
        medication = record["medication"]?.referencePath
        isDose = record["medication"] != nil || record["doseAmount"] != nil
        if let amount = record["doseAmount"]?.decimal { dose = Words.amount(amount, record["doseUnit"]?.text) }
    }
}

/// One of the assistant's standing instructions: an `llm/instruction` that
/// names the assistant the app talks to.
public struct Instruction: Equatable, Sendable {
    public var id: String
    public var text: String
    public var active: Bool
    /// Who wrote it: `owner`, `agent` or `import` (shipped with a package).
    public var source: String
    public var createdAt: Date
    public var version: Int

    init?(_ record: Record) {
        guard record.kind == TodayKinds.instruction, let text = record["text"]?.text else { return nil }
        guard record["agent"]?.referencePath?.hasSuffix("/" + TodayKinds.assistant) == true else { return nil }
        id = record.id
        self.text = text
        active = record["active"]?.bool ?? true
        source = record["source"]?.text ?? "owner"
        createdAt = record.createdDate
        version = record.version
    }
}

/// One occurrence of a series, as the window read computes it: its id is
/// `<series id>_<slot>`, the id its log takes.
public struct Occurrence: Equatable, Sendable {
    public var id: String
    public var series: String
    public var slot: Date

    /// From a window read's row: the series' kind, an id that ends in the
    /// slot, and the slot under `at`.
    init?(_ record: Record) {
        guard let cut = record.id.lastIndex(of: "_") else { return nil }
        let stamp = String(record.id[record.id.index(after: cut)...])
        guard let slot = record["at"]?.text.flatMap(Timestamp.parse) ?? Occurrence.slot(stamp) else { return nil }
        id = record.id
        series = "\(record.kind)/\(record.id[..<cut])"
        self.slot = slot
    }

    init(id: String, series: String, slot: Date) {
        self.id = id
        self.series = series
        self.slot = slot
    }

    /// "20261119T073000Z" as an instant.
    static func slot(_ stamp: String) -> Date? {
        guard stamp.count == 16, stamp.hasSuffix("Z") else { return nil }
        let s = Array(stamp)
        let text = "\(String(s[0 ..< 4]))-\(String(s[4 ..< 6]))-\(String(s[6 ..< 8]))T\(String(s[9 ..< 11])):\(String(s[11 ..< 13])):\(String(s[13 ..< 15]))Z"
        return Timestamp.parse(text)
    }

    /// The slot as the id's suffix: UTC `YYYYMMDDTHHMMSSZ`.
    static func stamp(_ slot: Date) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let p = calendar.dateComponents([.year, .month, .day, .hour, .minute, .second], from: slot)
        return String(format: "%04d%02d%02dT%02d%02d%02dZ", p.year ?? 0, p.month ?? 0, p.day ?? 0, p.hour ?? 0, p.minute ?? 0, p.second ?? 0)
    }
}

/// A `routines/routinelog`: what answered one occurrence.
public struct LogItem: Equatable, Sendable {
    public var id: String
    public var done: Bool
    public var at: Date
    public var scheduledAt: Date?
    public var version: Int

    init?(_ record: Record) {
        guard record.kind == TodayKinds.log else { return nil }
        id = record.id
        done = (record["status"]?.text ?? "done") == "done"
        at = record["at"]?.text.flatMap(Timestamp.parse) ?? record.updatedDate
        scheduledAt = record["scheduledAt"]?.text.flatMap(Timestamp.parse)
        version = record.version
    }
}

/// A meeting: one occurrence of a calendar's event.
public struct Meeting: Equatable, Sendable {
    public struct Person: Equatable, Sendable {
        public var name: String
        public var email: String?
        /// A contact's path, where the attendee is a reference to one.
        public var contact: String?
    }

    public var id: String
    public var kind: String
    public var title: String
    public var start: Date
    public var end: Date
    public var people: [Person]
    public var place: String?
    public var join: String?
    public var count: Int

    public var path: String { "\(kind)/\(id)" }

    /// Nil for what is not a meeting to go to: a cancelled or declined
    /// event, a day-long one, a working location, focus time, time out of
    /// office.
    init?(_ record: Record) {
        guard let start = record["at"]?.text.flatMap(Timestamp.parse) else { return nil }
        let end = record["endsAt"]?.text.flatMap(Timestamp.parse) ?? start
        guard end > start, end.timeIntervalSince(start) < 20 * 3600 else { return nil }
        if record["status"]?.text == "cancelled" { return nil }
        if let type = record["eventType"]?.text, type != "default" { return nil }
        let attendees = record["attendees"]?.array ?? []
        if attendees.contains(where: { $0["self"]?.bool == true && $0["responseStatus"]?.text == "declined" }) { return nil }
        id = record.id
        kind = record.kind
        title = record.title ?? "Untitled event"
        self.start = start
        self.end = end
        people = attendees.filter { $0["self"]?.bool != true && $0["resource"]?.bool != true }.compactMap { value in
            if let path = value.referencePath, RecordReference(path: path) != nil { return Person(name: "", email: nil, contact: path) }
            let email = value["value"]?.text ?? value["email"]?.text
            guard let name = value["displayName"]?.text ?? value["name"]?.text ?? email?.split(separator: "@").first.map(String.init) else { return nil }
            return Person(name: name, email: email, contact: value["contact"]?.referencePath)
        }
        count = attendees.count
        join = record["hangoutLink"]?.text ?? record["url"]?.text
        place = record["location"]?.text ?? (record["hangoutLink"]?.text != nil ? "Google Meet" : nil)
    }
}

/// What happened to one project since the owner last looked: a `today/digest`.
public struct Digest: Equatable, Sendable {
    public struct Item: Equatable, Sendable {
        public var at: Date
        public var text: String
        public var sources: [String]
        public var people: [String]
    }

    /// The list's id, or `unfiled`.
    public var key: String
    public var since: Date?
    public var until: Date?
    public var line: String?
    public var items: [Item]
    public var needsOwner: Bool
    public var needsOwnerReason: String?

    init?(_ record: Record) {
        guard record.kind == TodayKinds.digest else { return nil }
        key = record["project"]?.referenceID ?? record.id
        since = record["since"]?.text.flatMap(Timestamp.parse)
        until = record["until"]?.text.flatMap(Timestamp.parse)
        line = record["line"]?.text
        items = (record["items"]?.array ?? []).compactMap { value in
            guard let at = value["at"]?.text.flatMap(Timestamp.parse), let text = value["text"]?.text else { return nil }
            return Item(
                at: at, text: text, sources: (value["sources"]?.array ?? []).compactMap(\.referencePath),
                people: (value["people"]?.array ?? []).compactMap(\.referencePath))
        }.sorted { $0.at > $1.at }
        needsOwner = record["needsOwner"]?.bool ?? false
        needsOwnerReason = record["needsOwnerReason"]?.text
    }
}

/// The day's plan: a `today/plan`, one a day.
public struct PlanRecord: Equatable, Sendable {
    public struct Overlap: Equatable, Sendable {
        public var events: [String]
        public var chosen: String?
        public var at: Date?
    }

    public struct Send: Equatable, Sendable {
        public var id: String
        public var to: String?
        public var toName: String?
        public var text: String
        public var ticked: Bool
        public var sentAt: Date?
    }

    public struct Move: Equatable, Sendable {
        public var task: String
        public var to: String
    }

    public var id: String
    public var day: CivilDay?
    /// The paths of what was ticked: the assistant's picks while
    /// `savedAt` is absent, the owner's once it is set.
    public var picked: [String]
    public var overlaps: [Overlap]
    public var sends: [Send]
    public var moves: [Move]
    public var savedAt: Date?
    public var wrapSavedAt: Date?
    public var preparedAt: Date?
    public var version: Int

    init?(_ record: Record) {
        guard record.kind == TodayKinds.plan else { return nil }
        id = record.id
        day = record["day"]?.text.flatMap(CivilDay.init)
        picked = (record["picked"]?.array ?? []).compactMap(\.referencePath)
        overlaps = (record["overlaps"]?.array ?? []).map { value in
            Overlap(
                events: (value["events"]?.array ?? []).compactMap(\.referencePath), chosen: value["chosen"]?.referencePath,
                at: value["at"]?.text.flatMap(Timestamp.parse))
        }
        sends = (record["sends"]?.array ?? []).enumerated().compactMap { index, value in
            guard let text = value["text"]?.text else { return nil }
            return Send(
                id: value["id"]?.text ?? "send\(index)", to: value["to"]?.referencePath, toName: value["toName"]?.text, text: text,
                ticked: value["ticked"]?.bool ?? false, sentAt: value["sentAt"]?.text.flatMap(Timestamp.parse))
        }
        moves = (record["moves"]?.array ?? []).compactMap { value in
            guard let task = value["task"]?.referencePath, let to = value["to"]?.text else { return nil }
            return Move(task: task, to: to)
        }
        savedAt = record["savedAt"]?.text.flatMap(Timestamp.parse)
        wrapSavedAt = record["wrapSavedAt"]?.text.flatMap(Timestamp.parse)
        preparedAt = record["preparedAt"]?.text.flatMap(Timestamp.parse)
        version = record.version
    }

    /// The plan record's id for a day: `d<YYYYMMDD>`.
    static func id(_ day: CivilDay) -> String { String(format: "d%04d%02d%02d", day.year, day.month, day.day) }
}

/// The owner's settings for the app: the one `today/preference`.
public struct Preference: Equatable, Sendable {
    public var exists = false
    public var timezone: String?
    public var lastActiveAt: Date?
    public var nightReadAt: Date?
    /// Seconds an urgent mark nobody checked waits before it interrupts.
    public var urgentWait: TimeInterval = 120
    /// When the assistant prepares the morning, as "HH:MM" in `timezone`.
    public var prepareAt = "06:30"
    /// Whether the assistant prepares at all: the morning, the advice on
    /// a suggested task, the check of an urgent mark.
    public var assistantPrepares = true
    public var version = 0

    init() {}

    init?(_ record: Record) {
        guard record.kind == TodayKinds.preference else { return nil }
        exists = true
        timezone = record["timezone"]?.text
        lastActiveAt = record["lastActiveAt"]?.text.flatMap(Timestamp.parse)
        nightReadAt = record["nightReadAt"]?.text.flatMap(Timestamp.parse)
        if let wait = record["urgentWait"]?.text.flatMap(ISODuration.parse) { urgentWait = TimeInterval(max(0, wait)) }
        if let at = record["prepareAt"]?.text, !at.isEmpty { prepareAt = at }
        assistantPrepares = record["assistantPrepares"]?.bool ?? true
        version = record.version
    }
}

/// What the assistant suggests on a record that exists: an open
/// `core/recordpatchrequest` that sets `priority` or `list`.
public struct Suggestion: Equatable, Sendable {
    public var id: String
    /// The target's path.
    public var target: String
    public var flag: Flag?
    /// A list's id.
    public var list: String?
    public var reason: String?
    public var version: Int

    init?(_ record: Record) {
        guard record.kind == TodayKinds.patchRequest, (record["decision"]?.text ?? "proposed") == "proposed" else { return nil }
        guard (record["op"]?.text ?? "patch") == "patch", let target = record["target"]?.referencePath else { return nil }
        let diff = record["diff"]?["properties"]
        flag = diff?["priority"]?.text.flatMap(Flag.init(rawValue:))
        list = diff?["list"]?.referenceID
        guard flag != nil || list != nil else { return nil }
        id = record.id
        self.target = target
        reason = record["rationale"]?.text
        version = record.version
    }
}

/// What to know before a meeting: a `today/dossier`.
public struct Dossier: Equatable, Sendable {
    public var id: String
    public var purpose: String?
    public var since: String?
    public var raise: [String]
    public var after: String?
    public var tasks: [String]
    public var readFirst: [String]

    init?(_ record: Record) {
        guard record.kind == TodayKinds.dossier else { return nil }
        id = record.id
        purpose = record["purpose"]?.text
        since = record["since"]?.text
        raise = (record["raise"]?.array ?? []).compactMap(\.text)
        after = record["after"]?.text
        tasks = (record["tasks"]?.array ?? []).compactMap(\.referenceID)
        readFirst = (record["readFirst"]?.array ?? []).compactMap(\.referencePath)
    }
}

/// Where the owner left something: a `today/returnpoint`.
public struct ReturnPoint: Equatable, Sendable {
    public var record: String
    public var text: String
    public var at: Date

    init?(_ record: Record) {
        guard record.kind == TodayKinds.returnpoint, (record["mark"]?.text ?? "note") == "note", let text = record["text"]?.text else { return nil }
        self.record = record["record"]?.referencePath ?? record.id
        self.text = text
        at = record["at"]?.text.flatMap(Timestamp.parse) ?? record.updatedDate
    }
}

/// A source that has not synced, as the server's sync status said.
public struct StaleSource: Codable, Equatable, Sendable {
    public var name: String
    public var since: Date?

    public init(name: String, since: Date? = nil) {
        self.name = name
        self.since = since
    }
}

/// A contact, as much of it as a badge and a name need.
public struct Person: Equatable, Sendable {
    public var path: String
    public var name: String

    public var id: String { path }
    /// How the person is named inside a sentence: the first name.
    public var short: String { name.split(separator: " ").first.map(String.init) ?? name }
    public var badge: Badge { Badge(initials: Words.initials(name, last: true), hue: Words.hue(path)) }
}
