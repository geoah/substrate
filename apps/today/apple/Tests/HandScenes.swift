import Foundation
import SubstrateScreens
import TodayCore

/// Presentation values made by hand from the mock-up's invented day, for
/// drawing the views before, and beside, the core's own fixtures: every
/// name, message and dose here is made up. They hold the states the design
/// guide draws that one day at one time does not reach.
enum HandScenes {
    // MARK: Marks

    static func tile(_ initials: String, suggested: Bool = false) -> Tile { Tile(initials: initials, hue: 210, suggested: suggested) }
    static func badge(_ initials: String) -> Badge { Badge(initials: initials, hue: 200) }
    static func project(_ id: String, _ name: String, _ initials: String) -> ProjectChip {
        ProjectChip(id: id, name: name, tile: tile(initials))
    }
    static let security = project("security", "Security hardening", "SH")
    static let lantern = project("lantern", "Lantern", "LA")
    static let bastion = project("bastion", "Bastion", "BA")

    static func option(
        _ id: String, _ name: String, _ line: String? = nil, selected: Bool = false, suggested: Bool = false, reason: String? = nil,
        stripe: Stripe? = nil, icon: Icon? = nil
    ) -> OptionRow {
        OptionRow(
            id: id, name: name, line: line, selected: selected, suggested: suggested, reason: reason, stripe: stripe, icon: icon,
            command: .refresh)
    }

    static func flags(chosen: Flag?, suggested: Flag? = nil, reason: String? = nil) -> OptionList {
        OptionList(
            .round,
            Flag.allCases.map { flag in
                option(
                    flag.rawValue, flag.word, "After what is flagged above it.", selected: chosen == flag, suggested: suggested == flag,
                    reason: suggested == flag ? reason : nil, stripe: Stripe(flag))
            })
    }

    static func flagControl(_ flag: Flag?, suggested: Flag? = nil, open: Bool = false) -> FlagControl {
        FlagControl(
            stripe: Stripe(suggested ?? flag, suggested: suggested != nil && flag == nil), word: flag?.word ?? "No flag yet",
            options: flags(chosen: flag, suggested: suggested, reason: "The review is due before Monday."),
            notes: open ? ["The order follows when you close this."] : [], open: open)
    }

    // MARK: The strip

    static func strip(suggesting: Bool) -> StripPresentation {
        let standing: [StripEntry] = [
            StripEntry(id: "api", tile: tile("EA"), name: "External API reliability", line: "1 of 2 done", stripe: Stripe(.high), opens: .project("api")),
            StripEntry(id: "eu", tile: tile("EU"), name: "EU AI Act", line: "0 of 1 done", stripe: Stripe(.high), opens: .project("eu")),
            StripEntry(id: "lantern", tile: tile("LA"), name: "Lantern", line: "0 of 1 done", stripe: Stripe(.high), opens: .project("lantern")),
            StripEntry(id: "security", tile: tile("SH"), name: "Security hardening", line: "2 of 4 done", stripe: Stripe(.high), opens: .project("security")),
            StripEntry(id: "hiring", tile: tile("HI"), name: "Hiring", line: "0 of 1 done", stripe: Stripe(.medium), opens: .project("hiring")),
            StripEntry(id: "github", tile: tile("GH"), name: "GitHub orgs", line: "Nothing for today", stripe: Stripe(.low), opens: .project("github")),
        ]
        let suggested: [StripEntry] = [
            StripEntry(id: "api", tile: tile("EA"), name: "External API reliability", line: nil, stripe: Stripe(.high), opens: .project("api")),
            StripEntry(id: "eu", tile: tile("EU"), name: "EU AI Act", line: nil, stripe: Stripe(.high), opens: .project("eu")),
            StripEntry(id: "lantern", tile: tile("LA"), name: "Lantern", line: nil, stripe: Stripe(.high), opens: .project("lantern")),
            StripEntry(id: "hiring", tile: tile("HI"), name: "Hiring", line: nil, stripe: Stripe(.medium), opens: .project("hiring")),
            StripEntry(
                id: "github", tile: tile("GH"), name: "GitHub orgs", line: nil, suggestion: "to Low", stripe: Stripe(.medium),
                opens: .project("github")),
            StripEntry(
                id: "security", tile: tile("SH", suggested: true), name: "Security hardening", line: nil, suggestion: "add as High",
                stripe: Stripe(.high, suggested: true), opens: .priorities),
        ]
        return StripPresentation(date: "Thursday 19 November", dateShort: "Thu 19 Nov", entries: suggesting ? suggested : standing)
    }

    // MARK: Parts of the day

    static let nightRead = NightPresentation(line: "The night, read at 08:27.", unread: false, read: "Read at 08:27.")

    static let nightUnread = NightPresentation(
        line: "Overnight: one incident on External API reliability, fixed at 03:47. 31 other things, one may be yours.", unread: true,
        headline: "One incident on External API reliability, fixed at 03:47. One thing may be yours.",
        coverage: "Checked through 06:51: Slack, mail, calendar, Linear and GitHub.",
        stale: StaleNote("The chat bridge has not synced since Monday."),
        groups: [
            NightPresentation.Group(
                id: "yours", label: "Yours to decide",
                items: [
                    NightItem(
                        id: "security", tile: tile("SH"), name: "Security hardening", count: "4 new", pill: Pill(.caution, "Yours?"),
                        text: "Vigil filed over 90 cluster findings between 02:03 and 02:04, and nobody has them in the sources checked.",
                        lines: [
                            TimelineLine(
                                id: "l1", time: "02:03", who: "Vigil", text: "filed 92 findings on the production cluster.", source: Pill(.neutral, "Vigil")),
                            TimelineLine(
                                id: "l2", time: "22:04", who: "Patrick", text: "said Imran will manage the Vigil sensor.",
                                quote: "“Nobody needs adding to the access group.”", source: Pill(.neutral, "Slack")),
                        ],
                        decision: NightDecision(
                            question: "Nobody has these in the sources checked. Are they yours?",
                            options: OptionList(
                                .round,
                                [
                                    option(
                                        "mine", "They are mine", "A task for this morning, flagged High.", suggested: true, reason: "The 20:00 sync will ask."),
                                    option("ask", "Ask Patrick who has them"), option("not", "Not mine"),
                                ])),
                        open: ButtonPresentation("Open the project", opens: .project("security")),
                        gotIt: ButtonPresentation("Got it", glyph: .done, command: .gotIt(project: "security")))
                ]),
            NightPresentation.Group(
                id: "priorities", label: "On your priorities",
                items: [
                    NightItem(
                        id: "api", tile: tile("EA"), name: "External API reliability", stripe: Stripe(.high), count: "6 new",
                        text: "For 35 minutes about one request in five failed. The on-call rolled back the 02:50 release and it recovered."),
                    NightItem(
                        id: "lantern", tile: tile("LA"), name: "Lantern", stripe: Stripe(.high), count: "read 08:20",
                        text: "Wei posted the backend refactor with you tagged.", seen: true),
                ]),
        ],
        quiet: "Three had nothing: Hiring, Tally and Home.", gotIt: ButtonPresentation("Got it, all read", glyph: .done, role: .primary, command: .readNight))

