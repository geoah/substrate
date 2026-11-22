import SubstrateUI
import SwiftUI
import TodayCore

/// A meeting. As a line: its time, title and who, whether the owner goes,
/// one line, its project; only the next fixed point says how long until
/// it. Opened in place, or leading the day, it is a card with what to know
/// before it. The line and the card are two faces on one surface
/// (`PanelSurface`): one fades as the other comes, and the surface grows
/// from the list's to a card's.
struct MeetingRowView: View {
    @Environment(\.words) private var words
    var meeting: MeetingRow
    /// Where it stands among the closed meetings around it.
    var joins = Joins()
    /// Inside an overlap's block: drawn on the block's own card.
    var inBlock = false

    @Environment(PaneViewState.self) private var state

    private var openID: String { "meeting:" + meeting.id }
    var isCard: Bool { !meeting.off && (meeting.leads || state.isOpen(openID)) }

    var body: some View {
        // No stack of their own around the two faces: a ZStack around a tall one lays out what it holds a line short.
        let faces = Group {
            if isCard {
                card.padding(inBlock ? 12 : TodayLook.inset).transition(.face)
            } else {
                line.transition(.face)
            }
        }
        .rowLine(!isCard && joins.above)
        // Inside an overlap's block the block is the surface.
        if inBlock {
            faces.frame(maxWidth: .infinity, alignment: .topLeading).clipped()
        } else {
            faces.panel(isCard ? .card() : .line(joins))
        }
    }

