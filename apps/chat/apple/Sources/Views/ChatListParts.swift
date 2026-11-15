import ChatKit
import SubstrateKit
import SwiftUI

/// The sizes the list's own parts are drawn at, named here until the chat's
/// sizes move into the design tokens.
enum ChatListSizes {
    /// A row's lines apart, and a placeholder's bars.
    static let lineSpacing: CGFloat = 4
    static let placeholderTitle: CGFloat = 15
    static let placeholderLine: CGFloat = 12
    static let placeholderRadius: CGFloat = 4
    /// The Undo bar: its padding, its corner and how far it floats off the
    /// list's edges.
    static let undoPadding: CGFloat = 14
    static let undoRadius: CGFloat = 16
    static let undoInset: CGFloat = 12
    /// A search hit's indent under its chat.
    static let hitSpacing: CGFloat = 6
}

/// A section's heading: "Pinned" with its pin, a day as it is.
struct SectionTitle: View {
    let title: String

    var body: some View {
        if title == "Pinned" {
            Label(title, systemImage: "pin.fill").foregroundStyle(.quietText)
        } else {
            Text(title).foregroundStyle(.quietText)
        }
    }
}

/// A chat at work: a small spinner where its mark would stand. On the
/// selection it is drawn in the system's primary style, as the row's words
/// are: the list's tint is the selection's own fill.
struct RunningMark: View {
    var selected = false

    var body: some View {
        ProgressView()
            .controlSize(.small)
            .tint(selected ? Color.primary : nil)
            .accessibilityLabel("Working")
    }
}

/// Pin and Unpin, for a row's swipe and its menu.
struct PinButton: View {
    @Environment(ChatHost.self) private var host
    let row: ThreadRow
    /// A swipe's button says one word.
    var short = false

    var body: some View {
        Button(row.pinned ? "Unpin" : "Pin", systemImage: row.pinned ? "pin.slash" : "pin") {
            host.send(.pin(thread: row.id, pinned: !row.pinned))
        }
        .tint(.orange)
    }
}

/// Archive and Unarchive, for a row's swipe and its menu. Archive asks
/// nothing: the list offers Undo for a few seconds.
struct ArchiveButton: View {
    @Environment(ChatHost.self) private var host
    let row: ThreadRow
    var short = false

    var body: some View {
        Button(row.archived ? "Unarchive" : "Archive", systemImage: row.archived ? "tray.and.arrow.up" : "archivebox") {
            host.send(.archive(thread: row.id, archived: !row.archived))
        }
        .tint(.accentFill)
    }
}

/// The chat archived a moment ago, with Undo, floating over the list's end.
struct UndoBar: View {
    @Environment(ChatHost.self) private var host
    let undo: UndoPresentation

    var body: some View {
        HStack(spacing: ChatListSizes.undoPadding) {
            Label(undo.words, systemImage: "archivebox")
                .font(.subheadline)
                .lineLimit(1)
                .truncationMode(.middle)
            Spacer(minLength: 0)
            Button(undo.action) { host.send(.undoArchive) }
                .font(.subheadline.weight(.semibold))
                .tint(ChatColours.accent.color)
        }
        .padding(ChatListSizes.undoPadding)
        .glassEffect(.regular, in: .rect(cornerRadius: ChatListSizes.undoRadius))
        .padding(.horizontal, ChatListSizes.undoInset)
        .padding(.bottom, ChatListSizes.undoInset)
        .transition(.move(edge: .bottom).combined(with: .opacity))
        .accessibilityElement(children: .contain)
    }
}

/// Rows that stand in for the chats while the first read is out: the shape
/// of a row, with nothing to read in it.
struct PlaceholderRows: View {
    let count: Int