    /// A thing finished today, as "Earlier today" holds it: closed, crossed
    /// out, with a filled tick that takes it back.
    static func doneRow(_ id: String, _ glyph: Glyph, _ title: String, _ line: String, flag: Flag? = nil) -> Row {
        Row(
            id: id, glyph: glyph, title: title, line: line, stripe: Stripe(flag), state: .done,
            trailing: .tick(
                ButtonPresentation(
                    glyph == .task ? "Open again" : "Take it back", glyph: .done, iconOnly: true, role: .primary,
                    command: glyph == .task ? .reopen(id) : .unlogRow(id))),
            body: glyph == .task ? .task(TaskCard(id: id, said: line)) : .routine(RoutineCard(items: [], done: nil, skip: nil, said: line)))
    }

    static let earlier = EarlierPresentation(
        count: "16 done",
        entries: [
            .row(doneRow("r-morning", .routine, "Morning medication", "Taken at 07:33.")),
            .row(doneRow("r-skin", .routine, "Morning skincare", "Skipped at 07:40.")),
            .line(
                EarlierPresentation.Line(
                    id: "e2", time: "08:38", text: "Thirteen suggested tasks answered.", button: ButtonPresentation("Open", role: .quiet, command: .refresh)
                )),
            .row(doneRow("t-vendor", .task, "Verify the safety vendor endpoint claims", "Done at 10:40.", flag: .high)),
        ])

    static func happening(marked: Bool = false) -> HappeningPresentation {
        HappeningPresentation(
            line: "Kenji replied: the VMs can stay stopped until Monday.", time: "11:12", tile: tile("SH"), pill: marked ? .urgent : nil,
            groups: [
                HappeningGroup(
                    id: "lantern", tile: tile("LA"), name: "Lantern", stripe: Stripe(.high), opens: .project("lantern"),
                    items: [
                        HappeningItem(
                            id: "h1", time: "09:10",
                            text: marked ? "Review the backend refactor before Wei goes away" : "Callum accepted Friday at 16:30. The 17:20 slot is free.",
                            source: marked ? "A rule on Wei's message marked it urgent at 16:22." : "Calendar.", pill: marked ? .urgent : nil,
                            marked: marked
                                ? MarkedBlock(
                                    id: "t-wei", by: "Moss thinks it can wait: it is due on Friday and there is a gap today at 16:30.",
                                    options: OptionList(
                                        .round,
                                        [
                                            option("take", "Take it now", "It leads, and the Vigil triage waits."),
                                            option(
                                                "leave", "Leave it here", "It stays here with the urgent pill.", suggested: true,
                                                reason: "There is a gap at 16:30."),
                                            option("not", "Not urgent", "The pill comes off and High comes back."),
                                        ])) : nil)
                    ]),
                HappeningGroup(
                    id: "security", tile: tile("SH"), name: "Security hardening", stripe: Stripe(.high), opens: .project("security"),
                    items: [
                        HappeningItem(
                            id: "h2", time: "11:12", text: "Kenji replied: the VMs can stay stopped until Monday.",
                            source: "Slack, direct message. It answers what you asked at 09:20.", isNew: true, links: links),
                        HappeningItem(
                            id: "h3", time: "09:34", text: "Ned closed nine of the 26 noisy alert tickets.", source: "Slack, #security-ops.",
                            links: links),
                    ]),
                HappeningGroup(
                    id: "hiring", tile: tile("HI"), name: "Hiring", stripe: Stripe(.medium), opens: .project("hiring"),
                    items: [
                        // Not read yet: the place alone, and the record as its one link.
                        HappeningItem(
                            id: "h4", time: "10:15", text: "Grant's CV was updated before tonight's interview.", source: "Gmail.", links: [links[1]])
                    ]),
                HappeningGroup(
                    id: "home", tile: tile("HO"), name: "Home", opens: .project("home"),
                    items: [HappeningItem(id: "h5", time: "10:02", text: "A parcel is out for delivery.", source: "Mail.", isNew: true)]),
                HappeningGroup(
                    id: "unfiled", tile: nil, name: "On no project", opens: nil,
                    items: [HappeningItem(id: "h6", time: "09:50", text: "The dentist confirmed Thursday at 08:30.", source: "Mail.")]),
            ],
            gotIt: ButtonPresentation("Got it", glyph: .done, role: .primary, command: .gotIt(project: nil)))
    }

    static func moss(_ about: String, asks: [String], exchanges: [AnswerCard] = []) -> MossBox {
        MossBox(
            subject: .task("t-vigil"), title: "Moss, about \(about)",
            asks: asks.map { ButtonPresentation($0, command: .ask(.task("t-vigil"), $0)) }, exchanges: exchanges,
            placeholder: "Ask or tell Moss about \(about)")
    }

    static let whenDoor = Door(
        id: "when", label: "When", value: "Today, where it is",
        options: OptionList(
            .round,
            [
                option("today", "Today, where it is", selected: true), option("gap", "The 16:30 gap", "After Henrik."),
                option("tonight", "Tonight, 20:30"), option("friday", "Friday morning", suggested: true, reason: "Friday is free until 14:00."),
                option("next", "Next week"),
            ]))

