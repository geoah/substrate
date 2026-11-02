import ActivityKit
import Foundation

/// The session's Live Activity, shared by the app (which starts and updates
/// it) and the widget extension (which draws it). This folder is compiled
/// into both targets (project.yml).
///
/// The widget links no WorkoutsKit, so every string here arrives preformatted
/// by the app. The content stays far below ActivityKit's 4 KB limit.
/// Nonisolated so ActivityKit and the countdown coordinator can encode and
/// compare it off the main actor.
nonisolated struct WorkoutsActivityAttributes: ActivityAttributes, Sendable {
    nonisolated struct ContentState: Codable, Hashable, Sendable {
        /// Session start plus every closed pause, so `Text(timerInterval:)`
        /// shows the elapsed session time.
        var clockStart: Date
        /// While the session clock is paused: `Text(timerInterval:pauseTime:)`.
        var clockPausedAt: Date?
        /// The Up next row's exercise, "Chest Press (Machine)".
        var exercise: String
        /// The Up next row's group label, "A1".
        var groupLabel: String?
        /// "Set 2: 35 kg × 14", "All sets done".
        var next: String
        /// The row Done completes (and, while a timed set runs, the row Stop
        /// stops: starting a timed set makes it Up next).
        var nextRowID: String?
        /// Done can tick that row as shown, or Stop can stop it; false when a
        /// value must be typed in the app first, so the widget shows neither.
        var canComplete: Bool = true
        var countdown: Countdown?
        var setsDone: Int
        var setsPlanned: Int
        /// "Saved: 46 min, 24 sets" after Finish.
        var finishedSummary: String?
        var revision: Int

        nonisolated struct Countdown: Codable, Hashable, Sendable {
            nonisolated enum Kind: String, Codable, Hashable, Sendable {
                case rest, roundRest, blockRest, transition, pause, sideSwitch, timedSet, manual
            }

            var kind: Kind
            /// "Rest", "Round rest", "Plank, set 1". Short: on the Lock Screen
            /// it sits above the countdown, between the buttons.
            var label: String
            var startedAt: Date
            /// Nil for a stopwatch.
            var endsAt: Date?
        }
    }

    var sessionID: String
    /// The routine name or "Training session".
    var title: String
}

extension WorkoutsActivityAttributes.ContentState {
    /// The `staleDate` every update carries: the countdown's end, so the
    /// widget flips to "Rest over" at that instant with no app code running
    /// (DESIGN.md §9.1). A finished session is never stale.
    nonisolated var staleDate: Date? {
        finishedSummary == nil ? countdown?.endsAt : nil
    }
}

/// The links the Live Activity opens, parsed by the app's deep-link handler.
nonisolated enum WorkoutsLinks {
    /// Opens the session cover scrolled to Up next.
    static let session = URL(string: "substrate-workouts://session")!
}
