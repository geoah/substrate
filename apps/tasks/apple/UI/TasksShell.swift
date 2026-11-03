import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import TasksCore

/// The app's shell: the kit's `AppShell` over `TasksApp`.
public typealias TasksShell = AppShell<TasksApp>

/// What Substrate Tasks hands its shell: its manifest and package, where it
/// keeps its session and folders, its root, and how a reminder's tap and a
/// settings row reach it. The app target reaches the shell through the calls
/// here alone: the kit's types are linked into this framework, never into
/// the app, which would hold a second copy of them.
public enum Tasks {
    @MainActor static let shell = TasksShell(configuration)

    /// The app came to the front.
    @MainActor public static func cameForward() { shell.cameForward() }

    /// File > New Task.
    @MainActor public static func newTask() { shell.send(.newTask) }

    /// The updater found a newer build, or none.
    @MainActor public static func updateOffered(_ version: String?) { shell.send(.offered(version)) }

    static var configuration: TasksShell.Configuration {
        TasksShell.Configuration(
            manifest: TasksManifest.current, keychainService: "dev.reamde.substrate.tasks", folder: "SubstrateTasks",
            packages: TasksManifest.packages, words: TasksApp.words(device:),
            app: { TasksApp(cache: $0.cache, outbox: $0.outbox, device: $0.device, build: $0.build) },
            refresh: .foreground, respond: Reminders.command(for:), settings: { .settings($0) }, clearSaved: .clearSaved,
            settingsScreen: TasksSettings.screen)
    }
}

extension AppShell where App == TasksApp {
    /// A command for the editor open under `editor`.
    func send(_ command: TaskEditor.Command, to editor: ChildKey) {
        model.send(command, to: Link.appPath + [editor])
    }
}

