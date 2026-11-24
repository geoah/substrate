import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import TodayGlance

/// One invented day, as records, and the pane at each moment the design
/// draws, made by running the root's presenter over them at a fixed
/// instant: what each UI's gallery draws and what the tests pin.
///
/// Nothing here is a record of anyone. Every person, project, meeting and
/// health detail is made up. The day is Thursday 19 November 2026 in
/// Lisbon, where the clock is UTC in November.
public enum TodayFixtures {
    public struct Fixture: Sendable, Identifiable {
        public var name: String
        public var app: TodayPresentation
        /// The pages worth drawing from this moment.
        public var routes: [TodayRoute]
        public var id: String { name }
    }

    public static let zone = TimeZone(identifier: "Europe/Lisbon")!
    public static var calendar: Calendar { TodayApp.calendar(zone) }

    static let app = TodayApp(cache: nil, outbox: nil, words: TodayApp.words(device: "this iPhone"), build: BuildFacts(version: "0.1.0 (1)"))

    /// An instant of the invented day: "HH:MM", on the day `offset` days
    /// from Thursday 19 November 2026.
    public static func at(_ clock: String, day offset: Int = 0) -> Date {
        let parts = clock.split(separator: ":").compactMap { Int($0) }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        return calendar.date(from: DateComponents(year: 2026, month: 11, day: 19 + offset, hour: parts[0], minute: parts.count > 1 ? parts[1] : 0))
            ?? .distantPast
    }

    static func stamp(_ date: Date) -> String { Timestamp.format(date, precision: .seconds) }
    static func ts(_ clock: String, day offset: Int = 0) -> JSONValue { .string(stamp(at(clock, day: offset))) }

    // MARK: The records

    static let listKind = TodayKinds.list
    static let taskKind = TodayKinds.task
    static let scheduleKind = "substrate.reamde.dev/health/medicationschedule"
    static let routineKind = "substrate.reamde.dev/routines/routine"
    static let eventKind = "providers.substrate.reamde.dev/google/calendarevent"

    public static let discovery = Discovery(
        present: TodayKinds.linkable.sorted(), series: [scheduleKind, routineKind].sorted(), calendars: [eventKind])

    static func list(_ id: String, _ name: String, _ flag: String? = nil, summary: String? = nil) -> Record {
        var properties: [String: JSONValue] = ["name": .string(name), "status": "active"]
        if let flag { properties["priority"] = .string(flag) }
        if let summary { properties["description"] = .string(summary) }
        return Record(id: id, kind: listKind, properties: properties, version: 1, createdAt: stamp(at("09:00", day: -60)))
    }

    static func contact(_ id: String, _ name: String) -> Record { Record(id: id, kind: TodayKinds.contact, properties: ["name": .string(name)], version: 1) }
    static func person(_ id: String) -> String { "\(TodayKinds.contact)/\(id)" }

    /// What a suggested task was made from, as the invented day has it: a
    /// message, but for one made from an issue and one from a mail.
    public static func origin(_ task: String) -> String {
        switch task {
        case "p-ticket": issue("m-\(task)")
        case "p-casey": mail("m-\(task)")
        default: said("m-\(task)")
        }
    }

    /// Where the invented day's messages can be read at their source: most
    /// in a chat, one as a mail, one in an issue tracker, and one nowhere,
    /// so its task has the record alone.
    public static func sources(_ tasks: some Sequence<String>) -> [String: SourcePage] {
        var out: [String: SourcePage] = [:]
        for task in tasks {
            switch task {
            case "p-marco": continue
            case "p-ticket": out[origin(task)] = SourcePage(name: "Linear", url: "https://linear.app/example/issue/OPS-412")
            case "p-casey": out[origin(task)] = SourcePage(name: "Gmail", url: "https://mail.google.com/mail/u/owner@example.com/#all/18c2f0a1b2c3d4e5")
            default:
                out[origin(task)] = SourcePage(
                    name: "Slack", url: "https://example.slack.com/archives/C0EXAMPLE/p\(task.utf8.reduce(1_700_000) { ($0 &* 31 &+ Int($1)) % 1_000_000_000 })"
                )
            }
        }
        return out
    }

    /// The server the invented day's records are on.
    public static let server = URL(string: "https://substrate.example.com")

    static func task(
        _ id: String, _ name: String, list: String? = nil, flag: String = "none", status: String = "open", people: [String] = [],
        created: Date, why: String? = nil, quote: String? = nil, extra: [String: JSONValue] = [:]
    ) -> Record {
        var properties: [String: JSONValue] = ["name": .string(name), "priority": .string(flag), "status": .string(status)]
        if let list { properties["list"] = .string("\(listKind)/\(list)") }
        if !people.isEmpty { properties["people"] = .array(people.map { .string(person($0)) }) }
        if let why { properties["priorityReason"] = .string(why) }
        if let quote { properties["description"] = .string(quote) }
        for (key, value) in extra { properties[key] = value }
        return Record(id: id, kind: taskKind, properties: properties, version: 1, createdAt: stamp(created), updatedAt: stamp(created))
    }

    static func advice(_ task: String, _ answer: String, _ reason: String, objection: String? = nil) -> Record {
        var properties: [String: JSONValue] = ["task": .string("\(taskKind)/\(task)"), "answer": .string(answer), "reason": .string(reason)]
        if let objection { properties["objection"] = .string(objection) }
        return Record(id: task, kind: TodayKinds.advice, properties: properties, version: 1)
    }

    /// A dose on a rule. A strength ("200", "mg") is the medication's, and
    /// the dose is then one tablet; a counted unit ("1", "capsule") is the
    /// dose itself.
    static func schedule(_ id: String, _ name: String, _ amount: String, _ unit: String, group: String) -> Record {
        let counted = ["capsule", "tablet"].contains(unit)
        return Record(
            id: id, kind: scheduleKind,
            properties: [
                "name": .string(name), "medication": .string("\(TodayKinds.medication)/\(id)"),
                "doseAmount": counted ? ((try? JSONValue(data: Data(amount.utf8))) ?? 1) : 1, "doseUnit": .string(counted ? unit : "tablet"),
                "window": "exact", "status": "active", "group": .string(group),
            ], version: 1)
    }

    /// What a dose is of, with its strength where the label gives one.
    static func medication(_ id: String, _ name: String, _ amount: String, _ unit: String) -> Record {
        var properties: [String: JSONValue] = ["name": .string(name), "category": "supplement"]
        if !["capsule", "tablet"].contains(unit) {
            properties["strengthAmount"] = (try? JSONValue(data: Data(amount.utf8))) ?? .null
            properties["strengthUnit"] = .string(unit)
        }
        return Record(id: id, kind: TodayKinds.medication, properties: properties, version: 1)
    }