    static func flagDoor(_ flag: Flag?) -> Door {
        Door(id: "flag", label: "Flag", value: flag?.word ?? "No flag yet", stripe: Stripe(flag), options: flags(chosen: flag))
    }

    /// What a task was made from, as its two buttons: the place it was said, and the record.
    static let links = [
        SourceLink(id: "source", title: "Slack", label: "Open in Slack", url: "https://example.slack.com/archives/C0EXAMPLE/p1700000000000100"),
        SourceLink(id: "record", title: "Record", label: "Open the record in Substrate", url: "https://substrate.example.com/data/example/message/m1"),
    ]

    static let vigilCard: TaskCard = {
        var card = TaskCard(
            id: "t-vigil", chips: Chips(project: security), doors: [flagDoor(.high), whenDoor],
            done: ButtonPresentation("Done", glyph: .done, role: .primary, command: .done("t-vigil")),
            moss: moss("the Vigil triage", asks: ["Who else could take this?"]))
        card.links = links
        return card
    }()

    static func taskRow(_ id: String, _ title: String, _ line: String, flag: Flag?, state: Row.State, chips: Chips = .none, suggested: Bool = false) -> Row {
        Row(
            id: id, glyph: .task, title: title, line: line, stripe: Stripe(flag, suggested: suggested), state: state, chips: chips,
            trailing: .tick(ButtonPresentation("Done", glyph: .done, iconOnly: true, role: .secondary, command: .done(id))),
            body: .task(
                TaskCard(
                    id: id, chips: chips, doors: [flagDoor(flag), whenDoor],
                    done: ButtonPresentation("Done", glyph: .done, role: .primary, command: .done(id)),
                    moss: moss("this task", asks: ["Who else could take this?"]))))
    }

    static func meeting(
        _ id: String, _ start: String, _ end: String, _ title: String, people: [String], line: String?, until: String? = nil,
        leads: Bool = false, attendance: String? = nil, project: ProjectChip? = nil, off: Bool = false
    ) -> MeetingRow {
        MeetingRow(
            id: id, start: start, end: end, title: title, people: people.map(badge), attendance: attendance, line: line, until: until,
            leads: leads, off: off, project: project,
            card: MeetingCard(
                meta: "One to one, Google Meet, 20 min",
                chips: Chips(project: project, people: [PersonChip(id: "tomas", label: title, badge: badge(people.first ?? "TV"))]),
                purpose: "Weekly 1:1. Tomas has asked twice for this slot to stop clashing with two other meetings.",
                tasksTitle: "Your tasks with Tomas",
                tasks: [
                    TaskLine(
                        id: "t-clash", title: "Fix the 1:1 clash with Tomas", line: "Proposed at 08:44. No answer yet.",
                        stripe: Stripe(.medium, suggested: true), opens: .task("t-clash"))
                ],
                said: [
                    MeetingCard.Said(
                        id: "s1", badge: badge("TV"), name: "Tomas", text: "said the vendor tooling thread is yours to pick up together.", time: "Wed 15:06")
                ],
                since: "Started scale testing in the agent sandbox. Overnight Tomas unblocked Lantern's image registration with a flag.",
                raise: ["Tomas sent core PR 4127 with no words. It reads as a review request.", "The vendor tooling thread."],
                readFirst: [MeetingCard.Reading(id: "r1", title: "Scale test notes", kind: "doc", url: "https://example.com/notes")],
                join: leads ? ButtonPresentation("Join", glyph: .join, role: .primary, url: "https://example.com/meet") : nil,
                after: "The scorecard is due before Friday's debrief.",
                moss: MossBox(
                    subject: .meeting(id), title: "Moss, about \(title)",
                    asks: [ButtonPresentation("What is open with \(title)?", command: .ask(.meeting(id), "What is open?"))],
                    placeholder: "Ask or tell Moss about this meeting")))
    }

    static let overlapCard = OverlapCard(
        id: "o-1500", name: "The 15:00 overlap", text: "Henrik and Karim both start at 15:00.", pill: Pill(.neutral, "Chosen"),
        options: OptionList(
            .round,
            [
                option(
                    "henrik", "Henrik at 15:00, and ask Karim for 16:30", "A message to Karim, asking for 16:30, which is free on your calendar.",
                    selected: true, suggested: true, reason: "Henrik's proposal is waiting for your view."),
                option("karim", "Karim at 15:00, and ask Henrik for 16:30"), option("both", "Both: Henrik until 15:20, then Karim"),
            ]),
        chosen: "Chosen at 08:44.",
        ready: ReadyMessage(
            id: "m-karim", heading: "Sent to Karim", text: "Karim, could we move today's 1:1 to 16:30? I have a clash at 15:00.", send: nil,
            note: "Sent at 08:44. No answer yet, so both are still at 15:00.", sent: true))

    /// Two meetings that share time, as the two tied lines of the day:
    /// chosen (the second is struck through), or still to choose.
    static func tied(toChoose: Bool = false) -> [PaneItem] {
        var card = overlapCard
        if toChoose {
            card.pill = Pill(.caution, "To choose")
            card.options.rows = card.options.rows.map { row in
                var row = row
                row.selected = false
                return row
            }
            card.chosen = nil
            card.ready = nil
        }
        var first = meeting(
            "henrik", "15:00", "15:30", "Henrik", people: ["HA"], line: toChoose ? nil : "Weekly 1:1. Henrik will want your view on the proposal.",
            attendance: toChoose ? nil : "Going.", project: toChoose ? nil : bastion)
        var second = meeting("karim", "15:00", "15:30", "Karim", people: ["KS"], line: nil, attendance: toChoose ? nil : "Not going.", off: !toChoose)
        first.card.overlap = card
        second.card.overlap = card
        first.tie = MeetingTie(id: "o-1500", other: "karim", second: false, toChoose: toChoose)
        second.tie = MeetingTie(id: "o-1500", other: "henrik", second: true, toChoose: toChoose, ask: toChoose ? "These two overlap. Choose one." : nil)
        return [.meeting(first), .meeting(second)]
    }

