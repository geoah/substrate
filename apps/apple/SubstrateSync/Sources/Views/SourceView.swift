import Charts
import SwiftUI

/// One source's page: where it stands, what it has synced of what the phone
/// holds, its settings, and its last runs.
struct SourceView: View {
    @EnvironmentObject private var model: AppModel
    let source: Source
    @State private var enabled = true

    var body: some View {
        List {
            Section {
                StateCard(source: source, enabled: enabled)
            }

            if enabled {
                switch source {
                case .health: HealthCoverage()
                case .contacts, .calendars, .reminders, .places: CountCoverage(source: source)
                }
            }

            SourceSettings(source: source, enabled: $enabled)

            RecentRuns(source: source)
        }
        .readableWidth()
        .navigationTitle(source.title)
        .onAppear { enabled = Settings.enabled(source) }
    }
}

/// The source's state in one card: the badge, when it last synced, what it
/// is doing, the error while it is the news, and the button that syncs this
/// source alone. What each run did is under Recent runs.
private struct StateCard: View {
    @EnvironmentObject private var model: AppModel
    let source: Source
    let enabled: Bool

    var body: some View {
        let overview = model.overview(source)
        let status = model.state.status[source]
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                StatusBadge(text: overview.badge, color: overview.tone.color)
                Spacer()
                Group {
                    if let last = model.lastRun(source) {
                        Text("Last run \(last.shortAgo)")
                    } else {
                        Text("Never run")
                    }
                }
                .font(.footnote).foregroundStyle(.secondary)
            }
            if let p = model.progress[source] {
                ProgressRow(progress: p)
            } else if overview.badge == "Stopped", let reason = overview.error {
                Label(reason, systemImage: "stop.circle.fill")
                    .font(.subheadline)
                    .foregroundStyle(.secondary, .orange)
            } else if let error = overview.error {
                ErrorBox(title: "The last sync didn't finish", message: error)
                if let last = status?.lastSync {
                    Text("Last complete sync \(last.formatted(.relative(presentation: .named))).")
                        .font(.footnote).foregroundStyle(.secondary)
                }
            } else if source == .health, !model.healthWaiting.isEmpty {
                Text("\(model.healthWaiting.count) days were delivered and are waiting for the server to file them. The next sync checks them before it sends anything.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            if enabled {
                // One button, the same size in every state: what it would do,
                // or what is happening instead.
                Button {
                    Task { await model.syncNow([source]) }
                } label: {
                    switch overview.badge {
                    case "Syncing": WideLabel(title: "Syncing…", busy: true)
                    case "Queued": WideLabel(title: "Waiting Its Turn…", systemImage: "clock")
                    default: WideLabel(title: "Sync \(source.title) Now", systemImage: "arrow.triangle.2.circlepath")
                    }
                }
                .primaryAction()
                .disabled(overview.badge == "Syncing" || overview.badge == "Queued")
            } else {
                Text("\(source.title) is off. Turn it on below to sync it.")
                    .font(.subheadline).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 4)
    }
}

