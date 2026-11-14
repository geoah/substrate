#if os(macOS)
import AppKit
import ChatKit
import Foundation
import SubstrateKit
import SwiftUI
import Testing

@testable import SubstrateChat

/// A conversation's transcript drawn from ChatKit's fixtures by the views
/// the app draws it with, through the app's own runtime against the
/// fixture's server, so a picture is what the app shows and never a second
/// rendering of it. With
/// SUBSTRATE_GALLERY naming a folder (`mise run apps:gallery`), each is
/// written there as a PNG at an iPhone's width, in light and in dark: a
/// settled reply with every block the transcript draws, at its whole height,
/// and a reply caught as it is written, part way through an emphasis, a
/// table and a fenced block; and a reply that links records, each a pill
/// inside its line, in a paragraph, a list and a table.
///
/// And the places a failure is drawn, from ChatKit's fixtures, by the view
/// the app draws them with: each at an iPhone's size and at an iPad's, in
/// light and in dark. What surrounds a failure there (a bubble, a reply, a
/// row, the composer) stands in for the app's own: the failure itself is
/// the app's view over the presenter's words.
@MainActor
@Suite struct GalleryTests {
    static let scene = "gallery"
    static let question = "Where does the week stand?"
    static let now = Date(timeIntervalSince1970: 1_793_696_400)
    static let pad = CGSize(width: 834, height: 1112)

    /// The reply up to and including the first `marker`, and then `more`.
    static func cut(after marker: String, more: Int = 0) -> Int {
        guard let range = ChatFixtures.reply.range(of: marker) else { return 0 }
        return ChatFixtures.reply.distance(from: ChatFixtures.reply.startIndex, to: range.upperBound) + more
    }

    /// A reply that links records: each link is the record's pill, inside
    /// the line it stands in.
    static let linking = """
        The [Q4 budget](substrate://ada.example.com/tasks/task/q4) is blocked on the actuals, and \
        [Offsite agenda](substrate://ada.example.com/tasks/task/offsite) still has two sessions with no owner.

        - [Reading list](substrate://ada.example.com/tasks/task/reading) is ready to send
        - Ask [Grace Hopper](substrate://ada.example.com/people/person/grace) about the venue

        | Item | Owner |
        | --- | --- |
        | [Q4 budget](substrate://ada.example.com/tasks/task/q4) | [Ada Lovelace](substrate://ada.example.com/people/person/ada) |
        """

    /// The names, the reply, and where the stream stops for each; nil is
    /// the settled reply.
    static let pictures: [(name: String, reply: String, stop: Int?)] = [
        ("chat-reply", ChatFixtures.reply, nil),
        ("chat-reply-pills", linking, nil),
        ("chat-streaming-emphasis", ChatFixtures.reply, cut(after: "You have **three thi")),
        ("chat-streaming-table", ChatFixtures.reply, cut(after: "| Item | Owner | Due | State |")),
        ("chat-streaming-fence", ChatFixtures.reply, cut(after: "let due = try await client.list(")),
    ]

    /// A host on the fixture showing one chat, once its transcript is the
    /// one the picture is of.
    func host(reply: String = ChatFixtures.reply, stop: Int?) async throws -> (ChatHost, ChildKey) {
        // The whole reply in one piece: where it stops is exact.
        let server = FixtureServer(reply: reply, pace: .init(piece: 1, interval: .zero))
        let thread = stop == nil ? "t-reply" : "t-live"
        if let stop {
            server.empty(thread)
            server.answer(with: reply, holdAfter: stop)
        } else {
            server.seed(thread, message: Self.question, reply: reply)
        }
        let host = ChatHost(.fixture(server))
        host.send(.sceneAppeared(scene: Self.scene, restoring: nil))
        try await until { host.chats?.list.loading == false ? true : nil }
        host.send(.show(thread: thread, scene: Self.scene))
        let key = try await until { host.chats?.scenes[Self.scene]?.current }
        if let stop {
            host.send(.draft(Self.question), to: key)
            host.send(.send, to: key)
            try await until { Self.text(host, key)?.count == stop ? true : nil }
        } else {
            try await until { host.conversation(key)?.turns.count == 2 ? true : nil }
        }
        return (host, key)
    }

    static func text(_ host: ChatHost, _ key: ChildKey) -> String? {
        guard case let .assistant(turn)? = host.conversation(key)?.turns.last?.body else { return nil }
        return turn.text
    }

    /// The transcript's own rows, laid out whole at the conversation's
    /// margins and spacing: an offscreen drawing holds nothing a scroll view
    /// draws, nor the glass of the composer over it. A reply still being
    /// written is held at its end, as the screen follows it.
    func view(_ host: ChatHost, _ key: ChildKey, streaming: Bool) -> some View {
        let screen = host.conversation(key)
        return VStack(alignment: .leading, spacing: 22) {
            if let screen { TranscriptRows(screen: screen, key: key, onKey: { _ in }, act: { _ in }) }
        }
        .padding(.horizontal, 20)
        .padding(.top, 12)
        .padding(.bottom, 16)
        .environment(\.recordTitles, screen?.titles ?? [:])
        .replyLinks()
        .fixedSize(horizontal: false, vertical: true)
        .frame(maxHeight: .infinity, alignment: streaming ? .bottom : .top)
        .clipped()
        .recordViewer(host.recordDirectory())
        .environment(host)
    }

