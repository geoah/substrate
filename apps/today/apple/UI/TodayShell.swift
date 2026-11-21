import SubstrateScreens
import SubstrateUI
import TodayCore

/// The app's shell: the kit's `AppShell` over `TodayApp`.
public typealias TodayShell = AppShell<TodayApp>

/// What Substrate Today hands its shell: its manifest and package, where
/// it keeps its session and folders (the kit's layout: one folder per
/// repository in `Application Support/SubstrateToday`; an app that kept
/// its files elsewhere before the kit passes `parts:` instead), its root and
/// its Settings. The words are made once, by `words`, and handed to the root
/// in its parts. The app
/// target reaches the shell through the calls here alone: the kit's types
/// are linked into this framework, never into the app, which would hold a
/// second copy of them.
public enum Today {
    @MainActor static let shell = TodayShell(configuration)

    /// Makes the shell, which installs the kit's notification delegate:
    /// called from the app's initialiser, before the app finishes
    /// launching, so the tap that launched it is not lost.
    @MainActor public static func start() { _ = shell }

    /// The app came to the front.
    @MainActor public static func cameForward() { shell.cameForward() }

    /// The updater found a newer build, or none.
    @MainActor public static func updateOffered(_ version: String?) { shell.send(.offered(version)) }

    static var configuration: TodayShell.Configuration {
        TodayShell.Configuration(
            manifest: TodayManifest.current, keychainService: "dev.reamde.substrate.today", folder: "SubstrateToday",
            packages: TodayManifest.packages, words: TodayApp.words(device:),
            app: { TodayApp(cache: $0.cache, outbox: $0.outbox, words: $0.words, build: $0.build) },
            refresh: .foreground, clearSaved: .clearSaved, settingsScreen: TodayPresenter.settings)
    }
}
