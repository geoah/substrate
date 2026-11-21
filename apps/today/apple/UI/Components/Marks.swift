import SwiftUI
import TodayCore

/// A project, wherever it is named: a square in ink with two initials.
/// Only a project is a square with initials, and nothing sits on it.
struct TileView: View {
    var tile: Tile
    var size: MarkSize = .row

    var body: some View {
        Text(tile.initials)
            .font(.system(size: size.letters, weight: .bold, design: .rounded))
            .tracking(0.2)
            .foregroundStyle(tile.suggested ? AnyShapeStyle(.ink) : AnyShapeStyle(.onAction))
            .frame(width: size.points, height: size.points)
            .background {
                let shape = RoundedRectangle(cornerRadius: size.corner, style: .continuous)
                if tile.suggested { shape.strokeBorder(.ink, lineWidth: 1.5) } else { shape.fill(.action) }
            }
            .accessibilityHidden(true)
    }
}

/// A person, wherever they are named: a circle with two initials, in a
/// quiet tint of ink. Colour is kept for flags, for urgent and for the
/// assistant, so a badge has none of its own.
struct BadgeView: View {
    var badge: Badge
    var size: MarkSize = .row

    var body: some View {
        Text(badge.initials)
            .font(.system(size: size.letters, weight: .bold, design: .rounded))
            .foregroundStyle(.ink)
            .frame(width: size.points, height: size.points)
            .background(Circle().fill(TodayLook.ink.color.opacity(0.11)))
            .accessibilityHidden(true)
    }
}

/// What leads a row, a chip or a page's title: shape says the kind.
struct IconView: View {
    var icon: Icon
    var size: MarkSize = .row

    var body: some View {
        switch icon {
        case let .tile(tile): TileView(tile: tile, size: size == .lead ? .row : size)
        case let .badge(badge): BadgeView(badge: badge, size: size == .lead ? .row : size)
        case let .glyph(glyph): GlyphView(glyph: glyph, size: size)
        }
    }
}