    @Test func aReplysRecordLinksAreItsPillsInTheRowsItIsLaidOutIn() async throws {
        let (host, key) = try await host(reply: Self.linking, stop: nil)
        guard case let .assistant(reply)? = host.conversation(key)?.turns.last?.body else {
            Issue.record("The reply is not the last turn.")
            return
        }
        // Each line of the rows the presenter made is cut into its pills
        // by the text the row's view draws.
        let lines = reply.blocks.rows.flatMap { row -> [String] in
            switch row.block {
            case let .paragraph(text): [text]
            case let .list(_, _, items):
                items.flatMap { item in
                    item.blocks.compactMap { if case let .paragraph(text) = $0 { text } else { nil } }
                }
            case let .table(_, _, rows): rows.flatMap { $0 }
            default: []
            }
        }
        let named = lines.flatMap { inlinePieces(parseInline($0)) }.compactMap { piece -> String? in
            if case let .record(reference, name) = piece { "\(reference.recordID): \(String(name.characters))" } else { nil }
        }
        #expect(
            named == [
                "q4: Q4 budget", "offsite: Offsite agenda", "reading: Reading list", "grace: Grace Hopper", "q4: Q4 budget",
                "ada: Ada Lovelace",
            ])
    }

    @Test func aSettledReplyHoldsEveryBlock() async throws {
        let (host, key) = try await host(stop: nil)
        let blocks = Markdown.parse(try #require(Self.text(host, key)))
        let kinds = Set(
            blocks.map { block in
                switch block {
                case .paragraph: "paragraph"
                case .heading: "heading"
                case .code: "code"
                case .quote: "quote"
                case .list: "list"
                case .table: "table"
                case .rule: "rule"
                }
            })
        #expect(kinds == ["paragraph", "heading", "code", "quote", "list", "table", "rule"])
        // And is long enough to be drawn as more than one row.
        guard case let .assistant(reply)? = host.conversation(key)?.turns.last?.body else { return }
        #expect(reply.pages.count > 1 && reply.blocks.rows.map(\.block) == blocks)
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["SUBSTRATE_GALLERY"] != nil), .timeLimit(.minutes(5)))
    func everyPictureDraws() async throws {
        // These tests run inside the app, which the Mac sandboxes: it may
        // not write the folder asked for, so each picture is attached to
        // the test's result, where `.mise/gallery.sh` takes it from.
        let directory = URL(fileURLWithPath: ProcessInfo.processInfo.environment["SUBSTRATE_GALLERY"] ?? NSTemporaryDirectory())
        let writable = FileManager.default.isWritableFile(atPath: directory.path)
        for picture in Self.pictures {
            let (host, key) = try await host(reply: picture.reply, stop: picture.stop)
            // A settled reply at its whole height; a streaming one as a
            // phone shows it, held at its end.
            let whole = CGSize(width: OffscreenDrawing.phone.width, height: picture.reply == ChatFixtures.reply ? 960 : 420)
            let size = picture.stop == nil ? whole : OffscreenDrawing.phone
            for scheme in [ColorScheme.light, .dark] {
                let name = "\(picture.name)-\(scheme == .dark ? "dark" : "light").png"
                let png = try await OffscreenDrawing.png(view(host, key, streaming: picture.stop != nil), size: size, scheme: scheme)
                if writable {
                    try png.write(to: directory.appendingPathComponent(name))
                } else {
                    Attachment.record(png, named: name)
                }
            }
        }
    }

    @Test func everyPlaceHasAFailureToDraw() {
        let places = Set(FailureFixtures.places(now: Self.now).map(\.place))
        #expect(places == [.turn, .olderTurn, .chat, .list])
        #expect(FailureFixtures.table(now: Self.now).allSatisfy { !$0.sentence.isEmpty })
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["SUBSTRATE_GALLERY"] != nil))
    func everyFailureDraws() async throws {
        for fixture in FailureFixtures.places(now: Date()) {
            for (device, size) in [("iphone", OffscreenDrawing.phone), ("ipad", Self.pad)] {
                for scheme in [ColorScheme.light, .dark] {
                    let view = Scene(fixture: fixture, wide: device == "ipad").environment(\.colorScheme, scheme)
                    Attachment.record(
                        try await OffscreenDrawing.png(view, size: size),
                        named: "failure-\(fixture.name)-\(device)-\(scheme == .dark ? "dark" : "light").png")
                }
            }
        }
        for scheme in [ColorScheme.light, .dark] {
            let table = FailureFixtures.table(now: Date())
            let view = Table(failures: table).environment(\.colorScheme, scheme)
            Attachment.record(
                try await OffscreenDrawing.png(view, size: CGSize(width: 800, height: 1150)),
                named: "failure-table-\(scheme == .dark ? "dark" : "light").png")
        }
    }

    /// The first answer `read` gives; fails after a minute without one.
    @discardableResult
    func until<T>(_ read: () -> T?) async throws -> T {
        for _ in 0 ..< 1200 {
            if let value = read() { return value }
            try await Task.sleep(for: .milliseconds(50))
        }
        throw NotReached()
    }

    struct NotReached: Error {}
}

