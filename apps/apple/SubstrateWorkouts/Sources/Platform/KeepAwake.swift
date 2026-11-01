import SwiftUI
import UIKit

/// Whether the session cover is on screen, and the screen kept awake while it
/// is (DESIGN.md §9.4):
/// `isIdleTimerDisabled = keepAwake && coverVisible && scenePhase == .active`,
/// re-evaluated when any of the three changes, and off when the cover closes.
///
/// The notification delegate and the countdown coordinator read `isVisible`
/// too: while the cover shows, the rest bar and a haptic say a countdown
/// ended, so the banner stays quiet (§9.2).
@MainActor
final class SessionScreen {
    static let shared = SessionScreen()

    /// The session cover is on screen.
    private(set) var isVisible = false
    private var keepAwake = true
    private var isActive = true

    /// The cover is on screen and the app in the foreground.
    var isShowing: Bool { isVisible && isActive }

    fileprivate func update(visible: Bool? = nil, keepAwake: Bool? = nil, phase: ScenePhase? = nil) {
        if let visible { isVisible = visible }
        if let keepAwake { self.keepAwake = keepAwake }
        if let phase { isActive = phase == .active }
        let disabled = self.keepAwake && isVisible && isActive
        if UIApplication.shared.isIdleTimerDisabled != disabled {
            UIApplication.shared.isIdleTimerDisabled = disabled
        }
    }
}

extension View {
    /// Attach to the session cover's content. While it is on screen it
    /// marks the cover visible (no banner when a countdown ends, §9.2) and,
    /// with `keepAwake` (the setting, on by default), keeps the screen from
    /// dimming; both stop when the cover closes.
    func sessionScreen(keepAwake: Bool) -> some View {
        modifier(SessionScreenPresence(keepAwake: keepAwake))
    }
}

private struct SessionScreenPresence: ViewModifier {
    let keepAwake: Bool
    @Environment(\.scenePhase) private var scenePhase

    func body(content: Content) -> some View {
        content
            .onAppear {
                SessionScreen.shared.update(visible: true, keepAwake: keepAwake, phase: scenePhase)
            }
            .onDisappear {
                SessionScreen.shared.update(visible: false)
            }
            .onChange(of: keepAwake) { _, keepAwake in
                SessionScreen.shared.update(keepAwake: keepAwake)
            }
            .onChange(of: scenePhase) { _, phase in
                SessionScreen.shared.update(phase: phase)
            }
    }
}
