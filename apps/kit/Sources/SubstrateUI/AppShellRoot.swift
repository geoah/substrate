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

    public init(
        tagline: String, icon: Image? = nil,
        @ViewBuilder content: @escaping (LinkPresentation<App.Presentation>, App.Presentation) -> Content
    ) {
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
#endif