    private var line: some View {
        Group {
            if meeting.off {
                lineBody
            } else {
                Button {
                    state.toggle(openID)
                } label: {
                    lineBody.contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityHint("Opens in place")
            }
        }
        .padding(.leading, inBlock ? 10 : 20)
        .padding(.trailing, 10)
        .padding(.vertical, TodayLook.gap)
        .frame(minHeight: 50, alignment: .top)
        // A meeting that ended is a dimmed row of what is done; it still opens.
        .opacity(meeting.ended ? 0.6 : 1)
    }

    private var lineBody: some View {
        HStack(alignment: .top, spacing: 8) {
            GlyphView(glyph: .meeting, size: .row).padding(.top, -3)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .top, spacing: 8) {
                    WrapLayout(spacing: 8) {
                        Text(meeting.start).font(TodayLook.Text.time.weight(.bold))
                            .foregroundStyle(meeting.off ? AnyShapeStyle(.inkSecond) : AnyShapeStyle(.ink))
                        Text(meeting.title)
                            .font(TodayLook.Text.rowTitle)
                            .foregroundStyle(meeting.off ? AnyShapeStyle(.inkSecond) : AnyShapeStyle(.ink))
                            .strikethrough(meeting.off)
                            .multilineTextAlignment(.leading)
                        HStack(spacing: 3) {
                            ForEach(Array(meeting.people.prefix(3).enumerated()), id: \.offset) { _, badge in
                                BadgeView(badge: badge, size: .chip)
                            }
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    if !meeting.off {
                        if let until = meeting.until {
                            Text(until).font(TodayLook.Text.button).foregroundStyle(.ink).fixedSize()
                        }
                        RowChevron(turn: .closed)
                    }
                }
                if !meeting.off {
                    if let attendance = meeting.attendance {
                        Text(attendance).font(TodayLook.Text.second.weight(.semibold)).foregroundStyle(.inkSecond)
                    }
                    if let line = meeting.line {
                        Text(line).font(TodayLook.Text.second).foregroundStyle(.inkSecond)
                            .multilineTextAlignment(.leading)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    if let project = meeting.project { ProjectChipView(chip: project).allowsHitTesting(false) }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private var card: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: TodayLook.lead) {
                GlyphView(glyph: .meeting, size: meeting.leads ? .lead : .row).padding(.top, meeting.leads ? 3 : 0)
                VStack(alignment: .leading, spacing: 2) {
                    ViewThatFits(in: .horizontal) {
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            title
                            Spacer(minLength: 8)
                            until
                        }
                        VStack(alignment: .leading, spacing: 2) {
                            title
                            until
                        }
                    }
                    if let detail = meeting.detail {
                        Text(detail)
                            .font(TodayLook.Text.second)
                            .foregroundStyle(.inkSecond)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if !meeting.leads {
                    IconButton(Symbol.close, label: words.close, small: true) {
                        state.toggle(openID)
                    }
                }
            }
            // Inside an overlap's block the block has said the overlap and holds its options: the meeting does not say them again.
            MeetingCardView(card: meeting.card, scope: meeting.id, showsOverlap: !inBlock)
        }
    }

    private var title: some View {
        Text(meeting.start + " " + meeting.title)
            .font(meeting.leads ? TodayLook.Text.leadTitle : TodayLook.Text.openTitle)
            .foregroundStyle(.ink)
            .multilineTextAlignment(.leading)
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityAddTraits(.isHeader)
    }

    @ViewBuilder private var until: some View {
        if let until = meeting.until {
            Text(until).font(TodayLook.Text.button).foregroundStyle(meeting.live ? AnyShapeStyle(.urgentWords) : AnyShapeStyle(.ink)).fixedSize()
        }
    }
}

/// A meeting, in full: what to know before it.
struct MeetingCardView: View {
    var card: MeetingCard
    /// What keeps this card's ticked points apart from another's.
    var scope: String
    /// False where the overlap this meeting is in is drawn around it.
    var showsOverlap = true

    @Environment(PaneViewState.self) private var state
    @Environment(\.pane) private var pane

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ChipsView(chips: card.chips)
            if let purpose = card.purpose {
                Text(purpose).font(TodayLook.font(.callout)).foregroundStyle(.ink).fixedSize(horizontal: false, vertical: true)
            }
            if showsOverlap, let overlap = card.overlap { OverlapCardView(card: overlap) }
            if !card.tasks.isEmpty {
                section(card.tasksTitle) {
                    RowList {
                        ForEach(Array(card.tasks.enumerated()), id: \.element.id) { index, task in
                            TaskLineView(line: task, first: index == 0)
                        }
                    }
                }
            }
            if let since = card.since {
                section(card.labels.since) {
                    Text(since).font(TodayLook.Text.body).foregroundStyle(.inkSecond).fixedSize(horizontal: false, vertical: true)
                }
            }
            if !card.said.isEmpty {
                section(card.labels.said) {
                    ForEach(card.said) { said in
                        HStack(alignment: .top, spacing: 8) {
                            BadgeView(badge: said.badge, size: .chip)
                            Text(
                                "\(Text(said.name + " ").font(TodayLook.Text.button).foregroundStyle(.ink))\(Text(said.text).font(TodayLook.Text.second).foregroundStyle(.inkSecond))\(Text(said.time.map { " " + $0 } ?? "").font(TodayLook.Text.footnote).foregroundStyle(.inkSecond))"
                            )
                            .fixedSize(horizontal: false, vertical: true)
                        }
                    }
                }
            }
            if !card.raise.isEmpty {
                section(card.labels.raise) {
                    ForEach(Array(card.raise.enumerated()), id: \.offset) { index, point in
                        let id = "raise:\(scope):\(index)"
                        Button {
                            state.tick(id)
                        } label: {
                            HStack(alignment: .top, spacing: TodayLook.gap) {
                                OptionMark(mark: .square, selected: state.ticked.contains(id))
                                Text(point)
                                    .font(TodayLook.Text.body)
                                    .foregroundStyle(state.ticked.contains(id) ? AnyShapeStyle(.inkSecond) : AnyShapeStyle(.ink))
                                    .multilineTextAlignment(.leading)
                                    .fixedSize(horizontal: false, vertical: true)
                                Spacer(minLength: 0)
                            }
                            .frame(minHeight: TodayLook.target, alignment: .center)
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityAddTraits(state.ticked.contains(id) ? .isSelected : [])
                    }
                }
            }
            if !card.readFirst.isEmpty {
                section(card.labels.readFirst) {
                    RowList {
                        ForEach(Array(card.readFirst.enumerated()), id: \.element.id) { index, reading in
                            Button {
                                if let url = reading.url { pane.openURL(url) }
                            } label: {
                                HStack(spacing: TodayLook.gap) {
                                    GlyphView(glyph: .document, size: .chip)
                                    Text(reading.title).font(TodayLook.Text.bodyStrong).foregroundStyle(.ink)
                                        .multilineTextAlignment(.leading)
                                        .frame(maxWidth: .infinity, alignment: .leading)
                                    if let kind = reading.kind {
                                        Text(kind).font(TodayLook.Text.footnote).foregroundStyle(.inkSecond)
                                    }
                                }
                                .padding(.horizontal, 12)
                                .frame(minHeight: TodayLook.target)
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                            .disabled(reading.url == nil)
                            .rowLine(index > 0)
                        }
                    }
                }
            }
            if let join = card.join { PaneButton(join) }
            if let after = card.after {
                section(card.labels.after) {
                    Text(after).font(TodayLook.Text.body).foregroundStyle(.inkSecond).fixedSize(horizontal: false, vertical: true)
                }
            }
            if let moss = card.moss { MossBoxView(box: moss) }
        }
    }

    private func section(_ label: String?, @ViewBuilder content: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            if let label { SectionLabel(label) }
            content()
        }
    }
}

/// Two meetings that overlap, as one block around them with its options.
struct OverlapBlockView: View {
    var block: OverlapBlock
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ViewThatFits(in: .horizontal) {
                HStack(alignment: .firstTextBaseline) {
                    Text(block.card.name).font(TodayLook.Text.rowTitle).foregroundStyle(.ink)
                    Spacer(minLength: 8)
                    PillView(block.card.pill)
                }
                VStack(alignment: .leading, spacing: 4) {
                    Text(block.card.name).font(TodayLook.Text.rowTitle).foregroundStyle(.ink)
                    PillView(block.card.pill)
                }
            }
            Text(block.card.text).font(TodayLook.Text.second).foregroundStyle(.inkSecond)
                .fixedSize(horizontal: false, vertical: true)
            VStack(spacing: 0) {
                ForEach(Array(block.meetings.enumerated()), id: \.element.id) { index, meeting in
                    MeetingRowView(meeting: meeting, joins: Joins(above: index > 0), inBlock: true)
                }
            }
            .background(.card, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
            OptionListView(block.card.options)
            if let chosen = block.card.chosen { SavedLine(chosen) }
            if let ready = block.card.ready { ReadyMessageView(message: ready) }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            let shape = RoundedRectangle(cornerRadius: TodayLook.listRadius, style: .continuous)
            shape.fill(TodayLook.soft(scheme)).overlay(shape.strokeBorder(TodayLook.line(scheme), lineWidth: 1))
        }
    }
}
