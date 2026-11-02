import WorkoutsCore
import SwiftUI
import UIKit

/// Settings (DESIGN.md §7.10): units, rest defaults, the session switches,
/// alerts, Apple Health, where the data comes from, and the build's update
/// check. A session snapshots the rest and counting settings at Start.
struct SettingsView: View {
    @Environment(AppModel.self) private var app
    @Environment(SessionController.self) private var session
    @EnvironmentObject private var updates: Updater
    @Environment(\.openURL) private var openURL
    @State private var healthBodyMass: Mass?
    @State private var bodyMassText = ""
    @State private var warmupWheel = false
    @State private var connecting = false
    @FocusState private var bodyMassFocused: Bool

    var body: some View {
        @Bindable var app = app
        NavigationStack {
            Form {
                Section {
                    LabeledContent("Default unit") {
                        Picker("Default unit", selection: $app.settings.defaultUnit) {
                            Text("kg").tag(MassUnit.kg)
                            Text("lb").tag(MassUnit.lb)
                        }
                        .pickerStyle(.segmented)
                        .fixedSize()
                    }
                } header: {
                    Text("Units")
                } footer: {
                    Text("Loads keep the unit they were entered in; this unit is for new loads and totals.")
                }

                Section("Rest") {
                    LabeledContent("After a warm-up") {
                        Menu {
                            RestPresetItems(current: app.settings.warmupRest, defaultText: nil) { seconds in
                                app.settings.warmupRest = seconds ?? RestDefaults.warmupRest
                            } custom: {
                                warmupWheel = true
                            }
                        } label: {
                            Text(Format.rest(app.settings.warmupRest))
                                .monospacedDigit()
                        }
                    }
                    NavigationLink("Between sets, by exercise class") {
                        RestDefaultsView()
                    }
                    Toggle("Rest after an exercise\u{2019}s last set", isOn: $app.settings.restAfterLastSet)
                }

                Section {
                    LabeledContent("Layout") {
                        Picker("Layout", selection: $app.settings.sessionLayout) {
                            ForEach(SessionLayout.allCases, id: \.self) { layout in
                                Text(layout.title).tag(layout)
                            }
                        }
                        .pickerStyle(.segmented)
                        .fixedSize()
                    }
                    Toggle("Keep the screen awake", isOn: $app.settings.keepAwake)
                    Toggle("Count warm-ups in volume", isOn: $app.settings.countWarmupsInVolume)
                    Toggle("Scroll to the next exercise in a superset", isOn: $app.settings.supersetAutoScroll)
                } header: {
                    Text("Session")
                } footer: {
                    Text("Rows shows each set as a line, with the set to do now open and its planned RPE filled in. Table shows the grid. A session in progress keeps the rest and volume settings it started with.")
                }

                SubstrateSection()
                alertsSection
                healthSection
                dataSection

                Section("About") { BuildFooter() }
            }
            .navigationTitle("Settings")
            .sheet(isPresented: $warmupWheel) {
                RestPickerSheet(title: "After a warm-up", initial: app.settings.warmupRest) { seconds in
                    app.settings.warmupRest = seconds
                }
            }
            .task { await loadHealthBodyMass() }
            .onChange(of: bodyMassFocused) { _, focused in
                if !focused { saveTypedBodyMass() }
            }
        }
    }

    // MARK: - Alerts

    private var alertsSection: some View {
        Section("Alerts") {
            if session.alertsOff {
                Label("Rest alerts are off. Rest ends will not notify.", systemImage: "bell.slash")
                    .foregroundStyle(.orange)
                openSettingsButton
            } else if session.alertsNotAsked {
                Label("Rest alerts: not asked yet. Workouts asks at the first Start.", systemImage: "bell")
            } else if session.timeSensitiveOff {
                Label("Rest alerts: on, not time sensitive. A Focus may hold them back.", systemImage: "bell")
                openSettingsButton
            } else {
                Label("Rest alerts: on, time sensitive", systemImage: "bell.badge")
            }
        }
    }

    private var openSettingsButton: some View {
        Button("Open Settings") {
            if let url = URL(string: UIApplication.openNotificationSettingsURLString) { openURL(url) }
        }
    }

