import SubstrateUI
import SwiftUI

public extension View {
    /// About's Check for Updates runs `action`: the updater the scene holds.
    /// Compiled into each app's UI framework rather than declared in
    /// SubstrateUI, because the app target that holds the updater links no
    /// kit library and so can name only what its UI framework declares.
    func checkingForUpdates(_ action: @escaping @MainActor () -> Void) -> some View {
        environment(\.checkForUpdates, action)
    }
}
