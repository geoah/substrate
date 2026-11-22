#if os(macOS)
import SwiftUI
import TodayCore

@testable import SubstrateTodayUI

/// The design guide's seventeen components, each alone with every state
/// under a short caption, drawn by the views the screens are made of: what
/// `round8/order.html?guide=<id>` is to the mock-up.
@MainActor
enum ComponentSheets {
    /// Each component's id in the guide, and the height its sheet needs.
    static let all: [(String, CGFloat)] = [
        ("tile", 420), ("badge", 380), ("glyph", 420), ("stripe", 520), ("pill", 420), ("button", 900), ("options", 1500), ("row", 1120),
        ("card", 900), ("strip", 620), ("happening", 1300), ("urgent", 2100), ("save", 2500), ("bar", 760), ("moss", 760), ("page", 1500),
        ("ground", 900),
    ]

    @ViewBuilder static func sheet(_ name: String) -> some View {
        Sheet {
            switch name {
            case "tile": tile
            case "badge": badge
            case "glyph": glyph
            case "stripe": stripe
            case "pill": pill
            case "button": button
            case "options": options
            case "row": row
            case "card": card
            case "strip": strip
            case "happening": happening
            case "urgent": urgent
            case "save": save
            case "bar": bar
            case "moss": moss
            case "page": page
            default: ground
            }
        }
    }

    struct Sheet<Content: View>: View {
        @ViewBuilder var content: () -> Content
        @State private var state = PaneViewState()

        var body: some View {
            ZStack(alignment: .top) {
                GroundView(ground: .working)
                VStack(alignment: .leading, spacing: 14, content: content)
                    .padding(TodayLook.side)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            }
            .environment(state)
        }
    }

    struct Case<Content: View>: View {
        var caption: String
        var carded = true
        @ViewBuilder var content: () -> Content

        var body: some View {
            VStack(alignment: .leading, spacing: 6) {
                Text(caption).font(.system(.caption, weight: .semibold)).foregroundStyle(.inkSecond)
                if carded { content().card() } else { content() }
            }
        }
    }

    static let h = HandScenes.self

    static var tile: some View {
        Group {
            Case(caption: "Chip, row and page") {
                HStack(spacing: 16) {
                    TileView(tile: h.tile("EA"), size: .chip)
                    TileView(tile: h.tile("EA"), size: .row)
                    TileView(tile: h.tile("EA"), size: .page)
                }
            }
            Case(caption: "Suggested: outlined") { TileView(tile: h.tile("SH", suggested: true), size: .row) }
            Case(caption: "Named: the chip") { ProjectChipView(chip: h.security) }
        }
    }

    static var badge: some View {
        Group {
            Case(caption: "Chip, row and page") {
                HStack(spacing: 16) {
                    BadgeView(badge: h.badge("TV"), size: .chip)
                    BadgeView(badge: h.badge("TV"), size: .row)
                    BadgeView(badge: h.badge("TV"), size: .page)
                }
            }
            Case(caption: "Named: the chip") { PersonChipView(chip: PersonChip(id: "tomas", label: "for Tomas", badge: h.badge("TV"))) }
        }
    }

    static var glyph: some View {
        Group {
            Case(caption: "Kinds") {
                HStack(spacing: 12) {
                    ForEach([Glyph.task, .meeting, .routine, .night, .moss, .urgent, .plan], id: \.self) { GlyphView(glyph: $0) }
                }
            }
            Case(caption: "On the current item") {
                HStack(spacing: 12) { ForEach([Glyph.task, .meeting, .routine], id: \.self) { GlyphView(glyph: $0, size: .lead) } }
            }
            Case(caption: "Chevrons: a page, in place, open") {
                HStack(spacing: 16) {
                    RowChevron(turn: .page)
                    RowChevron(turn: .closed)
                    RowChevron(turn: .open)
                }
            }
        }
    }

