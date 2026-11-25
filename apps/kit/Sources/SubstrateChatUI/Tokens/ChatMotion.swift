#if canImport(SwiftUI)
import SubstrateChatCore
import SwiftUI

#if canImport(UIKit)
import UIKit
#else
import AppKit
#endif

/// Everything that moves in the chat, in one place: a view says what kind
/// of motion it is and never an animation, and this says how that kind
/// moves and whether it moves at all. With Reduce Motion on nothing slides,
/// scales, pulses or sweeps: what would have moved is where it ends up,
/// and what repeats holds still.
enum ChatMotion {
    /// What a motion is.
    enum Kind: CaseIterable, Sendable {
        /// A thing taking its place: a row of the list when the order
        /// changes, the transcript going to its end, one screen for
        /// another.
        case place
        /// A thing folding open or shut: a tool's row, the model's
        /// thinking, a failure's details, the rest of a run of pills.
        case fold
        /// What the composer shows changing: Send for Stop, a line over
        /// the field, the field growing.
        case status
        /// A thing arriving over a screen and leaving it: the Undo, Jump
        /// to Latest, a strip.
        case arrive
        /// A level followed as it moves: the microphone's loudness.
        case level
        /// A dot that pulses while something works.
        case pulse
        /// The light that passes over words that wait.
        case sweep
        /// One step of the three dots before a reply begins.
        case beat

        /// It goes on for as long as its view shows: under Reduce Motion
        /// it is never started, where the others end where they would
        /// have.
        var repeats: Bool { self == .pulse || self == .sweep || self == .beat }

        var animation: Animation {
            switch self {
            case .place, .fold, .status, .arrive: .snappy(duration: ChatMotion.settle)
            case .level: .easeOut(duration: ChatMotion.instant)
            case .pulse: .easeInOut(duration: ChatMotion.breath).repeatForever()
            case .sweep: .linear(duration: ChatMotion.pass).repeatForever(autoreverses: false)
            case .beat: .easeInOut(duration: ChatMotion.quick)
            }
        }
    }

    // MARK: How long

    /// A level's step: under a frame or two, so it follows.
    static let instant = 0.08
    /// A small change in place: a dot lighting.
    static let quick = 0.25
    /// A thing settling where it belongs: every move that is not a loop.
    static let settle = 0.5
    /// One breath of a pulse, and one pass of the light over waiting
    /// words.
    static let breath = 0.9
    static let pass = 1.4
    /// Between two steps of the three dots.
    static let beat: Duration = .milliseconds(320)
    /// How long a control says it did what it does: Copy as a check.
    static let confirms: Duration = .seconds(1.5)

    // MARK: Whether

    /// The device asks for less motion.
    @MainActor static var reduced: Bool {
        if stilled { return true }
        #if canImport(UIKit)
        return UIAccessibility.isReduceMotionEnabled
        #else
        return NSWorkspace.shared.accessibilityDisplayShouldReduceMotion
        #endif
    }

    /// Set where a build is asked to hold still whatever the device says:
    /// a test, and a picture of the app as Reduce Motion draws it
    /// (`-SUBSTRATE_CHAT_STILL YES`).
    @MainActor static var stilled = UserDefaults.standard.bool(forKey: "SUBSTRATE_CHAT_STILL")

    /// How `kind` moves; nil where it does not.
    static func animation(_ kind: Kind, reduced: Bool) -> Animation? { reduced ? nil : kind.animation }

    /// Makes a change with the motion of its kind. Under Reduce Motion the
    /// change is made with none, and one that repeats is not made at all.
    @MainActor static func animate(_ kind: Kind, _ change: () -> Void) {
        if reduced {
            if !kind.repeats { change() }
        } else {
            withAnimation(kind.animation, change)
        }
    }

    /// Steps the three dots for as long as the task runs.
    @MainActor static func beats(_ step: @escaping @MainActor () -> Void) async {
        while !Task.isCancelled {
            try? await Task.sleep(for: beat)
            if !Task.isCancelled { animate(.beat, step) }
        }
    }