    static func routine(_ id: String, _ name: String, group: String? = nil) -> Record {
        var properties: [String: JSONValue] = ["name": .string(name), "window": "exact", "status": "active", "category": "habit"]
        if let group { properties["group"] = .string(group) }
        return Record(id: id, kind: routineKind, properties: properties, version: 1)
    }

    static func occurrence(_ kind: String, _ series: String, _ slot: Date) -> Record {
        Record(id: "\(series)_\(Occurrence.stamp(slot))", kind: kind, properties: ["at": .string(stamp(slot))], version: 0)
    }

    static func log(_ kind: String, _ series: String, slot: Date, at: Date, done: Bool = true) -> Record {
        Record(
            id: "\(series)_\(Occurrence.stamp(slot))", kind: TodayKinds.log,
            properties: [
                "series": .string("\(kind)/\(series)"), "scheduledAt": .string(stamp(slot)), "at": .string(stamp(at)),
                "status": .string(done ? "done" : "skipped"),
            ], version: 1)
    }

    static func event(_ id: String, _ start: String, _ end: String, _ title: String, people: [String] = [], meet: Bool = true, count: Int = 0, day: Int = 0)
        -> Record
    {
        var attendees: [JSONValue] = [.object(["value": "you@example.com", "self": true, "responseStatus": "accepted"])]
        attendees += people.map { .object(["displayName": .string($0), "responseStatus": "accepted"]) }
        attendees += (0 ..< max(0, count - people.count - 1)).map { .object(["displayName": .string("Guest \($0 + 1)"), "responseStatus": "accepted"]) }
        var properties: [String: JSONValue] = [
            "summary": .string(title), "at": .string(stamp(at(start, day: day))), "endsAt": .string(stamp(at(end, day: day))),
            "attendees": .array(attendees), "status": "confirmed", "eventType": "default",
        ]
        if meet { properties["hangoutLink"] = .string("https://meet.example.com/\(id)") } else { properties["location"] = "Room 2" }
        return Record(id: id, kind: eventKind, properties: properties, version: 1)
    }

    /// A digest: what happened on the invented day as `(clock, text, what
    /// it was written from)`, and before it as `(days ago, clock, text,
    /// what it was written from)`.
    static func digest(
        _ key: String, line: String, needsOwner: String? = nil, _ items: [(String, String, String?)],
        before: [(Int, String, String, String?)] = []
    ) -> Record {
        var all = items.map { (at: at($0.0), text: $0.1, source: $0.2) }
        all += before.map { (at: at($0.1, day: -$0.0), text: $0.2, source: $0.3) }
        all.sort { $0.at > $1.at }
        var properties: [String: JSONValue] = [
            "line": .string(line), "since": .string(stamp(min(at("01:57"), all.last?.at ?? .distantFuture))), "until": ts("23:00"),
            "count": .number(Decimal(all.count)),
            "items": .array(
                all.map { item in
                    var value: JSONObject = ["at": .string(stamp(item.at)), "text": .string(item.text)]
                    if let source = item.source { value["sources"] = [.string(source)] }
                    return .object(value)
                }),
        ]
        if key != TodayKinds.unfiled { properties["project"] = .string("\(listKind)/\(key)") }
        if let needsOwner {
            properties["needsOwner"] = true
            properties["needsOwnerReason"] = .string(needsOwner)
        }
        return Record(id: key, kind: TodayKinds.digest, properties: properties, version: 1)
    }

    // MARK: What the digests were written from

    static let provider = "providers.substrate.reamde.dev"
    static let messageKind = "\(provider)/slack/message"
    static let channelKind = "\(provider)/slack/conversation"
    static let mailKind = "\(provider)/google/gmailmessage"

    /// A message of the invented workspace, as a digest's item names it.
    static func said(_ id: String) -> String { "\(messageKind)/\(id)" }
    static func mail(_ id: String) -> String { "\(mailKind)/\(id)" }
    static func issue(_ id: String) -> String { "\(provider)/linear/issue/\(id)" }
    static func pull(_ id: String) -> String { "\(provider)/github/pullrequest/\(id)" }
    static func invite(_ id: String) -> String { "\(provider)/google/calendarevent/\(id)" }
    static func file(_ id: String) -> String { "\(provider)/google/drivefile/\(id)" }
    /// A chat mirrored from a bridge, which has no page anywhere.
    static func chat(_ id: String) -> String { "\(provider)/beeper/message/\(id)" }

    /// The invented workspace's conversations: channels with a name, a
    /// direct message, which Slack gives none, and a group one, whose name
    /// Slack makes up.
    public static var channels: [Record] {
        func channel(_ id: String, _ properties: [String: JSONValue]) -> Record {
            Record(id: id, kind: channelKind, properties: properties.merging(["conversationId": .string(id.uppercased())]) { own, _ in own }, version: 1)
        }
        return [
            channel("c0reliab", ["name": "api-reliability", "isChannel": true]), channel("c0alerts", ["name": "api-alerts", "isChannel": true]),
            channel("c0secops", ["name": "security-ops", "isChannel": true]), channel("c0lantern", ["name": "lantern-dev", "isChannel": true]),
            channel("c0hiring", ["name": "hiring-loop", "isChannel": true, "isPrivate": true]),
            channel("c0north", ["name": "northfield", "isChannel": true]), channel("d0kenji", ["isIm": true]),
            channel("g0leads", ["name": "mpdm-leo--jun--you-1", "isMpim": true]),
        ]
    }

