import SubstrateUI
import SwiftUI
import TodayCore

/// The pane: the order of the day, from top to bottom. The strip, then the
/// items in the order they are drawn, with nothing between them: there are
/// no headers. The field is fixed at the bottom by whoever holds the pane.
struct PaneView<Header: View>: View {
    var pane: PanePresentation
    /// Opens Settings, where the system gives the app no menu for it: a
    /// quiet button under the last thing of the day.
    var openSettings: (() -> Void)?
    /// What stands in the way of the link, said above the strip.
    @ViewBuilder var header: () -> Header

    @Environment(PaneViewState.self) private var state
    @Environment(\.words) private var words

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: TodayLook.gap) {
                    header()
                    StripView(strip: pane.strip)
                    if let empty = pane.empty {
                        Text(empty).font(TodayLook.Text.body).foregroundStyle(.inkSecond)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .card()
                            .transition(.arrive)
                    }
                    if !pane.items.isEmpty { PaneItemsView(items: pane.items) }
                    if let openSettings {
                        Button(action: openSettings) {
                            HStack(spacing: 6) {
                                Image(systemName: Symbol.settings.systemName).font(TodayLook.font(.footnote, weight: .semibold))
                                Text(words.settings)
                            }
                        }
                        .buttonStyle(.today(.small))
                        .frame(maxWidth: .infinity, alignment: .center)
                        .padding(.top, 8)
                    }
                }
                .padding(.horizontal, TodayLook.side)
                .padding(.top, 6)
                .padding(.bottom, 16)
            }
            .scrollDismissesKeyboard(.interactively)
            .onChange(of: pane.composer.added) { _, added in
                // The task the field just made: the day scrolls to its row.
                guard let row = added.map({ "row:" + $0 }), pane.items.contains(where: { $0.id == row }) else { return }
                // After this update is laid out: a row drawn in the same
                // update has no place to scroll to yet.
                Task { @MainActor in withAnimation(state.motion) { proxy.scrollTo(row, anchor: .center) } }
            }
        }
    }
}

extension PaneView where Header == EmptyView {
    init(pane: PanePresentation) {
        self.init(pane: pane) { EmptyView() }
    }
}

/// The run of closed lines a thing of the day belongs to: lines of one run
/// that follow each other share a surface and a hairline, as a list does.
enum PaneRun: Equatable {
    case rows
    case meetings
    /// The small lines of what is done.
    case lines

    /// Each thing's place in its run, from what each is: nil for a thing
    /// drawn by itself, which joins nothing.
    static func joins(_ runs: [PaneRun?]) -> [Joins] {
        runs.indices.map { index in
            guard let run = runs[index] else { return Joins() }
            return Joins(above: index > 0 && runs[index - 1] == run, below: index + 1 < runs.count && runs[index + 1] == run)
        }
    }

    /// The run an item is drawn in while it is closed.
    static func of(_ item: PaneItem, open: Set<String>) -> PaneRun? {
        switch item {
        case let .row(row) where !(row.state == .now || open.contains("row:" + row.id)): .rows
        case let .meeting(meeting) where meeting.off || !(meeting.leads || open.contains("meeting:" + meeting.id)): .meetings
        default: nil
        }
    }
}

/// One thing of the day as it is drawn: the item, and where it stands
/// among the closed lines around it. Every item has a place of its own in
/// the day and keeps it, opened or closed, a line or the current item: a
/// list of closed rows is the look of rows that follow each other, not a
/// view that holds them. So a row that opens, closes or becomes the
/// current item is the same thing changing, and is seen to change.
struct PaneSlot: Identifiable {
    var item: PaneItem
    var joins = Joins()

    var id: String { item.id }

    static func slots(of items: [PaneItem], open: Set<String>) -> [PaneSlot] {
        let joins = PaneRun.joins(items.map { PaneRun.of($0, open: open) })
        return zip(items, joins).map { PaneSlot(item: $0, joins: $1) }
    }
}

/// The day's items from top to bottom, one place each, whatever each is
/// drawn as: what is finished leaves, the next takes its place and grows
/// into the current item, and the rest closes up (`Motion`).
struct PaneItemsView: View {
    var items: [PaneItem]

    @Environment(PaneViewState.self) private var state

    var body: some View {
        let slots = PaneSlot.slots(of: items, open: state.opened)
        VStack(alignment: .leading, spacing: 0) {
            ForEach(slots) { slot in
                PaneSlotView(slot: slot)
                    .padding(.top, slot.joins.above || slot.id == slots.first?.id ? 0 : TodayLook.gap)
                    .id(slot.id)
                    .transition(.arrive)
            }
        }
    }
}

struct PaneSlotView: View {
    var slot: PaneSlot

    var body: some View {
        switch slot.item {
        case let .night(night): NightView(night: night)
        case let .earlier(earlier): EarlierView(earlier: earlier)
        case let .happening(happening): HappeningView(happening: happening)
        case let .row(row): RowView(row: row, joins: slot.joins)
        case let .meeting(meeting): MeetingRowView(meeting: meeting, joins: slot.joins)
        case let .parked(line): ParkedLineView(line: line)
        case let .unfinished(card): UnfinishedCardView(card: card)
        case let .note(note): NoteLineView(note: note)
        }
    }
}
