import SubstrateKit
import SwiftUI

/// The server first, checked against its discovery document and its OAuth
/// metadata. A server whose approval sets up what Sync needs gets the
/// browser sign-in, which asks for no password here, under the kit's review
/// of the access it asks for and what it sets up; any other gets the
/// repository, the password, and a code when the server asks for one.
struct SignInView: View {
    @EnvironmentObject private var model: AppModel
    @AppStorage("signin.server") private var address = ""
    @AppStorage("signin.repository") private var repository = ""
    @State private var probe: ServerProbe.Result?

    /// `probe` is a server already found, as a snapshot draws the screen.
    init(probe: ServerProbe.Result? = nil) {
        _probe = State(initialValue: probe)
    }

    @State private var password = ""
    @State private var code = ""
    @State private var busy = false
    @State private var problem: String?

    private var door: SignInDoor? { probe.map { model.door(for: $0) } }
    private var totpRequired: Bool { probe?.discovery.registration.totpRequired ?? true }

    var body: some View {
        Form {
            Section {
                TextField("https://substrate.example.com", text: $address)
                    .textContentType(.URL)
                    .keyboardType(.URL)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .onSubmit(check)
                    .onChange(of: address) { _, typed in
                        // Writing the checked origin back is not a new address.
                        if let probe, typed == probe.origin.absoluteString { return }
                        probe = nil
                    }
            } header: {
                Text("Server")
            } footer: {
                if probe != nil {
                    Label("Server found.", systemImage: "checkmark.circle")
                } else {
                    Text("Your Substrate server's address. A name alone is read as https://.")
                }
            }

            if let probe, door == .app {
                ReviewChangesSections(appName: SyncManifest.appName, host: probe.host, heading: .firstSignIn,
                                      changes: (try? model.manifest())?.plannedChanges() ?? [])
                Section {
                    Button(action: signInWithApp) {
                        WideLabel(title: busy ? "Waiting for Your Browser…" : "Sign In with Your Substrate",
                                  systemImage: "person.badge.key", busy: busy)
                    }
                    .primaryAction()
                    .disabled(busy)
                    .buttonRow()
                } footer: {
                    Text("You sign in on \(probe.host) itself, and approve this there. "
                        + ReviewChangesSections.choosing(host: probe.host, setsUp: true))
                }
            } else if probe != nil {
                Section("Your repository") {
                    LabeledContent("Repository") {
                        TextField("you.example.com", text: $repository)
                            .textContentType(.username)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .multilineTextAlignment(.trailing)
                    }
                    LabeledContent("Password") {
                        SecureField("Required", text: $password)
                            .textContentType(.password)
                            .multilineTextAlignment(.trailing)
                    }
                    if totpRequired {
                        LabeledContent("Code") {
                            TextField("6 digits", text: $code)
                                .textContentType(.oneTimeCode)
                                .keyboardType(.numberPad)
                                .multilineTextAlignment(.trailing)
                        }
                    }
                }
                Section {
                    Button(action: signIn) {
                        WideLabel(title: "Sign In", systemImage: "person.badge.key", busy: busy)
                    }
                    .primaryAction()
                    .disabled(busy || repository.isEmpty || password.isEmpty || (totpRequired && code.isEmpty))
                    .buttonRow()
                }
            } else {
                Section {
                    Button(action: check) {
                        WideLabel(title: "Continue", systemImage: "arrow.right", busy: busy)
                    }
                    .primaryAction()
                    .disabled(busy || address.trimmingCharacters(in: .whitespaces).isEmpty)
                    .buttonRow()
                }
            }

            if let problem {
                Section {
                    ProblemBox(title: probe == nil ? "Couldn't reach that server" : "Couldn't sign in", message: problem)
                }
            }

            if !model.unrevoked.isEmpty {
                Section {
                    UnrevokedNotices(appName: SyncManifest.appName, unrevoked: model.unrevoked)
                }
            }

            Section {
                BuildFooter()
            }
        }
        .navigationTitle("Substrate Sync")
        .disabled(busy)
    }

    private func check() {
        guard let origin = ServerAddress.origin(of: address) else {
            problem = "That is not an http or https address."
            return
        }
        busy = true
        problem = nil
        Task {
            defer { busy = false }
            do {
                let found = try await ServerProbe(transport: Client.transport)
                    .probe(origin, requirement: SyncManifest.requirement, app: SyncManifest.appName)
                address = origin.absoluteString
                probe = found
            } catch {
                problem = (error as? LocalizedError)?.errorDescription ?? explain(error, server: origin)
            }
        }
    }

    private func signIn() {
        guard let server = probe?.origin else { return }
        busy = true
        problem = nil
        Task {
            defer { busy = false }
            do {
                try await model.signIn(
                    server: server, repository: repository.trimmingCharacters(in: .whitespaces),
                    password: password, totp: totpRequired ? code : nil)
                password = ""
                code = ""
            } catch {
                problem = explain(error, server: server)
            }
        }
    }

    private func signInWithApp() {
        guard let probe else { return }
        busy = true
        problem = nil
        Task {
            defer { busy = false }
            do {
                try await model.signIn(probe: probe)
            } catch where error is WebAuthenticator.Cancelled || isStop(error) {
                // The person closed the sheet: nothing to say.
            } catch {
                problem = explain(error, server: probe.origin)
            }
        }
    }
}