    /// The records the invented day's digests were written from, with the
    /// records read beside them: messages in those conversations, mails
    /// with their thread and account, issues, pull requests, an invitation
    /// and a document.
    public static var written: [Record] {
        func message(_ id: String, in channel: String, type: String? = nil) -> Record {
            var properties: [String: JSONValue] = [
                "permalink": .string(
                    "https://example.slack.com/archives/\(channel.uppercased())/p\(id.utf8.reduce(1_700_000) { ($0 &* 31 &+ Int($1)) % 1_000_000_000 })"),
                "channel": .string("\(channelKind)/\(channel)"),
            ]
            if let type { properties["channelType"] = .string(type) }
            return Record(id: id, kind: messageKind, properties: properties, version: 1)
        }
        func letter(_ id: String, thread: String) -> [Record] {
            [
                Record(
                    id: id, kind: mailKind,
                    properties: [
                        "messageId": .string(thread), "thread": .string("\(provider)/google/gmailthread/\(thread)"),
                        "account": .string("\(provider)/google/account/owner"),
                    ],
                    version: 1),
                Record(id: thread, kind: "\(provider)/google/gmailthread", properties: ["threadId": .string(thread)], version: 1),
            ]
        }
        var out = channels
        out += [
            message("m-note", in: "c0reliab"), message("m-writeup", in: "c0reliab"), message("m-cause", in: "c0reliab"), message("m-claims", in: "c0reliab"),
            message("m-alert", in: "c0alerts"), message("m-rollback", in: "c0alerts"), message("m-vigil", in: "c0secops"),
            message("m-patch", in: "c0secops"), message("m-kenji-reply", in: "d0kenji", type: "im"), message("m-wei-pr", in: "c0lantern"),
            message("m-wei-volume", in: "c0lantern"), message("m-marcus-thanks", in: "c0lantern"), message("m-noor", in: "c0hiring"),
            message("m-drain", in: "c0north"), message("m-stefan", in: "c0north"), message("m-leo", in: "g0leads"),
        ]
        out += letter("g-vendors", thread: "18c2f0a1b2c30001") + letter("g-grant", thread: "18c2f0a1b2c30002")
        out.append(Record(id: "owner", kind: "\(provider)/google/account", properties: ["email": "owner@example.com"], version: 1))
        out += [
            Record(id: "ops-2038", kind: "\(provider)/linear/issue", properties: ["url": "https://linear.app/example/issue/OPS-2038"], version: 1),
            Record(id: "ops-1987", kind: "\(provider)/linear/issue", properties: ["url": "https://linear.app/example/issue/OPS-1987"], version: 1),
            Record(
                id: "pr-4410", kind: "\(provider)/github/pullrequest",
                properties: ["url": "https://api.github.com/repos/example/api/pulls/4410", "htmlUrl": "https://github.com/example/api/pull/4410"], version: 1),
            Record(
                id: "pr-4402", kind: "\(provider)/github/pullrequest",
                properties: ["url": "https://api.github.com/repos/example/api/pulls/4402", "htmlUrl": "https://github.com/example/api/pull/4402"], version: 1),
            Record(
                id: "e-callum", kind: "\(provider)/google/calendarevent", properties: ["htmlLink": "https://www.google.com/calendar/event?eid=callumfriday"],
                version: 1),
            Record(
                id: "e-euai", kind: "\(provider)/google/calendarevent", properties: ["htmlLink": "https://www.google.com/calendar/event?eid=euaicall"],
                version: 1),
            Record(
                id: "e-review", kind: "\(provider)/google/calendarevent", properties: ["htmlLink": "https://www.google.com/calendar/event?eid=vendorreview"],
                version: 1),
            Record(
                id: "f-custody", kind: "\(provider)/google/drivefile", properties: ["webViewLink": "https://docs.google.com/document/d/custodydoc/edit"],
                version: 1),
            Record(id: "b-product-review", kind: "\(provider)/beeper/message", properties: ["text": "The review moved."], version: 1),
        ]
        return out
    }

    /// The page of each of those records, as reading them finds it
    /// (`SourcePages`): a message with its channel, a mail composed from
    /// its thread and account, and none for a chat that has no address.
    public static var writtenPages: [String: SourcePage] {
        let records = Dictionary(written.map { ("\($0.kind)/\($0.id)", $0) }, uniquingKeysWith: { first, _ in first })
        var out: [String: SourcePage] = [:]
        for (path, record) in records {
            var related: [String: Record] = [:]
            for other in SourcePages.related(record) { related[other] = records[other] }
            if let page = SourcePages.page(of: record, with: related) { out[path] = page }
        }
        return out
    }

    static let names: [String: String] = [
        "tomas": "Tomas Varga", "viktor": "Viktor Lund", "henrik": "Henrik Aalto", "karim": "Karim Saleh", "casey": "Casey Moreau",
        "callum": "Callum Reyes", "marcus": "Marcus Bell", "grant": "Grant Okafor", "stefan": "Stefan Kovac", "kenji": "Kenji Mori",
        "peter": "Peter Lindqvist", "marco": "Marco Rinaldi", "wei": "Wei Zhao", "maya": "Maya Keller", "ben": "Ben Arnold",
        "farid": "Farid Aziz", "patrick": "Patrick Doyle",
    ]

    static let morning = [
        "vitd": ("Vitamin D", "1000", "iu"), "mag": ("Magnesium", "200", "mg"), "omega": ("Omega-3", "1", "capsule"), "zinc": ("Zinc", "15", "mg"),
        "pro": ("Probiotic", "1", "capsule"),
    ]
    static let evening = ["mag2": ("Magnesium", "200", "mg"), "iron": ("Iron", "14", "mg"), "k2": ("Vitamin K2", "100", "mcg"), "zinc2": ("Zinc", "15", "mg")]

    /// The morning doses in name order, a minute apart from 07:30.
    static var morningSlots: [(key: String, slot: String)] {
        morning.sorted { $0.value.0 < $1.value.0 }.enumerated().map { (key: $1.key, slot: String(format: "07:%02d", 30 + $0)) }
    }

