import SwiftUI
import SubstrateKit

/// The server first, the kit's connect screen against Chat's manifest. A
/// substrate that can give the app access of its own is signed in to in the
/// browser, where the owner approves what Substrate Chat may do; one with no
/// app authorization takes the repository, the password and, when it asks,
/// a code; one whose app authorization cannot reach the agents says it
/// needs an update.
struct SignInView: View {
    @Environment(AppModel.self) private var model
    @AppStorage("signin.server") private var address = ""

    var body: some View {
        ConnectView(manifest: ChatManifest.manifest, address: $address) {
            // The app's own icon, drawn by scripts/icons.mjs like the AppIcon
            // set, light and dark.
            ConnectHeader(appName: ChatManifest.appName, tagline: "Talk to the agents in your substrate.",
                          icon: Image("AppMark"))
        } onSignIn: { probe in
            try await model.signIn(probe)
        } passwordDoor: { probe in
            PasswordDoor(probe: probe)
        }
    }
}

/// The password door, for a server without app authorization.
private struct PasswordDoor: View {
    @Environment(AppModel.self) private var model
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
                ProblemLine(title: "Couldn't sign in", message: problem)
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
                try await model.signIn(server: probe.origin, repository: repository.trimmingCharacters(in: .whitespaces),
                                       password: password, totp: totpRequired ? code : nil)
                password = ""
                code = ""
            } catch {
                problem = explain(error, app: ChatManifest.appName, server: probe.origin)
            }
        }
    }
}

/// The server refused the token: what happened, and the one way out. An
/// app session's access ended or was revoked, in the kit's words.
struct ExpiredView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let session = model.session, session.isApp {
            AccessEndedView(appName: ChatManifest.appName, host: ServerAddress.display(session.server)) {
                model.signInAgain()
            }
        } else {
            VStack(spacing: 20) {
                Image(systemName: "person.crop.circle.badge.exclamationmark")
                    .font(.system(size: 48))
                    .foregroundStyle(.orange)
                    .symbolRenderingMode(.hierarchical)
                Text("Signed out by the server")
                    .font(.title2.weight(.semibold))
                Text("\(model.session.map { ServerAddress.display($0.server) } ?? "The server") no longer accepts this sign-in: the token was revoked, or the repository was reset. Sign in again to carry on.")
                    .multilineTextAlignment(.center)
                    .foregroundStyle(.secondary)
                Button("Sign In Again") { model.signInAgain() }
                    .buttonStyle(.glassProminent)
                    .controlSize(.large)
            }
            .padding(32)
            .frame(maxWidth: 440)
        }
    }
}

/// Signed in, but the owner left out talking to the agents: the one thing
/// this app is for.
struct NoChatAccessView: View {
    @Environment(AppModel.self) private var model
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
            Text("The approval on \(model.session.map { ServerAddress.display($0.server) } ?? "your substrate") left out talking to your agents. Change its access and keep that one checked.")
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
            if let problem = model.authorizeProblem {
                ProblemLine(title: "Access didn't change", message: problem)
                    .card(tint: .red)
            }
            Button {
                changingAccess = true
            } label: {
                HStack(spacing: 8) {
                    if model.authorizing { ProgressView().controlSize(.small) }
                    Text("Change Access…")
                }
            }
            .buttonStyle(.glassProminent)
            .controlSize(.large)
            .disabled(model.authorizing)
            Button("Sign Out", role: .destructive) { model.signOut() }
                .buttonStyle(.borderless)
        }
        .padding(32)
        .frame(maxWidth: 440)
        .sheet(isPresented: $changingAccess) {
            ReviewChangesSheet(appName: ChatManifest.appName, host: model.session.map { ServerAddress.display($0.server) } ?? "your substrate",
                               changes: model.reviewChanges, heading: .changeAccess,
                               onContinue: {
                                   changingAccess = false
                                   Task { await model.reauthorize() }
                               },
                               onCancel: { changingAccess = false })
        }
    }
}
