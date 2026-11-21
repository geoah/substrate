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

    static let earlier = EarlierPresentation(
        count: "16 done",
        lines: [
            EarlierPresentation.Line(id: "e1", time: "07:33", text: "Morning medication, 5 items. Taken."),
            EarlierPresentation.Line(
                id: "e2", time: "08:38", text: "Thirteen suggested tasks answered.", button: ButtonPresentation("Open", role: .quiet, command: .refresh)),
            EarlierPresentation.Line(id: "e3", time: "10:40", text: "Verify the safety vendor endpoint claims. Done."),
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
                            source: "Slack. It answers what you asked at 09:20.", isNew: true),
                        HappeningItem(id: "h3", time: "09:34", text: "Ned closed nine of the 26 noisy alert tickets.", source: "Slack."),
                    ]),
                HappeningGroup(
                    id: "hiring", tile: tile("HI"), name: "Hiring", stripe: Stripe(.medium), opens: .project("hiring"),
                    items: [HappeningItem(id: "h4", time: "10:15", text: "Grant's CV was updated before tonight's interview.", source: "Mail.")]),
            ],
            rest: HappeningRest(
                line: "Three more things, one new",
                groups: [
                    HappeningGroup(
                        id: "home", tile: tile("HO"), name: "Home", opens: .project("home"),
                        items: [HappeningItem(id: "h5", time: "10:02", text: "A parcel is out for delivery.", source: "Mail.", isNew: true)])
                ]),
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

    static let vigilCard = TaskCard(
        id: "t-vigil", chips: Chips(project: security), doors: [flagDoor(.high), whenDoor],
        done: ButtonPresentation("Done", glyph: .done, role: .primary, command: .done("t-vigil")),
        moss: moss("the Vigil triage", asks: ["Who else could take this?"]))

    static func taskRow(_ id: String, _ title: String, _ line: String, flag: Flag?, state: Row.State, chips: Chips = .none, suggested: Bool = false) -> Row {
        Row(
            id: id, glyph: .task, title: title, line: line, stripe: Stripe(flag, suggested: suggested), state: state, chips: chips,
            trailing: .flag(flagControl(suggested ? nil : flag, suggested: suggested ? flag : nil)),
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

    static let overlap = OverlapBlock(
        id: "o-1500",
        card: OverlapCard(
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
                note: "Sent at 08:44. No answer yet, so both are still at 15:00.", sent: true)),
        meetings: [
            meeting(
                "henrik", "15:00", "15:30", "Henrik", people: ["HA"], line: "Weekly 1:1. Henrik will want your view on the proposal.", attendance: "Going.",
                project: bastion),
            meeting("karim", "15:00", "15:30", "Karim", people: ["KS"], line: "Weekly 1:1.", attendance: "Asked to move, no answer yet."),
        ])

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
                        state: .now, chips: Chips(project: security), trailing: .flag(flagControl(.high)), body: .task(vigilCard))),
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
                        line: "Your task: fix the 1:1 clash with Tomas. Proposed at 08:44. No answer yet.", until: "in 2 h 50")),
                .meeting(meeting("viktor", "14:40", "15:00", "Viktor", people: ["VL"], line: "Weekly 1:1.", project: lantern)),
                .overlap(overlap),
                .meeting(meeting("cloud", "21:00", "21:30", "Cloud standup", people: ["CR", "MB", "WZ"], line: "Moved to Friday.", off: true)),
                .row(
                    Row(
                        id: "wrap", glyph: .night, title: "Wrap the day", line: "What got done, what carries over, and what is first tomorrow.",
                        state: .later, body: .wrap(wrap))),
            ], answer: answer)
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
                                RoutineCard.Item(id: "o1", name: "Vitamin D", detail: "1,000 IU"),
                                RoutineCard.Item(id: "o2", name: "Magnesium", detail: "200 mg"),
                                RoutineCard.Item(id: "o3", name: "Omega-3", detail: "1 capsule"), RoutineCard.Item(id: "o4", name: "Zinc", detail: "15 mg"),
                                RoutineCard.Item(id: "o5", name: "Probiotic", detail: "1 capsule"),
                            ],
                            done: ButtonPresentation("Taken", glyph: .done, role: .primary, wide: true, command: .log(row: "r-morning", done: true)),
                            skip: nil, stale: StaleNote("Nothing has been logged since 17 October. A tick here is the log."))))),
            .row(
                Row(
                    id: "y-b12", glyph: .routine, title: "From yesterday: Vitamin B12", line: "Weekly, due Wednesday morning. Not logged.",
                    state: .next,
                    body: .yesterday(
                        YesterdayCard(
                            items: [
                                YesterdayCard.Item(
                                    id: "y-b12", glyph: .routine, title: "Vitamin B12",
                                    text: "Weekly, due Wednesday morning. Not logged.",
                                    options: OptionList(
                                        .round,
                                        [
                                            option("taken", "I took it", "It is logged for Wednesday."),
                                            option("today", "Take it today", "It joins the morning medication."),
                                            option("skip", "Skip this week"),
                                        ]))
                            ], note: "From a routine.")))),
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

    static func suggestedItem(
        _ id: String, _ title: String, origin: String, quote: String?, fade: Int = 0, pick: SuggestedAnswer = .task, own: Bool = false, objection: Bool = false
    ) -> SuggestedItem {
        SuggestedItem(
            id: id, title: title, origin: origin, quote: quote,
            fade: SuggestedItem.Fade(level: fade, label: fade == 0 ? "" : fade == 1 ? "Fades tomorrow." : "Gone tonight."),
            chips: Chips(project: security),
            options: OptionList(
                .round,
                [
                    option(
                        "task", "Make it a task", "A task for this morning, flagged High.", selected: pick == .task, suggested: true,
                        reason: "You said in the morning."),
                    option("done", "Already done", "It closes as done.", selected: pick == .done),
                    option("dismiss", "Dismiss", "It is dropped. What was said stays in memory.", selected: pick == .dismiss),
                ]),
            objection: objection
                ? SuggestedItem.Objection(line: "Marcus asked twice, and the list is on the agenda at 17:40.", note: "Moss says this once and leaves it.")
                : nil,
            save: own ? SaveRow(button: ButtonPresentation("Save this answer", glyph: .done, role: .primary, wide: true, command: .saveSuggested(id))) : nil,
            pending: true)
    }

    /// 08:30: the suggested tasks.
    static let gate = pane(
        .planning, strip: true,
        [
            .night(nightRead), .earlier(EarlierPresentation(count: "8 done", lines: [])),
            .row(
                Row(
                    id: "gate", glyph: .moss, title: "Thirteen suggested tasks to answer",
                    line: "Eleven from what you said, two that people asked for. Made a task, each gets a day and a flag.", state: .now,
                    body: .suggested(
                        SuggestedCard(
                            rule: "Not answered, a suggestion fades after a week and is gone after two. What was said stays in memory.",
                            batch: "Showing twelve at a time. The next take their place when you save.",
                            items: [
                                suggestedItem(
                                    "p-kenji", "Sync with Kenji about the stopped VMs", origin: "You said it to Kenji on Wednesday at 21:37.",
                                    quote: "“I will catch Kenji in the morning, we are in the same office”"),
                                suggestedItem(
                                    "p-tomas", "Fix the 1:1 clash with Tomas", origin: "Tomas asked on Tuesday at 15:06.", quote: nil, fade: 1, pick: .done),
                                suggestedItem(
                                    "p-marcus", "Go through Marcus's list", origin: "You said it to Marcus on 10 November.",
                                    quote: "“I will go over these tomorrow”", fade: 3, pick: .dismiss, objection: true),
                            ],
                            save: SaveRow(
                                button: ButtonPresentation(
                                    "Save 12 answers", glyph: .done, role: .primary, wide: true,
                                    command: .saveSuggestedCard(["p-kenji", "p-tomas", "p-marcus"]))),
                            dismissedOlder: "125 older than two weeks were dismissed when the app was set up.")))),
            .row(
                Row(
                    id: "plan", glyph: .plan, title: "Plan the day", line: "Thirteen things are on the table.", state: .next,
                    body: .plan(
                        PlanRowCard(
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
            .night(nightRead), .earlier(EarlierPresentation(count: "22 done", lines: [])),
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
        dismissed: [WrapCard.Titled(id: "x1", title: "Go through Marcus's list", line: "Dismissed at 08:38.")],
        tomorrow: "Presentation interview at 14:00. All hands at 17:00.",
        save: SaveRow(button: ButtonPresentation("Save and close the day", glyph: .done, role: .primary, wide: true, command: .saveWrap)))

    /// 22:15: the day closing.
    static let closing = pane(
        .closing,
        [
            .night(nightRead), .earlier(EarlierPresentation(count: "31 done", lines: [])),
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
                id: "overnight", label: "Overnight",
                lines: [
                    TimelineLine(
                        id: "l1", time: "03:47", who: "The on-call", text: "rolled back the 02:50 release and it recovered.",
                        source: Pill(.neutral, "PagerDuty")),
                    TimelineLine(id: "l2", time: "03:12", text: "About one request in five began to fail.", source: Pill(.neutral, "Grafana")),
                ]),
            ProjectPage.TimelineGroup(
                id: "today", label: "Today",
                lines: [TimelineLine(id: "l3", time: "19:30", text: "Reliability sync with the vendors.", opens: .meeting("vendors"))]),
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
        card: TaskCard(
            id: "t-vigil", chips: Chips(project: security), quote: "“I will go over these tomorrow”, Tue 20:26",
            left: TaskCard.Left(heading: "Where you left it, 13:55", text: "Findings sorted to OPS-2041. PR 3 of 4 is half read."),
            deferred: "Put off three times.", doors: [flagDoor(.high), whenDoor],
            done: ButtonPresentation("Done", glyph: .done, role: .primary, command: .done("t-vigil")),
            moss: moss("the Vigil triage", asks: ["Who else could take this?"])))

    static func planItem(_ id: String, _ title: String, why: String, flag: Flag, ticked: Bool = true, suggested: Bool = true) -> PlanPage.Item {
        PlanPage.Item(id: id, title: title, why: why, line: "This morning.", ticked: ticked, suggested: suggested, flag: flagControl(nil, suggested: flag))
    }

    static let planPage = PlanPage(
        title: "Today's plan", opening: "Free until Tomas at 14:20, then meetings until 21:30, with gaps at 16:30 and 20:30.",
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
        overlaps: [PlanPage.Overlap(id: "o-1500", name: "The 15:00 overlap", text: "Henrik and Karim both start at 15:00.", options: overlap.card.options)],
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
        .plan: .plan(planPage),
    ]

    static func app(_ pane: PanePresentation) -> TodayPresentation {
        TodayPresentation(
            pane: pane, pages: PageSource(fixed: pages), storage: SettingsPresenter.storage([], budget: nil),
            about: AboutPresentation.present(appName: "Substrate Today", version: "0.1.0 (1)", channel: nil, offered: nil, privacyPolicy: nil))
    }
}
