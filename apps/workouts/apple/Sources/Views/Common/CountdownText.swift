import SwiftUI
import UIKit
import WorkoutsCore

/// A countdown to `end` that ticks with no app code. The range is clamped so
/// an end in the past never makes an invalid range.
///
/// `Text(timerInterval:)` takes every point it is offered and draws its
/// digits at the leading edge of them, so a centred stack does not centre it
/// and a word after it drifts away. It is held to its own size here, as wide
/// as the longest time it can show, with the digits placed by `alignment`.
struct CountdownText: View {
    let start: Date
    let end: Date
    var alignment: Alignment = .center

    var body: some View {
        let lower = min(start, end)
        let upper = max(lower, end)
        TimerWidth(seconds: upper.timeIntervalSince(lower), alignment: alignment) {
            Text(timerInterval: lower ... upper, countsDown: true)
        }
    }
}

/// A ticking `Text(timerInterval:)` held to the width of the widest time a
/// span of `seconds` shows, so it sits beside its words and centres where
/// its stack does.
struct TimerWidth<Content: View>: View {
    let seconds: TimeInterval
    var alignment: Alignment = .center
    @ViewBuilder let content: Content

    var body: some View {
        ReservedWidth(template: Self.template(seconds), alignment: alignment) {
            content
        }
        .monospacedDigit()
        .fixedSize()
    }

    static func template(_ seconds: TimeInterval) -> String {
        seconds >= 3600 ? "0:00:00" : seconds >= 600 ? "00:00" : "0:00"
    }
}
