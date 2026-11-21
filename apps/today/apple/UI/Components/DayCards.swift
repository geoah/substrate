import SubstrateUI
import SwiftUI
import TodayCore

// What an open row holds, one view for each card of the design guide.

/// A routine, open: its items (names and doses, shown only inside the app),
/// one primary button across the card, and Skip.
struct RoutineCardView: View {
    var card: RoutineCard
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if !card.items.isEmpty {
                VStack(spacing: 0) {
                    ForEach(Array(card.items.enumerated()), id: \.element.id) { index, item in
                        ViewThatFits(in: .horizontal) {
                            HStack(alignment: .firstTextBaseline, spacing: 12) {
                                name(item)
                                Spacer(minLength: 12)
                                detail(item)
                            }
                            VStack(alignment: .leading, spacing: 2) {
                                name(item)
                                detail(item)
                            }
                        }
                        .padding(.vertical, 8)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .overlay(alignment: .top) {
                            if index > 0 { Rectangle().fill(TodayLook.line(scheme)).frame(height: 0.5) }
                        }
                        .accessibilityElement(children: .combine)
                    }
                }
            }
            if let done = card.done { PaneButton(done) }
            if let skip = card.skip { PaneButton(skip) }
            if let said = card.said { SavedLine(said) }
            if let stale = card.stale { StaleNoteView(note: stale) }
        }
    }

    private func name(_ item: RoutineCard.Item) -> some View {
        Text(item.name)
            .font(TodayLook.font(.callout, weight: .medium))
            .foregroundStyle(item.logged == nil ? AnyShapeStyle(.ink) : AnyShapeStyle(.inkSecond))
            .strikethrough(item.logged != nil)
            .multilineTextAlignment(.leading)
    }

    @ViewBuilder private func detail(_ item: RoutineCard.Item) -> some View {
        if let text = item.logged ?? item.detail {
            Text(text).font(TodayLook.font(.callout)).foregroundStyle(.inkSecond)
        }
    }
}

/// One pill and one sentence, where something has not been updated.
struct StaleNoteView: View {
    var note: StaleNote

    var body: some View {
        WrapLayout(spacing: 6) {
            PillView(note.pill)
            Text(note.text).font(TodayLook.Text.footnote).foregroundStyle(.inkSecond)
                .fixedSize(horizontal: false, vertical: true)
        }
        .accessibilityElement(children: .combine)
    }
}

/// What yesterday left: each thing with its option list. It acts on a tap:
/// it has no Save.
struct YesterdayCardView: View {
    var card: YesterdayCard
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            ForEach(Array(card.items.enumerated()), id: \.element.id) { index, item in
                VStack(alignment: .leading, spacing: 8) {
                    // One thing alone is the row itself: its head has said it.
                    if card.items.count > 1 {
                        HStack(alignment: .top, spacing: TodayLook.lead) {
                            GlyphView(glyph: item.glyph)
                            VStack(alignment: .leading, spacing: 4) {
                                Text(item.title).font(TodayLook.Text.rowTitle).foregroundStyle(.ink)
                                    .fixedSize(horizontal: false, vertical: true)
                                if let text = item.text {
                                    Text(text).font(TodayLook.Text.second).foregroundStyle(.inkSecond)
                                        .fixedSize(horizontal: false, vertical: true)
                                }
                            }
                        }
                    }
                    OptionListView(item.options)
                    if let chosen = item.chosen { SavedLine(chosen) }
                    if let ready = item.ready { ReadyMessageView(message: ready) }
                }
                .padding(.top, index > 0 ? 14 : 0)
                .overlay(alignment: .top) {
                    if index > 0 { Rectangle().fill(TodayLook.line(scheme)).frame(height: 0.5) }
                }
            }
            if let note = card.note { NoteText(note) }
        }
    }
}

