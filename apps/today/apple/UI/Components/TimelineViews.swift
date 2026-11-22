import SubstrateUI
import SwiftUI
import TodayCore

// The parts of the day that are not rows: the night, earlier today, what
// is happening, and the quiet lines between.

/// One line of a timeline: a time, who in bold, what happened, what was
/// said, and where it came from. Where it names a meeting or a task it
/// opens that page.
struct TimelineLineView: View {
    var line: TimelineLine
    @Environment(\.pane) private var pane

    var body: some View {
        if let route = line.opens {
            Button {
                pane.open(route)
            } label: {
                HStack(alignment: .top, spacing: 6) {
                    content
                    RowChevron(turn: .page)
                }
                .frame(minHeight: TodayLook.target, alignment: .top)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityHint("Opens its page")
        } else {
            content
        }
    }

    private var content: some View {
        HStack(alignment: .firstTextBaseline, spacing: TodayLook.gap) {
            Text(line.time).font(TodayLook.Text.time).foregroundStyle(.inkSecond)
                .frame(minWidth: 44, alignment: .leading)
            VStack(alignment: .leading, spacing: 3) {
                Text("\(Text(line.who.map { $0 + " " } ?? "").fontWeight(.semibold))\(Text(line.text))")
                    .font(TodayLook.Text.body)
                    .foregroundStyle(.ink)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
                if let quote = line.quote {
                    Text(quote).font(TodayLook.Text.second).italic().foregroundStyle(.inkSecond)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if let source = line.source { PillView(source) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .accessibilityElement(children: .combine)
    }
}

/// The night: always the first item of the timeline. Folded it is one line
/// with the moon glyph; it opens in place to what happened since the day
/// before ended, by project.
struct NightView: View {
    @Environment(\.words) private var words
    var night: NightPresentation

    @Environment(PaneViewState.self) private var state

    private var isOpen: Bool { night.leads || state.isOpen("night") }

    var body: some View {
        if isOpen { card } else { line }
    }

    private var line: some View {
        OpensInPlace(id: "night") {
            HStack(alignment: .top, spacing: TodayLook.lead) {
                GlyphView(glyph: .night, size: .chip).padding(.top, 1)
                Text(night.line)
                    .font(night.unread ? TodayLook.Text.bodyStrong : TodayLook.Text.body)
                    .foregroundStyle(.ink)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, TodayLook.gap)
        .frame(minHeight: TodayLook.target)
        .soft()
    }

    private var card: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: 4) {
                RowHead(icon: .glyph(.night), title: night.title, line: night.headline ?? night.line, size: night.leads ? .lead : .open)
                if !night.leads {
                    IconButton(Symbol.close, label: words.close, small: true) {
                        withAnimation(.snappy(duration: 0.2)) { state.toggle("night") }
                    }
                }
            }
            if let coverage = night.coverage { NoteText(coverage) }
            if let stale = night.stale { StaleNoteView(note: stale) }
            if let empty = night.empty {
                Text(empty).font(TodayLook.Text.body).foregroundStyle(.inkSecond)
            }
            ForEach(night.groups) { group in
                VStack(alignment: .leading, spacing: 4) {
                    SectionLabel(group.label)
                    ForEach(Array(group.items.enumerated()), id: \.element.id) { index, item in
                        NightItemView(item: item, first: index == 0)
                    }
                }
            }
            if let quiet = night.quiet { NoteText(quiet) }
            if let gotIt = night.gotIt { PaneButton(gotIt) }
            if let read = night.read { SavedLine(read) }
        }
        .card()
    }
}

/// One project's night: its tile and name, how much is new, its latest
/// line, and the lines under it once opened.
struct NightItemView: View {
    var item: NightItem
    var first = true

    @Environment(PaneViewState.self) private var state

    private var openID: String { "night:" + item.id }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            OpensInPlace(id: openID) {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(alignment: .center, spacing: 8) {
                        if let tile = item.tile { TileView(tile: tile, size: .row) }
                        Text(item.name).font(TodayLook.Text.rowTitle).foregroundStyle(item.seen ? AnyShapeStyle(.inkSecond) : AnyShapeStyle(.ink))
                            .multilineTextAlignment(.leading)
                        if let pill = item.pill { PillView(pill) }
                        Spacer(minLength: 4)
                        Text(item.count).font(TodayLook.Text.footnote.weight(.medium)).foregroundStyle(.inkSecond).fixedSize()
                    }
                    .stripe(item.stripe, inset: 2)
                    if !item.seen || state.isOpen(openID) {
                        Text(item.text).font(TodayLook.Text.body).foregroundStyle(item.seen ? AnyShapeStyle(.inkSecond) : AnyShapeStyle(.ink))
                            .multilineTextAlignment(.leading)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
            .frame(minHeight: TodayLook.target)
            if state.isOpen(openID) {
                ForEach(item.lines) { TimelineLineView(line: $0) }
                if let decision = item.decision { decisionView(decision) }
                WrapLayout(spacing: TodayLook.betweenButtons) {
                    if let gotIt = item.gotIt { PaneButton(gotIt) }
                    if let open = item.open { PaneButton(open) }
                }
            } else if let decision = item.decision, decision.chosen == nil {
                decisionView(decision)
            }
        }
        .padding(.vertical, 6)
        .rowLine(!first)
    }

    private func decisionView(_ decision: NightDecision) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(decision.question).font(TodayLook.Text.bodyStrong).foregroundStyle(.ink)
                .fixedSize(horizontal: false, vertical: true)
            OptionListView(decision.options)
            if let chosen = decision.chosen { SavedLine(chosen) }
            if let ready = decision.ready { ReadyMessageView(message: ready) }
        }
    }
}

/// What is done today, folded into one line with a count.
struct EarlierView: View {
    var earlier: EarlierPresentation

