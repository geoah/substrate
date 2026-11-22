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
        HStack(alignment: .top, spacing: TodayLook.lead) {
            IconView(icon: icon, size: size == .lead ? .lead : .row)
                .padding(.top, size == .lead ? 3 : 0)
            VStack(alignment: .leading, spacing: size == .lead ? 4 : 1) {
                Text(title)
                    .font(titleFont)
                    .foregroundStyle(struck ? AnyShapeStyle(.inkSecond) : AnyShapeStyle(.ink))
                    .strikethrough(struck)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityAddTraits(size == .line ? [] : .isHeader)
                if let line {
                    Text(line)
                        .font(size == .line ? TodayLook.Text.second : TodayLook.Text.body)
                        .foregroundStyle(.inkSecond)
                        .multilineTextAlignment(.leading)
                        .lineLimit(lineLimit)
                        .fixedSize(horizontal: false, vertical: true)
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
}

extension Stripe {
    /// A flag is drawn in colour alone, so VoiceOver is told its word.
    var spoken: String {
        guard let flag else { return "" }
        return suggested ? "\(flag.word) flag, suggested" : "\(flag.word) flag"
    }
}

/// One row of the day. Closed it is a line of a list; opened in place, or
/// as the current item, it is a card with its body under its head.
struct RowView: View {
    @Environment(\.words) private var words
    var row: Row
    /// Drawn inside a list of lines, which draws the hairline above it.
    var first = true

    @Environment(PaneViewState.self) private var state
    @Environment(\.pane) private var pane

    private var openID: String { "row:" + row.id }

    var isCard: Bool { row.state == .now || state.isOpen(openID) }

    /// Crossed out: finished today, or finished a moment ago and not yet
    /// written (`Finishing`), which a tap on its tick still takes back.
    private var struck: Bool { row.state == .done || state.phase(row.id) == .settled }

    private var lineLimit: Int? {
        if case .task = row.body { 3 } else { nil }
    }

    var body: some View {
        if isCard { card } else { line }
    }

    // MARK: A line

    private var line: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top, spacing: 4) {
                // The chips stand directly under the words, beside the
                // trailing control: under it they would wait for its whole
                // target, a gap on a row with a title and no line.
                VStack(alignment: .leading, spacing: 4) {
                    Button {
                        // A row that opens a page opens it; every other opens in place.
                        if case let .page(route) = row.trailing {
                            pane.open(route)
                        } else {
                            withAnimation(.snappy(duration: 0.2)) { state.toggle(openID) }
                        }
                    } label: {
                        RowHead(
                            icon: .glyph(row.glyph), title: row.title, line: row.line, stripe: row.stripe, struck: struck, lineLimit: lineLimit
                        )
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityHint(opensPage ? "Opens its page" : "Opens in place")
                    .accessibilityValue(row.state == .done ? "Finished" : "")
                    // What is done is one closed line: its chips are in its card.
                    if !row.chips.isEmpty, row.state != .done {
                        ChipsView(chips: row.chips).padding(.leading, TodayLook.leadBox + TodayLook.lead + (row.stripe.flag == nil ? 0 : 10))
                    }
                }
                trailing
            }
        }
        // A row with no flag keeps the stripe's room, so every row's icon
        // stands on one line down the list.
        .padding(.leading, row.stripe.flag == nil ? 20 : 10)
        .padding(.trailing, 8)
        .padding(.vertical, TodayLook.gap)
        .frame(minHeight: 56, alignment: .top)
        .rowLine(!first)
    }

    @ViewBuilder private var trailing: some View {
        switch row.trailing {
        case let .tick(button):
            PaneButton(button, small: true)
        case .page:
            RowChevron(turn: .page).frame(minHeight: 32)
        case .opens:
            RowChevron(turn: .closed).frame(minHeight: 32)
        }
    }

    private var opensPage: Bool {
        if case .page = row.trailing { true } else { false }
    }

    // MARK: A card

    private var card: some View {
        let lead = row.state == .now
        return VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: 4) {
                RowHead(
                    icon: .glyph(row.glyph), title: row.title, line: isUrgent ? nil : row.line, stripe: row.stripe,
                    size: lead ? .lead : .open, struck: struck, lineLimit: lineLimit)
                if !lead {
                    IconButton(Symbol.close, label: words.close, small: true) {
                        withAnimation(.snappy(duration: 0.2)) { state.toggle(openID) }
                    }
                }
            }
            RowBodyView(row: row)
        }
        .card(urgent: isUrgent)
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
                if line.opens != nil { RowChevron(turn: .page).padding(.top, 2) }
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