    // MARK: Scenes

    static func pane(_ ground: Ground, strip suggesting: Bool = false, _ items: [PaneItem], answer: AnswerCard? = nil) -> PanePresentation {
        PanePresentation(strip: strip(suggesting: suggesting), items: items, ground: ground, answer: answer)
    }

    /// 11:30: work in hand.
    static func focus(marked: Bool = false, answer: AnswerCard? = nil) -> PanePresentation {
        pane(
            .working,
            [
                .night(nightRead), .earlier(earlier), .happening(happening(marked: marked)),
                .row(
                    Row(
                        id: "t-vigil", glyph: .task, title: "Triage the Vigil findings and review the four lockdown PRs",
                        line: "Over 90 findings with no owner in the sources checked, and the 20:00 sync will ask.", stripe: Stripe(.high),
                        state: .now, chips: Chips(project: security),
                        trailing: .tick(ButtonPresentation("Done", glyph: .done, iconOnly: true, role: .secondary, command: .done("t-vigil"))),
                        body: .task(vigilCard))),
                .row(
                    taskRow(
                        "t-it", "File the IT ticket for the stopped VMs", "Kenji needs it before Monday.", flag: .high, state: .next,
                        chips: Chips(project: security))),
                .row(
                    taskRow(
                        "t-marcus", "Go through Marcus's list of alert gaps", "Marcus asked on Tuesday.", flag: .medium, state: .next,
                        chips: Chips(project: lantern, people: [PersonChip(id: "marcus", label: "for Marcus", badge: badge("MB"))]), suggested: true)),
                .row(
                    Row(
                        id: "r-walk", glyph: .routine, title: "Walk", line: "20 minutes, after lunch", state: .next,
                        trailing: .tick(ButtonPresentation("Done", glyph: .done, iconOnly: true, command: .log(row: "r-walk", done: true))),
                        body: .routine(
                            RoutineCard(
                                items: [], done: ButtonPresentation("Done", glyph: .done, role: .primary, wide: true, command: .log(row: "r-walk", done: true)),
                                skip: ButtonPresentation("Skip", role: .quiet, command: .log(row: "r-walk", done: false)))))),
                .meeting(
                    meeting(
                        "tomas", "14:20", "14:40", "Tomas", people: ["TV"],
                        line: "Your task: fix the 1:1 clash with Tomas. Proposed at 08:44. No answer yet.")),
                .meeting(meeting("viktor", "14:40", "15:00", "Viktor", people: ["VL"], line: "Weekly 1:1.", project: lantern)),
            ] + tied() + [
                .meeting(meeting("cloud", "21:00", "21:30", "Cloud standup", people: ["CR", "MB", "WZ"], line: "Moved to Friday.", off: true)),
                .row(
                    Row(
                        id: "wrap", glyph: .night, title: "Wrap the day", line: "What got done, what carries over, and what is first tomorrow.",
                        state: .later, body: .wrap(wrap))),
            ], answer: answer)
    }

    /// One item of a medication group, open with its tick or taken.
    static func dose(_ id: String, _ name: String, _ detail: String, taken: String? = nil) -> RoutineCard.Item {
        RoutineCard.Item(
            id: id, name: name, detail: detail, logged: taken.map { "Taken at \($0)." },
            tick: taken == nil
                ? ButtonPresentation("Taken", glyph: .done, iconOnly: true, role: .secondary, command: .logOccurrence(id, done: true))
                : ButtonPresentation("Undo", glyph: .done, iconOnly: true, role: .primary, command: .unlog(id)))
    }

    /// 07:05: waking.
    static let wake = pane(
        .waking, strip: true,
        [
            .night(nightUnread),
            .row(
                Row(
                    id: "r-morning", glyph: .routine, title: "Morning medication", line: "5 items", state: .now,
                    body: .routine(
                        RoutineCard(
                            items: [
                                dose("o1", "Vitamin D", "1,000 IU", taken: "07:02"), dose("o2", "Magnesium", "200 mg"),
                                dose("o3", "Omega-3", "1 capsule"), dose("o4", "Zinc", "15 mg"), dose("o5", "Probiotic", "1 capsule"),
                            ],
                            done: ButtonPresentation("All taken", glyph: .done, role: .primary, wide: true, command: .log(row: "r-morning", done: true)),
                            skip: ButtonPresentation("Skip", role: .quiet, command: .log(row: "r-morning", done: false)),
                            notLogged: "Not logged since 17 October.")))),
            .row(
                Row(
                    id: "y-b12", glyph: .routine, title: "Not logged yesterday", line: "Vitamin B12.", state: .next,
                    body: .yesterday(
                        YesterdayCard(
                            items: [
                                YesterdayCard.Item(
                                    id: "y-b12", glyph: .routine, title: "Vitamin B12", text: "It was due yesterday at 07:30. Not logged.",
                                    options: OptionList(
                                        .round,
                                        [
                                            option("took", "Took it yesterday", "Logged for yesterday."),
                                            option("skipped", "Skipped", "Logged as skipped."),
                                        ]))
                            ])))),
            .row(
                Row(
                    id: "r-skin", glyph: .routine, title: "Skincare", line: "5 steps", state: .next,
                    trailing: .tick(ButtonPresentation("Done", glyph: .done, iconOnly: true, command: .log(row: "r-skin", done: true))),
                    body: .routine(
                        RoutineCard(
                            items: [
                                RoutineCard.Item(id: "s1", name: "Cleanser", logged: "Done at 07:31."), RoutineCard.Item(id: "s2", name: "Toner"),
                                RoutineCard.Item(id: "s3", name: "Sunscreen", detail: "SPF 50"),
                            ],
                            done: ButtonPresentation("Done", glyph: .done, role: .primary, wide: true, command: .log(row: "r-skin", done: true)),
                            skip: ButtonPresentation("Skip", role: .quiet, command: .log(row: "r-skin", done: false)))))),
            .meeting(meeting("physio", "08:00", "08:30", "Physiotherapy call", people: ["DW"], line: "The booking came with no agenda.", until: "in 55 min")),
        ])

