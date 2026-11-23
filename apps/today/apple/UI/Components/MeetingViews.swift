import SubstrateUI
import SwiftUI
import TodayCore

/// A meeting. As a line: its time, title and who, one line, its project;
/// only the next fixed point says how long until it, in the hour before.
/// Opened in place, or leading the day, it is a card with what to know
/// before it. The line and the card are two faces on one surface
/// (`PanelSurface`): one fades as the other comes, and the surface grows
/// from the list's to a card's.
///
/// Two meetings that share time are two such lines, tied (`MeetingTie`):
/// nothing is drawn between them. While no choice is made a band marks
/// both and the second asks for one; a tap on either opens the options
/// under them, and they close by themselves once one is chosen. The one
/// the owner does not go to is then struck through, and a tap on it opens
/// the options again.
struct MeetingRowView: View {
    @Environment(\.words) private var words
    var meeting: MeetingRow
    /// Where it stands among the closed meetings around it.
    var joins = Joins()

    @Environment(PaneViewState.self) private var state

    private var openID: String { "meeting:" + meeting.id }
    var isCard: Bool { !meeting.off && (meeting.leads || state.isOpen(openID)) }

    private var tie: MeetingTie? { meeting.tie }
    private var choiceID: String? { tie.map { "overlap:" + $0.id } }
    /// The two are both lines: the choice between them opens under them.
    /// One whose other is drawn in full has the options in that card.
    private var paired: Bool {
        guard let tie else { return false }
        return !tie.otherLeads && !meeting.leads && !state.isOpen("meeting:" + tie.other) && !state.isOpen(openID)
    }
    /// A tap opens the choice, not the meeting: while none is made, and on
    /// the one the owner does not go to.
    private var opensChoice: Bool { paired && (tie?.toChoose == true || meeting.off) }
    private var choosing: Bool { paired && choiceID.map(state.isOpen) == true }

    var body: some View {
        // No stack of their own around the two faces: a ZStack around a tall one lays out what it holds a line short.
        Group {
            if isCard {
                card.padding(TodayLook.inset).transition(.face)
            } else {
                line.transition(.face)
            }
        }
        // The line moves as one thing. Its faces stand inside a transition, and without this the words of
        // a line that is carried down by what opens above it take a shorter way than its glyph, its
        // badges and its surface, and run ahead of their own line for a moment.
        .geometryGroup()
        // Nothing parts two lines that are tied.
        .rowLine(!isCard && joins.above && tie?.second != true)
        .panel(isCard ? .card() : .line(joins))
        .onChange(of: meeting.card.overlap?.options.selected?.id) { _, chosen in
            // One was chosen: it is seen filled, then the two close.
            guard tie?.second == true, chosen != nil, let choiceID, state.isOpen(choiceID) else { return }
            Task { @MainActor in
                try? await Task.sleep(for: .milliseconds(550))
                if state.isOpen(choiceID) { state.toggle(choiceID) }
            }
        }
    }

    private var line: some View {
        VStack(alignment: .leading, spacing: 0) {
            Group {
                if meeting.off, !opensChoice {
                    lineBody
                } else {
                    Button(action: tap) {
                        lineBody.contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityHint(opensChoice ? "Opens the choice between the two" : "Opens in place")
                    // On the button, never on what holds it: with an accessibility value on the line's
                    // container the line no longer moves as one thing (`geometryGroup` below).
                    .accessibilityValue(meeting.attendance ?? "")
                }
            }
            .padding(.leading, 20)
            .padding(.trailing, 10)
            // Two that are tied stand closer to each other than to the lines around them.
            .padding(.top, tie?.second == true ? 5 : TodayLook.gap)
            // The line that asks stands clear of the band's lower edge.
            .padding(.bottom, tie != nil && tie?.second != true ? 5 : TodayLook.gap + (tie?.ask != nil && paired ? 6 : 0))
            .frame(minHeight: tie == nil ? 50 : TodayLook.target, alignment: .top)
            // A meeting that ended is a dimmed row of what is done; it still opens.
            .opacity(meeting.ended ? 0.6 : 1)
            if choosing, tie?.second == true, let overlap = meeting.card.overlap {
                VStack(alignment: .leading, spacing: 8) {
                    OptionListView(overlap.options)
                    if let chosen = overlap.chosen { SavedLine(chosen) }
                    if let ready = overlap.ready { ReadyMessageView(message: ready) }
                }
                .padding(.horizontal, 14)
                .padding(.bottom, 14)
                .transition(.unfold)
            }
        }
        .background { band }
    }

    /// What marks two tied lines while no choice is made: one band behind
    /// both, and behind the options once they are open.
    @ViewBuilder private var band: some View {
        if let tie {
            let (top, bottom): (CGFloat, CGFloat) = tie.second ? (0, 16) : (16, 0)
            UnevenRoundedRectangle(
                topLeadingRadius: top, bottomLeadingRadius: bottom, bottomTrailingRadius: bottom, topTrailingRadius: top, style: .continuous
            )
            .fill(.cautionFill)
            .padding(.horizontal, 6)
            .padding(.top, tie.second ? 0 : 5)
            .padding(.bottom, tie.second ? 5 : 0)
            .opacity(tie.toChoose && paired ? 1 : 0)
        }
    }

    private func tap() {
        if opensChoice, let choiceID {
            state.toggle(choiceID)
        } else {
            // Opened in full, it holds the options itself: the ones under the two close.
            state.toggle(openID, closing: choiceID)
        }
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
                    if !meeting.off, let until = meeting.until {
                        Text(until).font(TodayLook.Text.button).foregroundStyle(.ink).fixedSize()
                    }
                    // Two that wait for a choice open as one thing, with one chevron: at the top right of
                    // their band, where every row's is.
                    if opensChoice ? (tie?.second != true || !(tie?.toChoose ?? false)) : !meeting.off {
                        RowChevron(turn: choosing && opensChoice ? .open : .closed)
                    }
                }
                if let ask = tie?.ask, paired {
                    Text(ask).font(TodayLook.Text.second.weight(.medium)).foregroundStyle(.cautionWords)
                        .multilineTextAlignment(.leading)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if !meeting.off {
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
            MeetingCardView(card: meeting.card, scope: meeting.id)
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

    @Environment(PaneViewState.self) private var state
    @Environment(\.pane) private var pane

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ChipsView(chips: card.chips)
            if let purpose = card.purpose {
                Text(purpose).font(TodayLook.font(.callout)).foregroundStyle(.ink).fixedSize(horizontal: false, vertical: true)
            }
            if let overlap = card.overlap { OverlapCardView(card: overlap) }
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
            if !card.links.isEmpty { SourceLinksView(links: card.links) }
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