/// One failure in its place, with a stand-in for what surrounds it.
private struct Scene: View {
    @Environment(\.colorScheme) private var scheme
    let fixture: FailureFixtures.Fixture
    let wide: Bool

    var body: some View {
        Group {
            if wide {
                HStack(spacing: 0) {
                    list.frame(width: 320)
                    Divider()
                    chat
                }
            } else if fixture.place == .list {
                list
            } else {
                chat
            }
        }
        .background(scheme == .dark ? Color.black : Color.white)
    }

    private var card: some View {
        FailureView(failure: fixture.failure, opened: fixture.detailsOpen) { _ in }
    }

    private var chat: some View {
        VStack(spacing: 0) {
            header("Balcony plants", "with Moss")
            VStack(alignment: .leading, spacing: 22) {
                bubble("How often should I water the balcony plants?")
                reply("Twice a week in summer, and once a week from October. The tomatoes are the thirsty ones: check their soil every second day.")
                bubble("And the tomatoes in August?")
                switch fixture.place {
                case .olderTurn:
                    card
                    bubble("Thanks. And the basil?")
                    reply("Basil likes the morning sun and dislikes wet feet. Water it at the base, early in the day.")
                case .turn:
                    card
                case .chat:
                    reply("Every second day in August, more when it is windy. Water at the")
                case .list:
                    reply("Every second day in August, more when it is windy.")
                }
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 20)
            .padding(.top, 16)
            .frame(maxWidth: 780)
            .frame(maxWidth: .infinity)
            VStack(spacing: 8) {
                if fixture.place == .chat { card.padding(.horizontal, 12) }
                composer
            }
            .frame(maxWidth: 820)
            .padding(.bottom, 16)
        }
    }

    private var list: some View {
        VStack(alignment: .leading, spacing: 0) {
            header("Chats", nil)
            VStack(alignment: .leading, spacing: 14) {
                if fixture.place == .list { card }
                Text("Today").font(.footnote.weight(.semibold)).foregroundStyle(.secondary)
                row("Balcony plants", "Moss  Every second day in August, more when it is windy.")
                row("Lunch with Sam on Friday", "Moss  Sent: Sam has your reply that Friday at 12:30 works.")
                Text("Yesterday").font(.footnote.weight(.semibold)).foregroundStyle(.secondary)
                row("Reading list", "Moss  Added “Dune” to your books.")
            }
            .padding(.horizontal, 16)
            .padding(.top, 12)
            Spacer(minLength: 0)
        }
    }

    private func header(_ title: String, _ subtitle: String?) -> some View {
        VStack(spacing: 2) {
            Text(title).font(.headline)
            if let subtitle { Text(subtitle).font(.caption).foregroundStyle(.secondary) }
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 54)
        .padding(.bottom, 10)
        .overlay(alignment: .bottom) { Divider() }
    }

    private func bubble(_ text: String) -> some View {
        Text(text)
            .padding(.horizontal, 14)
            .padding(.vertical, 9)
            .background(Color.accentColor.opacity(0.16), in: .rect(cornerRadius: 18))
            .frame(maxWidth: .infinity, alignment: .trailing)
    }

    private func reply(_ text: String) -> some View {
        Text(text).frame(maxWidth: .infinity, alignment: .leading)
    }

    private func row(_ title: String, _ preview: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.body.weight(.medium)).lineLimit(1)
            Text(preview).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
        }
    }

    private var composer: some View {
        HStack(spacing: 10) {
            Text("Message Moss")
                .foregroundStyle(.tertiary)
                .frame(maxWidth: .infinity, minHeight: 46, alignment: .leading)
                .padding(.horizontal, 16)
                .background(.fill.tertiary, in: .rect(cornerRadius: 24))
            Image(systemName: "mic.fill")
                .frame(width: 46, height: 46)
                .background(.fill.tertiary, in: .circle)
        }
        .padding(.horizontal, 12)
    }
}

/// Every row of the table of causes, in two columns.
private struct Table: View {
    @Environment(\.colorScheme) private var scheme
    let failures: [FailurePresentation]

    var body: some View {
        let half = (failures.count + 1) / 2
        HStack(alignment: .top, spacing: 12) {
            column(Array(failures.prefix(half)))
            column(Array(failures.dropFirst(half)))
        }
        .padding(16)
        .background(scheme == .dark ? Color.black : Color.white)
    }

    private func column(_ failures: [FailurePresentation]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(failures) { FailureView(failure: $0) { _ in } }
            Spacer(minLength: 0)
        }
    }
}
#endif