    /// The day as it stands before the owner wakes.
    static var base: [Record] {
        var out: [Record] = [
            list(
                "extapi", "External API reliability", "high",
                summary: "For 35 minutes overnight about one request in five failed. A release was rolled back and it has been steady since."),
            list("euai", "EU AI Act", "high", summary: "Your call is today at 19:30 with the data platform group."),
            list("lantern", "Lantern", "high", summary: "Wei is away from Monday until 22 January. Two things wait on you."),
            list("hiring", "Hiring", "medium"), list("ghorgs", "GitHub orgs", "medium"), list("security", "Security hardening"),
            list("northfield", "Northfield bring-up"), list("bastion", "Bastion"), list("tally", "Tally"), list("home", "Home"),
            list("admin", "Paperwork"),
        ]
        out += names.sorted { $0.key < $1.key }.map { contact($0.key, $0.value) }
        let early = at("18:00", day: -7)
        out += [
            task(
                "p-vendor", "Verify the safety vendor endpoint claims", list: "extapi", flag: "high", created: at("20:26", day: -2),
                why: "A day late, and the vendors saw last night's errors.", quote: "I will go over these tomorrow.", extra: ["dueDay": "2026-11-18"]),
            task(
                "t-vigil", "Triage the Vigil findings and review the four lockdown PRs", list: "security", flag: "high", people: ["patrick"],
                created: at("02:10"), why: "Over 90 findings with no owner in the sources checked, and the 20:00 sync will ask."),
            task(
                "t-proxy", "Get the proxy update PR approved and deployed", list: "security", flag: "medium", created: at("01:57"),
                why: "Two published vulnerabilities. A bot approved; it needs a person.", extra: ["dueDay": "2026-11-19"]),
            task(
                "t-incident", "Read the incident write-up", list: "extapi", flag: "medium", created: at("04:05"), why: "Due by noon.",
                extra: ["dueAt": ts("12:00"), "dueDay": "2026-11-19"]),
            task("t-lin", "Enter your score for Lin", list: "hiring", flag: "medium", created: early, why: "Friday's debrief is at 21:00."),
            task(
                "t-henrik", "Read Henrik's Bastion proposal", list: "bastion", flag: "medium", people: ["henrik"], created: early,
                why: "You see Henrik at 15:00."),
            task(
                "t-pharmacy", "Order the supplements refill", list: "home", flag: "low", created: early, why: "It is overdue. Nothing else waits on it today."),
            task("t-leo", "Read the Product Review decisions", flag: "low", created: at("01:52"), why: "Reading. Nothing waits on it."),
            task("t-tally", "Fix and resubmit Tally", list: "tally", flag: "low", created: early, why: "Your own project; no work hours today."),
            task(
                "t-passport", "Renew the passport", list: "admin", flag: "low", created: at("09:00", day: -40), why: "Saturday morning has nothing in it.",
                extra: ["deferrals": 3]),
            task(
                "t-form", "Fill in the council form", list: "admin", flag: "low", created: at("09:00", day: -2),
                why: "With the passport renewal: the same kind of sitting."),
            task("t-followup", "Book the follow-up visit", flag: "medium", created: early, why: "No date yet."),
            task(
                "t-keys", "Buy security keys", list: "admin", flag: "low", created: at("09:00", day: -56), why: "Say the word and Moss prepares the order.",
                extra: ["deferrals": 5]),
            task(
                "p-feedback", "Give Peter the interview feedback", list: "hiring", flag: "medium", people: ["peter"], created: at("10:00", day: -1),
                why: "The debrief is Friday at 21:00."),
            task("t-contrib", "Reply to a contributor on PR 31", list: "tally", flag: "low", created: early, why: "Not work hours."),
        ]
        // The thirteen suggested tasks, each with the assistant's answer.
        let suggested: [(String, String, String?, String, [String], Date, String, String, String?)] = [
            (
                "p-kenji", "Sync with Kenji about the stopped VMs", "security", "high", ["kenji"], at("21:37", day: -1), "task", "You said in the morning.",
                "I will catch Kenji in the morning, we are in the same office"
            ),
            (
                "p-tomas", "Fix the clash on the 1:1 with Tomas", nil, "medium", ["tomas"], at("15:06", day: -1), "task", "You see Tomas at 14:20.",
                "Yes. Fixing it."
            ),
            (
                "p-casey", "Answer Casey about renaming the account", nil, "medium", ["casey"], at("15:10", day: -1), "task",
                "Marco finished the rename at 00:42.", nil
            ),
            (
                "p-marcus", "Send Marcus the projects list", "lantern", "high", ["marcus"], at("16:00", day: -28), "task", "You see Marcus at 17:40.",
                "I can send you a couple of projects people are working on now and next"
            ),
            (
                "p-dataplatform", "Find the docs for the 19:30 call", "euai", "high", [], at("22:45", day: -1), "task",
                "The call is at 19:30 and the 16:30 gap is the only free time before it.", "I will try to find the relevant docs"
            ),
            (
                "p-ticket", "File the IT ticket about permissions", "security", "medium", [], at("20:54", day: -1), "task", "The permissions sync is at 20:00.",
                nil
            ),
            (
                "t-stefan", "Reply to Stefan on the inventory, Halden and Brightwell", "northfield", "high", ["stefan"], at("11:00", day: -2), "task",
                "Halden and Brightwell is Friday at 20:00.", nil
            ),
            ("t-wei", "Review Wei's refactor, PR 4188", "lantern", "medium", ["wei"], at("05:09"), "task", "Wei is away from Monday until 22 January.", nil),
            ("p-marco", "Tell Marco what you drink", nil, "low", ["marco"], at("00:36"), "task", "No day was said.", nil),
            ("p-origin", "Ask about the encryption question", "security", "low", [], at("22:29", day: -2), "task", "No day was said.", "I will ask, though"),
            (
                "o-farid", "Tell Farid what happened once the announcement is out", nil, "low", ["farid"], at("10:00", day: -8), "task",
                "It waits on the announcement.", nil
            ),
            (
                "o-ben", "Send Ben the retro action items", nil, "low", ["ben"], at("10:00", day: -9), "task", "No day was said.",
                "I will send some action items over after the weekend."
            ),
            (
                "o-maya", "Offer Maya any free slot for the urgent candidate interview", "hiring", "low", ["maya"], at("10:00", day: -14), "dismiss",
                "Two weeks old today, and the two days you offered have passed.", nil
            ),
        ]
        for s in suggested {
            // Each says when its words were said: its record was written a few minutes after.
            out.append(
                task(
                    s.0, s.1, list: s.2, flag: s.3, status: "proposed", people: s.4, created: s.5.addingTimeInterval(240), quote: s.8,
                    extra: ["originAt": .string(stamp(s.5)), "origin": .string(origin(s.0))]))
            out.append(
                advice(
                    s.0, s.6, s.7,
                    objection: s.0 == "p-marcus"
                        ? "This is four weeks old and you see Marcus at 17:40. The list can be drafted from Linear for you to check." : nil))
        }
        out += morning.sorted { $0.key < $1.key }.map { schedule($0.key, $0.value.0, $0.value.1, $0.value.2, group: "Morning medication") }
        out += evening.sorted { $0.key < $1.key }.map { schedule($0.key, $0.value.0, $0.value.1, $0.value.2, group: "Evening medication") }
        out.append(schedule("b12", "Vitamin B12", "1", "tablet", group: "Weekly medication"))
        out += (morning.merging(evening) { first, _ in first }).sorted { $0.key < $1.key }.map { medication($0.key, $0.value.0, $0.value.1, $0.value.2) }
        out += [routine("skin", "Skincare"), routine("meditate", "Meditate"), routine("skin2", "Evening skincare"), routine("read", "Read twenty pages")]
        // Nothing has been logged since 17 October.
        out.append(log(scheduleKind, "vitd", slot: at("07:30", day: -33), at: at("07:41", day: -33)))
        // Each item names what it was written from: a message in a channel, a mail, an issue, a pull request, an
        // invitation, a document. One names a chat that has no page, and one names nothing.
        out += [
            digest(
                "extapi", line: "Errors from 03:12 to 03:47, rolled back and fixed.",
                [
                    ("02:50", "A release went out.", pull("pr-4410")),
                    ("03:12", "The error rate rose to about one request in five. The alert fired.", said("m-alert")),
                    ("03:47", "Rolled back. Errors back to normal.", said("m-rollback")),
                    ("04:05", "The on-call posted a first note: the cause is not known yet. Write-up by noon.", said("m-note")),
                    ("12:02", "The write-up for last night's incident is posted.", said("m-writeup")),
                    ("20:12", "Wes posted the cause of both incidents: one config flag.", said("m-cause")),
                ],
                before: [
                    (1, "16:40", "The vendors were told a release was planned for the night.", mail("g-vendors")),
                    (1, "11:05", "Wes opened the config change behind the release.", pull("pr-4402")),
                    (1, "09:30", "The safety vendor published its endpoint claims.", said("m-claims")),
                    (2, "15:20", "The vendor review was booked for Thursday 26 November.", invite("e-review")),
                    (2, "10:10", "A ticket asks for a second alert on the error rate.", issue("ops-1987")),
                ]),
            digest(
                "euai", line: "Jun left suggestions on the custody doc at 00:56.",
                [
                    ("05:30", "Jun left more suggestions on the custody doc you sent.", file("f-custody")),
                    ("13:05", "Two more people accepted your 19:30 call. Eight are coming.", invite("e-euai")),
                ]),
            digest(
                "lantern", line: "Wei posted the backend refactor, PR 4188, with you tagged.",
                [
                    ("05:09", "Wei posted the backend refactor, PR 4188, with you tagged.", said("m-wei-pr")),
                    ("05:16", "Wei posted a volume API proposal.", said("m-wei-volume")),
                    ("09:10", "Callum accepted Friday at 16:30. The 17:20 slot is free.", invite("e-callum")),
                    ("16:12", "Marcus thanked you for the list and asked one follow-up question.", said("m-marcus-thanks")),
                ]),
            digest(
                "security", line: "Vigil filed over 90 cluster findings in one minute. None is triaged in the sources checked.",
                needsOwner: "Nobody has these in the sources checked. Are they yours?",
                [
                    ("02:03", "Vigil filed over 90 cluster findings in one minute. None is triaged in the sources checked.", said("m-vigil")),
                    ("09:34", "Ned closed nine of the 26 noisy alert tickets.", issue("ops-2038")),
                    ("11:12", "Kenji replied: the VMs can stay stopped until Monday.", said("m-kenji-reply")),
                ],
                before: [(1, "17:25", "The proxy vulnerabilities were announced, with a patch.", said("m-patch"))]),
            digest(
                "hiring", line: "Noor questioned one interview role as unapproved.",
                [
                    ("02:30", "Noor questioned one interview role as unapproved.", said("m-noor")),
                    ("10:15", "Grant's CV was updated before tonight's interview.", mail("g-grant")),
                ]),
            digest(
                "northfield", line: "Stefan gave the go-ahead on one node.",
                [
                    ("04:17", "Northfield asked for a list of nodes to be drained for a cable swap.", said("m-drain")),
                    ("06:23", "Stefan gave the go-ahead on one node.", said("m-stefan")),
                ]),
            digest(
                TodayKinds.unfiled, line: "Leo shared the Product Review decisions.",
                [
                    ("02:10", "Leo shared the decisions from the first Product Review.", said("m-leo")),
                    ("17:05", "The Product Review moved to next Thursday.", chat("b-product-review")),
                ]),
        ]
        out.append(
            Record(
                id: TodayKinds.preferenceID, kind: TodayKinds.preference,
                properties: ["lastActiveAt": ts("01:57"), "timezone": "Europe/Lisbon", "urgentWait": "PT2M"], version: 1))
        out.append(
            Record(
                id: "tomas", kind: TodayKinds.dossier,
                properties: [
                    "purpose": "Weekly 1:1. Tomas has asked twice for this slot to stop clashing with two other meetings.",
                    "since": "Started the scale testing of the agent sandbox. Overnight, unblocked Lantern's image registration with a flag.",
                    "raise": ["Tomas sent core PR 4127 with no words. It reads as a review request."], "after": "The scorecard is due before Friday's debrief.",
                ], version: 1))
        return out
    }

