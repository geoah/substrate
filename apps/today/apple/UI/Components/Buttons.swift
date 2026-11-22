import SubstrateUI
import SwiftUI
import TodayCore

/// Anything that does something: a filled, tinted or outlined capsule with
/// a target of at least 44 points. There are no bare words that act, and a
/// choice is never a button.
struct TodayButtonStyle: ButtonStyle {
    enum Kind {
        /// Filled in ink: Done, Taken, Join, Save. One on a card.
        case primary
        /// The primary button across the card: the one action of a save
        /// card or the plan's row.
        case wide
        /// Tinted: Got it, Send, Open them again.
        case secondary
        /// Tinted on a tinted box, where the quiet fill would not show:
        /// what the assistant can do, inside its box.
        case onTint
        /// Outlined: Not now.
        case outlined
        /// Outlined at the wide button's height, beside it: Skip.
        case beside
        /// Tinted and lower, with the same target: All projects, Open.
        case small
        /// Outlined in the urgent red: Remove.
        case destructive
    }

    var kind: Kind = .secondary
    /// The button is a countdown and the way back: how much of the wait is
    /// left, from 1 to 0. It keeps its size and is drawn as a quiet fill
    /// with a darker one draining out of it, so its word reads on both.
    /// Nil for a button that is not counting.
    var left: Double?
    @Environment(\.isEnabled) private var enabled
    @Environment(\.colorScheme) private var scheme

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(kind == .wide || kind == .beside ? TodayLook.Text.wideButton : TodayLook.Text.button)
            .multilineTextAlignment(.center)
            .foregroundStyle(left == nil ? words : .ink)
            .padding(.horizontal, kind == .small ? 13 : kind == .beside ? 20 : 16)
            .padding(.vertical, 6)
            .frame(maxWidth: kind == .wide ? .infinity : nil, minHeight: height)
            .background {
                if let left { drain(left) } else { fill }
            }
            .opacity(enabled ? (configuration.isPressed ? 0.7 : 1) : 0.35)
            .touchTarget()
    }

    /// The countdown: the quiet fill, and over it a darker one whose
    /// trailing edge runs back to the start as the wait runs out.
    private func drain(_ left: Double) -> some View {
        let ink = TodayLook.ink.color
        return Capsule().fill(ink.opacity(scheme == .dark ? 0.12 : 0.07))
            .overlay {
                Capsule().fill(ink.opacity(scheme == .dark ? 0.30 : 0.20))
                    .mask(alignment: .leading) { Rectangle().scaleEffect(x: max(0, min(1, left)), anchor: .leading) }
            }
    }

    private var height: CGFloat {
        switch kind {
        case .wide, .beside: 52
        case .small: 34
        default: TodayLook.target
        }
    }

    private var words: Color {
        switch kind {
        case .primary, .wide: .onAction
        case .secondary, .onTint, .outlined, .beside, .small: .ink
        case .destructive: .urgentWords
        }
    }

    @ViewBuilder private var fill: some View {
        let shape = Capsule()
        switch kind {
        case .primary, .wide: shape.fill(.action)
        case .secondary, .small: shape.fill(TodayLook.ink.color.opacity(scheme == .dark ? 0.12 : 0.07))
        case .onTint:
            shape.fill(.card).shadow(color: .black.opacity(0.08), radius: 1, y: 0.5)
        case .outlined, .beside: CapsuleOutline().strokeBorder(TodayLook.ink.color.opacity(0.28), lineWidth: 1.5)
        case .destructive: CapsuleOutline().strokeBorder(TodayLook.urgent.color.opacity(0.55), lineWidth: 1.5)
        }
    }
}

/// A capsule drawn as a path of its own: an outlined button's shape. The
/// system's capsule, stroked, is drawn with stray marks at its ends where a
/// view is cached offscreen (a gallery's picture), and a path is not.
struct CapsuleOutline: InsettableShape {
    var inset: CGFloat = 0