    @Environment(PaneViewState.self) private var state

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button {
                withAnimation(.snappy(duration: 0.2)) { state.toggle("earlier") }
            } label: {
                HStack(spacing: 8) {
                    Text(earlier.title).font(TodayLook.font(.subheadline, weight: .semibold)).foregroundStyle(.inkSecond)
                    Spacer(minLength: 8)
                    Text(earlier.count).font(TodayLook.font(.footnote, weight: .medium)).foregroundStyle(.inkSecond)
                    RowChevron(turn: state.isOpen("earlier") ? .open : .closed)
                }
                .padding(.horizontal, 6)
                .frame(minHeight: TodayLook.target)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityHint(state.isOpen("earlier") ? "Closes" : "Opens in place")
            if state.isOpen("earlier") {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(earlier.lines) { line in
                        HStack(alignment: .firstTextBaseline, spacing: TodayLook.gap) {
                            Text(line.time).font(TodayLook.Text.time).foregroundStyle(.inkSecond)
                                .frame(minWidth: 44, alignment: .leading)
                            Text(line.text).font(TodayLook.Text.second).foregroundStyle(.inkSecond)
                                .multilineTextAlignment(.leading)
                                .fixedSize(horizontal: false, vertical: true)
                                .frame(maxWidth: .infinity, alignment: .leading)
                            if let button = line.button { PaneButton(button, small: true) }
                        }
                        .padding(.vertical, 5)
                    }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
                .soft(radius: 18)
            }
        }
    }
}

/// What came in since the last look, directly above the current item.
/// Closed it is one line; open, every project that has something with its
/// lines under it, and nothing folded inside.
struct HappeningView: View {
    var happening: HappeningPresentation

    @Environment(PaneViewState.self) private var state

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            OpensInPlace(id: "happening") {
                HStack(alignment: .center, spacing: TodayLook.lead) {
                    if let tile = happening.tile { TileView(tile: tile, size: .row) }
                    VStack(alignment: .leading, spacing: 4) {
                        Text(
                            "\(Text(happening.time.map { $0 + " " } ?? "").font(TodayLook.Text.time.weight(.bold)).foregroundStyle(.ink))\(Text(happening.line).font(TodayLook.Text.body).foregroundStyle(happening.quiet ? Color.inkSecond : Color.ink))"
                        )
                        .multilineTextAlignment(.leading)
                        .fixedSize(horizontal: false, vertical: true)
                        if let pill = happening.pill { PillView(pill) }
                    }
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, TodayLook.gap)
            .frame(minHeight: TodayLook.target)
            .soft()
            if state.isOpen("happening") { card }
        }
    }

    private var card: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let settled = happening.settled {
                VStack(alignment: .leading, spacing: 4) {
                    if let label = happening.settledLabel { SectionLabel(label) }
                    HappeningItemView(item: settled)
                }
            }
            ForEach(Array(happening.groups.enumerated()), id: \.element.id) { index, group in
                HappeningGroupView(group: group, first: index == 0 && happening.settled == nil)
            }
            if let gotIt = happening.gotIt { PaneButton(gotIt) }
            if let nothing = happening.nothingNew { NoteText(nothing) }
        }
        .card()
    }
}

