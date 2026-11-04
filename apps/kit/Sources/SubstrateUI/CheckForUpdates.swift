#if canImport(SwiftUI)
import SubstrateAuth
import SubstrateRuntime
import SubstrateScreens
import SwiftUI

public extension EnvironmentValues {
    /// The updater's own check, which the scene that holds the updater runs:
    /// About's Check for Updates.
    @Entry var checkForUpdates: @MainActor () -> Void = {}
}
#endif