/// The suggested tasks: every list open, the assistant's option selected
/// and marked Suggested, and one Save. Nothing above the button takes
/// effect until it is pressed.
struct SuggestedCardView: View {
    var card: SuggestedCard

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            NoteText(card.rule)
            if let batch = card.batch { NoteText(batch) }
            ForEach(Array(card.items.enumerated()), id: \.element.id) { index, item in
                SuggestedItemView(item: item, first: index == 0)
            }
            SaveRowView(row: card.save)
            if let older = card.dismissedOlder { NoteText(older) }
        }
    }
}

/// One suggested task: where it came from and its three options. It dims
/// as it fades, and says so from the first day it does.
struct SuggestedItemView: View {
    var item: SuggestedItem
    var first = false
    /// False under a row's own head, which has said its title and where it
    /// came from already.
    var headed = true
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            VStack(alignment: .leading, spacing: 4) {
                if headed {
                    RowHead(icon: .glyph(.task), title: item.title, stripe: item.stripe)
                    Text(item.origin).font(TodayLook.Text.footnote).foregroundStyle(.inkSecond)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if let quote = item.quote { QuoteText(quote) }
            }
            .opacity(dim)
            if item.fade.level > 0 {
                Text(item.fade.label)
                    .font(TodayLook.Text.footnote.weight(.medium))
                    .foregroundStyle(item.fade.level > 1 ? AnyShapeStyle(.urgentWords) : AnyShapeStyle(.cautionWords))
            }
            ChipsView(chips: item.chips)
            OptionListView(item.options)
            if let objection = item.objection {
                VStack(alignment: .leading, spacing: 6) {
                    HStack(spacing: 6) {
                        Image(systemName: Glyph.moss.systemName).font(TodayLook.font(.footnote, weight: .semibold)).accessibilityHidden(true)
                        Text(objection.heading).font(TodayLook.Text.sectionLabel)
                    }
                    .foregroundStyle(.mossWords)
                    Text(objection.line).font(TodayLook.Text.body).foregroundStyle(.ink)
                        .fixedSize(horizontal: false, vertical: true)
                    if let note = objection.note { NoteText(note) }
                    if let ready = objection.ready { ReadyMessageView(message: ready) }
                }
                .padding(12)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(.mossFill, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
            }
            if let save = item.save { SaveRowView(row: save) }
        }
        .padding(.top, first ? 0 : 12)
        .overlay(alignment: .top) {
            if !first { Rectangle().fill(TodayLook.line(scheme)).frame(height: 0.5) }
        }
    }

    private var dim: Double {
        switch item.fade.level {
        case 1: 0.82
        case 2: 0.64
        case 3...: 0.5
        default: 1
        }
    }
}

/// What was said, where a task came from words. In a card or a row it is
/// three lines at most, whatever the text size: the core gives its opening
/// words, and the task's own page draws all of it (`whole`), where a long
/// path or address breaks onto the next line.
struct QuoteText: View {
    var text: String
    var whole = false

    init(_ text: String, whole: Bool = false) {
        self.text = text
        self.whole = whole
    }

    var body: some View {
        Text(text).font(TodayLook.Text.second).italic().foregroundStyle(.inkSecond)
            .multilineTextAlignment(.leading)
            .lineLimit(whole ? nil : 3)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// "Plan the day", in the day: one line and one button that opens the page.
struct PlanRowCardView: View {
    var card: PlanRowCard

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(card.line).font(TodayLook.Text.body).foregroundStyle(.inkSecond)
                .fixedSize(horizontal: false, vertical: true)
            if let saved = card.saved { SavedLine(saved) }
            PaneButton(card.open)
        }
    }
}