    // MARK: - Apple Health

    private var healthSection: some View {
        Section {
            HStack {
                Text(healthText)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if session.healthAccess == .notDetermined {
                    Button("Connect") {
                        connecting = true
                        Task {
                            await session.connectHealth()
                            await loadHealthBodyMass()
                            connecting = false
                        }
                    }
                    .buttonStyle(.bordered)
                    .disabled(connecting)
                }
            }
            if let healthBodyMass {
                Text("Body mass: \(Format.mass(healthBodyMass)) from Apple Health")
            } else {
                LabeledContent("Body mass") {
                    HStack(spacing: 4) {
                        TextField("None", text: $bodyMassText)
                            .keyboardType(.decimalPad)
                            .multilineTextAlignment(.trailing)
                            .focused($bodyMassFocused)
                            .monospacedDigit()
                        Text(app.settings.typedBodyMass?.unit.rawValue ?? app.settings.defaultUnit.rawValue)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        } header: {
            Text("Apple Health")
        } footer: {
            Text("Each finished session is saved as one strength workout. Body mass works out bodyweight and assisted sets.")
        }
    }

    private var healthText: String {
        switch session.healthAccess {
        case .authorized: "Saving workouts: allowed"
        case .notDetermined: "Saving workouts: not asked"
        case .denied: "Saving workouts: not allowed. Allow it in the Health app."
        case .unavailable: "Apple Health is not available on this iPhone."
        }
    }

    private func loadHealthBodyMass() async {
        healthBodyMass = await session.latestHealthBodyMass()
        if let typed = app.settings.typedBodyMass { bodyMassText = Format.number(typed.value) }
    }

    private func saveTypedBodyMass() {
        let text = bodyMassText.trimmingCharacters(in: .whitespaces)
        if text.isEmpty {
            app.settings.typedBodyMass = nil
        } else if let value = GridValue.decimal(text), value > 0 {
            app.settings.typedBodyMass = Mass(value: value, unit: app.settings.typedBodyMass?.unit ?? app.settings.defaultUnit)
        } else if let typed = app.settings.typedBodyMass {
            bodyMassText = Format.number(typed.value)
        }
    }

    // MARK: - Data

    private var dataSection: some View {
        let routines = app.catalog?.routines.count ?? 0
        let local = app.exercises.values.count(where: \.isLocal)
        let bundled = app.exercises.count - local
        let unreadable = app.unreadable
        return Section("Data") {
            if app.catalogFailure != nil {
                Label("This build\u{2019}s routines and exercises could not be read.", systemImage: "exclamationmark.triangle")
                    .foregroundStyle(.orange)
            } else if case let .server(host) = app.catalogOrigin {
                Text("Routines and exercises come from \(host): \(Format.count(routines, "routine")), \(Format.count(bundled, "exercise")).")
            } else {
                Text("Routines and exercises come with this build: \(Format.count(routines, "routine")), \(Format.count(bundled, "exercise")).")
            }
            LabeledContent("Exercises added on this iPhone", value: "\(local)")
            if unreadable.isEmpty {
                LabeledContent("Records that could not be read", value: "none")
            } else {
                NavigationLink {
                    UnreadableList(records: unreadable)
                } label: {
                    LabeledContent("Records that could not be read", value: "\(unreadable.count)")
                }
            }
        }
        .font(.subheadline)
    }
}

/// The records that did not decode: skipped, never deleted (§2.3).
private struct UnreadableList: View {
    let records: [UnreadableRecord]

    var body: some View {
        List(Array(records.enumerated()), id: \.offset) { _, record in
            VStack(alignment: .leading, spacing: 2) {
                Text(record.id ?? record.source)
                    .font(.subheadline.weight(.semibold))
                Text(record.source)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Text(record.reason)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .navigationTitle("Could not be read")
        .navigationBarTitleDisplayMode(.inline)
    }
}

#if DEBUG
#Preview {
    let app = PreviewData.app()
    SettingsView()
        .environment(app)
        .environment(PreviewData.noSession(app))
        .environmentObject(Updater())
}
#endif
