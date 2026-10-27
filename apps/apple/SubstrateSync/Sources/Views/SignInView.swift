import SwiftUI

/// The server first, checked against its discovery document the way the
/// console app's picker does; then the repository, the password, and a code
/// when the server asks for one.
struct SignInView: View {
    @EnvironmentObject private var model: AppModel
    @AppStorage("signin.server") private var address = ""
    @AppStorage("signin.repository") private var repository = ""
    @State private var server: URL?
    @State private var totpRequired = false
    @State private var password = ""
    @State private var code = ""
    @State private var busy = false
    @State private var problem: String?

    var body: some View {
        Form {
            Section {
                TextField("https://substrate.example.com", text: $address)
                    .textContentType(.URL)
                    .keyboardType(.URL)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .onSubmit(check)
                    .onChange(of: address) { _, _ in server = nil }
            } header: {
                Text("Server")
            } footer: {
                if let server {
                    Label("\(server.absoluteString) is a substrate.", systemImage: "checkmark.circle")
                } else {
                    Text("A bare host is HTTPS; localhost is plain HTTP.")
                }
            }

            if server != nil {
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
                }
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 4, trailing: 0))
            } else {
                Section {
                    Button(action: check) {
                        WideLabel(title: "Continue", systemImage: "arrow.right", busy: busy)
                    }
                    .primaryAction()
                    .disabled(busy || address.trimmingCharacters(in: .whitespaces).isEmpty)
                }
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 4, trailing: 0))
            }

            if let problem {
                Section {
                    ErrorBox(title: "Could not sign in", message: problem)
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
        guard let origin = Client.origin(of: address) else {
            problem = "That is not an http or https address."
            return
        }
        busy = true
        problem = nil
        Task {
            defer { busy = false }
            do {
                let discovery = try await Client(server: origin).discover()
                totpRequired = discovery.registration?.totpRequired ?? true
                address = origin.absoluteString
                server = origin
            } catch {
                problem = "\(origin.absoluteString) did not answer as a substrate: \(error.localizedDescription)"
            }
        }
    }

    private func signIn() {
        guard let server else { return }
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
                problem = error.localizedDescription
            }
        }
    }
}
