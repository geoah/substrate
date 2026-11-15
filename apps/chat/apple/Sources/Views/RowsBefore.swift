import ChatKit
import SwiftUI

/// The rows a transcript had on screen when rows last arrived above the
/// ones drawn (`TranscriptScroll`'s `above`): what its reader is kept at.
///
/// Rows that arrive above are laid out before anything can be asked of the
/// scroll view, and by then the rows it says are on screen are the ones
/// that arrived: where they are taller than the screen, the row its reader
/// had is as far below, out of sight. Asked then, the scroll view names a
/// row that arrived, and the reader is left at the oldest of them. So the
/// rows on screen are kept from the last time the transcript was drawn
/// before these, which is while its body is made: before any layout.
///
/// A reference the scroll view holds: it is written as the body is made and
/// draws nothing.
@MainActor
final class RowsBefore {
    private var above: RowsAbove?
    /// The rows on screen when `above` last changed.
    private(set) var seen: [String] = []
    /// Where each of them stood in the scroll view then, where it had
    /// said (`RowPlaces`).
    private(set) var frames: [String: CGRect] = [:]

    /// Told what the transcript is drawn with, each time its body is made.
    func drawn(above: RowsAbove?, seen: [String], places: RowPlaces) {
        guard above != self.above else { return }
        self.above = above
        self.seen = seen
        frames = places.frames.filter { seen.contains($0.key) }
    }

    /// The point of a row, and of the room it is scrolled in, that are put
    /// together to stand the row where it stood: a scroll view puts a
    /// row's point at `y` of its height on the room's point at `y` of its
    /// own, so a row whose top is at `top` is there again at
    /// `top / (room - height)`. Past either end of the row where it does
    /// not span the room, which the scroll view takes too. Nil where the
    /// row is as tall as the room: every `y` then puts it at the top.
    static func fraction(top: Double, height: Double, room: Double) -> Double? {
        abs(room - height) < 1 ? nil : top / (room - height)
    }

    /// The row that is put back, and the point of it: `row` where its own
    /// point says where it stood. A row as tall as the room has no such
    /// point, and one part way up the screen then would be put at its
    /// top: the next row on screen below says the same place, so that one
    /// is put back in its stead. Nil where no row on screen can say it.
    static func place(of row: String, among frames: [String: CGRect], room: Double) -> (row: String, y: Double)? {
        guard let frame = frames[row] else { return nil }
        if let y = fraction(top: frame.minY, height: frame.height, room: room) { return (row, y) }
        let others = frames.filter { $0.key != row }.sorted { ($0.value.minY, $0.key) < ($1.value.minY, $1.key) }
        for (other, stood) in others {
            if let y = fraction(top: stood.minY, height: stood.height, room: room) { return (other, y) }
        }
        return nil
    }

    /// What is put where once rows have arrived above `row`: the row where
    /// it stood, to the point, on an iPhone and an iPad. On a Mac the row
    /// at its top, since there the scroll view put at another point of a
    /// row was seen to refuse the test window's wheel after, and no wheel
    /// could be turned over the real one to tell that from a fault
    /// (`apps/chat/DESIGN.md`, "Rows that arrive above").
    func anchor(of row: String, room: Double) -> (row: String, anchor: UnitPoint) {
        #if os(iOS)
        guard let place = Self.place(of: row, among: frames, room: room) else { return (row, .top) }
        return (place.row, UnitPoint(x: UnitPoint.top.x, y: place.y))
        #else
        return (row, .top)
        #endif
    }
}

/// Where each drawn row of a transcript stands in its scroll view, top and
/// height, as the row last said (`placed(_:)`): what a row is put back at
/// when rows arrive above it. Of the rows drawn alone: a row that leaves
/// the screen takes its place with it, so the rows a transcript let go
/// are not kept. A reference the scroll view holds, written as rows move
/// and drawing nothing.
@MainActor
final class RowPlaces {
    var frames: [String: CGRect] = [:]
    /// Whose word each place is. A scroll view made anew draws its rows
    /// under the ids of the ones it replaces, and those leave after the
    /// new ones have said where they stand: a row that leaves takes its
    /// own word with it and never another's.
    private var said: [String: Row] = [:]

    /// One drawn row, to tell it from another under its id.
    final class Row {}

    func say(_ frame: CGRect, of id: String, by row: Row) {
        frames[id] = frame
        said[id] = row
    }

    func left(_ id: String, by row: Row) {
        guard said[id] === row else { return }
        frames[id] = nil
        said[id] = nil
    }

    /// Whether a row's end stands within the room's height of the end of
    /// the scroll view's room, `room` points high from the top of what it
    /// shows: a row the scroll view has only to finish moving to.
    func ends(_ row: String, near room: Double) -> Bool {
        frames[row].map { abs($0.maxY - room) <= room } ?? false
    }

    /// Whether any row stands in the room.
    func shows(in room: Double) -> Bool {
        frames.values.contains { $0.maxY > 0 && $0.minY < room }
    }
}

extension EnvironmentValues {
    /// The places the transcript's rows say theirs to.
    @Entry var rowPlaces: RowPlaces?
}

extension View {
    /// Says where this row of a transcript stands, under the id the
    /// scroll view knows it by. Only where a row is put back to the point
    /// (`RowsBefore.anchor`): elsewhere nothing reads it.
    @ViewBuilder
    func placed(_ id: String) -> some View {
        #if os(iOS)
        modifier(PlacedRow(id: id))
        #else
        self
        #endif
    }
}

#if os(iOS)
private struct PlacedRow: ViewModifier {
    @Environment(\.rowPlaces) private var places
    @State private var row = RowPlaces.Row()
    let id: String

    func body(content: Content) -> some View {
        content.onGeometryChange(for: CGRect.self) {
            $0.frame(in: .scrollView)
        } action: {
            places?.say($0, of: id, by: row)
        }
        .onDisappear { places?.left(id, by: row) }
    }
}
#endif
