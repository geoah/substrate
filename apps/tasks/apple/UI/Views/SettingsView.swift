import SubstrateUI
import SwiftUI
import TasksCore

/// Settings: the app's reminders and lists, then the kit's status,
/// repository, storage and about, as one presentation.
public struct SettingsView: View {
    public init() {}

    public var body: some View {
        AppSettingsView<TasksApp>()
            .environment(Tasks.shell)
    }
}

public extension View {
    /// About's Check for Updates runs `action`: the updater the scene holds.
    func checkingForUpdates(_ action: @escaping @MainActor () -> Void) -> some View {
        environment(\.checkForUpdates, action)
    }
}