    /// How a thing comes onto a screen and leaves it.
    enum Arrival {
        /// It appears.
        case fade
        /// It grows from nothing: a button on glass.
        case grow
        /// It comes up from the screen's end.
        case rise

        var transition: AnyTransition {
            switch self {
            case .fade: .opacity
            case .grow: .scale.combined(with: .opacity)
            case .rise: .move(edge: .bottom).combined(with: .opacity)
            }
        }
    }

    /// How what a view holds changes into what it holds next.
    enum Change {
        /// One mark for another.
        case mark
        /// A number counting.
        case digits
        /// Words for other words.
        case words

        var transition: ContentTransition {
            switch self {
            case .mark: .symbolEffect(.replace)
            case .digits: .numericText()
            case .words: .opacity
            }
        }
    }
}

private struct Moves<Value: Equatable>: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduce
    let kind: ChatMotion.Kind
    let value: Value

    func body(content: Content) -> some View {
        content.animation(ChatMotion.animation(kind, reduced: reduce || ChatMotion.stilled), value: value)
    }
}

private struct Arrives: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduce
    let arrival: ChatMotion.Arrival

    func body(content: Content) -> some View {
        // With less motion a thing appears, whatever else is moving.
        content.transition(reduce || ChatMotion.stilled ? .identity : arrival.transition)
    }
}

private struct Changes: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduce
    let change: ChatMotion.Change

    func body(content: Content) -> some View {
        content.contentTransition(reduce || ChatMotion.stilled ? .identity : change.transition)
    }
}

private struct Repeats: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduce
    let kind: ChatMotion.Kind
    @Binding var running: Bool

    func body(content: Content) -> some View {
        content
            .onAppear { start() }
            .onChange(of: reduce) { _, _ in start() }
    }

    private func start() {
        if reduce || ChatMotion.stilled {
            // Held still: back where it rests, with no motion.
            var still = Transaction()
            still.disablesAnimations = true
            withTransaction(still) { running = false }
        } else {
            withAnimation(kind.animation) { running = true }
        }
    }
}

private struct Pulses: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduce
    let active: Bool

    func body(content: Content) -> some View {
        content.symbolEffect(.pulse, isActive: active && !(reduce || ChatMotion.stilled))
    }
}

/// A reply's words are revealed at a steady rate, which is motion: the
/// chats are told to pace them unless the device asks for less. Said again
/// at each sign-in, which starts the chats over.
private struct PacesReplies: ViewModifier {
    @Environment(ChatHost.self) private var host
    @Environment(\.accessibilityReduceMotion) private var reduce

    func body(content: Content) -> some View {
        let paced = !(reduce || ChatMotion.stilled)
        content.task(id: [paced, host.chats?.signedIn == true]) { host.send(.pace(paced)) }
    }
}

extension View {
    /// What changes with `value` under this view moves as `kind` does.
    func motion(_ kind: ChatMotion.Kind, value: some Equatable) -> some View { modifier(Moves(kind: kind, value: value)) }

    /// How this view comes and goes.
    func arrives(_ arrival: ChatMotion.Arrival = .fade) -> some View { modifier(Arrives(arrival: arrival)) }

    /// How what this view holds changes into what it holds next.
    func changes(_ change: ChatMotion.Change) -> some View { modifier(Changes(change: change)) }

    /// A motion that repeats for as long as this view shows: `running`
    /// turns true with it, and stays false while the device asks for
    /// less.
    func repeats(_ kind: ChatMotion.Kind, running: Binding<Bool>) -> some View { modifier(Repeats(kind: kind, running: running)) }

    /// A mark that pulses while something works.
    func pulses(while active: Bool) -> some View { modifier(Pulses(active: active)) }

    /// Replies under this view are paced as the device allows.
    func pacesReplies() -> some View { modifier(PacesReplies()) }
}
#endif