    /// The open requests: what the assistant suggests on the priorities.
    static var requests: [Record] {
        [
            Record(
                id: "req-security", kind: TodayKinds.patchRequest,
                properties: [
                    "target": .string("\(listKind)/security"), "decision": "proposed", "diff": ["properties": ["priority": "high"]],
                    "rationale": "Not on your list. Over 90 findings came in overnight with no owner in the sources checked.",
                ], version: 1),
            Record(
                id: "req-ghorgs", kind: TodayKinds.patchRequest,
                properties: [
                    "target": .string("\(listKind)/ghorgs"), "decision": "proposed", "diff": ["properties": ["priority": "low"]],
                    "rationale": "Nothing new on it this week.",
                ], version: 1),
        ]
    }

    static var meetings: [Record] {
        [
            event("physio", "08:00", "08:15", "Physiotherapy call", people: ["Dana Whitlock"], meet: false),
            // The same call as a second account's calendar holds it, and what
            // that calendar shows of the 1:1: one meeting each, never two.
            event("physio-second", "08:00", "08:15", " physiotherapy call ", people: ["Dana Whitlock", "Ines Castel"]),
            event("tomas-busy", "14:20", "14:40", "Busy", meet: false),
            event("tomas", "14:20", "14:40", "Tomas", people: ["Tomas Varga"]),
            event("viktor", "14:40", "15:00", "Viktor", people: ["Viktor Lund"], meet: false),
            event("henrik", "15:00", "15:20", "Henrik", people: ["Henrik Aalto"]),
            event("karim", "15:00", "15:30", "Karim", people: ["Karim Saleh"], meet: false),
            event("casey", "15:40", "16:00", "Casey", people: ["Casey Moreau"], meet: false), event("debrief", "16:00", "16:30", "Hiring debrief", count: 12),
            event("standup", "17:00", "17:30", "Lantern standup", count: 11), event("callum", "17:20", "17:40", "Callum", people: ["Callum Reyes"]),
            event("weekly", "17:30", "18:15", "Beacon and Lantern weekly", count: 10),
            event("marcus", "17:40", "18:00", "Marcus", people: ["Marcus Bell"], meet: false),
            event("interview", "18:30", "19:30", "Interview: Grant Okafor", people: ["Grant Okafor"], meet: false),
            event("euai", "19:30", "20:00", "EU AI Act, Sprout, checkpoints", count: 6), event("groups", "20:00", "20:30", "Groups and permissions", count: 4),
            event("cloud", "21:00", "21:30", "Cloud standup", count: 12), event("audit", "21:00", "21:30", "Audit tiger team", count: 8),
            event("presentation", "14:00", "15:00", "Presentation interview", meet: false, day: 1),
            event("allhands", "17:00", "18:00", "All hands", count: 40, day: 1),
        ]
    }

    static var occurrences: [Record] {
        var out: [Record] = []
        for offset in -1 ... 2 {
            // The steps of a group sit a minute apart, in the order they are taken.
            out += morningSlots.map { occurrence(scheduleKind, $0.key, at($0.slot, day: offset)) }
            out += evening.keys.sorted().map { occurrence(scheduleKind, $0, at("19:30", day: offset)) }
            out += [
                occurrence(routineKind, "skin", at("07:35", day: offset)), occurrence(routineKind, "meditate", at("07:45", day: offset)),
                occurrence(routineKind, "skin2", at("21:45", day: offset)), occurrence(routineKind, "read", at("22:00", day: offset)),
            ]
        }
        // Weekly, on Wednesday: missed yesterday.
        out.append(occurrence(scheduleKind, "b12", at("07:30", day: -1)))
        // Yesterday's own are logged, all but the weekly one.
        return out
    }

