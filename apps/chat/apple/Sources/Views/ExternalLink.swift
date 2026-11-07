import ChatKit
import SubstrateKit
import SwiftUI

/// The mark beside every link that leaves the app, the console's
/// external-link mark: a link in a reply, a page a tool found, a source a
/// reply cites. It says so to a screen reader too, so a reader always knows
/// before they tap.
enum ExternalMark {
    static let symbol = "arrow.up.right.square"
    static let words = "opens in your browser"

    /// The mark as text, to follow a link inside a line.
    static var text: Text {
        Text(Image(systemName: symbol)).foregroundStyle(.tint).accessibilityLabel(words)
    }
}

/// Pages as pills: a search's results, the page a call read, the pages a
/// reply cites.
struct SourceListView: View {
    let list: SourceList

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let heading = list.heading {
                Text(heading)
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.secondary)
            }
            if let empty = list.empty {
                Text(empty)
                    .font(.footnote)
                    .foregroundStyle(.tertiary)
            } else {
                WrapLayout(spacing: 6) {
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
    let pill: SourcePill

    var body: some View {
        if let url = URL(string: pill.url) {
            Link(destination: url) { label }
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

    private var label: some View {
        HStack(spacing: 5) {
            if let index = pill.index {
                Text(index, format: .number)
                    .monospacedDigit()
                    .fontWeight(.medium)
                    .foregroundStyle(.tertiary)
            } else {
                Image(systemName: "globe").foregroundStyle(.tertiary)
            }
            Text(pill.host)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .layoutPriority(1)
            if let title = pill.title {
                Text(title)
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                    .truncationMode(.tail)
            }
            Image(systemName: ExternalMark.symbol)
                .imageScale(.small)
                .foregroundStyle(.tertiary)
        }
        .font(.caption)
        .padding(.horizontal, 9)
        .padding(.vertical, 4)
        // The wrap lays a pill out at its ideal width: a long title truncates
        // inside a phone's column instead of running past it.
        .frame(maxWidth: 280, alignment: .leading)
        .background(.fill.tertiary, in: .capsule)
        .contentShape(.capsule)
    }

    private var preview: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(pill.title ?? pill.host).font(.headline)
            Text(pill.url).font(.caption).foregroundStyle(.secondary).lineLimit(2)
            if let snippet = pill.snippet {
                Text(snippet).font(.subheadline)
            }
        }
        .padding(16)
        .frame(width: 320, alignment: .leading)
    }

    private var spoken: String {
        [pill.index.map(String.init), pill.host, pill.title, ExternalMark.words].compactMap { $0 }.joined(separator: ", ")
    }
}