    static var stripe: some View {
        Group {
            ForEach(Flag.allCases, id: \.self) { flag in
                Case(caption: flag.word) {
                    RowHead(icon: .glyph(.task), title: "File the IT ticket", line: "Kenji needs it before Monday.", stripe: Stripe(flag))
                }
            }
            Case(caption: "Suggested: hollow") {
                RowHead(icon: .glyph(.task), title: "File the IT ticket", line: "Kenji needs it before Monday.", stripe: Stripe(.high, suggested: true))
            }
            Case(caption: "None: no stripe") { RowHead(icon: .glyph(.task), title: "File the IT ticket", line: "Kenji needs it before Monday.") }
        }
    }

    static var pill: some View {
        Group {
            Case(caption: "Neutral") {
                HStack {
                    PillView(Pill(.neutral, "Saved"))
                    PillView(Pill(.neutral, "Chosen"))
                    PillView(.new)
                }
            }
            Case(caption: "Suggested, urgent, stale, caution") {
                HStack {
                    PillView(.suggested)
                    PillView(.urgent)
                    PillView(.stale)
                    PillView(Pill(.caution, "To choose"))
                }
            }
        }
    }

    static var button: some View {
        Group {
            Case(caption: "Primary") { PaneButton(ButtonPresentation("Done", glyph: .done, role: .primary)) }
            Case(caption: "Wide") { PaneButton(ButtonPresentation("Save 13 answers", glyph: .done, role: .primary, wide: true)) }
            Case(caption: "Secondary, and outlined") {
                HStack {
                    PaneButton(ButtonPresentation("Got it", glyph: .done))
                    PaneButton(ButtonPresentation("Not now", role: .quiet))
                }
            }
            Case(caption: "Small, and one that opens a page") {
                HStack {
                    PaneButton(ButtonPresentation("Open", role: .quiet), small: true)
                    PaneButton(ButtonPresentation("All projects", role: .quiet, opens: .projects), small: true)
                }
            }
            Case(caption: "Destructive") { PaneButton(ButtonPresentation("Remove", role: .destructive)) }
            Case(caption: "Icons") {
                HStack {
                    IconButton(Symbol.edit, label: "Edit", small: true) {}
                    IconButton(Symbol.back, label: "Back") {}
                    IconButton(Symbol.add, label: "Add") {}
                    IconButton(Symbol.dictate, label: "Dictate", look: .filled) {}
                    IconButton(Symbol.flag, label: "Flag", small: true) {}
                    IconButton(Symbol.flag, label: "Flag", small: true, look: .pressed) {}
                }
            }
            Case(caption: "Disabled") { PaneButton(ButtonPresentation("Save 0 answers", glyph: .done, role: .primary, wide: true, enabled: false)) }
        }
    }

    static var options: some View {
        Group {
            Case(caption: "Default: above a Save, Moss's option starts selected") { OptionListView(h.suggestedItem("a", "", origin: "", quote: nil).options) }
            Case(caption: "Changed: you picked another; the pill stays") {
                OptionListView(h.suggestedItem("a", "", origin: "", quote: nil, pick: .dismiss).options)
            }
            Case(caption: "Suggested: a tap takes effect at once, nothing is selected") {
                OptionListView(h.flags(chosen: nil, suggested: .high, reason: "The review is due before Monday."))
            }
            Case(caption: "Chosen: what you chose") { OptionListView(h.flags(chosen: .medium)) }
            Case(caption: "Several: square marks, nothing ticked for you") { OptionListView(h.planPage.sends) }
        }
    }

