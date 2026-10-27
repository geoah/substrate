import SwiftUI

/// What the repository still lacks, and the one button that does all of it.
struct SetupView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        Form {
            if let session = model.session {
                Section("Signed in") {
                    LabeledContent("Repository", value: session.repository)
                    LabeledContent("Server", value: session.server.absoluteString)
                }
            }

            Section {
                if !model.setupChecked, model.setupError == nil {
                    HStack { ProgressView(); Text("Reading the repository…").foregroundStyle(.secondary) }
                }
                ForEach(model.steps) { step in
                    VStack(alignment: .leading, spacing: 4) {
                        Label {
                            Text(step.title).font(.callout)
                        } icon: {
                            Image(systemName: step.done ? "checkmark.circle.fill" : "circle")
                                .foregroundStyle(step.done ? .green : .secondary)
                        }
                        if let detail = step.detail {
                            Text(detail).font(.footnote).foregroundStyle(.secondary)
                        }
                    }
                }
            } header: {
                Text("Set up")
            } footer: {
                Text("Installs the Apple and Apple Health providers, imports the observations, health and healthapple samples, creates this device's Health account and arms the webhook it posts to.")
            }

            if let error = model.setupError {
                Section("The server said") {
                    Text(error).foregroundStyle(.red).textSelection(.enabled)
                }
            }

            Section {
                Button {
                    Task { await model.runSetup() }
                } label: {
                    HStack {
                        Text(model.steps.contains { !$0.done } ? "Set Up" : "Done")
                        if model.setupBusy { Spacer(); ProgressView() }
                    }
                }
                .disabled(model.setupBusy || !model.setupChecked)
                Button("Check Again") { Task { await model.checkSetup() } }
                    .disabled(model.setupBusy)
                Button("Sign Out", role: .destructive) { Task { _ = await model.signOut() } }
            }
        }
        .navigationTitle("Set Up")
        .confirmationDialog(
            "The webhook already has a key",
            isPresented: $model.foreignKey, titleVisibility: .visible
        ) {
            Button("Use the Existing Key") { Task { await model.runSetup(replaceKey: false) } }
            Button("Replace It", role: .destructive) { Task { await model.runSetup(replaceKey: true) } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Something else set the applehealth-webhook key, such as Health Auto Export. Using it keeps that sender working; replacing it stops it.")
        }
    }
}
