import SwiftUI

/// What the repository still lacks, each step with its state, and the one
/// button that does all of it.
struct SetupView: View {
    @EnvironmentObject private var model: AppModel

    private var missing: Int { model.steps.filter { !$0.done }.count }

    var body: some View {
        Form {
            if let session = model.session {
                Section("Signed in") {
                    LabeledContent("Repository", value: session.repository)
                    LabeledContent("Server", value: session.server.absoluteString)
                }
            }

            // The action first: the steps below can run past the screen,
            // and the one thing to press must not be under them.
            Section {
                if !model.setupChecked, model.setupError == nil {
                    HStack(spacing: 10) {
                        ProgressView()
                        Text("Reading what the repository holds…").foregroundStyle(.secondary)
                    }
                } else if missing > 0 {
                    Button {
                        Task { await model.runSetup() }
                    } label: {
                        WideLabel(title: missing == 1 ? "Set Up the missing step" : "Set Up \(missing) missing steps",
                                  systemImage: "wand.and.stars", busy: model.setupBusy)
                    }
                    .primaryAction()
                    .disabled(model.setupBusy)
                }
                Button {
                    Task { await model.checkSetup() }
                } label: {
                    WideLabel(title: "Check Again", systemImage: "arrow.clockwise")
                }
                .secondaryAction()
                .disabled(model.setupBusy)
            }
            .listRowBackground(Color.clear)
            .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 4, trailing: 0))

            if let error = model.setupError {
                Section {
                    ErrorBox(title: "Set Up stopped", message: error)
                }
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 4, trailing: 0))
            }

            Section {
                ForEach(model.steps) { step in
                    StepRow(step: step)
                }
            } header: {
                Text(missing == 0 || !model.setupChecked ? "What this phone needs on the server" : "\(model.steps.count - missing) of \(model.steps.count) in place")
            } footer: {
                Text("Set Up does every missing step, in order: installs the providers, creates this phone's Health account and arms the webhook it posts to.")
            }

            Section {
                Button(role: .destructive) {
                    model.signOut()
                } label: {
                    WideLabel(title: "Sign Out", systemImage: "rectangle.portrait.and.arrow.right")
                }
                .secondaryAction()
            }
            .listRowBackground(Color.clear)
            .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 4, trailing: 0))
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

/// One step: what it is, its state as a word, and why when it needs work.
private struct StepRow: View {
    let step: SetupStep

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: step.done ? "checkmark.circle.fill" : "circle.dashed")
                .foregroundStyle(step.done ? .green : .orange)
                .font(.title3)
            VStack(alignment: .leading, spacing: 4) {
                Text(step.title).font(.callout)
                if let detail = step.detail {
                    Text(detail).font(.footnote).foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 8)
            StatusBadge(text: status, color: step.done ? .green : .orange)
        }
        .padding(.vertical, 2)
    }

    private var status: String {
        if step.done { return step.kind == .trigger ? "Armed" : step.kind == .account ? "Created" : "Installed" }
        switch step.kind {
        case .install:
            let detail = step.detail ?? ""
            if detail.hasPrefix("Installed") { return "Upgrade" }
            if detail.hasPrefix("This server ships") { return "Server too old" }
            if detail.hasPrefix("This server does not") { return "Not shipped" }
            return "Missing"
        case .importSample: return "Missing"
        case .account: return "Missing"
        case .trigger: return "Not armed"
        }
    }
}