    static var row: some View {
        Group {
            Case(caption: "Task, routine", carded: false) {
                VStack(spacing: 0) {
                    RowView(
                        row: h.taskRow(
                            "t1", "File the IT ticket for the stopped VMs", "Kenji needs it before Monday.", flag: .high, state: .next,
                            chips: Chips(project: h.security)))
                    RowView(
                        row: Row(
                            id: "r", glyph: .routine, title: "Skincare", line: "5 steps", state: .next,
                            trailing: .tick(ButtonPresentation("Done", glyph: .done, iconOnly: true)),
                            body: .routine(RoutineCard(items: [], done: nil, skip: nil))),
                        first: false)
                }
                .soft()
            }
            Case(caption: "Meeting: the next fixed point", carded: false) {
                VStack(spacing: 0) {
                    MeetingRowView(
                        meeting: h.meeting(
                            "m1", "14:20", "14:40", "Tomas", people: ["TV"], line: "Your task: fix the 1:1 clash with Tomas.", until: "in 2 h 50"))
                    MeetingRowView(meeting: h.meeting("m2", "14:40", "15:00", "Viktor", people: ["VL"], line: "Weekly 1:1.", project: h.lantern), first: false)
                }
                .soft()
            }
            Case(caption: "Page: a row that opens a page") {
                TaskLines(lines: [TaskLine(id: "t", title: "Read the incident write-up", line: "Today.", stripe: Stripe(.medium), opens: .task("t"))])
            }
            Case(caption: "Page, in the day: the suggested tasks and the saved plan", carded: false) {
                VStack(spacing: 0) {
                    RowView(
                        row: Row(
                            id: "suggested", glyph: .moss, title: "Thirteen suggested tasks to answer",
                            line: "Four on your priorities. Moss would make twelve tasks.", state: .next, trailing: .page(.suggested),
                            body: .suggested(PageRowCard(line: "", open: ButtonPresentation("Open the suggested tasks", opens: .suggested)))))
                    RowView(
                        row: Row(
                            id: "plan", glyph: .plan, title: "Today's plan", line: "Saved at 08:44. Thirteen tasks. One is left from yesterday.",
                            state: .next, trailing: .page(.plan),
                            body: .plan(PageRowCard(line: "", open: ButtonPresentation("Open the plan", opens: .plan)))),
                        first: false)
                }
                .soft()
            }
            Case(caption: "Closed, then open in place", carded: false) {
                VStack(spacing: 8) {
                    NightView(night: h.nightRead)
                    EarlierView(earlier: h.earlier)
                }
            }
        }
    }

    static var card: some View {
        Case(caption: "The current item: a task", carded: false) {
            RowView(
                row: Row(
                    id: "t-vigil", glyph: .task, title: "Triage the Vigil findings and review the four lockdown PRs",
                    line: "Over 90 findings with no owner in the sources checked, and the 20:00 sync will ask.", stripe: Stripe(.high), state: .now,
                    chips: Chips(project: h.security), trailing: .flag(h.flagControl(.high)), body: .task(h.vigilCard)))
        }
    }

    static var strip: some View {
        Group {
            Case(caption: "Standing", carded: false) { StripView(strip: h.strip(suggesting: false)) }
            Case(caption: "Suggested", carded: false) { StripView(strip: h.strip(suggesting: true)) }
            Case(caption: "Empty", carded: false) {
                StripView(strip: StripPresentation(date: "Thursday 19 November", dateShort: "Thu 19 Nov", entries: [], empty: "No project is flagged yet."))
            }
        }
    }

    static var happening: some View {
        Group {
            Case(caption: "Closed", carded: false) { HappeningView(happening: h.happening()) }
            Case(caption: "Open, with something marked urgent and held back", carded: false) {
                OpenHappening(happening: h.happening(marked: true))
            }
        }
    }

    struct OpenHappening: View {
        var happening: HappeningPresentation
        @State private var state: PaneViewState = {
            let state = PaneViewState()
            state.opened = ["happening"]
            return state
        }()

        var body: some View { HappeningView(happening: happening).environment(state) }
    }

    static func urgentRow(_ card: UrgentCard) -> some View {
        RowView(row: Row(id: card.id, glyph: .urgent, title: "External API is failing again", state: .now, body: .urgent(card)))
    }

    static var urgent: some View {
        Group {
            Case(caption: "Open", carded: false) { urgentRow(h.urgent()) }
            Case(caption: "Chosen: its message is ready, with its own Send", carded: false) { urgentRow(h.urgent(chosen: true)) }
            Case(caption: "Unchecked: nobody has checked it", carded: false) { urgentRow(h.urgent(unchecked: true)) }
        }
    }

