#if canImport(SwiftUI)
import SubstrateAuth
import SubstrateRuntime
import SubstrateScreens
import SwiftUI

/// The connect screen against the shell's manifest: the browser sign-in,
/// where the owner approves what the app may do.
struct AppSignIn<App: LinkedApp>: View {
    @Environment(AppShell<App>.self) private var shell
    @AppStorage("signin.server") private var address = ""
    let tagline: String
    let icon: Image?

    var body: some View {
        ConnectView(manifest: shell.manifest, address: $address, unrevoked: (try? shell.sessions.pendingRevocations()) ?? []) {
            ConnectHeader(appName: shell.manifest.appName, tagline: tagline, icon: icon)
        } onSignIn: { probe in
            try await shell.signIn(probe)
        }
    }
}
#endif