    /// - Parameter project: nil inside a project's group, whose header names it.
    static func suggestedItem(
        _ id: String, _ title: String, origin: String, quote: String?, fade: Int = 0, pick: SuggestedAnswer = .task, own: Bool = false,
        objection: Bool = false, project: ProjectChip? = security, people: [PersonChip] = []
    ) -> SuggestedItem {
        SuggestedItem(
            id: id, title: title, origin: origin, quote: quote,
            fade: SuggestedItem.Fade(
                level: fade, label: fade == 0 ? "" : fade == 1 ? "Starts to fade tomorrow." : "Past two weeks. It is gone tonight unless you answer."),
            chips: Chips(project: project, people: people),
            options: OptionList(
                .round,
                [
                    option(
                        "task", "Make it a task", "A task for this morning, flagged High.", selected: pick == .task, suggested: true,
                        reason: "You said in the morning."),
                    option("done", "Already done", "Closed. Nothing more is asked.", selected: pick == .done),
                    option("dismiss", "Dismiss", "Not a task. What was said stays in memory.", selected: pick == .dismiss),
                    option("other", "Other", "Say what is wrong, or what to do instead.", selected: pick == .other),
                ]),
            other: pick == .other
                ? SuggestedItem.Other(
                    placeholder: "What is wrong, or what to do instead", note: "Kept for whoever suggests tasks, and sent to Moss with this one.")
                : nil,
            objection: objection
                ? SuggestedItem.Objection(line: "Marcus asked twice, and the list is on the agenda at 17:40.", note: "Moss says this once and leaves it.")
                : nil,
            save: own
                ? SaveRow(button: ButtonPresentation("Save", glyph: .done, role: .primary, command: .saveSuggested([id], words: [:]))) : nil,
            pending: true)
    }

    /// A suggestion the owner answered in words, with the exchange that followed.
    static func toldItem(_ id: String, _ title: String, origin: String, words: String, answer: String?) -> SuggestedItem {
        SuggestedItem(
            id: id, title: title, origin: origin, fade: SuggestedItem.Fade(level: 0, label: ""), options: .empty, pending: false,
            told: SuggestedItem.Told(
                words: words,
                answer: AnswerCard(
                    id: "x-" + id, heading: "You said to Moss", question: words, text: answer ?? "", state: answer == nil ? .waiting : .done,
                    button: ButtonPresentation(answer == nil ? "Stop" : "Close", role: .quiet))))
    }

    /// The suggested tasks' page: a flagged project's group with one answered in words, and what is on no project.
    static let suggestedPage = SuggestedPage(
        line: "Four on your priorities. Moss would make twelve tasks.",
        rule: "Not answered, a suggestion fades after a week and is gone after two. What was said stays in memory.",
        groups: [
            SuggestedPage.Group(
                id: "security", tile: tile("SH"), name: "Security hardening", count: "3 suggested", stripe: Stripe(.high), opens: .project("security"),
                items: [
                    suggestedItem(
                        "p-kenji", "Sync with Kenji about the stopped VMs", origin: "Heard yesterday at 21:37, from a message, with Kenji.",
                        quote: "I will catch **Kenji** in the morning, we are in the same office", project: nil,
                        people: [PersonChip(id: "kenji", label: "for Kenji", badge: badge("KM"))]),
                    suggestedItem(
                        "p-ticket", "File the IT ticket about permissions", origin: "Heard yesterday at 20:54, from a transcript.", quote: nil,
                        pick: .other, project: nil),
                ],
                more: ButtonPresentation("Show the other 4", role: .secondary, command: .showSuggested(group: "security")),
                told: [
                    toldItem(
                        "p-origin", "Ask about the encryption question", origin: "Heard on Tuesday at 22:29, from a message.",
                        words: "Henrik answered this in the review. Close it, and stop making tasks from that thread.",
                        answer: "I dismissed it. Henrik's answer is in the review notes from Wednesday.")
                ],
                save: SaveRow(
                    button: ButtonPresentation(
                        "Save 2 answers", glyph: .done, role: .primary, wide: true, command: .saveSuggested(["p-kenji", "p-ticket"], words: [:])))),
            SuggestedPage.Group(
                id: "none", tile: nil, name: "On no project", count: "1 suggested",
                items: [
                    suggestedItem(
                        "p-marcus", "Go through Marcus's list", origin: "Suggested on 22 October, with Marcus.", quote: "I will go over these tomorrow",
                        fade: 3, pick: .dismiss, objection: true, project: nil)
                ],
                save: SaveRow(
                    button: ButtonPresentation("Save 1 answer", glyph: .done, role: .primary, wide: true, command: .saveSuggested(["p-marcus"], words: [:])))),
        ])

    /// 08:30: the suggested tasks.
    static let gate = pane(
        .planning, strip: true,
        [
            .night(nightRead), .earlier(EarlierPresentation(count: "8 done", entries: [])),
            .row(
                Row(
                    id: "gate", glyph: .moss, title: "Thirteen suggested tasks to answer",
                    line: "Four on your priorities. Moss would make twelve tasks.", state: .now, trailing: .page(.suggested),
                    body: .suggested(
                        PageRowCard(
                            line: "They open as a page, grouped by project. Each project has its own Save, and nothing changes until you press it.",
                            open: ButtonPresentation("Open the suggested tasks", role: .primary, wide: true, opens: .suggested))))),
            .row(
                Row(
                    id: "plan", glyph: .plan, title: "Plan the day",
                    line: "What is for today and how much each matters. One is left from yesterday.", state: .next, trailing: .page(.plan),
                    body: .plan(
                        PageRowCard(
                            line: "It opens as a page, and nothing changes until you save.",
                            open: ButtonPresentation("Open the plan", role: .primary, wide: true, opens: .plan))))),
        ])