    func path(in rect: CGRect) -> Path {
        let rect = rect.insetBy(dx: inset, dy: inset)
        let radius = min(rect.height, rect.width) / 2
        var path = Path()
        path.move(to: CGPoint(x: rect.minX + radius, y: rect.minY))
        path.addLine(to: CGPoint(x: rect.maxX - radius, y: rect.minY))
        path.addArc(
            center: CGPoint(x: rect.maxX - radius, y: rect.midY), radius: radius, startAngle: .degrees(-90), endAngle: .degrees(90),
            clockwise: false)
        path.addLine(to: CGPoint(x: rect.minX + radius, y: rect.maxY))
        path.addArc(
            center: CGPoint(x: rect.minX + radius, y: rect.midY), radius: radius, startAngle: .degrees(90), endAngle: .degrees(270),
            clockwise: false)
        path.closeSubpath()
        return path
    }

    func inset(by amount: CGFloat) -> CapsuleOutline { CapsuleOutline(inset: inset + amount) }
}

extension ButtonStyle where Self == TodayButtonStyle {
    static func today(_ kind: TodayButtonStyle.Kind) -> TodayButtonStyle { TodayButtonStyle(kind: kind) }
    static func today(_ kind: TodayButtonStyle.Kind, left: Double) -> TodayButtonStyle { TodayButtonStyle(kind: kind, left: left) }
}

/// A button's word with the glyph of what it does before it, or the right
/// chevron after it when it opens a page.
struct ButtonWord: View {
    var text: String
    var glyph: Glyph?
    var opensPage = false

    var body: some View {
        HStack(spacing: 6) {
            if let glyph { Image(systemName: glyph.systemName).font(TodayLook.font(.footnote, weight: .bold)) }
            Text(text)
            if opensPage { Image(systemName: Symbol.page.systemName).font(TodayLook.font(.caption, weight: .bold)) }
        }
    }
}

/// One glyph in a circle, 36 points or 32, with a 44 point target: an icon
/// acts only inside a shape.
struct IconButton: View {
    enum Look {
        /// Tinted: edit, back, add, close, the flag control.
        case quiet
        /// Filled in ink: dictate, send.
        case filled
        /// Open: the flag control while its list is shown.
        case pressed
    }

    var symbol: String
    /// What it does, for VoiceOver: the icon has no word of its own.
    var label: String
    var small = false
    var look: Look = .quiet
    var action: () -> Void

    @Environment(\.isEnabled) private var enabled
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: small ? 14 : 16, weight: .semibold))
                .foregroundStyle(look == .filled ? AnyShapeStyle(.onAction) : AnyShapeStyle(.ink))
                .frame(width: side, height: side)
                .background(Circle().fill(fill))
                .frame(minWidth: TodayLook.target, minHeight: TodayLook.target)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .opacity(enabled ? 1 : 0.35)
        .accessibilityLabel(label)
    }

    init(_ symbol: Symbol, label: String, small: Bool = false, look: Look = .quiet, action: @escaping () -> Void) {
        self.init(systemName: symbol.systemName, label: label, small: small, look: look, action: action)
    }

    init(_ glyph: Glyph, label: String, small: Bool = false, look: Look = .quiet, action: @escaping () -> Void) {
        self.init(systemName: glyph.systemName, label: label, small: small, look: look, action: action)
    }

    init(systemName: String, label: String, small: Bool = false, look: Look = .quiet, action: @escaping () -> Void) {
        symbol = systemName
        self.label = label
        self.small = small
        self.look = look
        self.action = action
    }

    private var side: CGFloat { small ? 32 : 36 }

    private var fill: Color {
        switch look {
        case .quiet: TodayLook.ink.color.opacity(scheme == .dark ? 0.12 : 0.07)
        case .filled: .action
        case .pressed: TodayLook.ink.color.opacity(scheme == .dark ? 0.24 : 0.16)
        }
    }
}
