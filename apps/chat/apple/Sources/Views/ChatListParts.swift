import ChatKit
import SubstrateKit
import SwiftUI

/// A section's heading: "Pinned" with its pin, a day as it is.
struct SectionTitle: View {
    let title: String

    var body: some View {
        if title == "Pinned" {
            Label(title, systemImage: "pin.fill").ink(.quiet, on: .list)
        } else {
            Text(title).ink(.quiet, on: .list)
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
            .tint(selected ? ChatColours.Ink.label.color : nil)
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
        .tint(ScreenTone.caution.fillColor)
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
        .prominentTint()
    }
}

/// The chat archived a moment ago, with Undo, floating over the list's end.
struct UndoBar: View {
    @Environment(ChatHost.self) private var host
    let undo: UndoPresentation

    var body: some View {
        // Undo under the words where the text size leaves it no room
        // beside them.
        SplitRow {
            Label(undo.words, systemImage: "archivebox")
                .font(ChatType.detail)
                .lineLimit(2)
                .truncationMode(.middle)
        } trailing: {
            Button(undo.action) { host.send(.undoArchive) }
                .font(ChatType.heading)
                .tinted(.accent, on: .list)
                .touchTarget()
        }
        .padding(ChatSpace.inset)
        .glassEffect(.regular, in: .rect(cornerRadius: ChatRadius.card))
        .padding(.horizontal, ChatSpace.gutter)
        .padding(.bottom, ChatSpace.gutter)
        .arrives(.rise)
        .accessibilityElement(children: .contain)
    }
}

/// Rows that stand in for the chats while the first read is out: a row's
/// own lines in a row's own type, with bars where the words would be, so
/// each is as tall as the chat that takes its place, at any text size.
struct PlaceholderRows: View {
    let count: Int

    /// Never read: how long each bar is.
    private static let titles = [22, 30, 17].map { String(repeating: "x", count: $0) }
    private static let previews = [64, 88].map { String(repeating: "x", count: $0) }

    var body: some View {
        ForEach(0 ..< count, id: \.self) { index in
            VStack(alignment: .leading, spacing: ChatSpace.mark) {
                Text(verbatim: Self.titles[index % Self.titles.count])
                    .font(ChatType.rowTitle())
                    .lineLimit(1)
                Text(verbatim: Self.previews[index % Self.previews.count])
                    .font(ChatType.detail)
                    .lineLimit(2)
            }
            .padding(.vertical, ChatSpace.pair)
            .redacted(reason: .placeholder)
            .shimmer()
            .accessibilityElement(children: .ignore)
            .accessibilityHidden(index > 0)
            .accessibilityLabel("Loading chats")
        }
        .selectionDisabled()
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
            HStack(spacing: ChatSpace.words) {
                if loading { ProgressView().controlSize(.small) }
                Text(loading ? "Loading…" : title)
                    .font(ChatType.detail)
                    .ink(.quiet, on: .list)
            }
            .frame(maxWidth: .infinity, minHeight: ChatSize.target)
            .contentShape(.rect)
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
        HStack(spacing: ChatSpace.words) {
            ProgressView().controlSize(.small)
            Text("Searching messages…")
                .font(ChatType.detail)
                .ink(.quiet, on: .list)
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
                VStack(alignment: .leading, spacing: ChatSpace.lines) {
                    HStack(alignment: .firstTextBaseline, spacing: ChatSpace.words) {
                        Text(group.title)
                            .font(ChatType.rowTitle())
                            .lineLimit(1)
                        if let archived = group.archived {
                            Text(archived)
                                .font(ChatType.smallLabel)
                                .ink(.quiet, on: .pill)
                                .padding(.horizontal, ChatSpace.pillSide)
                                .padding(.vertical, ChatSpace.pillEdge)
                                .background(ChatColours.pillStyle, in: .capsule)
                        }
                    }
                    ForEach(group.hits) { hit in
                        Button {
                            host.send(.open(thread: group.id, message: hit.id, scene: scene))
                        } label: {
                            SearchHitText(hit: hit, agent: group.agent)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .touchTarget()
                        }
                        .buttonStyle(.plain)
                    }
                    if let more = group.more {
                        Text(more).font(ChatType.caption).ink(.quiet, on: .list)
                    }
                }
                .padding(.vertical, ChatSpace.pair)
                .selectionDisabled()
                .accessibilityElement(children: .contain)
            }
        } header: {
            Text("Messages").ink(.quiet, on: .list)
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
            .font(ChatType.detail)
            .lineLimit(3)
            .accessibilityLabel("\(hit.who): \(hit.runs.map(\.text).joined())")
            .accessibilityAddTraits(.isButton)
    }

    private var text: Text {
        let who = Text("\(hit.who)  ").fontWeight(ChatType.strong)
        var out = hit.who == "You" ? who.ink(.label, on: .list) : who.inAgentInk(agent, on: .list)
        for run in hit.runs {
            let piece =
                run.match
                ? Text(run.text).fontWeight(ChatType.strong).ink(.accent, on: .list)
                : Text(run.text).ink(.quiet, on: .list)
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
            HStack(spacing: ChatSpace.words) {
                if older.loading {
                    ProgressView().controlSize(.small)
                } else {
                    Image(systemName: "clock.arrow.circlepath")
                }
                Text(older.label)
            }
            .font(ChatType.caption)
            .ink(.quiet, on: .page)
            .frame(maxWidth: .infinity, minHeight: ChatSize.target)
            .contentShape(.rect)
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
