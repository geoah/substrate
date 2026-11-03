import SubstrateCore
import SubstrateUI
import SwiftUI

/// Where the app points and how it signs in: the kit's connect screen, as in
/// Substrate Chat and Sync. A server that can grant the app full access is
/// signed in to on its own page in Safari; one that cannot hands over to the
/// console's own sign-in page. Below it, the other servers this device is
/// signed in to, and registering, which happens on the server's own page.
struct ConsoleConnect: View {
    @EnvironmentObject private var store: ServerStore
    @EnvironmentObject private var sessions: ConsoleSessions
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL
    /// Shown over a console, to switch servers: it can be closed.
    let cancellable: Bool

    @State private var address = ""

    var body: some View {
        ConnectView(manifest: ConsoleSignIn.manifest, address: $address, unrevoked: sessions.unrevoked)
        {
            ConnectHeader(appName: "Substrate", tagline: "Your substrate's console.", icon: Image("AppMark"))
        } onSignIn: { probe in
            try await sessions.signIn(probe)
            store.use(probe.origin)
            if cancellable { dismiss() }
        } passwordDoor: { probe in
            PageSignInDoor(host: probe.host) {
                sessions.usePageSignIn(on: probe.origin)
                store.use(probe.origin)
                if cancellable { dismiss() }
            }
        }
        .safeAreaInset(edge: .bottom) { footer }
        .onAppear {
            if address.isEmpty { address = store.current?.absoluteString ?? "" }
        }
    }

    /// Servers this device already opens, other than the one in the field.
    private var others: [URL] {
        let typed = ServerAddress.origin(of: address)
        return sessions.servers.filter { $0 != typed }
    }

    private var footer: some View {
        VStack(spacing: 10) {
            HStack(spacing: 16) {
                if !others.isEmpty {
                    Menu("Signed In Elsewhere") {
                        ForEach(others, id: \.self) { server in
                            Menu(ServerAddress.display(server)) {
                                Button("Open") {
                                    store.use(server)
                                    if cancellable { dismiss() }
                                }
                                Button("Sign Out and Forget", role: .destructive) {
                                    Task {
                                        await sessions.forget(server)
                                        store.forget(server)
                                    }
                                }
                            }
                        }
                    }
                    .fixedSize()
                }
                if let origin = ServerAddress.origin(of: address), let register = URL(string: "/register", relativeTo: origin) {
                    Button("Register…") { openURL(register.absoluteURL) }
                        .help("Choose your name on \(ServerAddress.display(origin))'s own page, then sign in here.")
                }
                if cancellable {
                    Button("Cancel", role: .cancel) { dismiss() }
                        .keyboardShortcut(.cancelAction)
                }
            }
            .buttonStyle(.borderless)
            .font(.callout)
            BuildFooter()
        }
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity)
        // The screen's own background, opaque, so what scrolls under the
        // footer is hidden without setting it apart as a band.
        .background(.background)
    }
}

/// A server that cannot grant the app full access yet: the console's own
/// sign-in page, inside the app, is the way in until it can.
private struct PageSignInDoor: View {
    let host: String
    let onContinue: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("\(host) can't sign this app in on its own page yet. Until it is updated, sign in on the console's page here.")
                .font(.callout)
                .foregroundStyle(.secondary)
            Button(action: onContinue) {
                Text("Continue to the Console's Sign-In")
                    .frame(maxWidth: .infinity, minHeight: 30)
            }
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.capsule)
            .controlSize(.large)
        }
    }
}
