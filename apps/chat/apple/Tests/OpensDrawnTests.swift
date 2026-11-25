#if os(iOS)
import Foundation
import SubstrateChatCore
import SubstrateKit
import SwiftUI
import Testing
import UIKit

@testable import SubstrateChat
@testable import SubstrateChatUI

/// A chat opens with its rows on screen, whatever their heights and
/// whatever the text size: through the conversation's own view, in a window
/// that is there before the rows arrive, as the app's is. What is asked is
/// what a person sees: the window is drawn, and the band of it between the
/// title and the composer must hold something.
///
/// An iPhone's and an iPad's test alone (`mise run apple:test:chat:sim`).
/// The fault it holds was theirs: a transcript sent to its end by a height
/// the lazy stack had guessed stood past its rows, 2 of 3 opens and more at
/// the accessibility sizes. A Mac's scroll view is sent to its edge, and a
/// window no screen shows draws nothing a scroll view holds, so the Mac's
/// test host can neither meet the fault nor see it.
@MainActor
@Suite(.serialized) struct OpensDrawnTests {
    static let scene = "opens"
    static let thread = "t-reply"
    /// A chat the fixtures hold four rows of, which a send adds to: a
    /// seeded one holds its newest turn alone.
    static let short = "t-short"
    nonisolated static let unseeded: (name: String, text: String?) = ("a chat of two turns", nil)

    /// The gallery's replies that are taller than a window at one text
    /// size or another, by name, each a chat of two turns, which is laid
    /// out whole; and with no reply the fixture's chat of 200 turns, whose
    /// stack is lazy.
    nonisolated static let replies: [(name: String, text: String?)] = [
        ("week", ChatFixtures.reply), ("tables", ChatFixtures.tables), ("lines", ChatFixtures.longLines),
        ("big", ChatFixtures.reply(bytes: 16 * 1024)), ("the chat of 200 turns", nil),
    ]

    /// The usual size, the accessibility sizes the fault was counted at,
    /// and the largest.
    nonisolated static let sizes: [DynamicTypeSize] = [.large, .accessibility1, .accessibility3, .accessibility5]

    nonisolated static var cases: [(reply: Int, size: Int)] { replies.indices.flatMap { reply in sizes.indices.map { (reply, $0) } } }

    @MainActor
    final class Stage {
        let window: UIWindow

        init(_ host: ChatHost, _ key: ChildKey, at size: DynamicTypeSize, height: CGFloat? = nil) throws {
            let view = NavigationStack { ConversationView(key: key, scene: OpensDrawnTests.scene) }
                .environment(host)
                .recordViewer(host.recordDirectory())
                .dynamicTypeSize(size)
                .transaction { $0.animation = nil }
            let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
            let scene = try #require(scenes.first, "the test's host app has no window scene")
            window = UIWindow(windowScene: scene)
            window.frame = scene.screen.bounds
            if let height { window.frame.size.height = height }
            window.rootViewController = UIHostingController(rootView: view)
            window.isHidden = false
        }

        func close() {
            window.isHidden = true
            window.rootViewController = nil
        }

        /// The transcript's scroll view: the one as large as the window.
        var scroll: UIScrollView? {
            func all(_ view: UIView) -> [UIScrollView] { ((view as? UIScrollView).map { [$0] } ?? []) + view.subviews.flatMap(all) }
            return all(window).first { $0.bounds.height >= window.bounds.height - 1 }
        }

        /// How much of the transcript's band is drawn on: the share of its
        /// points that are not the page's colour, which is the commonest.
        func drawn() -> Double? {
            guard let scroll else { return nil }
            let insets = scroll.adjustedContentInset
            let band = CGRect(
                x: 0, y: insets.top, width: window.bounds.width, height: window.bounds.height - insets.top - insets.bottom)
            guard band.height > 40 else { return nil }
            let format = UIGraphicsImageRendererFormat()
            format.scale = 1
            let image = UIGraphicsImageRenderer(bounds: window.bounds, format: format).image { _ in
                window.drawHierarchy(in: window.bounds, afterScreenUpdates: true)
            }
            guard let pixels = image.cgImage, let data = pixels.dataProvider?.data, let bytes = CFDataGetBytePtr(data) else { return nil }
            var counts: [UInt32: Int] = [:]
            var total = 0
            for y in stride(from: Int(band.minY), to: min(Int(band.maxY), pixels.height), by: 2) {
                for x in stride(from: 0, to: pixels.width, by: 2) {
                    let at = y * pixels.bytesPerRow + x * pixels.bitsPerPixel / 8
                    // A colour to five bits a part: a blur's grain is one.
                    let colour = UInt32(bytes[at] >> 3) << 10 | UInt32(bytes[at + 1] >> 3) << 5 | UInt32(bytes[at + 2] >> 3)
                    counts[colour, default: 0] += 1
                    total += 1
                }
            }
            guard total > 0, let page = counts.values.max() else { return nil }
            return 1 - Double(page) / Double(total)
        }
    }

    @discardableResult
    func until<T>(_ read: () -> T?) async throws -> T {
        for _ in 0 ..< 600 {
            if let value = read() { return value }
            try await Task.sleep(for: .milliseconds(50))
        }
        throw NotReached()
    }

    struct NotReached: Error {}

