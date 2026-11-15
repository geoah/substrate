#if os(macOS)
import AppKit
import ChatKit
import Foundation
import SubstrateKit
import SwiftUI
import Testing

@testable import SubstrateChat

/// Rows that arrive above a transcript's first (`TranscriptScroll`'s
/// `above`) meet a tall row: a reply that opens with a wide table and goes
/// on to a long block of code, first in the window. The transcript's own
/// scroll view and the rows' own views, in a window no screen draws; each
/// row says where it stands in the scroll view.
@MainActor
@Suite(.serialized) struct OlderRowsTests {
    @MainActor
    @Observable final class Chat {
        var turns: [TurnPresentation]
        var above: RowsAbove?
        /// The turn the chat opens at: a message a search found.
        var opens: String?
        /// Each drawn row's place in the scroll view, by its id.
        @ObservationIgnored var frames: [String: CGRect] = [:]

        init(turns: [TurnPresentation], opens: String? = nil) {
            self.turns = turns
            self.opens = opens
        }

        /// Under the last reply, as a chat at rest has it.
        var actions: ReplyActions? { turns.last.map { ReplyActions(turn: $0.id, copies: true, whole: true) } }

        /// Rows arrived above `turn`, for the `count`th time.
        func arrived(_ rows: [TurnPresentation], above turn: String, count: Int) throws {
            turns = rows + turns
            above = try JSONDecoder().decode(RowsAbove.self, from: Data(#"{"turn":"\#(turn)","count":\#(count)}"#.utf8))
        }
    }

    struct Rows: View {
        let chat: Chat

        var body: some View {
            let turns = chat.turns
            TranscriptScroll(
                opens: chat.opens.map { .turn($0) } ?? .end, anchor: chat.opens, sends: 0, above: chat.above,
                first: { TranscriptPiece.firstRow(among: $0, of: turns) }
            ) {
                LazyVStack(alignment: .leading, spacing: TranscriptPiece.spacing) {
                    ForEach(TranscriptPiece.pieces(of: turns, actions: chat.actions)) { piece in
                        TranscriptPieceView(piece: piece, cards: [:], expandThinking: false)
                            .onGeometryChange(for: CGRect.self) {
                                $0.frame(in: .scrollView)
                            } action: {
                                chat.frames[piece.id] = $0
                            }
                    }
                }
                .scrollTargetLayout()
                .padding(.horizontal, 20)
                .replyLinks()
                .replyTools(wraps: false)
            } bar: { _, _, _ in
                Color.clear.frame(height: 20)
            }
        }
    }

    @MainActor
    final class Stage: OffscreenTranscript {
        static let size = CGSize(width: 390, height: 640)
        let window: NSWindow

        init(_ chat: Chat, _ host: ChatHost) {
            _ = NSApplication.shared
            let view = Rows(chat: chat).environment(host).recordViewer(host.recordDirectory()).transaction { $0.animation = nil }
            let controller = NSHostingController(rootView: view)
            controller.sizingOptions = []
            window = NSWindow(contentRect: CGRect(origin: .zero, size: Self.size), styleMask: [.titled, .resizable], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false
            window.contentViewController = controller
            window.setContentSize(Self.size)
            window.setFrameOrigin(NSPoint(x: -20000, y: -20000))
            window.orderFrontRegardless()
        }

        /// The app's moves, and a wheel's rest, have come to theirs.
        func rest() async throws {
            try await Task.sleep(for: .milliseconds(600))
            try await settle()
        }

        var buttons: [String] {
            guard let content = window.contentView else { return [] }
            return LeavingTheApp.elements(under: content).filter { $0.accessibilityRole?() == .button }.compactMap { $0.accessibilityLabel?() ?? nil }
        }

        isolated deinit {
            window.orderOut(nil)
            window.contentViewController = nil
        }
    }

    /// The turns of a chat that holds one settled reply, under ids of the
    /// test's own.
    static func turns(_ reply: String, _ name: String) async throws -> (ChatHost, [TurnPresentation]) {
        let (host, key) = try await GalleryTests().host(reply: reply, stop: nil)
        var turns = try #require(host.conversation(key)?.turns)
        for index in turns.indices { turns[index].id = "\(name)\(index)" }
        return (host, turns)
    }

    static let tall = ChatFixtures.tables + "\n\n" + ChatFixtures.longLines

    func staged() async throws -> (Chat, Stage, older: [TurnPresentation]) {
        let (host, tall) = try await Self.turns(Self.tall, "tall")
        var held = [tall[1]]
        var older: [TurnPresentation] = []
        for round in 0 ..< 6 {
            held += try await Self.turns(ChatFixtures.short, "held\(round)-").1
            older += try await Self.turns(ChatFixtures.short, "older\(round)-").1
        }
        let chat = Chat(turns: held, opens: held[0].id)
        let stage = Stage(chat, host)
        try await stage.rest()
        return (chat, stage, older)
    }

    /// The same rows again under other ids: a page of rows before the
    /// ones held.
    static func page(_ rows: [TurnPresentation], _ name: String) -> [TurnPresentation] {
        rows.map { row in
            var row = row
            row.id = name + row.id
            return row
        }
    }

    /// The point a row is put back at, which an iPhone and an iPad use
    /// (`RowsBefore.anchor`): a scroll view asked to put a row's point at
    /// `y` of its height on the room's point at `y` of its own stands the
    /// row's top at `y * (room - height)`.
    @Test func theRowsPointThatStandsItWhereItStood() throws {
        func top(_ y: Double, height: Double, room: Double) -> Double { y * (room - height) }
        // Inside a row that spans the room, 100 points down it.
        let spanning = try #require(RowsBefore.fraction(top: -100, height: 777, room: 620))
        #expect(spanning > 0 && spanning < 1 && abs(top(spanning, height: 777, room: 620) + 100) < 0.001)
        // Further down it, where the row's end is on screen: past its end.
        let past = try #require(RowsBefore.fraction(top: -300, height: 777, room: 620))
        #expect(past > 1 && abs(top(past, height: 777, room: 620) + 300) < 0.001)
        // A short row whole on screen, and one cut by the top.
        let whole = try #require(RowsBefore.fraction(top: 100, height: 50, room: 620))
        #expect(abs(top(whole, height: 50, room: 620) - 100) < 0.001)
        let cut = try #require(RowsBefore.fraction(top: -20, height: 50, room: 620))
        #expect(cut < 0 && abs(top(cut, height: 50, room: 620) + 20) < 0.001)
        // As tall as the room: no point says more than the top does.
        #expect(RowsBefore.fraction(top: -5, height: 620.4, room: 620) == nil)
    }

    @Test(.timeLimit(.minutes(5)))
    func rowsThatArriveAboveATableAndCodeLeaveTheirReaderWhereTheyAre() async throws {
        let (chat, stage, older) = try await staged()
        let first = try #require(chat.turns.first?.id)
        func top(_ row: String) throws -> CGFloat { try #require(chat.frames[row], "\(row) is not drawn").minY }
        // Opened at the reply, as from a search: its table is at the top,
        // it is taller than the window, and the chat's last reply, with
        // the row under it, is far below.
        let opened = try top(first)
        #expect(abs(opened) < 1, "not opened at the reply: \(opened)")
        #expect(try #require(stage.reach).below > Stage.size.height)
        #expect(stage.buttons.contains("Copy Code") && !stage.buttons.contains("Copy"))

        // Rows arrive above: the table stays where it is.
        try chat.arrived(Self.page(older, "a-"), above: first, count: 1)
        try await stage.rest()
        let kept = try top(first)
        #expect(abs(kept - opened) < 1, "moved by the rows above: \(opened) then \(kept)")
        #expect(!stage.buttons.contains("Copy"))

        // Part way down the reply, inside its row, when more arrive: the
        // reader keeps the row, at its top, and is taken no further. (On
        // a Mac a row is kept by its top, which inside a row taller than
        // the screen is a move of what was read of it; an iPhone and an
        // iPad keep it to the point, `RowsBefore.anchor`.)
        stage.wheel(up: -300)
        try await stage.rest()
        let inside = try top(first)
        try #require(inside < -250, "not inside the row: \(inside)")
        try chat.arrived(Self.page(older, "b-"), above: first, count: 2)
        try await stage.rest()
        let stayed = try top(first)
        #expect(abs(stayed) < 1, "not at the row its reader was in: \(inside) then \(stayed)")
        // And the wheel moves it after: the scroll view takes its reader's
        // hand again once the app has put it somewhere.
        stage.wheel(up: -120)
        try await stage.rest()
        let on = try top(first)
        #expect(on < stayed - 60, "the wheel is refused after rows arrived: \(stayed) then \(on)")

        // Through all of it one reply had the row under it, the last: at
        // the chat's end it is drawn, once.
        let pieces = TranscriptPiece.pieces(of: chat.turns, actions: chat.actions)
        #expect(pieces.filter(\.drawsActions).map(\.id) == [pieces.last?.id] && pieces.last?.turn.id == chat.turns.last?.id)
        stage.wheel(up: -100_000)
        try await stage.rest()
        #expect(stage.buttons.filter { $0 == "Copy" }.count == 1 && stage.buttons.filter { $0 == "Share" }.count == 1)
    }
}
#endif
