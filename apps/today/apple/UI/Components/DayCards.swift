import SubstrateUI
import SwiftUI
import TodayCore

// What an open row holds, one view for each card of the design guide.

/// A routine, open: its items (names and doses, shown only inside the app),
/// each with a tick of its own, then one primary button across the card for
/// what is still open, with Skip beside it.
struct RoutineCardView: View {
    var card: RoutineCard
    /// The row it belongs to: while the whole row is being finished
    /// (`Finishing`), its items wait with it.
    var scope: String = ""
    @Environment(\.colorScheme) private var scheme
    @Environment(PaneViewState.self) private var state

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if !card.items.isEmpty { RoutineLinesView(items: card.items, scope: scope) }
            if card.done != nil || card.skip != nil {
                HStack(spacing: TodayLook.betweenButtons) {
                    if let done = card.done { PaneButton(done) }
                    if let skip = card.skip { PaneButton(skip, beside: card.done?.wide == true).fixedSize() }
                }
            }
            if let said = card.said { SavedLine(said) }
            if let notLogged = card.notLogged { NoteText(notLogged) }
        }
    }
}

/// The things of a routine, one line each: its name, its dose or how it
/// was logged, and its own tick. Today's routine draws them, and so does
/// what yesterday left.
struct RoutineLinesView: View {
    var items: [RoutineCard.Item]
    /// The row they belong to: while the whole row is being finished
    /// (`Finishing`), its items wait with it.
    var scope: String = ""
    @Environment(\.colorScheme) private var scheme
    @Environment(PaneViewState.self) private var state

    var body: some View {
        VStack(spacing: 0) {
            ForEach(Array(items.enumerated()), id: \.element.id) { index, item in
                HStack(alignment: .center, spacing: 8) {
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
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .accessibilityElement(children: .combine)
                    // While the whole row waits to be finished, an item is not ticked by itself.
                    if let tick = item.tick { PaneButton(tick, small: true).disabled(state.waiting(scope) != nil) }
                }
                // A line with a tick is as tall as the tick's target.
                .padding(.vertical, item.tick == nil ? 8 : 1)
                .overlay(alignment: .top) {
                    if index > 0 { Rectangle().fill(TodayLook.line(scheme)).frame(height: 0.5) }
                }
            }
        }
    }

    /// Crossed out: logged, or finished a moment ago and not yet written,
    /// by its own tick or with its whole row.
    private func struck(_ item: RoutineCard.Item) -> Bool {
        item.logged != nil || state.phase(item.id) == .settled || state.phase(scope) == .settled
    }