/// An overlap's options, wherever they are drawn. They stay; the chosen
/// one is filled; a message is sent separately.
struct OverlapCardView: View {
    var card: OverlapCard
    /// Its name and state are drawn here, where no row's head says them.
    var titled = true

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if titled {
                ViewThatFits(in: .horizontal) {
                    HStack(alignment: .firstTextBaseline) {
                        Text(card.name).font(TodayLook.Text.rowTitle).foregroundStyle(.ink)
                        Spacer(minLength: 8)
                        PillView(card.pill)
                    }
                    VStack(alignment: .leading, spacing: 4) {
                        Text(card.name).font(TodayLook.Text.rowTitle).foregroundStyle(.ink)
                        PillView(card.pill)
                    }
                }
            }
            Text(card.text).font(TodayLook.Text.second).foregroundStyle(.inkSecond)
                .fixedSize(horizontal: false, vertical: true)
            if !titled { PillView(card.pill) }
            OptionListView(card.options)
            if let chosen = card.chosen { SavedLine(chosen) }
            if let ready = card.ready { ReadyMessageView(message: ready) }
        }
    }
}

/// What cannot wait, under the flame and its title: the urgent pill with
/// who set it and when, the text, who else says so, what it took the place
/// of, then its options with none selected for the owner.
struct UrgentCardView: View {
    var card: UrgentCard
    /// The row's own line, where it has one.
    var line: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            WrapLayout(spacing: 8) {
                PillView(.urgent, onTint: true)
                Text(card.setBy).font(TodayLook.Text.bodyStrong).foregroundStyle(.ink)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if let text = card.text ?? line {
                Text(text).font(TodayLook.font(.callout)).foregroundStyle(.ink)
                    .fixedSize(horizontal: false, vertical: true)
            }
            ForEach(card.voices, id: \.self) { voice in
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(voice.who).font(TodayLook.Text.button).foregroundStyle(.ink)
                    Text(voice.says).font(TodayLook.Text.second).foregroundStyle(.inkSecond)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            if let unchecked = card.unchecked {
                Text(unchecked).font(TodayLook.Text.second.weight(.medium)).foregroundStyle(.urgentWords)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if let took = card.took { NoteText(took) }
            if let project = card.project { ProjectChipView(chip: project) }
            if !card.options.isEmpty { OptionListView(card.options, onTint: true) }
            if let chosen = card.chosen { SavedLine(chosen) }
            if let ready = card.ready { ReadyMessageView(message: ready) }
            if let done = card.done { PaneButton(done) }
            if let said = card.said { SavedLine(said) }
            if let moss = card.moss { MossBoxView(box: moss) }
        }
    }
}

/// Something marked urgent that waits: who marked it, and its options. A
/// tap takes effect at once, so nothing is selected for the owner.
struct MarkedBlockView: View {
    var block: MarkedBlock
    var title: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let title {
                WrapLayout(spacing: 8) {
                    PillView(.urgent)
                    Text(title).font(TodayLook.Text.bodyStrong).foregroundStyle(.ink)
                }
            }
            if let by = block.by {
                Text(by).font(TodayLook.Text.second).foregroundStyle(.inkSecond)
                    .fixedSize(horizontal: false, vertical: true)
            }
            OptionListView(block.options)
            if let chosen = block.chosen { SavedLine(chosen) }
        }
    }
}

/// A task, open: its chips, what was said, where it was left, its Flag and
/// When lines, one primary button, and the assistant's box.
struct TaskCardView: View {
    var card: TaskCard
    /// The row's chips, where the card has none of its own.
    var chips: Chips = .none
    /// What keeps this card's open lines apart from another's.
    var scope: String
    /// On the task's own page, which draws all of its description.
    var whole = false

    @Environment(PaneViewState.self) private var state
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ChipsView(chips: card.chips.isEmpty ? chips : card.chips)
            if let quote = card.quote { QuoteText(quote, whole: whole) }
            if let left = card.left {
                VStack(alignment: .leading, spacing: 3) {
                    SectionLabel(left.heading)
                    Text(left.text).font(TodayLook.Text.body).foregroundStyle(.ink)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            if let deferred = card.deferred {
                Text(deferred).font(TodayLook.Text.second.weight(.medium)).foregroundStyle(.cautionWords)
            }
            if let marked = card.marked { MarkedBlockView(block: marked, title: card.markedTitle) }
            if !card.doors.isEmpty {
                VStack(spacing: 0) {
                    ForEach(card.doors) { door in
                        DoorView(door: door, open: state.open("door:\(scope):\(door.id)"))
                    }
                }
                .overlay(alignment: .bottom) { Rectangle().fill(TodayLook.line(scheme)).frame(height: 0.5) }
            }
            if let done = card.done { PaneButton(done) }
            if let said = card.said { SavedLine(said) }
            if let moss = card.moss { MossBoxView(box: moss) }
        }
    }
}

