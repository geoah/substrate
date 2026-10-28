import SwiftUI

/// The phone's event calendars, by account, each with a switch: which ones
/// the sync sends.
struct CalendarPickerView: View {
    @EnvironmentObject private var model: AppModel
    @State private var excluded = Settings.excludedCalendars

    var body: some View {
        let accounts = Dictionary(grouping: model.calendarChoices, by: \.account)
        List {
            if model.calendarChoices.isEmpty {
                Text("Allow Calendars access to choose which calendars to sync.").foregroundStyle(.secondary)
            }
            ForEach(accounts.keys.sorted(), id: \.self) { account in
                let calendars = accounts[account]!
                Section {
                    ForEach(calendars) { calendar in
                        Toggle(isOn: binding(calendar.id)) {
                            HStack(spacing: 10) {
                                Circle().fill(calendar.color).frame(width: 12, height: 12)
                                VStack(alignment: .leading, spacing: 1) {
                                    Text(calendar.title)
                                    if let note = calendar.note {
                                        Text(note).font(.caption).foregroundStyle(.secondary)
                                    }
                                }
                            }
                        }
                    }
                } header: {
                    HStack {
                        Text(account)
                        Spacer()
                        let allOn = calendars.allSatisfy { !excluded.contains($0.id) }
                        Button(allOn ? "Turn All Off" : "Turn All On") {
                            for c in calendars { set(c.id, included: !allOn) }
                        }
                        .font(.caption)
                        .textCase(nil)
                    }
                }
            }
        }
        .navigationTitle("Calendars")
        .safeAreaInset(edge: .bottom) {
            Text("A calendar turned off is not sent, and the next sync removes it and its events from your repository. A calendar added on the phone later is sent until you turn it off.")
                .font(.footnote).foregroundStyle(.secondary)
                .padding()
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(.bar)
        }
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