    static func urgent(unchecked: Bool = false, chosen: Bool = false) -> UrgentCard {
        UrgentCard(
            id: "t-api", setBy: unchecked ? "A rule on the on-call tool marked it at 11:38." : "The on-call paged you at 11:38.",
            text: "Since 11:31 about one request in ten fails. The on-call has it, and asks whether to roll back this morning's release.",
            voices: unchecked ? [] : [UrgentCard.Voice(who: "Moss", says: "It is an incident on External API reliability, which is flagged High.")],
            unchecked: unchecked ? "Moss cannot be reached, so nobody has checked this." : nil,
            took: "It takes the place of the Vigil triage, which waits where you left it.",
            options: OptionList(
                .round,
                [
                    option("rollback", "Roll it back", "A message to the on-call. Nothing is sent until you press Send.", selected: chosen),
                    option("hold", "Hold, and let the on-call decide"), option("join", "Join the incident call"), option("not", "Not urgent"),
                ]),
            chosen: chosen ? "Chosen at 11:41." : nil,
            ready: chosen
                ? ReadyMessage(
                    id: "m-oncall", heading: "Ready to send to the on-call", text: "Roll back this morning's release. I am on the call in five minutes.",
                    send: ButtonPresentation("Send", glyph: .send, role: .primary, command: .send("m-oncall")), note: "Nothing is sent until you press Send.")
                : nil)
    }

    /// 11:40: something cannot wait.
    static func interrupt(unchecked: Bool = false, chosen: Bool = false) -> PanePresentation {
        pane(
            .urgent,
            [
                .night(nightRead), .earlier(earlier), .happening(happening()),
                .row(
                    Row(
                        id: "t-api", glyph: .urgent, title: "External API is failing again", state: .now,
                        body: .urgent(urgent(unchecked: unchecked, chosen: chosen)))),
                .parked(ParkedLine(id: "p1", what: "The Vigil triage", rest: "waits where you left it.")),
                .row(
                    taskRow(
                        "t-it", "File the IT ticket for the stopped VMs", "Kenji needs it before Monday.", flag: .high, state: .next,
                        chips: Chips(project: security))),
            ])
    }

    /// 14:12: a meeting is about to start.
    static let premeet = pane(
        .meeting,
        [
            .night(nightRead), .earlier(EarlierPresentation(count: "22 done", entries: [])),
            .parked(ParkedLine(id: "p1", what: "The Vigil triage", rest: "waits until you are out.")),
            .happening(
                HappeningPresentation(line: "Two more people accepted your 19:30 call. Eight are coming.", time: "13:05", tile: tile("EU"))),
            .meeting(meeting("tomas", "14:20", "14:40", "Tomas", people: ["TV"], line: nil, until: "in 8 min", leads: true)),
            .meeting(meeting("viktor", "14:40", "15:00", "Viktor", people: ["VL"], line: "Weekly 1:1.", project: lantern)),
            .unfinished(
                UnfinishedCard(
                    id: "u1", line: "The Vigil triage will not be finished before Tomas at 14:20. Where does it go?",
                    left: "Where you left it, 13:55: findings sorted to OPS-2041. PR 3 of 4 is half read.",
                    options: OptionList(
                        .round,
                        [
                            option("gap", "The 16:30 gap", "After Henrik.", suggested: true, reason: "It is the first gap long enough to finish the PRs."),
                            option("tonight", "Tonight, 20:30"), option("friday", "Friday morning"),
                        ]),
                    project: security)),
        ])

    static let wrap = WrapCard(
        done: [
            WrapCard.Done(id: "d1", title: "Verify the safety vendor endpoint claims", time: "10:40", done: true, stripe: Stripe(.high)),
            WrapCard.Done(id: "d2", title: "Triage the Vigil findings", time: "open", done: false, stripe: Stripe(.high)),
        ],
        others: "Four others: the proxy update PR, Lin's score, the IT ticket and the form.",
        suggested: "Suggested tasks: twelve made tasks and one dismissed.",
        moved: [WrapCard.Titled(id: "m1", title: "Read the incident write-up", line: "To the 16:30 gap, at 14:05.")],
        carry: [
            WrapCard.Carry(
                id: "c1", title: "Triage the Vigil findings and review the four lockdown PRs", why: "Two of four PRs are read.",
                options: OptionList(
                    .round,
                    [
                        option(
                            "tomorrow", "Tomorrow morning", "First thing, still flagged High.", selected: true, suggested: true,
                            reason: "Friday is free until 14:00."),
                        option("monday", "Monday"), option("drop", "Drop it"),
                    ]))
        ],
        unanswered: "One suggested task is still not answered.",
        answer: ButtonPresentation("Open the suggested tasks", role: .secondary, opens: .suggested),
        dismissed: [WrapCard.Titled(id: "x1", title: "Go through Marcus's list", line: "Dismissed at 08:38.")],
        tomorrow: "Presentation interview at 14:00. All hands at 17:00.",
        save: SaveRow(button: ButtonPresentation("Save and close the day", glyph: .done, role: .primary, wide: true, command: .saveWrap)))

    /// 22:15: the day closing.
    static let closing = pane(
        .closing,
        [
            .night(nightRead), .earlier(EarlierPresentation(count: "31 done", entries: [])),
            .row(
                Row(
                    id: "wrap", glyph: .night, title: "Wrap the day", line: "Two things carry over. One is flagged High.", state: .now,
                    body: .wrap(wrap))),
        ])

    // MARK: Pages

    static let priorities = PrioritiesPage(
        note: "Yours to set, as many as you like, each with a flag. Moss suggests a change when it sees a reason, and nothing changes until you choose.",
        suggestedTitle: "Moss suggests adding",
        suggested: [
            PriorityEntry(
                id: "security", tile: tile("SH", suggested: true), name: "Security hardening", line: "Four tasks today, one cannot wait long.",
                flag: flagControl(nil, suggested: .high, open: true), opens: .project("security"))
        ],
        entries: [
            PriorityEntry(
                id: "api", tile: tile("EA"), name: "External API reliability", line: "0 of 2 done", flag: flagControl(.high),
                remove: ButtonPresentation("Remove", role: .destructive, command: .removePriority(project: "api")), opens: .project("api")),
            PriorityEntry(
                id: "eu", tile: tile("EU"), name: "EU AI Act", line: "0 of 1 done", flag: flagControl(.high),
                remove: ButtonPresentation("Remove", role: .destructive, command: .removePriority(project: "eu")), opens: .project("eu")),
            PriorityEntry(
                id: "hiring", tile: tile("HI"), name: "Hiring", line: "0 of 1 done", flag: flagControl(.medium),
                remove: ButtonPresentation("Remove", role: .destructive, command: .removePriority(project: "hiring")), opens: .project("hiring")),
            PriorityEntry(
                id: "github", tile: tile("GH"), name: "GitHub orgs", line: "Nothing for today", flag: flagControl(.low),
                remove: ButtonPresentation("Remove", role: .destructive, command: .removePriority(project: "github")), opens: .project("github")),
        ],
        add: OptionList(
            .square,
            [
                option("home", "Home", "Two parcels coming", icon: .tile(tile("HO"))),
                option("paper", "Paperwork", "12 open, none dated", icon: .tile(tile("PA"))),
                option("tally", "Tally", "Fix in app review", icon: .tile(tile("TA"))),
            ]))

