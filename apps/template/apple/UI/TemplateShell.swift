import SubstrateScreens
import SubstrateUI
import TemplateCore

/// The app's shell: the kit's `AppShell` over `TemplateApp`.
public typealias TemplateShell = AppShell<TemplateApp>

/// What Substrate Template hands its shell: its manifest and package, where
/// it keeps its session and folders, its root and its Settings. The app
/// target reaches the shell through the calls here alone: the kit's types
/// are linked into this framework, never into the app, which would hold a
/// second copy of them.
public enum Template {
    @MainActor static let shell = TemplateShell(configuration)

    /// Makes the shell, which installs the kit's notification delegate:
    /// called from the app's initialiser, before the app finishes
    /// launching, so the tap that launched it is not lost.
    @MainActor public static func start() { _ = shell }

    /// The app came to the front.
    @MainActor public static func cameForward() { shell.cameForward() }

    static var configuration: TemplateShell.Configuration {
        TemplateShell.Configuration(
            manifest: TemplateManifest.current, keychainService: "dev.reamde.substrate.template", folder: "SubstrateTemplate",
            packages: TemplateManifest.packages, words: TemplateApp.words(device:),
            app: { TemplateApp(cache: $0.cache, outbox: $0.outbox, device: $0.device, build: $0.build) },
            refresh: .foreground, clearSaved: .clearSaved, settingsScreen: TemplatePresenter.settings)
    }
}
