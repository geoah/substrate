import SubstrateUI
import SwiftUI
import TodayCore

/// The head of a thing in the day: its icon, its title, one line. On the
/// current item it is larger; the flag's stripe runs down its leading
/// edge.
struct RowHead: View {
    enum Size {
        /// A line of a list.
        case line
        /// A row opened in place.
        case open
        /// The current item.
        case lead
    }

    var icon: Icon
    var title: String
    var line: String?
    var stripe: Stripe = .none
    var size: Size = .line
    var struck = false
    /// A task's line may be the opening of its description: three lines at
    /// most, whatever the text size. Nil draws every line.
    var lineLimit: Int?

    var body: some View {
        // The icon stands at the middle of the title's first line, on its baseline, whatever the title's size.
        HStack(alignment: .firstTextBaseline, spacing: TodayLook.lead) {
            FirstLineMark(width: iconBox, font: titleFont) {
                IconView(icon: icon, size: size == .lead ? .lead : .row)
            }
            VStack(alignment: .leading, spacing: size == .lead ? 4 : TodayLook.DayRow.textGap) {
                Text(title)
                    .font(titleFont)
                    .foregroundStyle(struck ? AnyShapeStyle(.inkSecond) : AnyShapeStyle(.ink))
                    .strikethrough(struck)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
                    // The same words at another size are the same words growing, not one title over another.
                    .contentTransition(.interpolate)
                    .accessibilityAddTraits(size == .line ? [] : .isHeader)
                if let line {
                    Text(line)
                        .font(size == .line ? TodayLook.Text.second : TodayLook.Text.body)
                        .foregroundStyle(.inkSecond)
                        .multilineTextAlignment(.leading)
                        .lineLimit(lineLimit)
                        .fixedSize(horizontal: false, vertical: true)
                        // An open row is narrower: its line breaks elsewhere, at once, and no word is seen crossing another.
                        .contentTransition(.identity)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .stripe(stripe, inset: size == .lead ? 2 : 6, room: size == .lead ? 12 : 10)
        .padding(.leading, stripe.flag != nil && size == .lead ? -12 : 0)
        .accessibilityElement(children: .combine)
        .accessibilityValue(stripe.spoken)
    }

    private var titleFont: Font {
        switch size {
        case .line: TodayLook.Text.rowTitle
        case .open: TodayLook.Text.openTitle
        case .lead: TodayLook.Text.leadTitle
        }
    }

    /// The width the icon is given: a kind's glyph is larger on the current item.
    private var iconBox: CGFloat {
        if case .glyph = icon, size == .lead { return MarkSize.lead.glyphBox }
        return TodayLook.leadBox
    }
}

extension Stripe {
    /// A flag is drawn in colour alone, so VoiceOver is told its word.
    var spoken: String {
        guard let flag else { return "" }
        return suggested ? "\(flag.word) flag, suggested" : "\(flag.word) flag"
    }
}

/// One row of the day. Closed it is a line of a list; opened in place, or
/// as the current item, it is a card with its body under its head. It is
/// one view in both: the head stays where it is and grows, the body comes
/// in under it, and the surface changes from the list's to a card's
/// (`PanelSurface`), so a row is seen to open, to close, and to become the
/// current item.
struct RowView: View {
    @Environment(\.words) private var words
    var row: Row
    /// Where it stands among the closed rows around it.
    var joins = Joins()

    @Environment(PaneViewState.self) private var state
    @Environment(\.pane) private var pane

    private var openID: String { "row:" + row.id }

    var isCard: Bool { row.state == .now || state.isOpen(openID) }

    private var lead: Bool { row.state == .now }

    /// Crossed out: finished today, or finished a moment ago and not yet
    /// written (`Finishing`), which a tap on its tick still takes back.
    private var struck: Bool { row.state == .done || state.phase(row.id) == .settled }

    private var lineLimit: Int? {
        if case .task = row.body { 3 } else { nil }
    }

    /// A row with no flag keeps the stripe's room while it is a line, so
    /// every row's icon stands on one line down the list.
    private var insets: EdgeInsets {
        if isCard { return EdgeInsets(top: TodayLook.inset, leading: TodayLook.inset, bottom: TodayLook.inset, trailing: TodayLook.inset) }
        return EdgeInsets(
            top: TodayLook.gap, leading: row.stripe.flag == nil ? TodayLook.DayRow.leading : 10, bottom: TodayLook.gap,
            trailing: TodayLook.DayRow.trailing)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: TodayLook.DayRow.controlGap) {
                // The chips stand directly under the words, beside the
                // trailing control: under it they would wait for its whole
                // target, a gap on a row with a title and no line.
                VStack(alignment: .leading, spacing: 4) {
                    head
                    // What is done is one closed line, and an open row says its chips in its card.
                    if !isCard, !row.chips.isEmpty, row.state != .done {
                        ChipsView(chips: row.chips).padding(.leading, TodayLook.leadBox + TodayLook.lead + (row.stripe.flag == nil ? 0 : 10))
                            .transition(.face)
                    }
                }
                trailing
            }
            if isCard { RowBodyView(row: row).transition(.unfold) }
        }
        .padding(insets)
        // A closed line shorter than the least height stands in the middle of it.
        .frame(minHeight: isCard ? nil : TodayLook.DayRow.minimumHeight)
        .rowLine(!isCard && joins.above)
        .panel(isCard ? .card(urgent: isUrgent) : .line(joins))
    }

    // MARK: Its head

    /// The same head closed and open. Closed it is a button over the whole
    /// line, which opens the row; open it is the card's title and does
    /// nothing.
    private var head: some View {
        Button {
            guard !isCard else { return }
            // A row that opens a page opens it; every other opens in place.
            if case let .page(route) = row.trailing { pane.open(route) } else { state.toggle(openID) }
        } label: {
            HStack(alignment: .top, spacing: TodayLook.DayRow.controlGap) {
                // Closed, a row with no line of its own says the opening of what it holds.
                RowHead(
                    icon: .glyph(row.glyph), title: row.title, line: isCard ? (isUrgent ? nil : row.line) : row.line ?? row.opening,
                    stripe: row.stripe, size: isCard ? (lead ? .lead : .open) : .line, struck: struck, lineLimit: lineLimit
                )
                .frame(maxWidth: .infinity, alignment: .leading)
                // The chevron is part of what it opens: a tap on it opens the row as a tap on its words does. It
                // stands at the middle of the title's first line, and ends where a tick's box does on another row.
                if !isCard, let turn = chevronTurn {
                    FirstLineMark(width: TodayLook.DayRow.chevron) { RowChevron(turn: turn) }
                        .padding(.trailing, TodayLook.DayRow.chevronInset)
                        .transition(.face)
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .allowsHitTesting(!isCard)
        .accessibilityRemoveTraits(isCard ? .isButton : [])
        .accessibilityHint(isCard ? "" : (opensPage ? "Opens its page" : "Opens in place"))
        .accessibilityValue(row.state == .done ? "Finished" : "")
    }

    /// What stands beside the head: the way to close an open row, and a
    /// closed row's tick. The tick's box stands at the middle of the
    /// title's first line, as a chevron and a meeting's marks do; its
    /// target is a whole 44 points around it, into the row's own room
    /// above and below.
    @ViewBuilder private var trailing: some View {
        if isCard {
            if !lead {
                IconButton(Symbol.close, label: words.close, small: true) { state.toggle(openID) }.transition(.face)
            }
        } else if case let .tick(button) = row.trailing {
            Text(verbatim: "H")
                .font(TodayLook.Text.rowTitle)
                .hidden()
                .frame(width: TodayLook.target)
                .overlay { PaneButton(button, small: true) }
                .transition(.face)
        }
    }

    /// The chevron a closed row ends with: right where it opens a page,
    /// down where it opens in place. A row with a tick has none.
    private var chevronTurn: RowChevron.Turn? {
        switch row.trailing {
        case .tick: nil
        case .page: .page
        case .opens: .closed
        }
    }

    private var opensPage: Bool {
        if case .page = row.trailing { true } else { false }
    }

    private var isUrgent: Bool {
        if case .urgent = row.body { true } else { false }
    }
}

/// What an open row holds under its head, by what the row is.
struct RowBodyView: View {
    var row: Row

    var body: some View {
        switch row.body {
        case let .routine(card): RoutineCardView(card: card, scope: row.id)
        case let .yesterday(card): YesterdayCardView(card: card)
        case let .suggested(card): PageRowCardView(card: card)
        case let .heard(item): SuggestedItemView(item: item, first: true, headed: false)
        case let .plan(card): PageRowCardView(card: card)
        case let .overlap(card): OverlapCardView(card: card, titled: false)
        case let .urgent(card): UrgentCardView(card: card, line: row.line)
        case let .task(card): TaskCardView(card: card, chips: row.chips, scope: row.id)
        case let .wrap(card): WrapCardView(card: card)
        }
    }
}

/// A task as one line of a list on a page: it opens the task's page, so it
/// ends with a right chevron.
struct TaskLineView: View {
    var line: TaskLine
    var first = true
    /// On a tinted box, where a pill's pale fill would not show.
    var onTint = false
    @Environment(\.pane) private var pane

    var body: some View {
        Button {
            if let route = line.opens { pane.open(route) }
        } label: {
            HStack(alignment: .top, spacing: 8) {
                VStack(alignment: .leading, spacing: 4) {
                    RowHead(icon: .glyph(line.glyph), title: line.title, line: line.line, stripe: line.stripe)
                    if let pill = line.pill {
                        PillView(pill, onTint: onTint).padding(.leading, TodayLook.leadBox + TodayLook.lead + (line.stripe.flag == nil ? 0 : 10))
                    }
                }
                if line.opens != nil { FirstLineMark(width: 20) { RowChevron(turn: .page) } }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, TodayLook.gap)
            .frame(minHeight: TodayLook.target, alignment: .top)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(line.opens == nil)
        .rowLine(!first)
        .accessibilityHint(line.opens == nil ? "" : "Opens its page")
    }
}

/// A list of rows on a card or a page: a quiet bed with a hairline between
/// its rows.
struct RowList<Content: View>: View {
    @ViewBuilder var content: () -> Content
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(spacing: 0, content: content)
            .background(
                TodayLook.ink.color.opacity(scheme == .dark ? 0.10 : 0.045),
                in: RoundedRectangle(cornerRadius: 18, style: .continuous))
    }
}