    private func name(_ item: RoutineCard.Item) -> some View {
        Text(item.name)
            .font(TodayLook.font(.callout, weight: .medium))
            .foregroundStyle(struck(item) ? AnyShapeStyle(.inkSecond) : AnyShapeStyle(.ink))
            .strikethrough(struck(item))
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

/// What was not logged yesterday: each routine by name, with when it was
/// due, what it left line by line with a tick on each, and its option
/// list for the rest. One alone is named by the row's own line, so only
/// when it was due is said under it. It acts on a tap: it has no Save.
struct YesterdayCardView: View {
    var card: YesterdayCard
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            ForEach(Array(card.items.enumerated()), id: \.element.id) { index, item in
                VStack(alignment: .leading, spacing: 8) {
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
                    } else if let text = item.text {
                        Text(text).font(TodayLook.Text.second).foregroundStyle(.inkSecond)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    if !item.lines.isEmpty { RoutineLinesView(items: item.lines, scope: item.id) }
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

/// One suggested task: where it came from and its options, with the field
/// "Other" shows while it is the pick. One the owner answered in words is
/// drawn with what was said and the exchange that followed, and nothing
/// else. It dims as it fades, and says so from the first day it does.
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
                if !item.links.isEmpty { SourceLinksView(links: item.links) }
                if let quote = item.quote {
                    Folded(brief: item.brief, id: "said:" + item.id) { all in QuoteText(all ? quote : item.brief?.text ?? quote, whole: all) }
                }
            }
            .opacity(dim)
            if let told = item.told {
                SuggestedToldView(told: told)
            } else {
                SuggestedAnswersView(item: item)
            }
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

/// What a suggested task holds under what was said: how far it has faded,
/// its chips, its options, the field "Other" shows, the assistant's one
/// objection, and its own Save where it is drawn alone.
struct SuggestedAnswersView: View {
    var item: SuggestedItem

    var body: some View {
        if item.fade.level > 0 {
            Text(item.fade.label)
                .font(TodayLook.Text.footnote.weight(.medium))
                .foregroundStyle(item.fade.level > 1 ? AnyShapeStyle(.urgentWords) : AnyShapeStyle(.cautionWords))
        }
        ChipsView(chips: item.chips)
        OptionListView(item.options)
        if let other = item.other { SuggestedOtherField(id: item.id, other: other) }
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
}

/// The field under "Other": what is wrong with the suggestion, or what to
/// do instead. What is typed is the view's until the Save above which it
/// stands, which sends it with the answers (`DayScreen` adds it to the
/// command).
struct SuggestedOtherField: View {
    var id: String
    var other: SuggestedItem.Other

    @Environment(PaneViewState.self) private var state
    @Environment(\.colorScheme) private var scheme
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            TextField(other.placeholder, text: Binding(get: { state.words[id] ?? "" }, set: { state.words[id] = $0 }), axis: .vertical)
                .font(TodayLook.Text.body)
                .lineLimit(2 ... 6)
                .textFieldStyle(.plain)
                .focused($focused)
                .writes(focused)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .frame(minHeight: TodayLook.target)
                .background(TodayLook.bed(scheme), in: RoundedRectangle(cornerRadius: TodayLook.bedRadius, style: .continuous))
                .accessibilityLabel(other.placeholder)
            NoteText(other.note).padding(.horizontal, 6)
        }
    }
}

/// A suggestion the owner answered in words: what was said, and the
/// exchange with the assistant that followed, in its hue.
struct SuggestedToldView: View {
    var told: SuggestedItem.Told

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Image(systemName: Glyph.moss.systemName).font(TodayLook.font(.footnote, weight: .semibold)).accessibilityHidden(true)
                Text(told.heading).font(TodayLook.Text.sectionLabel)
            }
            .foregroundStyle(.mossWords)
            Text(told.words).font(TodayLook.Text.bodyStrong).foregroundStyle(.ink)
                .fixedSize(horizontal: false, vertical: true)
            // The heading has said who was told and the words are above: the exchange is its answer alone.
            if let answer = told.answer {
                switch answer.state {
                case .waiting:
                    ProgressView().controlSize(.small).frame(minHeight: 22)
                case .writing, .done:
                    RecordMarkdownText(answer.text).font(TodayLook.Text.body).foregroundStyle(.ink)
                case .failed:
                    Text(answer.text).font(TodayLook.Text.body).foregroundStyle(.urgentWords)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if let button = answer.button { PaneButton(button, onTint: true, small: true) }
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.mossFill, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
    }
}

/// What was said, where a task came from words, drawn as Markdown: a mark
/// of emphasis is its emphasis, and a record it names is the record's
/// pill. In a card or a row it is three lines at most, whatever the text
/// size: the core gives its opening words, and the task's own page draws
/// all of it (`whole`), where a long path or address breaks onto the next
/// line.
struct QuoteText: View {
    var text: String
    var whole = false

    init(_ text: String, whole: Bool = false) {
        self.text = text
        self.whole = whole
    }

    var body: some View {
        RecordMarkdownText(text)
            .font(TodayLook.Text.second)
            .italic()
            .foregroundStyle(.inkSecond)
            .multilineTextAlignment(.leading)
            .lineLimit(whole ? nil : 3)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// A text that may be long. Where it has a `brief`, its opening words are
/// drawn first, with a button under them that shows all of it in place
/// and puts it back; with none, all of it is drawn and there is no button.
struct Folded<Content: View>: View {
    var brief: Brief?
    /// What keeps this text's state apart from another's.
    var id: String
    /// The text: all of it, or as much as its brief says.
    @ViewBuilder var content: (_ all: Bool) -> Content

    @Environment(PaneViewState.self) private var state

    var body: some View {
        if let brief {
            let all = state.isOpen(id)
            VStack(alignment: .leading, spacing: 6) {
                // The opening words and the whole text are two faces: one
                // fades as the other comes, cut to the room the text has
                // while that room grows, so nothing is drawn over what
                // stands under it.
                Group {
                    if all { content(true).transition(.face) } else { content(false).transition(.face) }
                }
                .frame(maxWidth: .infinity, alignment: .topLeading)
                .clipped()
                Button {
                    state.toggle(id)
                } label: {
                    HStack(spacing: 6) {
                        Text(all ? brief.less : brief.more)
                        Image(systemName: (all ? Symbol.close : Symbol.open).systemName)
                            .font(TodayLook.font(.caption, weight: .bold))
                            .accessibilityHidden(true)
                    }
                }
                .buttonStyle(.today(.small))
                .accessibilityHint(all ? "Shows its opening words" : "Shows all of it in place")
            }
        } else {
            content(true)
        }
    }
}

/// A task's description, to be read: its Markdown as text, every line of
/// it, with a record it names as that record's pill.
struct DetailsText: View {
    var text: String

    init(_ text: String) { self.text = text }

    var body: some View {
        RecordMarkdownText(text)
            .font(TodayLook.Text.body)
            .foregroundStyle(.ink)
            .multilineTextAlignment(.leading)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// What a row that opens a page holds where it leads the day: one line and
/// one button that opens the page.
struct PageRowCardView: View {
    var card: PageRowCard

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
            ActionRow(action: card.done, links: card.links)
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

    @Environment(PaneViewState.self) private var state
    @Environment(\.colorScheme) private var scheme
    @Environment(\.pane) private var pane

    private var flagDoor: String { "door:\(scope):flag" }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ChipsView(chips: card.chips.isEmpty ? chips : card.chips)
            if let details = card.details {
                Folded(brief: card.brief, id: "details:" + scope) { all in DetailsText(all ? details : card.brief?.text ?? details) }
            }
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
            ActionRow(action: card.done, links: card.links)
            if let said = card.said { SavedLine(said) }
            if let moss = card.moss { MossBoxView(box: moss) }
        }
        // The flag is changed here, in the opened row. The order of the
        // day follows it once its list is closed, or the card is.
        .onChange(of: state.isOpen(flagDoor)) { _, open in
            if !open { pane.send(.settleFlags) }
        }
        .onDisappear {
            if state.isOpen(flagDoor) { pane.send(.settleFlags) }
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
            if let answer = card.answer { PaneButton(answer) }
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