    static let projects = ProjectsPage(
        priorities: [
            ProjectLine(
                id: "api", tile: tile("EA"), name: "External API reliability", line: "Today: the incident write-up. Done: the vendor check at 10:40.",
                stripe: Stripe(.high)),
            ProjectLine(
                id: "security", tile: tile("SH"), name: "Security hardening", line: "Today: the Vigil triage and the IT ticket.",
                news: "New at 11:12. Kenji replied: the VMs can stay stopped until Monday.", stripe: Stripe(.high)),
            ProjectLine(id: "hiring", tile: tile("HI"), name: "Hiring", line: "Today: Lin's score. The hiring debrief at 16:00.", stripe: Stripe(.medium)),
        ],
        others: [
            ProjectLine(id: "home", tile: tile("HO"), name: "Home", line: "Two parcels coming"),
            ProjectLine(id: "paper", tile: tile("PA"), name: "Paperwork", line: "12 open, none dated", pill: .stale),
        ])

    static let projectPage = ProjectPage(
        id: "api", tile: tile("EA"), name: "External API reliability", flag: flagControl(.high),
        summary: "For 35 minutes overnight about one request in five failed. A release was rolled back and it has been steady since.",
        stale: nil,
        urgent: TaskLine(id: "t-api", title: "External API is failing again", line: "The on-call paged you at 11:38.", pill: .urgent, opens: .task("t-api")),
        moss: MossBox(
            subject: .project("api"), title: "Moss, about External API reliability",
            asks: [
                ButtonPresentation("What should I do next here?", command: .ask(.project("api"), "next")),
                ButtonPresentation("Draft the reliability update for Patrick", command: .ask(.project("api"), "draft")),
                ButtonPresentation("What changed this week?", command: .ask(.project("api"), "changed")),
            ], placeholder: "Ask or tell Moss about External API reliability"),
        tasks: [
            TaskLine(id: "t1", title: "Read the incident write-up", line: "Today.", stripe: Stripe(.medium), opens: .task("t1")),
            TaskLine(
                id: "t2", title: "Book the reliability call with Imran", line: "Friday.", pill: .suggested, stripe: Stripe(.high, suggested: true),
                opens: .task("t2")),
        ],
        done: [
            TaskLine(
                id: "t3", glyph: .done, title: "Verify the safety vendor endpoint claims", line: "Done at 10:40.", stripe: Stripe(.high), opens: .task("t3"))
        ],
        suggested: [suggestedItem("p-api", "Send Patrick the reliability update", origin: "Patrick asked on Wednesday at 22:04.", quote: nil, own: true)],
        timeline: [
            ProjectPage.TimelineGroup(
                id: "coming", label: "Coming up",
                sections: [
                    .init(id: "coming:", lines: [TimelineLine(id: "l3", time: "19:30", text: "Reliability sync with the vendors.", opens: .meeting("vendors"))])
                ]),
            ProjectPage.TimelineGroup(
                id: "night", label: "Overnight",
                sections: [
                    .init(
                        id: "night:Slack, #api-alerts", source: "Slack, #api-alerts",
                        lines: [
                            TimelineLine(
                                id: "l1", time: "03:47", who: "The on-call", text: "rolled back the 02:50 release and it recovered.", links: links),
                            TimelineLine(id: "l2", time: "03:12", text: "About one request in five began to fail.", links: links),
                        ]),
                    // Not read yet: the place alone, and the record as its one link.
                    .init(
                        id: "night:GitHub", source: "GitHub",
                        lines: [TimelineLine(id: "l4", time: "02:50", text: "A release went out.", links: [links[1]])]),
                ]),
            ProjectPage.TimelineGroup(
                id: "day-2026-11-18", label: "Yesterday",
                sections: [
                    // Written from no record: first in its day, with no label over it and no link under it.
                    .init(id: "day-2026-11-18:", lines: [TimelineLine(id: "l6", time: "10:40", who: "You", text: "finished: verify the vendor claims.")]),
                    .init(
                        id: "day-2026-11-18:Gmail", source: "Gmail",
                        lines: [
                            TimelineLine(
                                id: "l5", time: "16:40",
                                text: "The vendors were told a release was planned for the night, and two of them asked for the window in writing.",
                                links: [
                                    SourceLink(
                                        id: "source", title: "Gmail", label: "Open in Gmail", url: "https://mail.google.com/mail/u/owner@example.com/#all/18c2"),
                                    links[1],
                                ])
                        ]),
                ]),
        ],
        meetings: [MeetingLine(id: "vendors", title: "Reliability sync", line: "19:30 to 20:00, in 8 h.")])

    static let personPage = PersonPage(
        id: "tomas", badge: badge("TV"), name: "Tomas Varga", sees: "You see Tomas at 14:20.", tasksTitle: "Tasks with Tomas",
        tasks: [TaskLine(id: "t-clash", title: "Fix the 1:1 clash with Tomas", line: "Today.", stripe: Stripe(.medium), opens: .task("t-clash"))],
        suggested: [suggestedItem("p-pr", "Review core PR 4127", origin: "Tomas sent it on Wednesday with no words.", quote: nil, own: true)],
        meetings: [MeetingLine(id: "tomas", title: "Tomas", line: "14:20 to 14:40, in 2 h 50.")],
        moss: MossBox(
            subject: .person("tomas"), title: "Moss, about Tomas",
            asks: [ButtonPresentation("What is open with Tomas?", command: .ask(.person("tomas"), "open"))],
            exchanges: [
                AnswerCard(
                    id: "a1", heading: "You asked", question: "What is open with Tomas?",
                    text:
                        "Three things. Tomas asked whether your 1:1 could stop clashing with two other meetings, and you said you were fixing it. Tomas sent core PR 4127 with no words, which reads as a review request.",
                    state: .done, sources: ["What you said, Wed 15:06", "core PR 4127"])
            ], placeholder: "Ask or tell Moss about Tomas"))

