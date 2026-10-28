import SwiftUI

/// The phone's event calendars, by account, each with a switch: which ones
/// the sync sends.
struct CalendarPickerView: View {
    @EnvironmentObject private var model: AppModel
    @State private var excluded = Settings.excludedCalendars

    var body: some View {
        let accounts = Dictionary(grouping: model.calendarChoices, by: \.account)
        List {
            Section {
                if model.calendarChoices.isEmpty {
                    Text(model.access[.calendars] == "Allowed" ? "This phone has no calendars." : "Allow Calendars access to choose which calendars to sync.")
                        .foregroundStyle(.secondary)
                } else {
                    Text("A calendar turned off is not sent, and its copy on your server is removed at the next sync. The calendar on this phone stays as it is.")
                        .font(.subheadline).foregroundStyle(.secondary)
                }
            }
            ForEach(accounts.keys.sorted(), id: \.self) { account in
                let calendars = accounts[account]!
                let allOn = calendars.allSatisfy { !excluded.contains($0.id) }
                Section(account) {
                    ForEach(calendars) { calendar in
                        Toggle(isOn: binding(calendar.id)) {
                            HStack(spacing: 10) {
                                Circle().fill(calendar.color).frame(width: 12, height: 12).accessibilityHidden(true)
                                VStack(alignment: .leading, spacing: 1) {
                                    Text(calendar.title)
                                    if let note = calendar.note {
                                        Text(note).font(.caption).foregroundStyle(.secondary)
                                    }
                                }
                            }
                        }
                    }
                    if calendars.count > 1 {
                        Button(allOn ? "Turn All \(account) Calendars Off" : "Turn All \(account) Calendars On") {
                            for c in calendars { set(c.id, included: !allOn) }
                        }
                    }
                }
            }
        }
        .readableWidth()
        .navigationTitle("Calendars to Sync")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear { model.loadCalendars() }
        .onDisappear { Task { await model.loadCoverage() } }
    }

    private func binding(_ id: String) -> Binding<Bool> {
        Binding(get: { !excluded.contains(id) }, set: { set(id, included: $0) })
    }

    private func set(_ id: String, included: Bool) {
        Settings.setCalendar(id, included: included)
        excluded = Settings.excludedCalendars
    }
}
