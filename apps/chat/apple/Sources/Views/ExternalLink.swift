import ChatKit
import SubstrateKit
import SwiftUI

/// The mark beside every link that leaves the app, the console's
/// external-link mark: a link in a reply, a page a tool found, a source a
/// reply cites, a link out to the console. It says so to a screen reader
/// too, so a reader always knows before they tap.
enum ExternalMark {
    static let symbol = "arrow.up.right.square"
    static let words = "opens in your browser"

    /// The mark as text, to follow a link inside a line. With its filler
    /// it is as long as its words, which is what keeps the links after it
    /// on the line read as their own words on a Mac (`SpokenPiece`).
    static var text: Text {
        // An image in a text is one unit.
        let spoken = SpokenPiece.fill(1, for: words)
        return Text("\(Text(Image(systemName: symbol)))\(Text(verbatim: spoken.filler))").ink(.accent, on: .page, .card, .table).accessibilityLabel(
            spoken.label)
    }
}

/// A link out to the console, for what the app may not do itself: its
/// words beside the external mark, and the mark's words as its hint.
struct ConsoleLink: View {
    let title: String
    let destination: URL

    var body: some View {
        Link(destination: destination) {
            Label(title, systemImage: ExternalMark.symbol)
        }
        .accessibilityHint(ExternalMark.words)
    }
}

/// Pages as pills: a search's results, the page a call read, the pages a
/// reply cites.
struct SourceListView: View {
    let list: SourceList

    var body: some View {
        VStack(alignment: .leading, spacing: ChatSpace.lines) {
            if let heading = list.heading {
                Text(heading)
                    .font(ChatType.label)
                    .ink(.quiet, on: .page)
            }
            if let empty = list.empty {
                Text(empty)
                    .font(ChatType.note)
                    .ink(.quiet, on: .page)
            } else {
                WrapLayout(spacing: ChatSpace.words) {
                    ForEach(list.pills) { SourcePillView(pill: $0) }
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(list.label)
    }
}

/// One web page as a pill: its number or a globe, the site, the page's
/// title and the external mark. A tap opens it in the browser; what a
/// search said of it is its help on a Mac and its preview on a long press.
struct SourcePillView: View {
    @Environment(\.openURL) private var openURL
    @Environment(\.dynamicTypeSize) private var size
    let pill: SourcePill

    var body: some View {
        if let url = URL(string: pill.url) {
            Link(destination: url) { label }
                // The pill's own inks, not a link's tint over them: its
                // title is the label colour, as a record's pill's is.
                .buttonStyle(.plain)
                .help(pill.snippet.map { "\(pill.title ?? pill.host)\n\n\($0)" } ?? pill.url)
                .contextMenu {
                    Button("Open", systemImage: ExternalMark.symbol) { openURL(url) }
                    Button("Copy Link", systemImage: "link") { copyToPasteboard(pill.url) }
                    ShareLink(item: url)
                } preview: {
                    preview
                }
                .accessibilityLabel(spoken)
                .accessibilityHint(pill.snippet ?? "")
        }
    }

    /// On one line; at the accessibility text sizes the title stands under
    /// the site, on up to two lines, since one line there holds neither.
    @ViewBuilder private var label: some View {
        if size.isAccessibilitySize, let title = pill.title {
            VStack(alignment: .leading, spacing: ChatSpace.pair) {
                HStack(spacing: RecordPillStyle.spacing) {
                    site
                    mark
                }
                Text(title).ink(.label, on: .pill).lineLimit(2)
            }
            .pill(in: .rect(cornerRadius: ChatRadius.card))
        } else {
            HStack(spacing: RecordPillStyle.spacing) {
                site
                if let title = pill.title {
                    Text(title)
                        .ink(.label, on: .pill)
                        .lineLimit(1)
                        .truncationMode(.tail)
                }
                mark
            }
            // The wrap lays a pill out at its ideal width: a long title
            // truncates inside a phone's column instead of running past it.
            .frame(maxWidth: ChatSize.pillMax, alignment: .leading)
            .pill(in: .capsule)
        }
    }

    /// The page's number among the sources, or a globe, then its site.
    @ViewBuilder private var site: some View {
        if let index = pill.index {
            Text(index, format: .number)
                .monospacedDigit()
                .fontWeight(ChatType.strong)
                .ink(.quiet, on: .pill)
        } else {
            Image(systemName: "globe").ink(.faint, on: .pill)
        }
        Text(pill.host)
            .ink(.quiet, on: .pill)
            .lineLimit(1)
            .layoutPriority(1)
    }

    private var mark: some View {
        Image(systemName: ExternalMark.symbol)
            .imageScale(.small)
            .ink(.faint, on: .pill)
    }

    private var preview: some View {
        VStack(alignment: .leading, spacing: ChatSpace.lines) {
            Text(pill.title ?? pill.host).font(ChatType.heading(level: 3))
            Text(pill.url).font(ChatType.caption).ink(.quiet, on: .page).lineLimit(2)
            if let snippet = pill.snippet {
                Text(snippet).font(ChatType.detail)
            }
        }
        .padding(ChatSpace.edge)
        .frame(width: ChatSize.preview, alignment: .leading)
    }

    private var spoken: String {
        [pill.index.map(String.init), pill.host, pill.title, ExternalMark.words].compactMap { $0 }.joined(separator: ", ")
    }
}

extension View {
    /// A pill's inset, type, fill and room to touch, in `shape`.
    fileprivate func pill(in shape: some Shape) -> some View {
        font(ChatType.note)
            .padding(.horizontal, ChatSpace.pillSide)
            .padding(.vertical, ChatSpace.pillEdge)
            .background(ChatColours.pillStyle, in: shape)
            // A pill is lower than a finger: it is touched in a whole target.
            .touchTarget()
    }
}