    static var save: some View {
        Group {
            Case(caption: "Draft: a project's suggestions, every list open, nothing in effect; Other with its field; one answered in words") {
                SuggestedGroupView(group: h.suggestedPage.groups[0])
            }
            Case(caption: "A task yesterday left, on the plan page: nothing chosen until you choose") {
                PlanLeftView(left: h.planPage.left[1])
            }
            Case(caption: "Saved: one line") {
                SaveRowView(row: SaveRow(button: nil, saved: "Saved at 08:38. Twelve made tasks and one dismissed."))
            }
        }
    }

    static var bar: some View {
        Group {
            Case(caption: "Empty", carded: false) { ComposerBar(composer: ComposerPresentation()) }
            Case(caption: "Not kept", carded: false) {
                ComposerBar(composer: ComposerPresentation(notKept: "This iPhone could not keep the change: the disk is full."))
            }
            Case(caption: "An answer, in place over the field", carded: false) {
                ComposerBar(
                    composer: ComposerPresentation(),
                    answer: AnswerCard(
                        id: "a", heading: "You asked Moss", question: "What can I drop this afternoon?", text: "Four slots are double-booked.", state: .done,
                        button: ButtonPresentation("Close", role: .quiet)))
            }
            Case(caption: "Words the assistant could not take, kept as a task", carded: false) {
                ComposerBar(
                    composer: ComposerPresentation(),
                    answer: AnswerCard(
                        id: "b", heading: "You said to Moss", question: "Order the SSD caddies",
                        text: "Moss could not be reached. This iPhone is offline.", state: .failed,
                        button: ButtonPresentation("Close", role: .quiet), keep: ButtonPresentation("Add as a task", role: .secondary)))
            }
        }
    }

    static var moss: some View {
        Group {
            Case(caption: "Ready") { MossBoxView(box: h.moss("the Vigil triage", asks: ["Who else could take this?", "What changed this week?"])) }
            Case(caption: "After an answer") { MossBoxView(box: h.personPage.moss!) }
            Case(caption: "Cannot be reached") {
                MossBoxView(
                    box: MossBox(
                        subject: .day, title: "Moss, about the Vigil triage", placeholder: "",
                        unreachable: "Moss cannot be reached right now. What you ask is sent when it can."))
            }
        }
    }

    static var page: some View {
        Group {
            Case(caption: "Project") {
                PageTitle(icon: .tile(h.tile("EA")), title: "External API reliability", stripe: Stripe(.high)) {
                    IconButton(Symbol.flag, label: "Flag", small: true) {}
                }
            }
            Case(caption: "Person") { PageTitle(icon: .badge(h.badge("TV")), title: "Tomas Varga") }
            Case(caption: "Meeting") { PageTitle(eyebrow: "14:20 to 14:40, in 2 h 50", icon: .glyph(.meeting), title: "Tomas") }
            Case(caption: "Task") {
                PageTitle(icon: .glyph(.task), title: "Triage the Vigil findings and review the four lockdown PRs", stripe: Stripe(.high))
            }
        }
    }

    static var ground: some View {
        VStack(spacing: 8) {
            ForEach(Ground.allCases, id: \.self) { ground in
                ZStack(alignment: .topLeading) {
                    GroundSwatch(ground: ground)
                    Text(ground.rawValue.capitalized)
                        .font(.system(.caption, weight: .semibold))
                        .foregroundStyle(ground == .closing ? Color.white : TodayLook.ink.color)
                        .padding(10)
                }
                .frame(height: 120)
                .clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous))
            }
        }
    }

    struct GroundSwatch: View {
        var ground: Ground
        @Environment(\.colorScheme) private var scheme

        var body: some View { LinearGradient(stops: ground.stops(scheme), startPoint: .top, endPoint: .bottom) }
    }
}
#endif
