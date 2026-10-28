import SwiftUI

/// What the repository still lacks, each step with its state, and the one
/// button that does all of it.
struct SetupView: View {
    @EnvironmentObject private var model: AppModel
    /// Opened from Settings, which already shows the account and Sign Out.
    var fromSettings = false

    private var missing: Int { model.steps.filter { !$0.done }.count }

    var body: some View {
        Form {
            if !fromSettings, let session = model.session {
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
                        Text("Reading what your server holds…").foregroundStyle(.secondary)
                    }
                } else if missing > 0 {
                    Button {
                        Task { await model.runSetup() }
                    } label: {
                        WideLabel(title: missing == 1 ? "Set Up the Missing Step" : "Set Up \(missing) Missing Steps",
                                  systemImage: "wand.and.stars", busy: model.setupBusy)
                    }
                    .primaryAction()
                    .disabled(model.setupBusy)
                    .buttonRow()
                }
                // In place, the check is a toolbar action, not the page's.
                if missing > 0 || model.setupError != nil || !model.setupChecked {
                    Button {
                        Task { await model.checkSetup() }
                    } label: {
                        WideLabel(title: "Check Again", systemImage: "arrow.clockwise", busy: model.setupChecking)
                    }
                    .secondaryAction()
                    .disabled(model.setupBusy || model.setupChecking)
                    .buttonRow()
                }
            }

            if let error = model.setupError {
                Section {
                    ErrorBox(title: "Set Up stopped", message: error)
                }
            }

            if !model.steps.isEmpty {
                Section {
                    ForEach(model.steps) { step in
                        StepRow(step: step)
                    }
                } header: {
                    Text(missing == 0 ? "Everything is in place" : "\(model.steps.count - missing) of \(model.steps.count) in place")
                } footer: {
                    Text("What this phone writes to on your server. Set Up does every missing step in order.")
                }
            }

            if !fromSettings {
                Section {
                    Button(role: .destructive) {
                        model.signOut()
                    } label: {
                        WideLabel(title: "Sign Out", systemImage: "rectangle.portrait.and.arrow.right")
                    }
                    .destructiveAction()
                    .buttonRow()
                }
            }
        }
        .readableWidth()
        .navigationTitle(fromSettings ? "Server Set Up" : "Set Up")
        .toolbar {
            if model.setupChecked, missing == 0, model.setupError == nil {
                ToolbarItem(placement: .primaryAction) {
                    if model.setupChecking {
                        ProgressView()
                    } else {
                        Button("Check Again", systemImage: "arrow.clockwise") { Task { await model.checkSetup() } }
                    }
                }
            }
        }
        .confirmationDialog(
            "The webhook already has a key",
            isPresented: $model.foreignKey, titleVisibility: .visible
        ) {
            Button("Use the Existing Key") { Task { await model.runSetup(replaceKey: false) } }
            Button("Replace It", role: .destructive) { Task { await model.runSetup(replaceKey: true) } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Something else set this webhook's key, such as Health Auto Export. Keeping it keeps that app working; replacing it stops it.")
        }
    }
}

/// One step: what it is, the server's name for it, its state as a word,
/// and why when it needs work.
private struct StepRow: View {
    let step: SetupStep

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: step.done ? "checkmark.circle.fill" : "circle.dashed")
                .foregroundStyle(step.done ? .green : .orange)
                .font(.title3)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(step.title)
                if let code = step.code {
                    Text(code).font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
                }
                if let detail = step.detail {
                    Text(detail).font(.footnote).foregroundStyle(.secondary).padding(.top, 2)
                }
            }
            Spacer(minLength: 8)
            StatusBadge(text: status, color: step.done ? .green : .orange)
        }
        .padding(.vertical, 2)
    }

    private var status: String {
        if step.done { return "Ready" }
        switch step.kind {
        case .install:
            let detail = step.detail ?? ""
            if detail.hasPrefix("Installed") { return "Upgrade" }
            if detail.hasPrefix("This server ships") { return "Server too old" }
            if detail.hasPrefix("This server does not") { return "Not shipped" }
            return "Needed"
        case .account, .trigger: return "Needed"
        }
    }
}
