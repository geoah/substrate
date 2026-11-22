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
        let blocks = PaneBlock.blocks(of: pane.items, open: state.opened)
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: TodayLook.gap) {
                    header()
                    StripView(strip: pane.strip)
                    if let empty = pane.empty {
                        Text(empty).font(TodayLook.Text.body).foregroundStyle(.inkSecond)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .card()
                    }
                    ForEach(blocks) { block in
                        PaneBlockView(block: block).id(block.id)
                    }
                    // What is finished moves to what is done, and the rest closes up.
                    .animation(.snappy(duration: 0.3), value: pane.items.map(\.id))
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
                // The task the field just made: the day scrolls to its row,
                // which in a list of closed rows has a name of its own.
                guard let added, let block = blocks.first(where: { $0.holds("row:" + added) }) else { return }
                let row = if case .rows = block.kind { PaneBlock.within(added) } else { block.id }
                // After this update is laid out: a row drawn in the same
                // update has no place to scroll to yet.
                Task { @MainActor in withAnimation { proxy.scrollTo(row, anchor: .center) } }
            }
        }
    }
}

extension PaneView where Header == EmptyView {
    init(pane: PanePresentation) {
        self.init(pane: pane) { EmptyView() }
    }
}

/// The items as they are drawn: a card for each thing in full, and one
/// list for each run of lines of a kind, so closed rows share a surface and
/// a hairline as a list does.
struct PaneBlock: Identifiable {
    enum Kind {
        case item(PaneItem)
        case rows([Row])
        case meetings([MeetingRow])
    }

    var id: String
    var kind: Kind

    /// What a row inside a list of closed rows is called for scrolling: the
    /// list itself carries its first row's name.
    static func within(_ row: String) -> String { "within:" + row }

    func holds(_ item: String) -> Bool {
        switch kind {
        case let .item(one): one.id == item
        case let .rows(rows): rows.contains { "row:" + $0.id == item }
        case let .meetings(meetings): meetings.contains { "meeting:" + $0.id == item }
        }
    }

    static func blocks(of items: [PaneItem], open: Set<String>) -> [PaneBlock] {
        var out: [PaneBlock] = []
        for item in items {
            switch item {
            case let .row(row) where !(row.state == .now || open.contains("row:" + row.id)):
                if case let .rows(rows)? = out.last?.kind {
                    out[out.count - 1].kind = .rows(rows + [row])
                } else {
                    out.append(PaneBlock(id: item.id, kind: .rows([row])))
                }
            case let .meeting(meeting) where meeting.off || !(meeting.leads || open.contains("meeting:" + meeting.id)):
                if case let .meetings(meetings)? = out.last?.kind {
                    out[out.count - 1].kind = .meetings(meetings + [meeting])
                } else {
                    out.append(PaneBlock(id: item.id, kind: .meetings([meeting])))
                }
            default:
                out.append(PaneBlock(id: item.id, kind: .item(item)))
            }
        }
        return out
    }
}

struct PaneBlockView: View {
    var block: PaneBlock

    var body: some View {
        switch block.kind {
        case let .rows(rows):
            VStack(spacing: 0) {
                ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                    RowView(row: row, first: index == 0).id(PaneBlock.within(row.id))
                }
            }
            .soft()
        case let .meetings(meetings):
            VStack(spacing: 0) {
                ForEach(Array(meetings.enumerated()), id: \.element.id) { index, meeting in
                    MeetingRowView(meeting: meeting, first: index == 0)
                }
            }
            .soft()
        case let .item(item):
            switch item {
            case let .night(night): NightView(night: night)
            case let .earlier(earlier): EarlierView(earlier: earlier)
            case let .happening(happening): HappeningView(happening: happening)
            case let .row(row): RowView(row: row)
            case let .meeting(meeting): MeetingRowView(meeting: meeting)
            case let .overlap(block): OverlapBlockView(block: block)
            case let .parked(line): ParkedLineView(line: line)
            case let .unfinished(card): UnfinishedCardView(card: card)
            case let .note(note): NoteLineView(note: note)
            }
        }
    }
}