    var body: some View {
        ForEach(0 ..< count, id: \.self) { index in
            VStack(alignment: .leading, spacing: ChatListSizes.lineSpacing * 2) {
                bar(width: index % 3 == 0 ? 0.55 : index % 3 == 1 ? 0.7 : 0.45, height: ChatListSizes.placeholderTitle)
                bar(width: 0.9, height: ChatListSizes.placeholderLine)
                bar(width: index % 2 == 0 ? 0.6 : 0.75, height: ChatListSizes.placeholderLine)
            }
            .padding(.vertical, ChatListSizes.lineSpacing)
            .shimmer()
            .accessibilityHidden(index > 0)
            .accessibilityLabel("Loading chats")
        }
        .selectionDisabled()
    }

    private func bar(width: CGFloat, height: CGFloat) -> some View {
        RoundedRectangle(cornerRadius: ChatListSizes.placeholderRadius)
            .fill(.fill.secondary)
            .frame(height: height)
            .containerRelativeFrame(.horizontal) { length, _ in length * width }
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// The row at a list's end while the server's list goes on: it reads the
/// next page when it comes on screen, and again when pressed.
struct MoreRow: View {
    let loading: Bool
    let title: String
    let more: () -> Void

    var body: some View {
        Button(action: more) {
            HStack(spacing: ChatListSizes.hitSpacing) {
                if loading { ProgressView().controlSize(.small) }
                Text(loading ? "Loading…" : title)
                    .font(.subheadline)
                    .foregroundStyle(.quietText)
            }
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.plain)
        .listRowSeparator(.hidden)
        .selectionDisabled()
        .onAppear(perform: more)
    }
}

/// Under the archived chats: the row that reads more of them.
struct ShelfFooter: View {
    @Environment(ChatHost.self) private var host
    let shelf: ArchivedList

    var body: some View {
        if shelf.loading {
            PlaceholderRows(count: 3)
        } else if shelf.more {
            MoreRow(loading: shelf.loadingMore, title: "Earlier chats") { host.send(.more(archived: true)) }
        }
    }
}

/// The server is being asked for the messages that match.
struct SearchingRow: View {
    var body: some View {
        HStack(spacing: ChatListSizes.hitSpacing) {
            ProgressView().controlSize(.small)
            Text("Searching messages…")
                .font(.subheadline)
                .foregroundStyle(.quietText)
        }
        .listRowSeparator(.hidden)
        .selectionDisabled()
    }
}

/// The messages a search found, by chat: each chat's title and who it is
/// with, then its messages with the words that matched. A message opens
/// its chat at itself.
struct SearchGroups: View {
    @Environment(ChatHost.self) private var host
    let groups: [SearchGroup]
    let scene: String

    var body: some View {
        Section {
            ForEach(groups) { group in
                VStack(alignment: .leading, spacing: ChatListSizes.hitSpacing) {
                    HStack(alignment: .firstTextBaseline, spacing: ChatListSizes.hitSpacing) {
                        Text(group.title)
                            .font(.body.weight(.medium))
                            .lineLimit(1)
                        if let archived = group.archived {
                            Text(archived)
                                .font(.caption2.weight(.semibold))
                                .foregroundStyle(.quietText)
                                .padding(.horizontal, ChatListSizes.hitSpacing)
                                .padding(.vertical, 2)
                                .background(.fill.tertiary, in: .capsule)
                        }
                    }
                    ForEach(group.hits) { hit in
                        Button {
                            host.send(.open(thread: group.id, message: hit.id, scene: scene))
                        } label: {
                            SearchHitText(hit: hit, agent: group.agent)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                    }
                    if let more = group.more {
                        Text(more).font(.caption).foregroundStyle(.quietText)
                    }
                }
                .padding(.vertical, 2)
                .selectionDisabled()
                .accessibilityElement(children: .contain)
            }
        } header: {
            Text("Messages").foregroundStyle(.quietText)
        }
    }
}

/// One message a search found: who said it, then its words, the ones that
/// matched in the accent and heavier.
struct SearchHitText: View {
    let hit: SearchHit
    let agent: String?

    var body: some View {
        text
            .font(.subheadline)
            .lineLimit(3)
            .accessibilityLabel("\(hit.who): \(hit.runs.map(\.text).joined())")
            .accessibilityAddTraits(.isButton)
    }

    private var text: Text {
        var out = Text("\(hit.who)  ").fontWeight(.semibold).foregroundStyle(
            hit.who == "You" ? AnyShapeStyle(.primary) : AnyShapeStyle(AgentColours(agent).ink))
        for run in hit.runs {
            let piece =
                run.match
                ? Text(run.text).fontWeight(.semibold).foregroundStyle(ChatColours.accent.color)
                : Text(run.text).foregroundStyle(.quietText)
            out = Text("\(out)\(piece)")
        }
        return out
    }
}

/// The row at a transcript's top while its thread holds rows before the
/// ones drawn: it reads them when it comes on screen, when pressed, and
/// again when rows arrived and it still stands on screen (the last of them
/// may be fewer than a screen). Never again for a read that brought none.
struct OlderRowsRow: View {
    @Environment(ChatHost.self) private var host
    let older: OlderRows
    let thread: String?
    /// How many times rows have arrived above the transcript.
    var arrived = 0

    var body: some View {
        Button {
            ask()
        } label: {
            HStack(spacing: ChatListSizes.hitSpacing) {
                if older.loading {
                    ProgressView().controlSize(.small)
                } else {
                    Image(systemName: "clock.arrow.circlepath")
                }
                Text(older.label)
            }
            .font(.caption)
            .foregroundStyle(.quietText)
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.plain)
        // Asked for once the row has stood on screen a moment, and again
        // each time rows arrive while it does. Not at once: rows that
        // arrive are laid out above the reader before the transcript is
        // put back where they were, so this row comes on screen for that
        // moment at every arrival, and asking then read a whole thread
        // in, page after page, for a reader who reached its top once.
        .task(id: arrived) {
            guard (try? await Task.sleep(for: Self.dwell)) != nil else { return }
            ask()
        }
    }

    /// How long the row stands on screen before it asks.
    static let dwell: Duration = .milliseconds(250)
    /// The row's name in the transcript, which no turn's id is: what a
    /// chat still read toward the row it opens at is put at.
    static let row = "older-rows"

    private func ask() {
        if let thread { host.send(.olderRows(thread: thread)) }
    }
}

extension TranscriptPiece {
    /// The first of `rows` (the ids of the rows on screen) in the
    /// transcript's order: the row a reader has at the top.
    static func firstRow(among rows: [String], of turns: [TurnPresentation]) -> String? {
        let onScreen = Set(rows)
        return pieces(of: turns).first { onScreen.contains($0.id) }?.id
    }
}

/// What a window that just appeared tells the list, and what it keeps for
/// the next launch: the chat it shows and when it was last in it. The list
/// decides what the window opens to (`ChatApp.land`): that chat while it
/// was left less than half an hour ago, else a chat that is running, else
/// a new chat.
enum FirstScreen {
    static func appeared(scene: String, restored: String, left: Double) -> ChatApp.Command {
        .sceneAppeared(
            scene: scene, restoring: restored.isEmpty ? nil : restored, left: left > 0 ? Date(timeIntervalSince1970: left) : nil, lands: true)
    }
}

/// Keeps a window's chat and the last moment it was in it, for the next
/// launch: when the chat changes or gets its thread, and when the window
/// leaves the front.
struct KeepsPlace: ViewModifier {
    @Environment(ChatHost.self) private var host
    @Environment(\.scenePhase) private var phase
    let scene: String
    @Binding var restored: String
    @Binding var left: Double

    func body(content: Content) -> some View {
        content
            .onChange(of: host.chats?.scenes[scene]?.thread) { _, thread in
                // A new chat gets its thread after it is shown.
                if let thread { restored = thread }
                left = Date().timeIntervalSince1970
            }
            .onChange(of: phase) { _, _ in left = Date().timeIntervalSince1970 }
    }
}