    // MARK: A repository that was not answered

    /// A description of three paragraphs that names where it came from, as
    /// a task made from a long message holds one.
    static let longDescription = """
        Asked in the finance channel whether the October ledger was checked against the bank export, and you said you would look before the quarter closes.

        The working notes list four accounts that did not match, two of them by less than a euro, and one with no export at all for the second week.

        Source: substrate.reamde.dev/chat/message/7f3c1b1e-2f0a-4c57-9a55-0d1b6f6a2c11 and https://example.com/finance/ledger/2026/october/reconciliation-working-notes?view=full&highlight=unmatched
        """

    /// Contacts as an address book leaves them: one with no `name`, one
    /// whose `name` is a handle in lower case, one with a title alone.
    static var namelessContacts: [Record] {
        [
            Record(
                id: "odile", kind: TodayKinds.contact,
                properties: ["givenName": "Odile", "familyName": "Marchetti", "nickname": "odilem", "title": "odilem"], version: 1),
            Record(id: "rsato", kind: TodayKinds.contact, properties: ["name": "rsato", "title": "rsato"], version: 1),
            Record(id: "pilar", kind: TodayKinds.contact, properties: ["title": "Pilar Quint"], version: 1),
        ]
    }

    /// A hundred invented suggestions nobody answered, spread over the last
    /// thirteen days. Three have the assistant's answer; the newest has a
    /// long description, and two name a contact with no `name`.
    static var backlog: [Record] {
        let verbs = ["Reply about", "Send", "Check", "Book a slot for", "Review", "Ask about", "Write up", "Confirm", "Chase", "Draft"]
        let things = [
            "the venue quote", "the audit sample", "the travel form", "the onboarding notes", "the rota change", "the storage bill",
            "the badge request", "the survey answers", "the release notes", "the seating plan",
        ]
        var out: [Record] = []
        for index in 1 ... 100 {
            let id = String(format: "b-%03d", index)
            let created = at("06:00", day: -(index % 13)).addingTimeInterval(Double(index) * 60)
            let people = index == 78 ? ["odile"] : index == 65 ? ["rsato"] : []
            out.append(
                task(
                    id, "\(verbs[(index - 1) % 10]) \(things[(index - 1) / 10])", status: "proposed", people: people, created: created,
                    quote: index == 91 ? longDescription : index % 4 == 0 ? "I will get to it this week." : nil))
        }
        out += [
            advice("b-001", "task", "It was asked for by Friday."), advice("b-002", "done", "The form was sent on Tuesday."),
            advice("b-003", "dismiss", "The rota was settled without you."),
        ]
        return out
    }

    /// The backlog laid over a moment of the invented day: the hundred, the
    /// contacts they name, and the assistant's answer on two of the day's own.
    static func crowd(_ state: inout TodayApp.State) {
        for (key, record) in state.linked.records where record.kind == TodayKinds.advice && !["p-kenji", "o-maya"].contains(record.id) {
            state.linked.records[key] = nil
        }
        for record in backlog { state.linked.records[RecordFold.key(record.kind, record.id)] = record }
        for record in namelessContacts { state.contacts["\(record.kind)/\(record.id)"] = record }
    }

    /// "Other" picked on one suggestion, and another whose description is
    /// Markdown that names where it came from in brackets.
    static func other(_ state: inout TodayApp.State) {
        state.drafts.suggested["p-ticket"] = .other
        state.linked.records[RecordFold.key(taskKind, "p-kenji")]?.properties["description"] = """
            Agreed in the sync: you would catch Kenji **in the morning**, since you share an office.

            **Open question:** can the VMs stay stopped until Monday? (source: providers.example.dev/chat/message/chat-51c2a9)
            """
    }

    /// One suggestion answered in words, with the assistant's answer.
    static func told(_ state: inout TodayApp.State) {
        let words = "Henrik answered this in the review. Close it, and stop making tasks from that thread."
        let record = Record(
            id: "c-origin", kind: TodayKinds.correction,
            properties: ["subject": ["ref": .string("\(taskKind)/p-origin")], "text": .string(words)], version: 1, createdAt: stamp(at("08:29")))
        state.linked.records[RecordFold.key(record.kind, record.id)] = record
        state.exchanges = [
            Exchange(
                id: "x-origin", subject: .task("p-origin"), question: words,
                text: "I dismissed it. Henrik's answer is in the review notes from Wednesday.", state: .done, at: at("08:29"))
        ]
    }

    /// Two tasks with the long description: one due today, which leads,
    /// and one moved to this morning, whose row has no other line.
    static func longTasks(_ state: inout TodayApp.State) {
        let tasks = [
            task(
                "t-ledger", "Check the October ledger against the bank export", list: "extapi", flag: "high", created: at("09:00", day: -3),
                quote: longDescription, extra: ["dueDay": "2026-11-19"]),
            task(
                "t-archive", "Move the old exports to the archive", flag: "medium", created: at("09:00", day: -4), quote: longDescription,
                extra: ["notBefore": ts("10:00")]),
        ]
        for record in tasks { state.linked.records[RecordFold.key(record.kind, record.id)] = record }
    }

    // MARK: The day as the clock moves

