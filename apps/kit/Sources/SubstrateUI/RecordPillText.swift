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
    /// reply. `link` is what a tap on the name opens.
    public static func text(_ reference: RecordReference, name: AttributedString, link: URL?) -> Text {
        var name = name
        name.link = link
        // A link's words take the tint; the pill's name is the name's colour.
        name.foregroundColor = RecordPillStyle.name
        let part = RecordPillPart(reference: reference)
        // The tile's place: a symbol's width at the size of the text around
        // it, which the renderer draws the tile in. Its label is the kind,
        // so that a screen reader reads the pill as `RecordPill`'s label.
        let tile = Text(Image(systemName: "square.fill"))
            .foregroundStyle(.clear)
            .accessibilityLabel("\(kindDisplayName(reference.kind)):")
            .customAttribute(RecordPillTile(kind: reference.kind))
            .customAttribute(part)
        // Narrow spaces that do not break: the capsule's padding, and the
        // tile kept with the name's first word.
        let gap = Text(verbatim: "\u{202F}").customAttribute(part)
        return Text("\(gap)\(tile)\(gap)\(Text(name).customAttribute(part))\(gap)")
    }
}

/// A run of text that is part of one record's pill.
struct RecordPillPart: TextAttribute, Hashable {
    var reference: RecordReference
}

/// The run that holds the place of the kind's tile.
struct RecordPillTile: TextAttribute, Hashable {
    var kind: String
}

public extension View {
    /// Draws the pills of every `RecordPillText` in the text below.
    func recordPillText() -> some View {
        textRenderer(RecordPillRenderer())
    }
}

/// Draws a line's pills behind its text: for each run of one pill's parts
/// on a line, a capsule, and the kind's tile where its place is. A pill
/// that wraps is a capsule on each of its lines.
struct RecordPillRenderer: TextRenderer {
    func draw(layout: Text.Layout, in context: inout GraphicsContext) {
        for line in layout {
            var open: (pill: RecordPillPart, rect: CGRect)?
            var capsules: [CGRect] = []
            var tiles: [(kind: String, rect: CGRect)] = []
            for run in line {
                let rect = run.typographicBounds.rect
                if let pill = run[RecordPillPart.self] {
                    if let current = open, current.pill == pill {
                        open = (pill, current.rect.union(rect))
                    } else {
                        if let current = open { capsules.append(current.rect) }
                        open = (pill, rect)
                    }
                    if let tile = run[RecordPillTile.self] { tiles.append((tile.kind, rect)) }
                } else if let current = open {
                    capsules.append(current.rect)
                    open = nil
                }
            }
            if let current = open { capsules.append(current.rect) }
            for rect in capsules {
                let capsule = rect.insetBy(dx: 0, dy: RecordPillStyle.lineInset)
                context.fill(Capsule().path(in: capsule), with: .style(RecordPillStyle.fill))
            }
            for tile in tiles {
                let side = tile.rect.height - 2 * (RecordPillStyle.lineInset + RecordPillStyle.tileInset)
                let square = CGRect(x: tile.rect.midX - side / 2, y: tile.rect.midY - side / 2, width: side, height: side)
                KindGlyph.draw(kind: tile.kind, in: square, context: &context)
            }
            context.draw(line)
        }
    }
}
#endif
