import SwiftUI

/// A kind's glyph: one stable symbol on one stable hue, so the same kind
/// looks the same on every surface and in the console (web/console's
/// kind-glyph.ts, whose table this mirrors with SF Symbols). The symbol
/// comes from the words of the kind's name, last word first; the hue from
/// the same words where they have one, else from a hash of the reference.
public struct KindGlyphSpec: Hashable, Sendable {
    public var symbol: String
    public var hue: KindHue

    public init(kind: String) {
        let name = KindReference(kind)?.name ?? kind
        let words = KindNames.split(name) ?? [name]
        let rule = words.reversed().lazy.compactMap { KindGlyphSpec.keywords[$0] }.first
        symbol = rule?.symbol ?? "shippingbox"
        hue = rule?.hue ?? KindHue.hash(kind)
    }

    private static let keywords: [String: (symbol: String, hue: KindHue?)] = [
        "task": ("checkmark.circle", .blue),
        "issue": ("checkmark.circle", .purple),
        "project": ("folder", .orange),
        "person": ("person", .teal),
        "user": ("person", .teal),
        "contact": ("at", .teal),
        "organization": ("building.2", .brown),
        "team": ("person.2", .green),
        "group": ("person.2", .green),
        "note": ("doc.text", .yellow),
        "document": ("doc.text", .yellow),
        "page": ("doc.text", .yellow),
        "file": ("doc.text", .yellow),
        "transcript": ("doc.text", .gray),
        "calendar": ("calendar", .red),
        "event": ("calendar", .red),
        "series": ("repeat", .gray),
        "email": ("envelope", .purple),
        "mail": ("envelope", .purple),
        "gmail": ("envelope", .red),
        "message": ("message", .purple),
        "conversation": ("message", .purple),
        "chat": ("message", .purple),
        "recipe": ("book", .green),
        "trip": ("globe", .pink),
        "label": ("tag", .gray),
        "account": ("gearshape", .gray),
        "config": ("gearshape", .gray),
        "setting": ("gearshape", .gray),
        "sync": ("gearshape", .gray),
        "log": ("clock.arrow.circlepath", .gray),
        "agent": ("sparkles", .purple),
        "bot": ("sparkles", .purple),
        "scratchpad": ("pencil", .gray),
    ]
}

/// The console's ten kind hues.
public enum KindHue: String, CaseIterable, Hashable, Sendable {
    case gray, brown, orange, yellow, green, teal, blue, purple, pink, red

    /// FNV-1a over the reference's UTF-16 units, as the console hashes it:
    /// stable across sessions, builds and surfaces.
    static func hash(_ reference: String) -> KindHue {
        var h: UInt32 = 0x811C_9DC5
        for unit in reference.utf16 {
            h ^= UInt32(unit)
            h = h &* 0x0100_0193
        }
        return allCases[Int(h % UInt32(allCases.count))]
    }

    public var color: Color {
        switch self {
        case .gray: .gray
        case .brown: .brown
        case .orange: .orange
        case .yellow: .yellow
        case .green: .green
        case .teal: .teal
        case .blue: .blue
        case .purple: .purple
        case .pink: .pink
        case .red: .red
        }
    }
}

/// The kind's tile: its symbol on its hue. Every surface that marks a kind,
/// or a record of one, draws this.
public struct KindGlyph: View {
    @Environment(\.colorScheme) private var scheme
    @ScaledMetric private var side: CGFloat
    private let spec: KindGlyphSpec

    public init(kind: String, size: CGFloat = 18) {
        spec = KindGlyphSpec(kind: kind)
        _side = ScaledMetric(wrappedValue: size)
    }

    public var body: some View {
        let hue = spec.hue.color
        Image(systemName: spec.symbol)
            .font(.system(size: side * 0.58, weight: .semibold))
            .foregroundStyle(scheme == .dark ? hue : hue.mix(with: .black, by: 0.3))
            .frame(width: side, height: side)
            .background(hue.opacity(scheme == .dark ? 0.28 : 0.18), in: .rect(cornerRadius: side * 0.26))
            .accessibilityHidden(true)
    }
}
