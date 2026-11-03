import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// The sign-in while there is no session, the access-ended screen while
/// the token is refused, else the app.
public struct RootView: View {
    @Environment(TasksShell.self) private var shell

    public init() {}

    public var body: some View {
        let presentation = shell.model.presentation
        Group {
            if presentation.snapshot.phase == .signedOut, !shell.stored {
                SignInView()
            } else if let ended = presentation.ended {
                AccessEndedScreen(ended) { Task { try? await shell.changeAccess() } }
            } else if let app = presentation.app {
                AppView(link: presentation, app: app)
            } else {
                ProgressView()
            }
        }
        .onChange(of: presentation.snapshot.phase) { _, _ in shell.refreshStored() }
    }
}

/// The connect screen against Tasks' manifest: the browser sign-in, where
/// the owner approves what Substrate Tasks may do.
struct SignInView: View {
    @Environment(TasksShell.self) private var shell
    @AppStorage("signin.server") private var address = ""

    var body: some View {
        ConnectView(manifest: shell.manifest, address: $address, unrevoked: shell.sessions.pendingRevocations()) {
            ConnectHeader(appName: TasksApp.appName, tagline: "Your to-do list, kept in your substrate.", icon: Image("AppMark"))
        } onSignIn: { probe in
            try await shell.signIn(probe)
        }
    }
}