    /// A chat of the fixtures shown in a window that is there before its
    /// rows are, and left until its scroll view has come to rest.
    func opened(_ reply: (name: String, text: String?), at size: DynamicTypeSize, height: CGFloat? = nil, technical: Bool = false) async throws
        -> (host: ChatHost, key: ChildKey, stage: Stage)
    {
        let server = FixtureServer(reply: "")
        let thread = reply.name == Self.unseeded.name ? Self.short : reply.text == nil ? ChatFixtures.long : Self.thread
        if let text = reply.text { server.seed(thread, message: "Show me.", reply: text) }
        // The rows arrive once the chat is on screen, as they do against a
        // server: a scroll view made with its rows opens at its own end,
        // and the fault was one that got them after.
        server.rowsTake = .milliseconds(400)
        server.tallied = technical
        let host = ChatHost(.fixture(server))
        host.send(.sceneAppeared(scene: Self.scene, restoring: nil))
        try await until { host.chats?.list.loading == false ? true : nil }
        if technical { host.send(.settings(.toggle(row: .app("technicalDetails"), on: true))) }
        host.send(.show(thread: thread, scene: Self.scene))
        let key = try await until { () -> ChildKey? in
            guard let key = host.chats?.scenes[Self.scene]?.current, host.conversation(key)?.thread == thread else { return nil }
            return key
        }
        let stage = try Stage(host, key, at: size, height: height)
        do {
            try await until { (host.conversation(key)?.turns.count ?? 0) >= (thread == ChatFixtures.long ? 200 : 2) ? true : nil }
            try await rest(stage)
        } catch {
            stage.close()
            throw error
        }
        return (host, key, stage)
    }

    /// Once the scroll view has come to rest: where it stands is the same
    /// for a second, which is longer than any move of the app's.
    func rest(_ stage: Stage) async throws {
        var rested = 0
        var stood: [CGFloat]?
        for _ in 0 ..< 200 where rested < 20 {
            try await Task.sleep(for: .milliseconds(50))
            let stands = stage.scroll.map { [$0.contentOffset.y, $0.contentSize.height, $0.adjustedContentInset.bottom] }
            rested = stands == stood ? rested + 1 : 0
            stood = stands
        }
    }

    /// Rows are drawn, and the scroll view stands at its end by its own
    /// account where the chat is taller than the room for it.
    func expectTheEnd(_ stage: Stage, _ said: String) throws {
        let scroll = try #require(stage.scroll, "no transcript in the window: \(said)")
        let drawn = try #require(stage.drawn(), "the window could not be drawn: \(said)")
        #expect(drawn > 0.01, "an empty screen: \(said): \(drawn) of the transcript is drawn on")
        let room = scroll.bounds.height - scroll.adjustedContentInset.top - scroll.adjustedContentInset.bottom
        let below = scroll.contentSize.height - (scroll.contentOffset.y + scroll.bounds.height - scroll.adjustedContentInset.bottom)
        if scroll.contentSize.height > room { #expect(abs(below) < 1, "not at its end: \(said): \(below) below, a room of \(room)") }
    }

    @Test(.timeLimit(.minutes(10)), arguments: OpensDrawnTests.cases.indices)
    func aChatOpensWithItsRowsOnScreen(_ index: Int) async throws {
        let (reply, size) = (Self.replies[Self.cases[index].reply], Self.sizes[Self.cases[index].size])
        // Three opens of each: the fault was not every open's.
        for open in 1 ... 3 {
            let (_, _, stage) = try await opened(reply, at: size)
            defer { stage.close() }
            try expectTheEnd(stage, "\(reply.name) at \(size), open \(open)")
        }
    }

    /// With the technical details on, which stand under the last turn and
    /// are there before the turns are: a chat of two turns in a window
    /// short enough that they are more than three times the room high, and
    /// the chat of 200 turns. Each opens at its end, and is at its end
    /// again after a send made from its top, where it was put with no
    /// finger on it. A last row named before the turns were held opened
    /// the long chat 54 points past its end; the send from the top came to
    /// the end with the last row named either way, so that half holds the
    /// way there and no fault that was seen.
    nonisolated static let sends: [(reply: (name: String, text: String?), size: DynamicTypeSize, height: CGFloat?)] = [
        (unseeded, .accessibility5, 400), (replies[4], .large, nil), (replies[4], .accessibility3, nil),
    ]

    @Test(.timeLimit(.minutes(10)), arguments: OpensDrawnTests.sends.indices)
    func aSendFromTheTopComesToTheEnd(_ index: Int) async throws {
        let (reply, size, height) = Self.sends[index]
        let said = "\(reply.name) at \(size)\(height.map { " in a window \(Int($0)) high" } ?? "")"
        let (host, key, stage) = try await opened(reply, at: size, height: height, technical: true)
        defer { stage.close() }
        try expectTheEnd(stage, "\(said), opened")
        let scroll = try #require(stage.scroll)
        let held = host.conversation(key)?.turns.count ?? 0
        scroll.setContentOffset(CGPoint(x: 0, y: -scroll.adjustedContentInset.top), animated: false)
        try await Task.sleep(for: .milliseconds(500))
        host.send(.draft("And then?"), to: key)
        host.send(.send, to: key)
        try await until { (host.conversation(key)?.turns.count ?? 0) > held ? true : nil }
        try await rest(stage)
        try expectTheEnd(stage, "\(said), after a send from its top")
    }
}
#endif