    /// What happened by `now`, laid over the base: logs, answers, the plan,
    /// what got done, and what was marked urgent.
    /// - Parameter kept: yesterday's routines were logged; false is a
    ///   repository where logging stopped on 17 October.
    static func records(at now: Date, urgent: Bool = true, held: Bool = true, kept: Bool = true) -> [Record] {
        var byKey: [String: Record] = [:]
        for record in base { byKey["\(record.kind)/\(record.id)"] = record }
        func set(_ record: Record) { byKey["\(record.kind)/\(record.id)"] = record }
        func change(_ id: String, _ properties: [String: JSONValue], at stamp: Date? = nil) {
            guard var record = byKey["\(taskKind)/\(id)"] else { return }
            for (key, value) in properties { record.properties[key] = value }
            record.version += 1
            if let stamp { record.updatedAt = Self.stamp(stamp) }
            set(record)
        }
        // Yesterday's routines were kept, the weekly dose aside.
        if kept {
            for dose in morningSlots { set(log(scheduleKind, dose.key, slot: at(dose.slot, day: -1), at: at("07:40", day: -1))) }
            for key in evening.keys { set(log(scheduleKind, key, slot: at("19:30", day: -1), at: at("20:31", day: -1))) }
            for (key, slot) in [("skin", "07:35"), ("meditate", "07:45"), ("skin2", "21:45"), ("read", "22:00")] {
                set(log(routineKind, key, slot: at(slot, day: -1), at: at(slot, day: -1)))
            }
        }
        func past(_ clock: String) -> Bool { at(clock) <= now }
        if past("07:33") {
            for dose in morningSlots { set(log(scheduleKind, dose.key, slot: at(dose.slot), at: at("07:33"))) }
            set(log(scheduleKind, "b12", slot: at("07:30", day: -1), at: at("07:33")))
        }
        if past("07:41") { set(log(routineKind, "skin", slot: at("07:35"), at: at("07:41"))) }
        if past("07:51") { set(log(routineKind, "meditate", slot: at("07:45"), at: at("07:51"))) }
        if past("08:27") {
            set(
                Record(
                    id: TodayKinds.preferenceID, kind: TodayKinds.preference,
                    properties: ["lastActiveAt": ts("08:27"), "nightReadAt": ts("08:27"), "timezone": "Europe/Lisbon"], version: 2))
            for key in ["extapi", "euai", "lantern", "security", "hiring", "northfield", TodayKinds.unfiled] {
                set(Record(id: key, kind: TodayKinds.reviewmark, properties: ["through": ts("08:27")], version: 1))
            }
        }
        if past("08:38") {
            for id in [
                "p-kenji", "p-tomas", "p-casey", "p-marcus", "p-dataplatform", "p-ticket", "t-stefan", "t-wei", "p-marco", "p-origin", "o-farid", "o-ben",
            ] {
                change(id, ["status": "open", "prioritySetBy": "owner"])
            }
            change("o-maya", ["status": "abandoned"])
        }
        let planned = [
            "p-kenji", "p-vendor", "t-proxy", "t-stefan", "t-vigil", "t-incident", "p-marcus", "t-lin", "t-pharmacy", "t-henrik", "p-dataplatform", "p-ticket",
            "t-contrib",
        ]
        if past("08:44") {
            set(
                Record(
                    id: "d20261119", kind: TodayKinds.plan,
                    properties: [
                        "day": "2026-11-19", "picked": .array(planned.map { .string("\(taskKind)/\($0)") }), "savedAt": ts("08:44"),
                        "overlaps": [
                            [
                                "events": .array(["henrik", "karim"].map { .string("\(eventKind)/\($0)") }), "chosen": .string("\(eventKind)/henrik"),
                                "at": ts("08:44"),
                            ],
                            [
                                "events": .array(["standup", "callum"].map { .string("\(eventKind)/\($0)") }), "chosen": .string("\(eventKind)/standup"),
                                "at": ts("08:44"),
                            ],
                            ["events": .array(["weekly", "marcus"].map { .string("\(eventKind)/\($0)") }), "chosen": "both", "at": ts("08:44")],
                            [
                                "events": .array(["cloud", "audit"].map { .string("\(eventKind)/\($0)") }), "chosen": .string("\(eventKind)/audit"),
                                "at": ts("08:44"),
                            ],
                        ],
                    ], version: 1))
            // What yesterday left was answered on the plan page: the vendor check is for today.
            change("p-vendor", ["dueDay": "2026-11-19"])
            change("p-dataplatform", ["notBefore": ts("16:30")])
            change("p-ticket", ["notBefore": ts("16:30")])
            change("t-contrib", ["notBefore": ts("21:30")])
        }
        for (id, clock) in [
            ("p-kenji", "09:20"), ("t-proxy", "10:05"), ("t-stefan", "10:50"), ("t-incident", "12:10"), ("p-marcus", "13:05"), ("t-lin", "13:30"),
            ("t-henrik", "13:50"), ("p-dataplatform", "16:52"), ("p-ticket", "20:44"),
        ]
        where past(clock) { change(id, ["status": "done", "completedAt": ts(clock)]) }
        if past("13:55") {
            set(
                Record(
                    id: "t-vigil", kind: TodayKinds.returnpoint,
                    properties: [
                        "record": .string("\(taskKind)/t-vigil"), "mark": "note", "text": "Findings sorted to OPS-2041. PR 3 of 4 is half read.",
                        "at": ts("13:55"),
                    ], version: 1))
        }
        // The one thing in the day that cannot wait, until it is settled.
        if urgent, past("11:38"), !past("12:05") {
            set(
                task(
                    "inc-2", "External API is failing again", list: "extapi", flag: "urgent", created: at("11:38"),
                    why: "Since 11:31 about one request in ten fails. The on-call has it, and asks whether to roll back this morning's release.",
                    extra: ["prioritySetBy": "automation", "priorityWas": "high", "urgentCheckedAt": ts("11:39")]))
        }
        if past("12:05") {
            set(
                task(
                    "inc-2", "External API is failing again", list: "extapi", flag: "high", status: "done", created: at("11:38"),
                    extra: ["completedAt": ts("12:05")]))
        }
        // Heard during the day: a suggested task with its own Save.
        if past("14:31") {
            set(
                task(
                    "h-tomas-pr", "Review core PR 4127 by Monday", flag: "medium", status: "proposed", people: ["tomas"], created: at("14:31"),
                    extra: ["originAt": ts("14:28")]))
            set(advice("h-tomas-pr", "task", "Tomas said Monday."))
        }
        // Marked urgent by a rule, and the assistant thinks it can wait.
        if past("16:22") {
            var extra: [String: JSONValue] = [
                "priority": "urgent", "prioritySetBy": "automation", "priorityWas": "medium",
                "priorityReason": "Wei wrote again about the refactor review at 16:22.",
            ]
            if held {
                extra["urgentHeldAt"] = ts("16:23")
                extra["urgentHeldReason"] = "It is a task for Friday morning, and Wei is here until Monday."
            }
            change("t-wei", extra, at: at("16:22"))
        }
        if past("20:31") { for key in evening.keys { set(log(scheduleKind, key, slot: at("19:30"), at: at("20:31"))) } }
        return byKey.values.sorted { ($0.kind, $0.id) < ($1.kind, $1.id) }
    }

