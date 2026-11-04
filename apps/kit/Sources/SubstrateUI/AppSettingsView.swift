#if canImport(SwiftUI)
import SubstrateAuth
import SubstrateRuntime
import SubstrateScreens
import SwiftUI

/// Settings as one screen: the app's sections, then the link's status and
/// repository, then storage and about (`Configuration.settingsScreen`), each
/// row's command to whatever answers it.
public struct AppSettingsView<App: LinkedApp>: View {
    @Environment(AppShell<App>.self) private var shell
    @Environment(\.checkForUpdates) private var checkForUpdates

    public init() {}

    public var body: some View {
        if let screen = shell.settingsScreen {
            SettingsScreen(screen) { shell.perform($0, checkForUpdates: checkForUpdates) }
                .navigationTitle(screen.title)
        }
    }
}
#endif
