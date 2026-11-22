import SwiftUI
import TodayCore

extension Ground {
    /// The ground as a soft gradient, from its colour at the top down to
    /// near white (near black in dark mode). The closing ground is dark in
    /// both modes.
    func stops(_ scheme: ColorScheme) -> [Gradient.Stop] {
        let (top, middle, bottom, at): (UInt32, UInt32, UInt32, Double) =
            switch (self, scheme) {
            case (.closing, _): (0x1C1730, 0x14131C, 0x0E0F13, 0.50)
            case (.waking, .dark): (0x4A3324, 0x2A211B, 0x121214, 0.45)
            case (.planning, .dark): (0x4A3B1C, 0x2A2418, 0x121214, 0.45)
            case (.working, .dark): (0x1F3247, 0x182230, 0x101216, 0.45)
            case (.meeting, .dark): (0x35294A, 0x221C2E, 0x121116, 0.42)
            case (.urgent, .dark): (0x5A2A1D, 0x301C17, 0x141112, 0.45)
            case (.waking, _): (0xF8D3B4, 0xFAE6D5, 0xF8F1EA, 0.45)
            case (.planning, _): (0xF7D9A6, 0xF8E7CC, 0xF6EFE6, 0.45)
            case (.working, _): (0xD5E6F7, 0xEAF1F9, 0xF7F8FA, 0.45)
            case (.meeting, _): (0xCDBCE0, 0xE0D2EA, 0xEFE5EE, 0.42)
            case (.urgent, _): (0xF6B7A3, 0xF9D9CC, 0xF8EEE8, 0.45)
            }
        return [
            .init(color: Color(hex: top), location: 0), .init(color: Color(hex: middle), location: at),
            .init(color: Color(hex: bottom), location: 1),
        ]
    }

    /// The scheme everything on this ground is drawn in: cards are dark on
    /// the closing ground whatever the system's mode.
    func scheme(in system: ColorScheme) -> ColorScheme { self == .closing ? .dark : system }
}

extension Color {
    init(hex: UInt32) {
        self.init(
            .sRGB, red: Double((hex >> 16) & 0xFF) / 255, green: Double((hex >> 8) & 0xFF) / 255, blue: Double(hex & 0xFF) / 255, opacity: 1)
    }
}

/// The colour behind the pane and every page: what the moment is. It is
/// never the only sign of a moment: the card on it says the same.
struct GroundView: View {
    var ground: Ground
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        LinearGradient(stops: ground.stops(scheme), startPoint: .top, endPoint: .bottom)
            .ignoresSafeArea()
    }
}

extension Ground {
    /// The ground's colour at the top of the screen, and at its bottom.
    func top(_ scheme: ColorScheme) -> Color { stops(scheme).first?.color ?? .clear }
    func bottom(_ scheme: ColorScheme) -> Color { stops(scheme).last?.color ?? .clear }
}

/// A clean edge where the day scrolls under the status bar or under what is
/// fixed at the bottom: the ground's own colour, whole at the screen's edge
/// and fading out toward the content. Without it the lines scrolling past
/// collide with the clock, and read as garbled glass under the field.
struct EdgeFade: ViewModifier {
    enum Edge {
        case top
        case bottom
    }

    var edge: Edge
    /// How far the colour takes to fade out, past the bar it covers.
    static let reach: CGFloat = 18
    @Environment(\.ground) private var ground
    @Environment(\.colorScheme) private var scheme

    func body(content: Content) -> some View {
        switch edge {
        case .top:
            // Over the content: whole under the status bar, gone a little below it.
            content.overlay(alignment: .top) {
                GeometryReader { proxy in
                    let inset = proxy.safeAreaInsets.top
                    let colour = ground.top(scheme)
                    LinearGradient(
                        stops: [
                            .init(color: colour, location: 0), .init(color: colour, location: inset / (inset + Self.reach)),
                            .init(color: colour.opacity(0), location: 1),
                        ], startPoint: .top, endPoint: .bottom
                    )
                    .frame(height: inset + Self.reach)
                    .offset(y: -inset)
                }
                .allowsHitTesting(false)
                .accessibilityHidden(true)
            }
        case .bottom:
            // Behind what is fixed there: whole under it and down to the screen's edge, fading in just above it.
            content.background {
                let colour = ground.bottom(scheme)
                LinearGradient(
                    stops: [
                        .init(color: colour.opacity(0), location: 0), .init(color: colour, location: 0.3), .init(color: colour, location: 1),
                    ], startPoint: .top, endPoint: .bottom
                )
                .padding(.top, -Self.reach)
                .ignoresSafeArea(edges: .bottom)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
            }
        }
    }
}

