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
/// table and a fenced block.
@MainActor
@Suite struct GalleryTests {
    static let scene = "gallery"
    static let question = "Where does the week stand?"

    /// The reply up to and including the first `marker`, and then `more`.
    static func cut(after marker: String, more: Int = 0) -> Int {
        guard let range = ChatFixtures.reply.range(of: marker) else { return 0 }
        return ChatFixtures.reply.distance(from: ChatFixtures.reply.startIndex, to: range.upperBound) + more
    }

    /// The names, and where the stream stops for each; nil is the settled
    /// reply.
    static let pictures: [(name: String, stop: Int?)] = [
        ("chat-reply", nil),
        ("chat-streaming-emphasis", cut(after: "You have **three thi")),
        ("chat-streaming-table", cut(after: "| Item | Owner | Due | State |")),
        ("chat-streaming-fence", cut(after: "let due = try await client.list(")),
    ]

    /// A host on the fixture showing one chat, once its transcript is the
    /// one the picture is of.
    func host(stop: Int?) async throws -> (ChatHost, ChildKey) {
        // The whole reply in one piece: where it stops is exact.
        let server = FixtureServer(reply: ChatFixtures.reply, pace: .init(piece: 1, interval: .zero))
        let thread = stop == nil ? "t-reply" : "t-live"
        if let stop {
            server.empty(thread)
            server.answer(with: ChatFixtures.reply, holdAfter: stop)
        } else {
            server.seed(thread, message: Self.question, reply: ChatFixtures.reply)
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
            if let screen { TranscriptRows(screen: screen, key: key, onKey: { _ in }) }
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
            let (host, key) = try await host(stop: picture.stop)
            // A settled reply at its whole height; a streaming one as a
            // phone shows it, held at its end.
            let size = picture.stop == nil ? CGSize(width: OffscreenDrawing.phone.width, height: 960) : OffscreenDrawing.phone
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
#endif
