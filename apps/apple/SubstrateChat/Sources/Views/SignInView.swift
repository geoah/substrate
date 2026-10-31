import SwiftUI

/// The server first, checked against its discovery document; then the
/// repository, the password, and a code only when the server asks for one.
struct SignInView: View {
    @Environment(AppModel.self) private var model
    @AppStorage("signin.server") private var address = ""
    @AppStorage("signin.repository") private var repository = ""
    @State private var server: URL?
    @State private var totpRequired = false
    @State private var password = ""
    @State private var code = ""
    @State private var busy = false
    @State private var problem: String?
    @FocusState private var focus: Field?

    private enum Field { case address, repository, password, code }

    var body: some View {
        ScrollView {
            VStack(spacing: 28) {
                VStack(spacing: 14) {
                    Image(systemName: "bubble.left.and.text.bubble.right.fill")
                        .font(.system(size: 52, weight: .medium))
                        .foregroundStyle(.tint)
                        .symbolRenderingMode(.hierarchical)
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
                                if typed != server?.absoluteString { server = nil }
                            }
                    }
                    if server != nil {
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
                        Text(server == nil ? "Continue" : "Sign In")
                    }
                    .frame(maxWidth: .infinity, minHeight: 30)
                }
                .buttonStyle(.glassProminent)
                .controlSize(.large)
                .disabled(busy || !ready)
                .keyboardShortcut(.defaultAction)

                if server != nil {
                    Label("Server found: \(server!.host() ?? "")", systemImage: "checkmark.circle.fill")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                } else {
                    Text("Your Substrate server's address. A name alone is read as https://.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }
            }
            .padding(24)
            .frame(maxWidth: 440)
            .frame(maxWidth: .infinity)
        }
        .disabled(busy)
        .onAppear { focus = address.isEmpty ? .address : nil }
    }

    private var ready: Bool {
        if server == nil { return !address.trimmingCharacters(in: .whitespaces).isEmpty }
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
                address = origin.absoluteString
                server = origin
                focus = repository.isEmpty ? .repository : .password
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
                try await model.signIn(server: server, repository: repository.trimmingCharacters(in: .whitespaces),
                                       password: password, totp: totpRequired ? code : nil)
                password = ""
                code = ""
            } catch {
                problem = explain(error, server: server)
            }
        }
    }
}

/// The server refused the token: what happened, and the one way out.
struct ExpiredView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        VStack(spacing: 20) {
            Image(systemName: "person.crop.circle.badge.exclamationmark")
                .font(.system(size: 48))
                .foregroundStyle(.orange)
                .symbolRenderingMode(.hierarchical)
            Text("Signed out by the server")
                .font(.title2.weight(.semibold))
            Text("\(model.session?.server.host() ?? "The server") no longer accepts this sign-in: the token was revoked, or the repository was reset. Sign in again to carry on.")
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
