import ChatKit
import SubstrateKit
import SwiftUI

/// The server first, the kit's connect screen against Chat's manifest. A
/// substrate that can give the app access of its own is signed in to in the
/// browser, where the owner approves what Substrate Chat may do; one with no
/// app authorization takes the repository, the password and, when it asks,
/// a code; one whose app authorization cannot reach the agents says it
/// needs an update. Below the door, a token an earlier sign-in left that its
/// server has not revoked yet, with where to revoke it by hand.
struct SignInView: View {
    @Environment(ChatHost.self) private var host
    @AppStorage("signin.server") private var address = ""

    var body: some View {
        ConnectView(manifest: ChatManifest.manifest, address: $address, unrevoked: SessionStore.chat.pendingRevocations()) {
            // The app's own icon, drawn by scripts/icons.mjs like the AppIcon
            // set, light and dark.
            ConnectHeader(appName: ChatManifest.appName, tagline: "Talk to the agents in your substrate.",
                          icon: Image("AppMark"))
        } onSignIn: { probe in
            try await host.signIn(probe)
        } passwordDoor: { probe in
            PasswordDoor(probe: probe)
        }
    }
}

/// The password door, for a server without app authorization.
private struct PasswordDoor: View {
    @Environment(ChatHost.self) private var host
    @AppStorage("signin.repository") private var repository = ""
    let probe: ServerProbe.Result
    @State private var password = ""
    @State private var code = ""
    @State private var busy = false
    @State private var problem: String?
    @FocusState private var focus: Field?

    private enum Field { case repository, password, code }

    private var totpRequired: Bool { probe.discovery.registration.totpRequired }

    private var ready: Bool {
        !repository.trimmingCharacters(in: .whitespaces).isEmpty && !password.isEmpty && (!totpRequired || !code.isEmpty)
    }

    var body: some View {
        VStack(spacing: 12) {
            field("Repository", systemImage: "person.crop.circle") {
                TextField("you.example.com", text: $repository)
                    .textContentType(.username)
                    #if os(iOS)
                    .textInputAutocapitalization(.never)
                    #endif
                    .autocorrectionDisabled()
                    .focused($focus, equals: .repository)
                    .onSubmit { focus = .password }
            }
            field("Password", systemImage: "key") {
                SecureField("Required", text: $password)
                    .textContentType(.password)
                    .focused($focus, equals: .password)
                    .onSubmit { totpRequired ? (focus = .code) : signIn() }
            }
            if totpRequired {
                field("Code", systemImage: "lock.shield") {
                    TextField("6 digits", text: $code)
                        .textContentType(.oneTimeCode)
                        #if os(iOS)
                        .keyboardType(.numberPad)
                        #endif
                        .focused($focus, equals: .code)
                        .onSubmit(signIn)
                }
            }

            if let problem {
                ProblemBox(title: "Couldn't sign in", message: problem)
                    .card(tint: .red)
                    .padding(.top, 16)
            }

            Button(action: signIn) {
                HStack(spacing: 8) {
                    if busy { ProgressView().controlSize(.small) }
                    Text("Sign In")
                }
                .frame(maxWidth: .infinity, minHeight: 30)
            }
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.capsule)
            .controlSize(.large)
            .disabled(busy || !ready)
            .keyboardShortcut(.defaultAction)
            .padding(.top, 16)
        }
        .disabled(busy)
        .onAppear { focus = repository.isEmpty ? .repository : .password }
    }

    private func field(_ label: String, systemImage: String, @ViewBuilder content: () -> some View) -> some View {
        HStack(spacing: 12) {
            Image(systemName: systemImage)
                .foregroundStyle(.secondary)
                .frame(width: 22)
            content()
                .textFieldStyle(.plain)
        }
        .padding(.horizontal, 16)
        .frame(minHeight: 50)
        .background(.fill.tertiary, in: .rect(cornerRadius: 14))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(label)
    }

    private func signIn() {
        guard ready, !busy else { return }
        busy = true
        problem = nil
        Task {
            defer { busy = false }
            do {
                try await host.signIn(server: probe.origin, repository: repository.trimmingCharacters(in: .whitespaces),
                                       password: password, totp: totpRequired ? code : nil)
                password = ""
                code = ""
            } catch {
                problem = explain(error, app: ChatManifest.appName, server: probe.origin)
            }
        }
    }
}

/// Signed in, but the owner left out talking to the agents: the one thing
/// this app is for.
struct NoChatAccessView: View {
    @Environment(ChatHost.self) private var host
    @State private var changingAccess = false

    var body: some View {
        VStack(spacing: 20) {
            Image("AppMark")
                .resizable()
                .frame(width: 72, height: 72)
                .accessibilityHidden(true)
            Text("Substrate Chat can't talk to your assistants")
                .font(.title2.weight(.semibold))
                .multilineTextAlignment(.center)
            Text("The approval on \(host.chats?.host ?? "your substrate") left out talking to your agents. Change its access and keep that one checked.")
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
            if let problem = host.authorizeProblem {
                ProblemBox(title: "Access didn't change", message: problem)
                    .card(tint: .red)
            }
            Button {
                changingAccess = true
            } label: {
                HStack(spacing: 8) {
                    if host.authorizing { ProgressView().controlSize(.small) }
                    Text("Change Access…")
                }
            }
            .buttonStyle(.glassProminent)
            .controlSize(.large)
            .disabled(host.authorizing && !WebAuthenticator.opensBrowser)
            if host.authorizing, WebAuthenticator.opensBrowser {
                Button("Cancel") { host.cancelAuthorization() }
                    .buttonStyle(.borderless)
            }
            Button("Sign Out", role: .destructive) { host.signOut() }
                .buttonStyle(.borderless)
        }
        .padding(32)
        .frame(maxWidth: 440)
        .sheet(isPresented: $changingAccess) { ReviewSheet(isPresented: $changingAccess, changingAccess: true) }
    }
}