/// Health's days with data inside the history against those synced, a chart
/// of them, every period on a page of its own, and the days older than the
/// history, which go only when asked for.
private struct HealthCoverage: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        Section {
            if let inRange = model.healthInRange, !inRange.isEmpty {
                let synced = inRange.intersection(model.state.healthDays).count
                let waiting = model.healthWaiting.intersection(inRange).count
                let toSend = inRange.count - synced - waiting
                VStack(alignment: .leading, spacing: 10) {
                    Figures(synced: synced, total: inRange.count, noun: "days synced")
                    Text(explanation(days: inRange.count, synced: synced, waiting: waiting, toSend: toSend))
                        .font(.footnote).foregroundStyle(.secondary)
                    let buckets = buckets(inRange)
                    if buckets.count > 1 {
                        CoverageChart(buckets: buckets, granularity: granularity(inRange), unit: "days")
                    }
                }
                .padding(.vertical, 4)
                let all = buckets(inRange)
                if all.count > 1 {
                    NavigationLink("Each \(granularity(inRange).rawValue)") {
                        CoverageListView(buckets: all, unit: "days")
                    }
                }
            } else if model.coverageLoading {
                HStack(spacing: 10) { ProgressView(); Text("Reading what the phone holds…").foregroundStyle(.secondary) }
            } else {
                Text(model.healthAvailable == nil ? "Allow Health access to see what the phone holds." : "No steps, heart rate or sleep in your history.")
                    .font(.callout).foregroundStyle(.secondary)
            }
            if let older = model.healthOutsideRange, !older.isEmpty {
                // Offered for the days neither sent nor already waiting for
                // the next sync, so a second tap never queues them twice.
                let unsent = older.subtracting(model.state.healthDays)
                let toQueue = unsent.subtracting(model.state.healthQueue)
                VStack(alignment: .leading, spacing: 8) {
                    Text("The phone holds \(older.count.formatted()) more days from before your history, back to \(older.min() ?? "").")
                        .font(.footnote).foregroundStyle(.secondary)
                    if unsent.isEmpty {
                        Text("All of them are sent.").font(.footnote).foregroundStyle(.secondary)
                    } else if toQueue.isEmpty {
                        Text("\(unsent.count.formatted()) of them go with the next Health sync.").font(.footnote).foregroundStyle(.secondary)
                    } else {
                        Button("Send These \(toQueue.count.formatted()) Days Too") {
                            Task { await model.send(days: toQueue.sorted()) }
                        }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                        .disabled(model.syncing)
                    }
                }
                .padding(.vertical, 2)
            }
            if let error = model.coverageError {
                ErrorBox(title: "Could not read what the phone holds", message: error)
            }
        } header: {
            HStack {
                Text("Coverage · \(Settings.history.title)")
                Spacer()
                if model.coverageLoading { ProgressView().controlSize(.mini) }
            }
        }
    }

    /// What the figures count, in a sentence: "Health has data on 13 of the
    /// last 14 days. 11 are on the server, 2 go at the next sync."
    private func explanation(days: Int, synced: Int, waiting: Int, toSend: Int) -> String {
        var calendar = Calendar.current
        calendar.timeZone = .current
        let today = calendar.startOfDay(for: Date())
        let scope: String
        if let start = Settings.history.start(today: today, calendar: calendar) {
            let span = (calendar.dateComponents([.day], from: start, to: today).day ?? 0) + 1
            scope = "Health has data on \(days.formatted()) of the last \(span.formatted()) days."
        } else {
            scope = "Health has data on \(days.formatted()) days in all."
        }
        var parts = ["\(synced.formatted()) \(synced == 1 ? "is" : "are") on the server"]
        if waiting > 0 { parts.append("\(waiting.formatted()) \(waiting == 1 ? "is" : "are") waiting for the server to file \(waiting == 1 ? "it" : "them")") }
        if toSend > 0 { parts.append("\(toSend.formatted()) go\(toSend == 1 ? "es" : "") at the next sync") }
        let list = parts.count > 1 ? parts.dropLast().joined(separator: ", ") + " and " + parts.last! : parts[0]
        return "\(scope) \(list)."
    }

    private func granularity(_ days: Set<String>) -> Granularity {
        guard let first = days.min(), let date = Coverage.dayParser(.current).date(from: first) else { return .day }
        return Granularity.fitting(days: Calendar.current.dateComponents([.day], from: date, to: Date()).day ?? 0)
    }

    private func buckets(_ days: Set<String>) -> [CoverageBucket] {
        var calendar = Calendar.current
        calendar.timeZone = .current
        return Coverage.days(available: days, synced: model.state.healthDays, by: granularity(days), calendar: calendar)
    }
}

/// Contacts and Calendars: what was synced of what the phone holds, in one row.
private struct CountCoverage: View {
    @EnvironmentObject private var model: AppModel
    let source: Source

    var body: some View {
        Section("Coverage") {
            if let f = model.figures(source), f.total > 0 {
                VStack(alignment: .leading, spacing: 10) {
                    Figures(synced: f.synced, total: f.total, noun: "\(f.noun) synced")
                    if source == .calendars {
                        Text("From \(Settings.windowBack) days back to \(Settings.windowAhead) days ahead, in the calendars turned on below.")
                            .font(.footnote).foregroundStyle(.secondary)
                    }
                }
                .padding(.vertical, 4)
            } else if model.lastRun(source) != nil {
                Text(empty).font(.callout).foregroundStyle(.secondary)
            } else {
                Text("Sync once to see what the phone holds.").font(.callout).foregroundStyle(.secondary)
            }
        }
    }

    /// What a sync found nothing of, in the source's words.
    private var empty: String {
        switch source {
        case .contacts: "No contacts on this phone."
        case .calendars: "No events in the calendars turned on, from \(Settings.windowBack) days back to \(Settings.windowAhead) ahead."
        case .reminders: "No reminders on this phone."
        case .places: "No visits recorded yet. They appear as you arrive at places and leave them."
        case .health: "No Health data in your history."
        }
    }
}

/// "6 of 13 days synced", its share as a badge, and a bar.
private struct Figures: View {
    let synced: Int
    let total: Int
    let noun: String

    /// Complete is one line; a bar and a share only while something is
    /// missing, where they say how much.
    var body: some View {
        if synced >= total {
            Label("All \(total.formatted()) \(noun)", systemImage: "checkmark.circle.fill")
                .font(.callout.weight(.medium))
                .foregroundStyle(.primary, .green)
        } else {
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    Text("\(synced.formatted()) of \(total.formatted()) \(noun)").font(.callout.weight(.medium))
                    Spacer()
                    Text("\(Int((Double(synced) / Double(max(total, 1)) * 100).rounded()))%")
                        .font(.callout.monospacedDigit()).foregroundStyle(.secondary)
                }
                ProgressView(value: Double(min(synced, total)), total: Double(max(total, 1)))
            }
            .accessibilityElement(children: .combine)
        }
    }
}

private struct CoverageChart: View {
    let buckets: [CoverageBucket]
    let granularity: Granularity
    let unit: String