/// The assistant, about something flagged High that will not be finished:
/// it says so and asks where it goes, as an option list.
struct UnfinishedCardView: View {
    var card: UnfinishedCard

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Image(systemName: Glyph.moss.systemName).font(TodayLook.font(.footnote, weight: .semibold)).accessibilityHidden(true)
                Text(card.heading).font(TodayLook.Text.sectionLabel)
            }
            .foregroundStyle(.mossWords)
            Text(card.line).font(TodayLook.Text.bodyStrong).foregroundStyle(.ink)
                .fixedSize(horizontal: false, vertical: true)
            if let left = card.left { NoteText(left) }
            if let project = card.project { ProjectChipView(chip: project) }
            OptionListView(card.options)
            if let chosen = card.chosen { SavedLine(chosen) }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            let shape = RoundedRectangle(cornerRadius: 20, style: .continuous)
            shape.fill(.mossFill).overlay(shape.strokeBorder(TodayLook.moss.color.opacity(0.35), lineWidth: 1))
        }
    }
}

/// The end of the day: what was done, where each unfinished thing goes as
/// a list with the assistant's option selected, and one button that saves
/// them all and closes the day.
struct WrapCardView: View {
    var card: WrapCard
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if !card.done.isEmpty {
                section(card.labels.done) {
                    ForEach(card.done) { item in
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Text(item.title)
                                .font(TodayLook.Text.body)
                                .foregroundStyle(item.done ? AnyShapeStyle(.inkSecond) : AnyShapeStyle(.ink))
                                .strikethrough(item.done)
                                .fixedSize(horizontal: false, vertical: true)
                            Spacer(minLength: 8)
                            Text(item.time).font(TodayLook.Text.time).foregroundStyle(.inkSecond)
                        }
                        .stripe(item.stripe, inset: 1, room: 10)
                        .frame(minHeight: 28)
                    }
                }
            }
            if let others = card.others { NoteText(others) }
            if let suggested = card.suggested { NoteText(suggested) }
            if !card.moved.isEmpty {
                section(card.labels.moved) { ForEach(card.moved) { titled($0) } }
            }
            if !card.carry.isEmpty {
                section(card.labels.carry) {
                    ForEach(card.carry) { item in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(item.title).font(TodayLook.Text.rowTitle).foregroundStyle(.ink)
                                .fixedSize(horizontal: false, vertical: true)
                            Text(item.why).font(TodayLook.Text.second).foregroundStyle(.inkSecond)
                                .fixedSize(horizontal: false, vertical: true)
                            OptionListView(item.options).padding(.top, 4)
                        }
                        .padding(.bottom, 6)
                    }
                }
            }
            if let unanswered = card.unanswered { NoteText(unanswered) }
            if !card.dismissed.isEmpty {
                section(card.labels.dismissed) { ForEach(card.dismissed) { titled($0) } }
            }
            if let tomorrow = card.tomorrow {
                section(card.labels.tomorrow) {
                    Text(tomorrow).font(TodayLook.Text.body).foregroundStyle(.ink).fixedSize(horizontal: false, vertical: true)
                }
            }
            SaveRowView(row: card.save)
        }
    }

    private func section(_ label: String, @ViewBuilder content: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            SectionLabel(label)
            content()
        }
    }

    private func titled(_ item: WrapCard.Titled) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(item.title).font(TodayLook.Text.bodyStrong).foregroundStyle(.ink).fixedSize(horizontal: false, vertical: true)
            Text(item.line).font(TodayLook.Text.footnote).foregroundStyle(.inkSecond).fixedSize(horizontal: false, vertical: true)
        }
    }
}