extension View {
    func edgeFade(_ edge: EdgeFade.Edge) -> some View { modifier(EdgeFade(edge: edge)) }
}

/// Room at the top of the day for the controls the system draws in the top
/// leading corner of an iPad's window, which would sit over the strip's
/// title. The system says where they are only to a view laid out in that
/// corner: a line there is narrowed by them, and that is how the day knows
/// to start under them. Nothing on an iPhone, or with the window filling
/// the screen.
struct WindowControlsRoom: View {
    @State private var whole: CGFloat = 0
    @State private var clear: CGFloat = 0
    /// The controls' height, with the gap the system leaves above them.
    private static let room: CGFloat = 30

    var body: some View {
        #if os(iOS)
        Color.clear
            .frame(maxWidth: .infinity)
            .frame(height: clear < whole - 1 ? Self.room : 0)
            .onGeometryChange(for: CGFloat.self) {
                $0.size.width
            } action: {
                whole = $0
            }
            .overlay(alignment: .top) {
                Color.clear
                    .frame(maxWidth: .infinity)
                    .frame(height: 1)
                    .onGeometryChange(for: CGFloat.self) {
                        $0.size.width
                    } action: {
                        clear = $0
                    }
                    .containerCornerOffset(.leading, sizeToFit: true)
            }
            .groundBar(clear < whole - 1)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
        #else
        Color.clear.frame(height: 0)
        #endif
    }
}

/// What is fixed at the top stands on the ground itself: what scrolls
/// under it fades into the ground's colour, so nothing shows through it.
struct GroundBar: ViewModifier {
    var shown = true
    @Environment(\.ground) private var ground
    @Environment(\.colorScheme) private var scheme

    func body(content: Content) -> some View {
        content.background {
            if shown {
                let colour = ground.top(scheme)
                LinearGradient(
                    stops: [.init(color: colour, location: 0), .init(color: colour, location: 0.82), .init(color: colour.opacity(0), location: 1)],
                    startPoint: .top, endPoint: .bottom
                )
                .padding(.bottom, -EdgeFade.reach)
                .ignoresSafeArea(edges: .top)
                .allowsHitTesting(false)
            }
        }
    }
}

extension View {
    func groundBar(_ shown: Bool = true) -> some View { modifier(GroundBar(shown: shown)) }
}

/// A card: white on every ground (dark on the closing one), rounded, with
/// its inset and a soft shadow.
struct CardSurface: ViewModifier {
    var radius = TodayLook.cardRadius
    var inset = TodayLook.inset
    /// The urgent card: its fill and outline are the urgent red's.
    var urgent = false
    @Environment(\.colorScheme) private var scheme

    func body(content: Content) -> some View {
        let shape = RoundedRectangle(cornerRadius: radius, style: .continuous)
        content
            .padding(inset)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background {
                if urgent {
                    shape.fill(.urgentFill).overlay(shape.strokeBorder(.urgent, lineWidth: 1.5))
                } else {
                    shape.fill(.card)
                }
            }
            .cardShadow()
    }
}

/// A surface over the ground that is not a card: a list of rows, a closed
/// line.
struct SoftSurface: ViewModifier {
    var radius = TodayLook.listRadius
    @Environment(\.colorScheme) private var scheme

    func body(content: Content) -> some View {
        content
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(TodayLook.soft(scheme), in: RoundedRectangle(cornerRadius: radius, style: .continuous))
    }
}

/// Where a closed line stands among the lines it shares a surface with: a
/// run of closed rows reads as one list, square where two of them meet.
struct Joins: Equatable, Sendable {
    /// A line of the same run stands directly above it.
    var above = false
    var below = false
}

