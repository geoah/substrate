import SwiftUI

/// A pulsing dot: a chat is running.
struct PulsingDot: View {
    /// The colour of what is working: an agent's.
    let color: Color
    @State private var on = false
    @Scaled(.dot) private var dot

    var body: some View {
        Circle()
            .fill(color)
            .frame(width: dot, height: dot)
            .opacity(on ? ChatColours.dimmed : 1)
            .repeats(.pulse, running: $on)
            .accessibilityLabel("Working")
    }
}