/// A project's group in what is happening. Its header is a row that opens
/// the project's page: stripe, tile and name, and no pill. What is on no
/// project has a header that opens nothing: its name alone.
struct HappeningGroupView: View {
    var group: HappeningGroup
    var first = true
    @Environment(\.pane) private var pane

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let route = group.opens {
                Button {
                    pane.open(route)
                } label: {
                    header.contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityValue(group.stripe.spoken)
                .accessibilityHint("Opens its page")
            } else {
                header.accessibilityAddTraits(.isHeader)
            }
            ForEach(group.items) { HappeningItemView(item: $0).padding(.leading, group.stripe.flag == nil ? 0 : 10) }
        }
        .padding(.top, first ? 0 : 6)
        .rowLine(!first)
    }

    private var header: some View {
        HStack(spacing: 8) {
            if let tile = group.tile { TileView(tile: tile, size: .row) }
            Text(group.name).font(TodayLook.Text.rowTitle).foregroundStyle(.ink)
                .multilineTextAlignment(.leading)
            Spacer(minLength: 4)
            if group.opens != nil { RowChevron(turn: .page) }
        }
        .stripe(group.stripe, inset: 4)
        .frame(minHeight: TodayLook.target)
    }
}

/// One thing that came in: its time, what happened, where it came from,
/// and the New pill when it is new. Something marked urgent that the
/// assistant holds back carries the urgent pill and its options.
struct HappeningItemView: View {
    var item: HappeningItem

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: TodayLook.gap) {
                Text(item.time)
                    .font(item.isNew ? TodayLook.Text.time.weight(.bold) : TodayLook.Text.time)
                    .foregroundStyle(item.isNew ? AnyShapeStyle(.ink) : AnyShapeStyle(.inkSecond))
                    .frame(minWidth: 44, alignment: .trailing)
                VStack(alignment: .leading, spacing: 2) {
                    if let pill = item.pill { PillView(pill) }
                    Text(item.text).font(TodayLook.Text.body).foregroundStyle(.ink)
                        .multilineTextAlignment(.leading)
                        .fixedSize(horizontal: false, vertical: true)
                    if let source = item.source {
                        Text(source).font(TodayLook.Text.footnote).foregroundStyle(.inkSecond)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if item.isNew { PillView(.new) }
            }
            .accessibilityElement(children: .combine)
            if let marked = item.marked { MarkedBlockView(block: marked).padding(.leading, 54) }
        }
        .padding(.vertical, 3)
    }
}

/// What waits while a meeting leads: the things in bold, then the rest of
/// the sentence.
struct ParkedLineView: View {
    var line: ParkedLine

    var body: some View {
        Text("\(Text(line.what).fontWeight(.semibold).foregroundStyle(.ink))\(Text(" " + line.rest).foregroundStyle(.inkSecond))")
            .font(TodayLook.Text.body)
            .multilineTextAlignment(.leading)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, 14)
            .padding(.vertical, 11)
            .soft(radius: 18)
    }
}

/// One quiet line of the day, with a way back to what it names where it
/// has one.
struct NoteLineView: View {
    var note: NoteLine

    var body: some View {
        HStack(alignment: .center, spacing: 8) {
            Text(note.text).font(TodayLook.Text.second).foregroundStyle(.inkSecond)
                .multilineTextAlignment(.leading)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            if let button = note.button { PaneButton(button, small: true) }
        }
        .padding(.horizontal, 6)
        .frame(minHeight: note.button == nil ? 0 : TodayLook.target)
    }
}
