#if canImport(SwiftUI)
import SubstrateAuth
import SubstrateRuntime
import SubstrateScreens
import SwiftUI

/// An app's root under its shell: the sign-in while there is no session, the
/// access-ended screen while the token is refused, else the app's own.
public struct AppShellRoot<App: LinkedApp, Content: View>: View {
    @Environment(AppShell<App>.self) private var shell
    let tagline: String
    let icon: Image?
    let content: (LinkPresentation<App.Presentation>, App.Presentation) -> Content

    public init(tagline: String, icon: Image? = nil,
                @ViewBuilder content: @escaping (LinkPresentation<App.Presentation>, App.Presentation) -> Content)
    {
        self.tagline = tagline
        self.icon = icon
        self.content = content
    }

    public var body: some View {
        let presentation = shell.presentation
        Group {
            if presentation.snapshot.phase == .signedOut, !shell.stored {
                AppSignIn<App>(tagline: tagline, icon: icon)
            } else if let ended = presentation.ended {
                AccessEndedScreen(ended) { Task { try? await shell.changeAccess() } }
            } else if let app = presentation.app {
                content(presentation, app)
            } else {
                ProgressView()
            }
        }
        .onChange(of: presentation.snapshot.phase) { _, _ in shell.refreshStored() }
    }
}

/// The connect screen against the shell's manifest: the browser sign-in,
/// where the owner approves what the app may do.
struct AppSignIn<App: LinkedApp>: View {
    @Environment(AppShell<App>.self) private var shell
    @AppStorage("signin.server") private var address = ""
    let tagline: String
    let icon: Image?

    var body: some View {
        ConnectView(manifest: shell.manifest, address: $address, unrevoked: shell.sessions.pendingRevocations()) {
            ConnectHeader(appName: shell.manifest.appName, tagline: tagline, icon: icon)
        } onSignIn: { probe in
            try await shell.signIn(probe)
        }
    }
}

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

public extension EnvironmentValues {
    /// The updater's own check, which the scene that holds the updater runs:
    /// About's Check for Updates.
    @Entry var checkForUpdates: @MainActor () -> Void = {}
}
#endif
