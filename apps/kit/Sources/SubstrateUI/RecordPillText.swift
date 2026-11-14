#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDeviceApple

/// A record's pill inside a line of text: the same tile, name and capsule
/// as `RecordPill` (`RecordPillStyle`), as text, so it wraps with its line,
/// sits on the baseline and takes the size of the text around it. A capsule
/// view cannot stand in a sentence; this is the pill drawn the one way a
/// sentence allows. The text carries the pill's parts as attributes, and
/// `recordPillText()` on a view above draws the capsule and the tile behind
/// them.
public enum RecordPillText {
    /// The pill for `reference`, named by `name`: the link's own words in a
    /// reply. `link` is what a tap anywhere on the pill opens. `occurrence`
    /// tells one pill from the next in the same text, so two of one record
    /// side by side stay two capsules.
    public static func text(_ reference: RecordReference, name: AttributedString, link: URL?, occurrence: Int = 0) -> Text {
        var name = name
        name.link = link
        // A link's words take the tint; the pill's name is the name's colour.
        name.foregroundColor = RecordPillStyle.name
        let part = RecordPillPart(reference: reference, occurrence: occurrence)
        // Spaces that do not break, and are the link too, so the whole
        // capsule opens the record: a narrow one for the capsule's padding
        // at each end and between the tile and the name, and four for the
        // tile's place, about its width at the size of the text around it.
        func space(_ characters: String) -> AttributedString {
            var space = AttributedString(characters)
            space.link = link
            return space
        }
        let gap = Text(space("\u{202F}")).customAttribute(part)
        // Its label is the kind, so that a screen reader reads the pill as
        // `RecordPill`'s label.
        let tile = Text(space("\u{00A0}\u{00A0}\u{00A0}\u{00A0}"))
            .accessibilityLabel("\(kindDisplayName(reference.kind)):")
            .customAttribute(RecordPillTile(kind: reference.kind))
            .customAttribute(part)
        return Text("\(gap)\(tile)\(gap)\(Text(name).customAttribute(RecordPillName()).customAttribute(part))\(gap)")
    }
}

/// A run of text that is part of one record's pill.
struct RecordPillPart: TextAttribute, Hashable {
    var reference: RecordReference
    var occurrence = 0
}

/// A run of the pill's name.
struct RecordPillName: TextAttribute, Hashable {}

/// The run that holds the place of the kind's tile.
struct RecordPillTile: TextAttribute, Hashable {
    var kind: String
}

public extension View {
    /// Draws the pills of every `RecordPillText` in the text below, and
    /// those of records the scene's directory found gone as `RecordPill`
    /// draws one: dimmed and struck through.
    func recordPillText() -> some View {
        modifier(RecordPillTextDrawing())
    }
}

private struct RecordPillTextDrawing: ViewModifier {
    @Environment(\.recordDirectory) private var directory

    func body(content: Content) -> some View {
        content.textRenderer(RecordPillRenderer(gone: directory?.gone ?? []))
    }
}

/// Draws a line's pills behind its text: for each run of one pill's parts
/// on a line, a capsule, and the kind's tile where its place is. A pill
/// that wraps is a capsule on each of its lines.
struct RecordPillRenderer: TextRenderer {
    /// The `kind/id` paths of records that are gone.
    var gone: Set<String> = []

    func draw(layout: Text.Layout, in context: inout GraphicsContext) {
        for line in layout {
            var open: (pill: RecordPillPart, rect: CGRect)?
            var capsules: [(pill: RecordPillPart, rect: CGRect)] = []
            var tiles: [(pill: RecordPillPart, kind: String, rect: CGRect)] = []
            for run in line {
                let rect = run.typographicBounds.rect
                if let pill = run[RecordPillPart.self] {
                    if let current = open, current.pill == pill {
                        open = (pill, current.rect.union(rect))
                    } else {
                        if let current = open { capsules.append(current) }
                        open = (pill, rect)
                    }
                    if let tile = run[RecordPillTile.self] { tiles.append((pill, tile.kind, rect)) }
                } else if let current = open {
                    capsules.append(current)
                    open = nil
                }
            }
            if let current = open { capsules.append(current) }
            for capsule in capsules {
                let rect = capsule.rect.insetBy(dx: 0, dy: RecordPillStyle.lineInset)
                context.fill(Capsule().path(in: rect), with: .style(RecordPillStyle.fill))
            }
            for tile in tiles {
                let side = tile.rect.height - 2 * (RecordPillStyle.lineInset + RecordPillStyle.tileInset)
                let square = CGRect(x: tile.rect.midX - side / 2, y: tile.rect.midY - side / 2, width: side, height: side)
                var dimmed = context
                dimmed.opacity = gone.contains(tile.pill.reference.path) ? RecordPillStyle.goneOpacity : 1
                KindGlyph.draw(kind: tile.kind, in: square, context: &dimmed)
            }
            guard !gone.isEmpty, capsules.contains(where: { gone.contains($0.pill.reference.path) }) else {
                context.draw(line)
                continue
            }
            // A gone record's words, dimmed, with a line through them.
            for run in line {
                guard let pill = run[RecordPillPart.self], gone.contains(pill.reference.path) else {
                    context.draw(run)
                    continue
                }
                var dimmed = context
                dimmed.opacity = RecordPillStyle.goneOpacity
                dimmed.draw(run)
                guard run[RecordPillName.self] != nil else { continue }
                let rect = run.typographicBounds.rect
                var strike = Path()
                strike.move(to: CGPoint(x: rect.minX, y: rect.midY))
                strike.addLine(to: CGPoint(x: rect.maxX, y: rect.midY))
                dimmed.stroke(strike, with: .color(RecordPillStyle.name), lineWidth: 1)
            }
        }
    }
}
#endif
