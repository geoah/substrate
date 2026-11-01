import SubstrateKit
import SwiftUI

/// The account, the app's own settings, the server set up, and signing out.
/// What one source lets the owner choose is on that source's page. An app
/// session's account is the kit's: what the owner approved and until when,
/// and the way to change it.
struct SettingsView: View {
    @EnvironmentObject private var model: AppModel
    @AppStorage(Settings.frequencyKey) private var frequency = Settings.Frequency.hourly.rawValue
    @State private var sampleNote: String?
    @State private var changingAccess = false

    private var isApp: Bool { model.session?.isApp == true }

    var body: some View {
        Form {
            if let session = model.session, session.isApp {
                AccountSection(session: session, manifest: try? model.manifest(), serverVersion: model.status.status?.serverVersion,
                               unrevoked: model.unrevoked, onChangeAccess: { changingAccess = true }, onSignOut: { model.signOut() })
            } else if let session = model.session {
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
                if isApp {
                    NavigationLink {
                        ServerStatusView(fromSettings: true)
                    } label: {
                        HStack {
                            Text("Server Status")
                            Spacer()
                            let badge = statusBadge
                            StatusBadge(text: badge.text, color: badge.color)
                        }
                    }
                } else {
                    NavigationLink {
                        SetupView(fromSettings: true)
                    } label: {
                        HStack {
                            Text("Server Set Up")
                            Spacer()
                            StatusBadge(text: model.setupComplete ? "Complete" : "Needs work", color: model.setupComplete ? .green : .orange)
                        }
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

            // An app session's Sign Out is in its account section.
            if !isApp {
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
        .navigationTitle("Settings")
        .sheet(isPresented: $changingAccess) {
            ReviewChangesSheet(appName: SyncManifest.appName, host: model.session.map { ServerAddress.display($0.server) } ?? "your server",
                               changes: model.reviewChanges, heading: .changeAccess,
                               onContinue: {
                                   changingAccess = false
                                   Task { await model.authorize() }
                               },
                               onCancel: { changingAccess = false })
        }
    }

    /// The server status in a word: the status screen's own. A server that
    /// could not be asked was not checked, and one only an update helps
    /// needs that, not the owner's approval.
    private var statusBadge: (text: String, color: Color) {
        guard let issues = model.status.status?.issues else { return ("Not checked", .secondary) }
        if issues.isEmpty { return ("Up to date", .green) }
        if issues.allSatisfy({ if case .unreachable = $0 { true } else { false } }) { return ("Not checked", .secondary) }
        if issues.waitsForUpdate { return ("Needs an update", .orange) }
        return ("Needs approval", .orange)
    }
}
