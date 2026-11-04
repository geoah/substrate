import SwiftUI

/// A pulsing dot: a chat is running.
struct PulsingDot: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var color: Color = .accentColor
    @State private var on = false

    var body: some View {
        Circle()
            .fill(color)
            .frame(width: 6, height: 6)
            .opacity(on ? 0.3 : 1)
            .onAppear {
                guard !reduceMotion else { return }
                withAnimation(.easeInOut(duration: 0.9).repeatForever()) { on = true }
            }
            .accessibilityLabel("Working")
    }
}