    /// The root's state at a moment of the invented day.
    public static func state(at clock: String, urgent: Bool = true, held: Bool = true, reachable: Bool = true, edit: (inout TodayApp.State) -> Void = { _ in })
        -> TodayApp.State
    {
        let now = at(clock)
        var state = TodayApp.State()
        state.discovery = discovery
        state.linked.loaded = true
        for record in records(at: now, urgent: urgent, held: held) where record.kind != TodayKinds.contact {
            state.linked.records[RecordFold.key(record.kind, record.id)] = record
        }
        for record in base where record.kind == TodayKinds.contact { state.contacts["\(record.kind)/\(record.id)"] = record }
        state.windows.occurrences = occurrences
        state.windows.meetings = meetings
        state.windows.readAt = now
        // The pages of what the tasks were made from and the digests were written from are read already.
        state.sources = sources(state.linked.records.values.filter { $0.kind == taskKind && $0["origin"] != nil }.map(\.id))
            .merging(writtenPages) { _, written in written }
        state.linked.link = LinkSnapshot(phase: .signedIn, server: server, online: true)
        // The suggestions on the priorities are taken at 08:41.
        state.requests = now < at("08:41") ? requests : []
        if now >= at("08:41") {
            for (id, flag) in [("security", "high"), ("ghorgs", "low")] {
                let key = RecordFold.key(listKind, id)
                state.linked.records[key]?.properties["priority"] = .string(flag)
            }
        }
        var notes = DayNotes(day: CivilDay(year: 2026, month: 11, day: 19))
        notes.nightFrom = at("01:57")
        notes.wokeAt = at("07:05")
        notes.lastLook = now
        if now >= at("08:38") {
            notes.suggestedSavedAt = at("08:38")
            notes.suggestedSaved = [12, 0, 1]
        }
        if now >= at("08:44") {
            notes.yesterday["y-task:p-vendor"] = "today"
            notes.yesterdayAt["y-task:p-vendor"] = at("08:44")
        }
        state.notes = notes
        state.notesRead = true
        state.zone = zone.identifier
        state.reachable = reachable
        state.permission = .on
        edit(&state)
        return state
    }

    public static func present(_ state: TodayApp.State, at clock: String) -> TodayPresentation {
        TodayPresenter.present(state, linked: app.linked, build: app.build, now: at(clock), calendar: calendar)
    }

    static func fixture(
        _ name: String, at clock: String, routes: [TodayRoute] = [], urgent: Bool = true, held: Bool = true, reachable: Bool = true,
        edit: (inout TodayApp.State) -> Void = { _ in }
    ) -> Fixture {
        Fixture(name: name, app: present(state(at: clock, urgent: urgent, held: held, reachable: reachable, edit: edit), at: clock), routes: routes)
    }

    /// One scene per moment the design draws.
    public static var all: [Fixture] {
        [
            fixture("today-wake", at: "07:05"),
            // Nothing has been logged since 17 October: the routine says so in one line.
            fixture("today-wake-stale", at: "07:05") { state in
                state.linked.records = [:]
                for record in records(at: at("07:05"), kept: false) where record.kind != TodayKinds.contact {
                    state.linked.records[RecordFold.key(record.kind, record.id)] = record
                }
            },
            fixture("today-routine", at: "07:38"),
            fixture("today-precall", at: "07:52"),
            fixture("today-catchup", at: "08:20"),
            fixture("today-suggested", at: "08:30", routes: [.suggested]),
            fixture("today-suggested-dismiss", at: "08:30", routes: [.suggested]) { $0.drafts.suggested["p-marcus"] = .dismiss },
            // "Other" is the pick on one, whose description is Markdown and names its source in brackets.
            fixture("today-suggested-other", at: "08:30", routes: [.suggested], edit: other),
            // One was answered in words: it is with the assistant, and its exchange is still here.
            fixture("today-suggested-told", at: "08:30", routes: [.suggested, .task("p-origin")], edit: told),
            // A repository with over a hundred suggestions: grouped by project, six of a group at a time.
            fixture("today-backlog", at: "08:30", routes: [.suggested, .task("b-091"), .person(person("odile"))], edit: crowd),
            fixture("today-plan-row", at: "08:40", routes: [.plan, .priorities]),
            fixture("today-plan-draft", at: "08:40", routes: [.plan]) { state in
                var draft = PlanDraft()
                draft.picked["t-leo"] = true
                draft.flags["t-leo"] = .medium
                draft.overlaps["ov-henrik+karim"] = "\(eventKind)/henrik"
                state.drafts.plan = draft
            },
            fixture(
                "today-working", at: "11:30",
                routes: [
                    .projects, .priorities, .project("extapi"), .project("security"), .task("p-vendor"), .task("t-vigil"), .person(person("tomas")),
                    .meeting("tomas"), .plan,
                ]),
            fixture("today-flag-open", at: "11:30") { $0.drafts.held["p-vendor"] = .high },
            // A description of three paragraphs: its opening words in the day, all of it on the task's page.
            fixture("today-long-description", at: "11:30", routes: [.task("t-ledger"), .task("t-archive")], edit: longTasks),
            fixture("today-interrupt", at: "11:40", routes: [.project("extapi"), .task("inc-2")]),
            fixture("today-premeet", at: "14:12", routes: [.meeting("tomas")]),
            fixture("today-meeting", at: "14:25"),
            fixture("today-gap", at: "16:35", routes: [.task("t-wei"), .task("h-tomas-pr")]),
            fixture("today-unreachable", at: "16:35", held: false, reachable: false),
            fixture("today-evening", at: "19:35"),
            fixture("today-wrap", at: "22:15"),
            fixture("today-backlog-wrap", at: "22:15", edit: crowd),
            fixture("today-asked", at: "11:30") { state in
                state.exchanges = [
                    Exchange(
                        id: "x1", subject: .day, question: "What can I drop this afternoon?",
                        text: "Callum is at the standup too, and what Callum has for you is covered at 14:40.", state: .done, at: at("11:29")),
                    Exchange(
                        id: "x2", subject: .task("p-vendor"), question: "Break it into steps", text: "1. Open the vendor thread. 2. Call each endpoint",
                        state: .writing, at: at("11:30")),
                ]
            },
            Fixture(name: "today-empty", app: present(TodayApp.State(), at: "11:30"), routes: [.projects, .priorities, .plan]),
        ]
    }
}

// MARK: The widgets' day

public extension TodayFixtures {
    /// A moment of the invented day as a widget draws it.
    static func glance(at clock: String, edit: (inout TodayApp.State) -> Void = { _ in }) -> GlanceDay {
        GlancePresenter.day(state(at: clock, edit: edit), now: at(clock), calendar: calendar)
    }

    /// The two meetings at 15:00 with no choice made between them: what the
    /// saved plan chose is taken away.
    static func unchosen(_ state: inout TodayApp.State) {
        let key = RecordFold.key(TodayKinds.plan, "d20261119")
        guard let overlaps = state.linked.records[key]?.properties["overlaps"]?.array else { return }
        state.linked.records[key]?.properties["overlaps"] = .array(Array(overlaps.dropFirst()))
    }

    /// One named day per moment a widget gallery draws: waking, with the
    /// routine leading and a count to the first call; the morning, with the
    /// plan saved and the day ahead; the afternoon, in a meeting, with two
    /// that share time still to choose between; a repository with nothing
    /// to draw a day from; and signed out.
    static var glances: [(name: String, day: GlanceDay)] {
        [
            ("wake", glance(at: "07:05")), ("morning", glance(at: "10:30")), ("afternoon", glance(at: "14:25", edit: unchosen)),
            ("empty", GlancePresenter.day(TodayApp.State(), now: at("11:30"), calendar: calendar)), ("closed", .closed(at: at("11:30"))),
        ]
    }
}
