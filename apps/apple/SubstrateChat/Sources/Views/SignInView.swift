import SwiftUI

/// The server first, checked against its discovery document. A substrate with
/// app authorization is signed in to in the browser: the owner signs in there
/// and approves what Substrate Chat may do, and the app never sees the
/// password. A server that predates it takes the repository, the password
/// and, when it asks, a code.
struct SignInView: View {
    @Environment(AppModel.self) private var model
    @AppStorage("signin.server") private var address = ""
    @AppStorage("signin.repository") private var repository = ""
    @State private var server: URL?
    /// The server's OAuth metadata; nil with a server found means the
    /// password door.
    @State private var oauth: OAuth.Metadata?
    @State private var totpRequired = false
    @State private var password = ""
    @State private var code = ""
    @State private var busy = false
    @State private var problem: String?
    @FocusState private var focus: Field?

    private enum Field { case address, repository, password, code }

    private var passwordDoor: Bool { server != nil && oauth == nil }

    var body: some View {
        ScrollView {
            VStack(spacing: 28) {
                VStack(spacing: 14) {
                    // The app's own icon, drawn by scripts/icons.mjs like the
                    // AppIcon set, light and dark.
                    Image("AppMark")
                        .resizable()
                        .interpolation(.high)
                        .frame(width: 96, height: 96)
                        .shadow(color: .black.opacity(0.12), radius: 12, y: 6)
                        .accessibilityHidden(true)
                    Text("Substrate Chat")
                        .font(.largeTitle.weight(.bold))
                    Text("Talk to the agents in your substrate.")
                        .foregroundStyle(.secondary)
                }
                .padding(.top, 48)

                VStack(spacing: 12) {
                    field("Server", systemImage: "server.rack") {
                        TextField("substrate.example.com", text: $address)
                            .textContentType(.URL)
                            #if os(iOS)
                            .keyboardType(.URL)
                            .textInputAutocapitalization(.never)
                            #endif
                            .autocorrectionDisabled()
                            .focused($focus, equals: .address)
                            .onSubmit(check)
                            .onChange(of: address) { _, typed in
                                // Writing the checked origin back is not a new address.
                                if typed != server?.absoluteString {
                                    server = nil
                                    oauth = nil
                                }
                            }
                    }
                    if passwordDoor {
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
                    }
                }
                .animation(.snappy, value: server)

                if let problem {
                    ProblemLine(title: server == nil ? "Couldn't reach that server" : "Couldn't sign in", message: problem)
                        .card(tint: .red)
                }

                Button(action: server == nil ? check : signIn) {
                    HStack(spacing: 8) {
                        if busy { ProgressView().controlSize(.small) }
                        Text(server == nil ? "Continue" : oauth != nil ? "Sign In with \(server?.host() ?? "Your Substrate")" : "Sign In")
                    }
                    .frame(maxWidth: .infinity, minHeight: 30)
                }
                .buttonStyle(.glassProminent)
                .controlSize(.large)
                .disabled(busy || !ready)
                .keyboardShortcut(.defaultAction)

                footer
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }
            .padding(24)
            .frame(maxWidth: 440)
            .frame(maxWidth: .infinity)
        }
        .disabled(busy)
        .onAppear { focus = address.isEmpty ? .address : nil }
    }

    @ViewBuilder private var footer: some View {
        if let server, oauth != nil {
            VStack(spacing: 6) {
                Label("Server found: \(server.host() ?? "")", systemImage: "checkmark.circle.fill")
                Text("You sign in on \(server.host() ?? "your substrate") itself and choose what Substrate Chat may do there: talk to your assistants, and apply what they suggest. You can revoke it any time in its Settings → Apps.")
            }
        } else if let server {
            VStack(spacing: 6) {
                Label("Server found: \(server.host() ?? "")", systemImage: "checkmark.circle.fill")
                Text("This server predates signing in apps through the browser, so your password goes to it directly.")
            }
        } else {
            Text("Your Substrate server's address. A name alone is read as https://.")
        }
    }

    private var ready: Bool {
        if server == nil { return !address.trimmingCharacters(in: .whitespaces).isEmpty }
        if oauth != nil { return true }
        return !repository.isEmpty && !password.isEmpty && (!totpRequired || !code.isEmpty)
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

    private func check() {
        guard let origin = API.origin(of: address) else {
            problem = "That is not an http or https address."
            return
        }
        busy = true
        problem = nil
        Task {
            defer { busy = false }
            do {
                let discovery = try await API(server: origin).discover()
                totpRequired = discovery.registration?.totpRequired ?? true
                oauth = try await OAuth.metadata(server: origin)
                address = origin.absoluteString
                server = origin
                if oauth == nil { focus = repository.isEmpty ? .repository : .password }
            } catch {
                problem = "\(origin.absoluteString) did not answer as a Substrate server. \(explain(error, server: origin))"
            }
        }
    }

    private func signIn() {
        guard let server, ready else { return }
        busy = true
        problem = nil
        Task {
            defer { busy = false }
            do {
                if let oauth {
                    try await model.signIn(server: server, metadata: oauth)
                } else {
                    try await model.signIn(server: server, repository: repository.trimmingCharacters(in: .whitespaces),
                                           password: password, totp: totpRequired ? code : nil)
                    password = ""
                    code = ""
                }
            } catch is WebSignIn.Cancelled {
                // The person closed the sheet: nothing to say.
            } catch {
                problem = explain(error, server: server)
            }
        }
    }
}

/// The server refused the token: what happened, and the one way out.
struct ExpiredView: View {
    @Environment(AppModel.self) private var model

    private var app: Bool { model.session?.grants != nil }

    var body: some View {
        VStack(spacing: 20) {
            Image(systemName: "person.crop.circle.badge.exclamationmark")
                .font(.system(size: 48))
                .foregroundStyle(.orange)
                .symbolRenderingMode(.hierarchical)
            Text(app ? "Your approval ended" : "Signed out by the server")
                .font(.title2.weight(.semibold))
            Text(app
                ? "\(model.session?.server.host() ?? "The server") no longer accepts Substrate Chat: the time you approved it for is up, or it was revoked in Settings → Apps. Sign in again to approve it."
                : "\(model.session?.server.host() ?? "The server") no longer accepts this sign-in: the token was revoked, or the repository was reset. Sign in again to carry on.")
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

/// Signed in, but the owner left out talking to the agents: the one thing
/// this app is for.
struct NoChatAccessView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        VStack(spacing: 20) {
            Image("AppMark")
                .resizable()
                .frame(width: 72, height: 72)
                .accessibilityHidden(true)
            Text("Substrate Chat can't talk to your assistants")
                .font(.title2.weight(.semibold))
                .multilineTextAlignment(.center)
            Text("The approval on \(model.session?.server.host() ?? "your substrate") left out talking to your agents. Sign in again and keep that access checked.")
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
