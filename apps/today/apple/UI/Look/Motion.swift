import SwiftUI

/// How the day moves (proposal 098, `DESIGN-GUIDE.md`, "Motion"). Nothing
/// is swapped for something else: a panel that opens, closes, arrives or
/// leaves is seen to do it, and what stands around it moves to make room.
/// That holds for a change the day makes by itself (the current item is
/// finished and the next takes its place) as for one a tap makes.
///
/// Everything that moves, moves on one curve, so two things that change
/// together arrive together. What leaves is gone early in it, and what
/// arrives is drawn late, so one thing is never read through another. With
/// Reduce Motion the curve is shorter and has no spring, and what arrives
/// only fades.
enum Motion {
    /// A panel opens, closes, arrives or leaves, and the day closes up.
    static let panel = Animation.spring(duration: 0.36, bounce: 0.1)
    /// The same with Reduce Motion.
    static let still = Animation.easeInOut(duration: 0.18)

    static func panel(still reduced: Bool) -> Animation { reduced ? still : panel }

    /// What leaves is gone while the day still moves into its place.
    static let gone = Animation.easeIn(duration: 0.11)
    /// What arrives is drawn once there is room for it.
    static let seen = Animation.easeOut(duration: 0.22).delay(0.07)
    /// What opens under a head is drawn once its panel has grown over most
    /// of it: the panel does not cut what it holds, so this keeps it from
    /// being seen below the panel's edge, over what is moving out of its way.
    static let under = Animation.easeOut(duration: 0.2).delay(0.12)
}

/// How one thing stands while it arrives or leaves: faded, and unless
/// motion is reduced a little out of place.
private struct Arriving: ViewModifier {
    var shown: Bool
    /// From where it comes, in points: under a head it comes down from it.
    var rise: CGFloat = 0
    /// How small it starts: a panel of its own grows a little into place.
    var scale: CGFloat = 1

    @Environment(\.accessibilityReduceMotion) private var still

    func body(content: Content) -> some View {
        content
            .opacity(shown ? 1 : 0)
            .scaleEffect(shown || still ? 1 : scale, anchor: .top)
            .offset(y: shown || still ? 0 : rise)
    }
}

extension AnyTransition {
    /// Each of these fades on its own clock while its place moves on the
    /// day's: what leaves is gone early (`Motion.gone`), and what arrives
    /// is drawn late (`Motion.seen`).
    private static func timed(_ transition: AnyTransition) -> AnyTransition {
        .asymmetric(insertion: transition.animation(Motion.seen), removal: transition.animation(Motion.gone))
    }

    /// A thing that arrives faded and a little out of place (`Arriving`).
    /// It is a pair of plain modifiers, which leave the thing's own layout
    /// as it is: a `Transition` type measures a text of several lines at
    /// another width than it is drawn at, and what stands under it is then
    /// drawn over its last line.
    private static func arriving(rise: CGFloat = 0, scale: CGFloat = 1) -> AnyTransition {
        .modifier(active: Arriving(shown: false, rise: rise, scale: scale), identity: Arriving(shown: true, rise: rise, scale: scale))
    }

    /// Under a head, inside its panel: it fades in a little late and from
    /// just above, while the panel grows over it, and is gone early when
    /// the panel closes, so it is never seen over what moves under it.
    static var unfold: AnyTransition {
        let under = arriving(rise: -6)
        return .asymmetric(insertion: under.animation(Motion.under), removal: under.animation(Motion.gone))
    }

    /// A panel that arrives in the day or leaves it by itself: a row that
    /// was added, one that was finished, a card the assistant puts there.
    static var arrive: AnyTransition { timed(arriving(scale: 0.97)) }

    /// One face of a panel in the place of another, on the same surface (a
    /// closed line and the open card): the old one is gone before the new
    /// one is there, so the two are never read over each other.
    static var face: AnyTransition { timed(.opacity) }
}
