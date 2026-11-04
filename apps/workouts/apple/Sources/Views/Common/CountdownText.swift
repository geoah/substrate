import SwiftUI
import UIKit

/// A countdown to `end` that ticks with no app code. The range is clamped so
/// an end in the past never makes an invalid range.
struct CountdownText: View {
    let start: Date
    let end: Date

    var body: some View {
        let lower = min(start, end)
        Text(timerInterval: lower ... max(lower, end), countsDown: true)
            .monospacedDigit()
    }
}