    var body: some View {
        Chart {
            ForEach(buckets.suffix(31)) { b in
                BarMark(x: .value("Period", b.start, unit: granularity.component), y: .value(unit, b.synced))
                    .foregroundStyle(by: .value("State", "Synced"))
                BarMark(x: .value("Period", b.start, unit: granularity.component), y: .value(unit, b.available - b.synced))
                    .foregroundStyle(by: .value("State", "Not yet"))
            }
        }
        .chartForegroundStyleScale(["Synced": Color.green, "Not yet": Color.gray.opacity(0.35)])
        .chartLegend(position: .bottom)
        .frame(height: 140)
    }
}

/// Every period, newest first, a section per year.
private struct CoverageListView: View {
    let buckets: [CoverageBucket]
    let unit: String

    var body: some View {
        let years = Dictionary(grouping: buckets) { Calendar.current.component(.year, from: $0.start) }
        List {
            ForEach(years.keys.sorted(by: >), id: \.self) { year in
                Section(String(year)) {
                    ForEach(years[year]!.sorted { $0.start > $1.start }) { bucket in
                        HStack {
                            Text(bucket.label).font(.callout)
                            Spacer()
                            Text("\(bucket.synced) of \(bucket.available)").font(.callout.monospacedDigit()).foregroundStyle(.secondary)
                            Image(systemName: bucket.fraction >= 1 ? "checkmark.circle.fill" : "circle.dashed")
                                .foregroundStyle(bucket.fraction >= 1 ? .green : .orange)
                        }
                    }
                }
            }
        }
        .navigationTitle("Coverage")
    }
}

/// The switch, the access, and what each source lets the owner choose.
private struct SourceSettings: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.openURL) private var openURL
    let source: Source
    @Binding var enabled: Bool
    @AppStorage(Settings.historyKey) private var history = Settings.history.rawValue

    var body: some View {
        Section {
            Toggle("Sync \(source.title)", isOn: $enabled)
                .onChange(of: enabled) { _, on in
                    Settings.setEnabled(source, on)
                    if on, source == .health { Background.observeHealth() }
                    if source == .places {
                        Task {
                            if on { await model.requestAccess(.places) }
                            PlaceMonitor.shared.start()
                        }
                    }
                }
            LabeledContent("Access") {
                HStack(spacing: 8) {
                    Text(access).foregroundStyle(.secondary)
                    if access == "Not asked yet" {
                        Button("Allow") { Task { await model.requestAccess(source) } }
                            .buttonStyle(.bordered).controlSize(.small)
                    } else if access.hasPrefix("Not allowed") || access.hasPrefix("Add only") || access.hasPrefix("While using") {
                        Button("Settings") {
                            if let url = URL(string: UIApplication.openSettingsURLString) { openURL(url) }
                        }
                        .buttonStyle(.bordered).controlSize(.small)
                    }
                }
            }
            switch source {
            case .health:
                Picker("History", selection: $history) {
                    ForEach(Settings.History.allCases) { Text($0.title).tag($0.rawValue) }
                }
                .onChange(of: history) { _, _ in Task { await model.loadCoverage() } }
            case .calendars:
                NavigationLink {
                    CalendarPickerView()
                } label: {
                    LabeledContent("Calendars", value: calendarsValue)
                }
            case .contacts, .reminders, .places:
                EmptyView()
            }
        } header: {
            Text("Settings")
        } footer: {
            switch source {
            case .health:
                Text("History is how far back Health is sent. Every sync sends the last three days again; older days in the history go once.")
            case .calendars:
                Text("A calendar turned off is not sent, and the next sync removes it and its events from your repository.")
            case .contacts:
                if access == "Allowed for some contacts" {
                    Text("Only the contacts you chose to share are sent, and none is removed from your repository.")
                }
            case .reminders:
                Text("Every list and every reminder, completed ones included. A reminder deleted on the phone is removed from your repository.")
            case .places:
                Text("Records the places you stay at while it is on, named by Apple Maps. Needs Always location access; iOS reports only arrivals and departures, so it costs little battery. Keeps a year of visits on this phone.")
            }
        }
        .onAppear { if source == .calendars { model.loadCalendars() } }
    }

    private var access: String { model.access[source] ?? "…" }

    private var calendarsValue: String {
        let all = model.calendarChoices.count
        guard all > 0 else { return "None" }
        let excluded = Settings.excludedCalendars
        let on = model.calendarChoices.filter { !excluded.contains($0.id) }.count
        return on == all ? "All \(all)" : "\(on) of \(all)"
    }
}

/// This source's last three runs, and the way to all of its activity.
private struct RecentRuns: View {
    @EnvironmentObject private var model: AppModel
    let source: Source

    var body: some View {
        let runs = model.state.runs.filter { $0.source == source }
        if !runs.isEmpty {
            Section("Recent runs") {
                ForEach(Array(runs.prefix(3))) { run in
                    RunRow(run: run, showsSource: false)
                }
                NavigationLink("All \(source.title) activity") {
                    ActivityView(only: source)
                }
            }
        }
    }
}
