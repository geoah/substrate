import SwiftUI
import UIKit

/// The session clock: elapsed time that ticks with no app code, or the
/// frozen time while paused.
struct SessionClockText: View {
    let clockStart: Date
    let pausedAt: Date?

    var body: some View {
        Text(timerInterval: clockStart ... Date.distantFuture, pauseTime: pausedAt, countsDown: false)
            .monospacedDigit()
    }
}
