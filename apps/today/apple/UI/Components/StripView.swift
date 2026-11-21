import SubstrateUI
import SwiftUI
import TodayCore

/// The strip on top of the day: the owner's standing priorities, the
/// projects flagged, in flag order, in two columns. It changes only when
/// the owner changes it; what the assistant suggests is said on the entry
/// and takes effect only when chosen.
struct StripView: View {
    var strip: StripPresentation

    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            ViewThatFits(in: .horizontal) {
                header(date: strip.date, stacked: false)
                header(date: strip.dateShort, stacked: false)
                header(date: strip.date, stacked: true)
            }
            if let empty = strip.empty {
                NoteText(empty).padding(.horizontal, 8).padding(.bottom, 8)
            } else {
                LazyVGrid(columns: columns, alignment: .leading, spacing: 0) {
                    ForEach(strip.entries) { StripEntryView(entry: $0) }
                }
            }
        }
        .padding(.horizontal, 6)
        .padding(.top, 4)
        .padding(.bottom, 6)
        .glass(in: RoundedRectangle(cornerRadius: TodayLook.listRadius, style: .continuous))
    }

    /// Two columns on a phone; one when the text is set very large, so a
    /// project's name still has room.
    private var columns: [GridItem] {
        let column = GridItem(.flexible(), spacing: 2, alignment: .topLeading)
        return typeSize.isAccessibilitySize ? [column] : [column, column]
    }

    /// The title and the date, then the strip's two buttons. Where the
    /// date does not fit beside the title it goes under it.
    private func header(date: String, stacked: Bool) -> some View {
        HStack(alignment: .center, spacing: 6) {
            let title = Text(strip.title).font(TodayLook.font(.headline, weight: .bold)).foregroundStyle(.ink).lineLimit(1)
                .accessibilityAddTraits(.isHeader)
            let day = Text(date).font(TodayLook.font(.footnote, weight: .medium)).foregroundStyle(.inkSecond).lineLimit(1)
            Group {
                if stacked {
                    VStack(alignment: .leading, spacing: 0) {
                        title
                        day
                    }
                } else {
                    HStack(alignment: .firstTextBaseline, spacing: 7) {
                        title
                        day
                    }
                }
            }
            .padding(.leading, 8)
            .fixedSize()
            Spacer(minLength: 2)
            PaneButton(strip.allProjects, small: true).fixedSize()
            PaneButton(strip.edit, small: true)
        }
        .frame(minHeight: TodayLook.target)
    }
}

/// One priority: its stripe, its tile, its name, and under the name how
/// today stands on it, or what the assistant suggests in its hue. It opens
/// the project's page.
struct StripEntryView: View {
    var entry: StripEntry
    @Environment(\.pane) private var pane

    var body: some View {
        Button {
            pane.open(entry.opens)
        } label: {
            HStack(alignment: .center, spacing: 7) {
                TileView(tile: entry.tile, size: .row)
                VStack(alignment: .leading, spacing: 1) {
                    Text(entry.name)
                        .font(TodayLook.Text.stripEntry)
                        .foregroundStyle(.ink)
                        .lineLimit(2)
                        .multilineTextAlignment(.leading)
                    if let suggestion = entry.suggestion {
                        HStack(spacing: 2) {
                            if !entry.tile.suggested {
                                Image(systemName: Glyph.moss.systemName).font(.system(size: 9, weight: .bold))
                            }
                            Text(suggestion).font(TodayLook.Text.stripSecond).lineLimit(1)
                        }
                        .foregroundStyle(.mossWords)
                    } else if let line = entry.line {
                        Text(line).font(TodayLook.Text.stripSecond).foregroundStyle(.inkSecond).lineLimit(1)
                    }
                }
                Spacer(minLength: 0)
            }
            .padding(.leading, 6)
            .padding(.vertical, 3)
            .stripe(entry.stripe, inset: 6, room: 8)
            .padding(.leading, 2)
            .frame(maxWidth: .infinity, minHeight: TodayLook.target, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityHint("Opens it")
    }
}
