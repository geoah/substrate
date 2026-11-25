#if canImport(SwiftUI)
import SubstrateScreens
import SubstrateUI
import SwiftUI

/// What the chat's Settings reads of the app's updater. The updater is
/// compiled into the app target (Sparkle on the Mac), so a library cannot
/// name it: the app's scene hands this in from the one it holds
/// (`chatUpdates(_:)`), and Settings reads it from the environment.
public struct ChatUpdates {
    /// This build as its release stamped it. `channel` is the stamp as it
    /// is, empty for a build nobody released.
    public var build: BuildFacts
    /// The build reads a feed: About says its channel and can check it, and
    /// Settings offers pre-releases.
    public var enabled: Bool
    /// Pre-releases are offered too.
    public var prereleases: Binding<Bool>
    /// The check somebody asked for.
    public var checkNow: @MainActor () -> Void

    public init(build: BuildFacts, enabled: Bool, prereleases: Binding<Bool>, checkNow: @escaping @MainActor () -> Void) {
        self.build = build
        self.enabled = enabled
        self.prereleases = prereleases
        self.checkNow = checkNow
    }
}

extension EnvironmentValues {
    /// A build that reads no feed, where no scene set one: a gallery's
    /// picture, a test.
    @Entry var chatUpdates = ChatUpdates(build: .bundle(), enabled: false, prereleases: .constant(false), checkNow: {})
}

public extension View {
    /// The updater the scene holds, for the chat's Settings under this view.
    func chatUpdates(_ updates: ChatUpdates) -> some View { environment(\.chatUpdates, updates) }
}
#endif
