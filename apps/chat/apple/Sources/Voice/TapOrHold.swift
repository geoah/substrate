import SwiftUI

/// A control that a tap and a hold each act on: the microphone. A press
/// that lasts `TapOrHold.after` is a hold, which begins then and ends when
/// it is let go; a shorter one is a tap, done when it is let go. Where
/// `holds` is false a press of any length is a tap.
///
/// The view it is on must stay the same view for the whole press: one
/// replaced mid-press never hears the release.
struct TapOrHold: ViewModifier {
    /// How long a press lasts before it is a hold.
    static let after: Duration = .milliseconds(300)

    var holds: Bool
    var tap: () -> Void
    var holdBegan: () -> Void
    var holdEnded: () -> Void

    @State private var pressed = false
    @State private var holding = false
    @State private var wait: Task<Void, Never>?

    func body(content: Content) -> some View {
        content
            .contentShape(.circle)
            .gesture(
                DragGesture(minimumDistance: 0)
                    .onChanged { _ in
                        guard !pressed else { return }
                        pressed = true
                        guard holds else { return }
                        wait = Task { @MainActor in
                            try? await Task.sleep(for: Self.after)
                            guard !Task.isCancelled, pressed else { return }
                            holding = true
                            holdBegan()
                        }
                    }
                    .onEnded { _ in
                        wait?.cancel()
                        wait = nil
                        pressed = false
                        if holding {
                            holding = false
                            holdEnded()
                        } else {
                            tap()
                        }
                    }
            )
            .sensoryFeedback(.start, trigger: holding) { _, now in now }
            .sensoryFeedback(.stop, trigger: holding) { was, _ in was }
            // A screen reader's double tap is the tap; the hold is the
            // tap's twin in the settings.
            .accessibilityAddTraits(.isButton)
            .accessibilityAction { tap() }
    }
}

extension View {
    /// The microphone's tap and hold (`TapOrHold`).
    func tapOrHold(holds: Bool, tap: @escaping () -> Void, holdBegan: @escaping () -> Void, holdEnded: @escaping () -> Void) -> some View {
        modifier(TapOrHold(holds: holds, tap: tap, holdBegan: holdBegan, holdEnded: holdEnded))
    }
}
