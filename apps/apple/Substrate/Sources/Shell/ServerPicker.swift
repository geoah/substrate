import SubstrateKit
import SwiftUI

/// Where the app points: an address, checked against the server's discovery
/// document before it is used, and the servers it pointed at before.
struct ServerPicker: View {
    @EnvironmentObject private var store: ServerStore
    @Environment(\.dismiss) private var dismiss
    let cancellable: Bool

    @State private var address = ""
    @State private var checking = false
    @State private var problem: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            VStack(alignment: .leading, spacing: 4) {
                Text("Connect to a substrate").font(.title2.weight(.semibold))
                Text("The app runs its own copy of the console and talks to this server's API.")
                    .font(.callout).foregroundStyle(.secondary)
            }

            VStack(alignment: .leading, spacing: 6) {
                Text("Server address").font(.subheadline.weight(.medium))
                TextField("https://substrate.example.com", text: $address)
                    .textFieldStyle(.roundedBorder)
                    .autocorrectionDisabled()
                    #if os(iOS)
                    .textInputAutocapitalization(.never)
                    .keyboardType(.URL)
                    #endif
                    .onSubmit(connect)
                if let problem {
                    Text(problem).font(.footnote).foregroundStyle(.red)
                } else {
                    Text("A bare host is HTTPS; localhost is plain HTTP.")
                        .font(.footnote).foregroundStyle(.secondary)
                }
            }

            if !store.recent.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Recent").font(.subheadline.weight(.medium))
                    ForEach(store.recent, id: \.self) { server in
                        HStack {
                            Button(server.absoluteString) {
                                address = server.absoluteString
                                connect()
                            }
                            .buttonStyle(.borderless)
                            Spacer()
                            if server == store.current {
                                Text("Current").font(.caption).foregroundStyle(.secondary)
                            } else {
                                Button("Forget", role: .destructive) { store.forget(server) }
                                    .buttonStyle(.borderless).font(.caption)
                            }
                        }
                    }
                }
            }

            HStack {
                if checking { ProgressView().controlSize(.small) }
                Spacer()
                if cancellable {
                    Button("Cancel", role: .cancel) { dismiss() }
                        .keyboardShortcut(.cancelAction)
                }
                Button("Connect", action: connect)
                    .keyboardShortcut(.defaultAction)
                    .buttonStyle(.borderedProminent)
                    .disabled(checking || address.trimmingCharacters(in: .whitespaces).isEmpty)
            }

            Divider()
            BuildFooter()
        }
        .padding(24)
        .frame(maxWidth: 520)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .onAppear {
            if address.isEmpty {
                address = store.current?.absoluteString ?? "http://localhost:8080"
            }
        }
    }

    private func connect() {
        guard let server = ServerAddress.origin(of: address) else {
            problem = "That is not an http or https address."
            return
        }
        problem = nil
        checking = true
        Task {
            defer { checking = false }
            do {
                // Every substrate serves its discovery document; anything
                // else at the address is not one.
                _ = try await SubstrateClient(server: server).discovery()
                store.use(server)
                dismiss()
            } catch {
                problem = "\(server.absoluteString) did not answer as a substrate: \(explain(error, app: "Substrate", server: server))"
            }
        }
    }
}