    static let meetingPage = MeetingPage(
        id: "tomas", eyebrow: "14:20 to 14:40, in 2 h 50", title: "Tomas",
        card: meeting("tomas", "14:20", "14:40", "Tomas", people: ["TV"], line: nil).card)

    static let taskPage = TaskPage(
        id: "t-vigil", title: "Triage the Vigil findings and review the four lockdown PRs",
        line: "Over 90 findings with no owner in the sources checked, and the 20:00 sync will ask.", stripe: Stripe(.high),
        card: {
            var card = TaskCard(
                id: "t-vigil", chips: Chips(project: security), details: "“I will go over these tomorrow”, Tue 20:26",
                left: TaskCard.Left(heading: "Where you left it, 13:55", text: "Findings sorted to OPS-2041. PR 3 of 4 is half read."),
                deferred: "Put off three times.", doors: [flagDoor(.high), whenDoor],
                done: ButtonPresentation("Done", glyph: .done, role: .primary, command: .done("t-vigil")),
                moss: moss("the Vigil triage", asks: ["Who else could take this?"]))
            card.links = links
            return card
        }())

    static func planItem(_ id: String, _ title: String, why: String, flag: Flag, ticked: Bool = true, suggested: Bool = true) -> PlanPage.Item {
        PlanPage.Item(id: id, title: title, why: why, line: "This morning.", ticked: ticked, suggested: suggested, flag: flagControl(nil, suggested: flag))
    }

    static let planPage = PlanPage(
        title: "Today's plan", opening: "Free until Tomas at 14:20, then meetings until 21:30, with gaps at 16:30 and 20:30.",
        leftTitle: "Left from yesterday",
        left: [
            PlanPage.Left(
                id: "l1", title: "Reply to Stefan on the inventory", line: "It was due yesterday. Halden and Brightwell is Friday at 20:00.",
                chips: Chips(project: lantern, people: [PersonChip(id: "stefan", label: "for Stefan", badge: badge("SK"))]), stripe: Stripe(.high),
                options: OptionList(
                    .round,
                    [
                        option("today", "Today", "It goes into today's order.", selected: true),
                        option("tomorrow", "Tomorrow", "It leaves today and comes back tomorrow."), option("drop", "Drop it", "Closed as not done."),
                    ])),
            PlanPage.Left(
                id: "l2", title: "Book the follow-up visit", line: "It was due on Tuesday.",
                options: OptionList(
                    .round,
                    [
                        option("today", "Today", "It goes into today's order."), option("tomorrow", "Tomorrow", "It leaves today and comes back tomorrow."),
                        option("drop", "Drop it", "Closed as not done."),
                    ])),
        ],
        suggested: "Thirteen suggested tasks wait for an answer, on a page of their own.",
        suggestedOpen: ButtonPresentation("Open the suggested tasks", role: .secondary, opens: .suggested),
        groups: [
            PlanPage.Group(
                id: "api", tile: tile("EA"), name: "External API reliability", count: "2 today", stripe: Stripe(.high), opens: .project("api"),
                items: [
                    planItem("c1", "Verify the safety vendor endpoint claims", why: "A day late, and the vendors saw last night's errors.", flag: .high),
                    planItem("c2", "Read the incident write-up", why: "Reading. It is due by noon.", flag: .medium),
                ]),
            PlanPage.Group(
                id: "eu", tile: tile("EU"), name: "EU AI Act", count: "1 today", stripe: Stripe(.high), opens: .project("eu"),
                meetings: "Meetings today: EU AI Act, Sprout, checkpoints at 19:30.",
                items: [planItem("c3", "Find the docs for the data platform meeting", why: "Your own call is at 19:30.", flag: .high)],
                moreTitle: "Not suggested for today (2)",
                more: [planItem("c4", "Read the guidance update", why: "Nothing waits on it.", flag: .low, ticked: false, suggested: false)]),
        ],
        overlapsTitle: "The four overlaps",
        overlaps: [PlanPage.Overlap(id: "o-1500", name: "The 15:00 overlap", text: "Henrik and Karim both start at 15:00.", options: overlapCard.options)],
        sendsTitle: "What Moss may send when you save", sendsHint: "Moss sends only what you tick. Nothing is ticked for you.",
        sends: OptionList(
            .square,
            [
                OptionRow(
                    id: "s1", name: "To Karim, for the 15:00 overlap", quote: "Karim, could we move today's 1:1 to 16:30? I have a clash at 15:00.",
                    command: .planSend("s1"))
            ]),
        note: "Lantern is flagged High and has nothing ticked for today.", barLine: "Save 13 for today, 6 of them High. No message goes.",
        save: ButtonPresentation("Save the plan", glyph: .done, role: .primary, wide: true, command: .planSave),
        reset: nil)

    static let pages: [TodayRoute: PagePresentation] = [
        .priorities: .priorities(priorities), .projects: .projects(projects), .project("api"): .project(projectPage),
        .person("tomas"): .person(personPage), .meeting("tomas"): .meeting(meetingPage), .task("t-vigil"): .task(taskPage),
        .plan: .plan(planPage), .suggested: .suggested(suggestedPage),
    ]

    static func app(_ pane: PanePresentation) -> TodayPresentation {
        TodayPresentation(
            pane: pane, pages: PageSource(fixed: pages), storage: SettingsPresenter.storage([], budget: nil),
            about: AboutPresentation.present(appName: "Substrate Today", version: "0.1.0 (1)", channel: nil, offered: nil, privacyPolicy: nil))
    }
}
