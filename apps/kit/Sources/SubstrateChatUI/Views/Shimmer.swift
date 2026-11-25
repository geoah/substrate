#if canImport(SwiftUI)
import SwiftUI

/// A gentle light passing over words that wait: "Thinking…", the rows that
/// stand in for the chats. Under Reduce Motion it never starts, and rests
/// off the words, where it draws nothing.
struct Shimmer: ViewModifier {
    @State private var passing = false

    func body(content: Content) -> some View {
        content
            .overlay {
                GeometryReader { geo in
                    // A band over a little more than half the words,
                    // from before their start to past their end.
                    LinearGradient(colors: [.clear, ChatColours.shimmer, .clear], startPoint: .leading, endPoint: .trailing)
                        .frame(width: geo.size.width * 0.6)
                        .offset(x: (passing ? 1 : -1) * geo.size.width * 1.4)
                        .blendMode(.plusLighter)
                }
                .mask(content)
                .allowsHitTesting(false)
            }
            .repeats(.sweep, running: $passing)
    }
}

extension View {
    func shimmer() -> some View { modifier(Shimmer()) }
}
#endif
