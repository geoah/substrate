import Observation
import SwiftUI
import UIKit

/// The moments that get a haptic (DESIGN.md §9.3). No sound in the
/// foreground: the owner may be listening to music (04 §4.4).
enum HapticCue: Equatable, Sendable {
    /// A set ticked (the Table layout).
    case tick
    /// A set logged and journaled (the Rows layout's Log).
    case logged
    /// A stepper step or an RPE pick (the Rows layout): a light tick.
    case step
    /// A countdown that notifies ended while the session screen was showing.
    case countdownEnded
    /// A short pause, side switch or transition ended (no notification).
    case shortCountdownEnded
    /// A timed set reached its target.
    case targetReached
    /// The entry check asks (§5.7), or the engine refused a command.
    case warning
    /// Finish saved the session.
    case finishSaved

    var feedback: SensoryFeedback {
        switch self {
        case .tick: .impact(weight: .medium)
        case .step: .selection
        case .countdownEnded, .targetReached, .finishSaved, .logged: .success
        case .shortCountdownEnded: .impact(weight: .heavy)
        case .warning: .warning
        }
    }
}

/// The app's haptics, played by `sensoryFeedback` on the view the
/// `workoutsHaptics()` modifier sits on. The session controller, the
/// countdown coordinator and the views call `play(_:)`; nothing else touches
/// a feedback generator.
@MainActor
@Observable
final class Haptics {
    static let shared = Haptics()

    /// A cue with a sequence number, so the same cue twice in a row still
    /// changes the trigger.
    struct Played: Equatable {
        var cue: HapticCue
        var sequence: Int
    }

    private(set) var last: Played?

    /// Plays `cue` now, or never: a tick from the Lock Screen's Done runs
    /// with the app in the background, and must not buzz when the owner
    /// next opens it.
    func play(_ cue: HapticCue) {
        guard UIApplication.shared.applicationState == .active else { return }
        last = Played(cue: cue, sequence: (last?.sequence ?? 0) + 1)
    }
}

extension View {
    /// Plays `Haptics.shared`'s cues. Attach once, at the root of the
    /// window's content: it stays mounted while the session cover and its
    /// sheets come and go, so Finish's `.success` still plays as the cover
    /// closes.
    func workoutsHaptics() -> some View {
        modifier(HapticsPlayer(haptics: .shared))
    }
}

private struct HapticsPlayer: ViewModifier {
    let haptics: Haptics

    func body(content: Content) -> some View {
        content.sensoryFeedback(trigger: haptics.last) { _, played in
            played?.cue.feedback
        }
    }
}
