import SwiftUI

/// The account, the app's own settings, the server set up, and signing out.
/// What one source lets the owner choose is on that source's page.
struct SettingsView: View {
    @EnvironmentObject private var model: AppModel
    @AppStorage(Settings.frequencyKey) private var frequency = Settings.Frequency.hourly.rawValue
    @State private var sampleNote: String?

    var body: some View {
        Form {
            if let session = model.session {
                Section("Account") {
                    LabeledContent("Repository", value: session.repository)
                    LabeledContent("Server") {
                        Text(session.server.absoluteString).multilineTextAlignment(.trailing)
                    }
                }
            }

            Section {
                Picker("Sync in the background", selection: $frequency) {
                    ForEach(Settings.Frequency.allCases) { Text($0.title).tag($0.rawValue) }
                }
                .onChange(of: frequency) { _, raw in
                    if let f = Settings.Frequency(rawValue: raw) { model.setFrequency(f) }
                }
            } footer: {
                Text("iOS decides when a background sync runs; this is the soonest it may. A background sync sends only the last three days of Health.")
            }

            Section {
                NavigationLink {
                    SetupView()
                } label: {
                    HStack {
                        Text("Server Set Up")
                        Spacer()
                        StatusBadge(text: model.setupComplete ? "Complete" : "Needs work", color: model.setupComplete ? .green : .orange)
                    }
                }
                LabeledContent("Health account", value: model.state.healthAccount)
            } footer: {
                Text("The providers this phone writes to, its Health account and the webhook Health is sent to.")
            }

            Section {
                BuildFooter()
            }

            if SampleData.available {
                Section {
                    Button("Add Sample Data") {
                        sampleNote = "Adding…"
                        Task {
                            do { sampleNote = "Added \(try await SampleData.add())." } catch {
                                sampleNote = "Adding sample data failed: \(error.localizedDescription)"
                            }
                            model.refreshAccess()
                            await model.loadCoverage()
                        }
                    }
                    if let sampleNote { Text(sampleNote).font(.footnote).foregroundStyle(.secondary) }
                } header: {
                    Text("Debug")
                } footer: {
                    Text("Writes yesterday's Health samples, three contacts and a sample calendar on this device.")
                }
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
        .navigationTitle("Settings")
    }
}
