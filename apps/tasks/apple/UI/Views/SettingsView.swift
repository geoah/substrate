import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// Settings: the app's reminders and lists, then the kit's status,
/// repository, storage and about, as one presentation; each row's command
/// goes to the controller that answers it.
public struct SettingsView: View {
    @Environment(TasksShell.self) private var shell
    @Environment(\.checkForUpdates) private var checkForUpdates

    public init() {}

    public var body: some View {
        let link = shell.model.presentation
        if let app = link.app {
            SettingsScreen(TasksSettings.screen(app, link: link)) { command in
                if case .checkForUpdates = TasksSettings.route(command) {
                    checkForUpdates()
                } else {
                    shell.settings(command)
                }
            }
            .navigationTitle("Settings")
        }
    }
}

public extension EnvironmentValues {
    /// The updater's own check, which the app that holds the updater runs:
    /// About's Check for Updates.
    @Entry var checkForUpdates: @MainActor () -> Void = {}
}