/// The surface of a thing of the day that is a line while it is closed and
/// a card while it is open. It is one surface whose shape, fill and shadow
/// change, so opening and closing are seen to happen and nothing is put in
/// the place of something else. It does not cut what it holds: what opens
/// under a head waits until the surface has grown over it (`Motion.under`).
/// The shadow is the surface's own, so nothing on a card casts one.
struct PanelSurface: ViewModifier {
    enum Kind: Equatable {
        /// Closed: the soft surface of a list, square where it joins the
        /// line above or the line below.
        case line(Joins)
        /// Open: a card on the ground. The urgent card's fill and outline
        /// are the urgent red's.
        case card(urgent: Bool = false)
    }

    var kind: Kind
    /// A closed line's corners where it joins nothing.
    var radius = TodayLook.listRadius
    @Environment(\.colorScheme) private var scheme

    func body(content: Content) -> some View {
        let shape = UnevenRoundedRectangle(cornerRadii: radii, style: .continuous)
        content
            .frame(maxWidth: .infinity, alignment: .topLeading)
            .background {
                shape.fill(fill)
                    .overlay(shape.strokeBorder(Color.urgent.opacity(kind == .card(urgent: true) ? 1 : 0), lineWidth: 1.5))
                    .cardShadow(shown: isCard)
            }
    }

    private var isCard: Bool {
        if case .card = kind { true } else { false }
    }

    private var radii: RectangleCornerRadii {
        switch kind {
        case .card:
            return RectangleCornerRadii(
                topLeading: TodayLook.cardRadius, bottomLeading: TodayLook.cardRadius, bottomTrailing: TodayLook.cardRadius,
                topTrailing: TodayLook.cardRadius)
        case let .line(joins):
            let top = joins.above ? 0 : radius
            let bottom = joins.below ? 0 : radius
            return RectangleCornerRadii(topLeading: top, bottomLeading: bottom, bottomTrailing: bottom, topTrailing: top)
        }
    }

    private var fill: Color {
        switch kind {
        case .card(urgent: true): .urgentFill
        case .card: .card
        case .line: TodayLook.soft(scheme)
        }
    }
}

/// The control layer: the strip on top of the day and the field at its
/// bottom. Glass where the system draws it, a translucent fill where it
/// cannot (a gallery's offscreen window).
struct GlassSurface<S: InsettableShape>: ViewModifier {
    var shape: S
    /// The field, which what scrolls under must not show through.
    var dense = false
    @Environment(\.colorScheme) private var scheme
    @Environment(\.drawsGlass) private var drawsGlass

    func body(content: Content) -> some View {
        if drawsGlass {
            content.glassEffect(.regular, in: shape)
        } else {
            content
                .background(TodayLook.glass(scheme).opacity(dense ? 1.28 : 1), in: shape)
                .overlay(shape.strokeBorder(Color.white.opacity(scheme == .dark ? 0.12 : 0.85), lineWidth: 0.5))
                .cardShadow()
        }
    }
}

extension View {
    func card(urgent: Bool = false) -> some View { modifier(CardSurface(urgent: urgent)) }
    func soft(radius: CGFloat = TodayLook.listRadius) -> some View { modifier(SoftSurface(radius: radius)) }
    func glass(in shape: some InsettableShape, dense: Bool = false) -> some View { modifier(GlassSurface(shape: shape, dense: dense)) }
    /// A line while closed, a card while open, on one surface (`PanelSurface`).
    func panel(_ kind: PanelSurface.Kind, radius: CGFloat = TodayLook.listRadius) -> some View {
        modifier(PanelSurface(kind: kind, radius: radius))
    }

    /// The hairline between two rows of a list.
    func rowLine(_ show: Bool = true) -> some View { modifier(RowLine(show: show)) }
}

struct RowLine: ViewModifier {
    var show: Bool
    @Environment(\.colorScheme) private var scheme

    func body(content: Content) -> some View {
        content.overlay(alignment: .top) {
            if show { Rectangle().fill(TodayLook.line(scheme)).frame(height: 0.5).padding(.horizontal, 8) }
        }
    }
}

/// A section's label on a page: plain words above a group of rows.
struct SectionLabel: View {
    var text: String

    init(_ text: String) { self.text = text }

    var body: some View {
        Text(text)
            .font(TodayLook.Text.sectionLabel)
            .foregroundStyle(.inkSecond)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }
}
