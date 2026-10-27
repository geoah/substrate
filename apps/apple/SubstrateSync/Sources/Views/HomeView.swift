import Charts
import SwiftUI

/// The one page: what is running, each source's state, what the device holds
/// against what was synced, what the runs did, and the settings.
struct HomeView: View {
    @EnvironmentObject private var model: AppModel
    @AppStorage(Settings.historyKey) private var history = Settings.history.rawValue
    @AppStorage(Settings.frequencyKey) private var frequency = Settings.Frequency.hourly.rawValue
    @State private var sampleNote: String?

    var body: some View {
        Form {
            StatusSection()

            ForEach(Source.allCases) { source in
                SourceSection(source: source)
            }

            CoverageSection()

            ActivitySection()

            HistorySection()

            Section {
                Picker("Health history", selection: $history) {
                    ForEach(Settings.History.allCases) { Text($0.title).tag($0.rawValue) }
                }
                .onChange(of: history) { _, _ in Task { await model.loadCoverage() } }
                Picker("Sync in the background", selection: $frequency) {
                    ForEach(Settings.Frequency.allCases) { Text($0.title).tag($0.rawValue) }
                }
                .onChange(of: frequency) { _, raw in
                    if let f = Settings.Frequency(rawValue: raw) { model.setFrequency(f) }
                }
                LabeledContent("Calendar window", value: "\(Settings.windowBack) days back, \(Settings.windowAhead) ahead")
            } header: {
                Text("Settings")
            } footer: {
                Text("Health history is how far back Health is sent. Every sync sends the last three days again; older days in the range go once, and only days that hold data are read. iOS decides when a background sync runs; the frequency is the soonest it may.")
            }

            if let session = model.session {
                Section("Signed in") {
                    LabeledContent("Repository", value: session.repository)
                    LabeledContent("Server", value: session.server.absoluteString)
                    LabeledContent("Health account", value: model.state.healthAccount)
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
                    .buttonStyle(.bordered)
                    if let sampleNote { Text(sampleNote).font(.footnote).foregroundStyle(.secondary) }
                } header: {
                    Text("Debug")
                } footer: {
                    Text("Writes yesterday's Health samples, three contacts and a sample calendar on this device.")
                }
            }

            Section {
                BuildFooter()
            }
        }
        .navigationTitle("Substrate Sync")
        .refreshable { await model.syncNow() }
        .task { await model.loadCoverage() }
    }
}

/// What the app is doing right now, at the top of the page: the source and
/// step a sync is on, how far it has got, and a way to stop it; when idle,
/// the button that starts one and how the last one ended.
private struct StatusSection: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        Section {
            if model.syncing {
                let running = Source.allCases.filter { model.progress[$0] != nil }
                if running.isEmpty {
                    HStack(spacing: 10) { ProgressView(); Text("Starting…").foregroundStyle(.secondary) }
                }
                ForEach(running) { source in
                    VStack(alignment: .leading, spacing: 6) {
                        Label(source.title, systemImage: source.symbol).font(.headline)
                        if let p = model.progress[source] { ProgressRow(progress: p) }
                    }
                }
                Button(role: .destructive) {
                    model.cancelSync()
                } label: {
                    WideLabel(title: "Stop", systemImage: "stop.fill")
                }
                .secondaryAction()
            } else {
                Button {
                    Task { await model.syncNow() }
                } label: {
                    WideLabel(title: "Sync Now", systemImage: "arrow.triangle.2.circlepath")
                }
                .primaryAction()
                if let summary {
                    if summary.failed {
                        ErrorBox(title: "The last sync did not finish", message: summary.text)
                    } else {
                        Label(summary.text, systemImage: "checkmark.circle.fill")
                            .font(.footnote)
                            .foregroundStyle(.green)
                    }
                }
            }
        } header: {
            Text(model.syncing ? "Syncing" : "Status")
        }
    }

    /// How the last run of each source ended, in one line.
    private var summary: (text: String, failed: Bool)? {
        let statuses = Source.allCases.filter(Settings.enabled).compactMap { s in model.state.status[s].map { (s, $0) } }
        guard !statuses.isEmpty else { return nil }
        let failed = statuses.filter { $0.1.lastError != nil }.map(\.0.title)
        if !failed.isEmpty {
            return ("\(failed.joined(separator: ", ")): see the error under each, and Activity below.", true)
        }
        guard let last = statuses.compactMap(\.1.lastSync).max() else { return nil }
        return ("Everything synced \(last.formatted(.relative(presentation: .named))).", false)
    }
}

/// One source's step, with a bar and counts where the step can count.
private struct ProgressRow: View {
    let progress: SyncProgress

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(progress.step).font(.callout)
                Spacer()
                if progress.total > 0 {
                    Text("\(progress.done.formatted()) of \(progress.total.formatted())")
                        .font(.footnote.monospacedDigit()).foregroundStyle(.secondary)
                }
            }
            if let fraction = progress.fraction {
                ProgressView(value: fraction)
            } else {
                ProgressView().progressViewStyle(.linear)
            }
        }
    }
}

/// One source: its switch, access, what it holds against what was synced,
/// and how its last run went.
private struct SourceSection: View {
    @EnvironmentObject private var model: AppModel
    let source: Source
    @State private var enabled = true

    var body: some View {
        let status = model.state.status[source] ?? SourceStatus()
        Section {
            Toggle(isOn: $enabled) {
                Label(source.title, systemImage: source.symbol).font(.headline)
            }
            .onChange(of: enabled) { _, on in
                Settings.setEnabled(source, on)
                if on, source == .health { Background.observeHealth() }
            }
            if let (synced, total, noun) = totals, total > 0 {
                VStack(alignment: .leading, spacing: 6) {
                    LabeledContent("Synced", value: "\(synced.formatted()) of \(total.formatted()) \(noun)")
                    ProgressView(value: Double(min(synced, total)), total: Double(total))
                }
            }
            if let now = model.progress[source] {
                ProgressRow(progress: now)
            }
            LabeledContent("Access") {
                HStack(spacing: 8) {
                    Text(model.access[source] ?? "…").foregroundStyle(.secondary)
                    if model.access[source] == "Not asked yet" {
                        Button("Ask") { Task { await model.requestAccess(source) } }
                            .buttonStyle(.bordered)
                            .controlSize(.small)
                    }
                }
            }
            LabeledContent("Last sync") {
                if let last = status.lastSync {
                    Text(last, format: .relative(presentation: .named))
                } else {
                    Text("Never")
                }
            }
            if let counts = status.counts {
                LabeledContent("Last run", value: counts)
            }
            if let error = status.lastError {
                ErrorBox(title: "Last sync of \(source.title) failed", message: error)
            }
        }
        .onAppear { enabled = Settings.enabled(source) }
    }

    /// Synced, held on the device, and what they count.
    private var totals: (Int, Int, String)? {
        let state = model.state
        switch source {
        case .health:
            guard let inRange = model.healthInRange else { return nil }
            return (inRange.intersection(state.healthDays).count, inRange.count, "days with data in your history")
        case .contacts:
            guard let total = state.device.contacts else { return nil }
            return (state.contacts.count, total, "contacts")
        case .calendars:
            guard let total = model.eventsAvailable?.count ?? state.device.events else { return nil }
            return (state.events.values.filter { $0.start.map(Self.window.contains) ?? false }.count, total, "events in the window")
        }
    }

    private static var window: Range<Date> {
        let cal = Calendar.current
        let now = Date()
        return cal.startOfDay(for: cal.date(byAdding: .day, value: -Settings.windowBack, to: now)!)
            ..< cal.startOfDay(for: cal.date(byAdding: .day, value: Settings.windowAhead, to: now)!)
    }
}

/// What the device holds against what was synced, inside the range synced:
/// a summary with one action, a chart of the latest periods, the few most
/// recent, and every period on a page of its own.
private struct CoverageSection: View {
    @EnvironmentObject private var model: AppModel
    @AppStorage("coverage.source") private var sourceRaw = Source.health.rawValue
    @AppStorage("coverage.granularity") private var granularityRaw = Granularity.month.rawValue
    @AppStorage(Settings.historyKey) private var history = Settings.history.rawValue

    private var source: Source { Source(rawValue: sourceRaw) == .calendars ? .calendars : .health }
    private var granularity: Granularity { Granularity(rawValue: granularityRaw) ?? .month }
    private var unit: String { source == .health ? "days" : "events" }

    var body: some View {
        Section {
            Picker("Source", selection: $sourceRaw) {
                Text("Health").tag(Source.health.rawValue)
                Text("Calendars").tag(Source.calendars.rawValue)
            }
            .pickerStyle(.segmented)

            let buckets = self.buckets
            if model.coverageLoading, buckets.isEmpty {
                HStack(spacing: 10) { ProgressView(); Text("Reading what the device holds…").foregroundStyle(.secondary) }
            } else if buckets.isEmpty {
                Text(emptyText).font(.callout).foregroundStyle(.secondary)
            } else {
                summary(buckets)
                Picker("Group by", selection: $granularityRaw) {
                    ForEach(Granularity.allCases) { Text($0.title).tag($0.rawValue) }
                }
                .pickerStyle(.segmented)
                chart(buckets)
                ForEach(buckets.suffix(4).reversed()) { bucket in
                    BucketRow(bucket: bucket, unit: unit, canSend: source == .health)
                }
                if buckets.count > 4 {
                    NavigationLink("All \(buckets.count) \(granularity.title.lowercased())") {
                        CoverageListView(buckets: buckets, unit: unit, canSend: source == .health)
                    }
                }
            }
            if source == .health, let older = model.healthOutsideRange, !older.isEmpty {
                Text(olderText(older)).font(.footnote).foregroundStyle(.secondary)
            }
            if let error = model.coverageError {
                ErrorBox(title: "Could not read what the device holds", message: error)
            }
        } header: {
            HStack {
                Text("Coverage")
                Spacer()
                if model.coverageLoading { ProgressView().controlSize(.mini) }
            }
        }
    }

    private var emptyText: String {
        switch source {
        case .health: model.healthAvailable == nil ? "Allow Health access to see what the device holds." : "Health holds no steps, heart rate or sleep in your history range."
        case .calendars: model.eventsAvailable == nil ? "Allow Calendars access to see what the device holds." : "No events in the window."
        default: ""
        }
    }

    private var buckets: [CoverageBucket] {
        var calendar = Calendar.current
        calendar.timeZone = .current
        switch source {
        case .health:
            guard let available = model.healthInRange else { return [] }
            return Coverage.days(available: available, synced: model.state.healthDays, by: granularity, calendar: calendar)
        default:
            guard let available = model.eventsAvailable else { return [] }
            return Coverage.events(
                available: available, synced: model.state.events.values.compactMap(\.start), by: granularity, calendar: calendar)
        }
    }

    private func olderText(_ older: Set<String>) -> String {
        let first = older.min() ?? ""
        return "The phone also holds \(older.count.formatted()) days of Health data from before your history range, back to \(first). Choose a longer Health history in Settings to sync them."
    }

    @ViewBuilder
    private func summary(_ buckets: [CoverageBucket]) -> some View {
        let available = buckets.map(\.available).reduce(0, +)
        let synced = buckets.map(\.synced).reduce(0, +)
        let missing = buckets.flatMap(\.missing)
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("\(synced.formatted()) of \(available.formatted()) \(unit) synced").font(.callout.weight(.medium))
                Spacer()
                StatusBadge(text: synced >= available ? "Complete" : "\(Int((Double(synced) / Double(max(available, 1)) * 100).rounded()))%",
                            color: synced >= available ? .green : .orange)
            }
            ProgressView(value: Double(synced), total: Double(max(available, 1)))
            if source == .health, !missing.isEmpty {
                Button {
                    Task { await model.send(days: missing) }
                } label: {
                    WideLabel(title: "Send the \(missing.count.formatted()) missing days", systemImage: "arrow.up.circle")
                }
                .buttonStyle(.bordered)
                .disabled(model.syncing)
            }
        }
    }

    private func chart(_ buckets: [CoverageBucket]) -> some View {
        let shown = Array(buckets.suffix(granularity == .week ? 12 : granularity == .month ? 12 : 10))
        return Chart {
            ForEach(shown) { b in
                BarMark(x: .value("Period", b.start, unit: granularity.component), y: .value(unit, b.synced))
                    .foregroundStyle(by: .value("State", "Synced"))
                BarMark(x: .value("Period", b.start, unit: granularity.component), y: .value(unit, b.available - b.synced))
                    .foregroundStyle(by: .value("State", "Not synced"))
            }
        }
        .chartForegroundStyleScale(["Synced": Color.green, "Not synced": Color.gray.opacity(0.4)])
        .chartLegend(position: .bottom)
        .frame(height: 160)
        .padding(.vertical, 4)
    }
}

/// Every period, newest first, a section per year.
private struct CoverageListView: View {
    let buckets: [CoverageBucket]
    let unit: String
    let canSend: Bool

    var body: some View {
        let years = Dictionary(grouping: buckets) { Calendar.current.component(.year, from: $0.start) }
        List {
            ForEach(years.keys.sorted(by: >), id: \.self) { year in
                Section(String(year)) {
                    ForEach(years[year]!.sorted { $0.start > $1.start }) { bucket in
                        BucketRow(bucket: bucket, unit: unit, canSend: canSend)
                    }
                }
            }
        }
        .navigationTitle("Coverage")
    }
}

private struct BucketRow: View {
    @EnvironmentObject private var model: AppModel
    let bucket: CoverageBucket
    let unit: String
    let canSend: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(bucket.label).font(.callout)
                Spacer()
                Text("\(bucket.synced) of \(bucket.available) \(unit)").font(.callout.monospacedDigit()).foregroundStyle(.secondary)
            }
            ProgressView(value: bucket.fraction)
                .tint(bucket.fraction >= 1 ? .green : .orange)
            if canSend, !bucket.missing.isEmpty {
                Button("Send \(bucket.missing.count) missing \(bucket.missing.count == 1 ? "day" : "days")") {
                    Task { await model.send(days: bucket.missing) }
                }
                .font(.footnote)
                .buttonStyle(.bordered)
                .controlSize(.small)
                .disabled(model.syncing)
            }
        }
    }
}

/// The recent runs, newest first.
private struct HistorySection: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        let runs = model.state.runs
        Section("Recent runs") {
            if runs.isEmpty {
                Text("No sync has run yet.").foregroundStyle(.secondary)
            }
            ForEach(Array(runs.prefix(5))) { run in
                RunRow(run: run)
            }
            if runs.count > 5 {
                NavigationLink("All \(runs.count) runs") {
                    List(runs) { RunRow(run: $0) }.navigationTitle("Recent runs")
                }
            }
        }
    }
}

private struct RunRow: View {
    let run: SyncRun

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Label(run.source.title, systemImage: run.error == nil ? run.source.symbol : "exclamationmark.triangle.fill")
                    .foregroundStyle(run.error == nil ? Color.primary : Color.red)
                Spacer()
                Text(run.finished, format: .dateTime.day().month().hour().minute())
                    .font(.footnote).foregroundStyle(.secondary)
            }
            Text(run.error ?? run.summary ?? "")
                .font(.footnote)
                .foregroundStyle(run.error == nil ? Color.secondary : Color.red)
            Text("Took \(Duration.seconds(run.finished.timeIntervalSince(run.started)).formatted(.units(allowed: [.minutes, .seconds], width: .abbreviated)))")
                .font(.caption).foregroundStyle(.tertiary)
        }
    }
}

/// The latest activity: what each run read, wrote, skipped and why it
/// failed, newest first.
private struct ActivitySection: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        Section {
            if model.activity.isEmpty {
                Text("Nothing has run yet.").foregroundStyle(.secondary)
            }
            ForEach(model.activity.prefix(8)) { entry in
                ActivityRow(entry: entry)
            }
            if !model.activity.isEmpty {
                NavigationLink("All activity (\(model.activity.count))") { ActivityView() }
            }
        } header: {
            Text("Activity")
        }
    }
}

private struct ActivityRow: View {
    let entry: ActivityEntry

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 6) {
                Image(systemName: icon).foregroundStyle(color)
                Text(entry.source?.title ?? "Sync").font(.footnote.weight(.semibold))
                Spacer()
                Text(entry.at, format: .dateTime.hour().minute().second())
                    .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
            }
            Text(entry.message)
                .font(.footnote)
                .foregroundStyle(entry.level == .info ? Color.primary : color)
                .textSelection(.enabled)
        }
    }

    private var icon: String {
        switch entry.level {
        case .info: "info.circle"
        case .warning: "exclamationmark.triangle"
        case .error: "xmark.octagon"
        }
    }

    private var color: Color {
        switch entry.level {
        case .info: .secondary
        case .warning: .orange
        case .error: .red
        }
    }
}

/// The whole log, to read or to share.
struct ActivityView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        List(model.activity) { entry in
            ActivityRow(entry: entry)
        }
        .navigationTitle("Activity")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                ShareLink(item: Activity.shared.text, preview: SharePreview("Substrate Sync activity"))
            }
            ToolbarItem(placement: .secondaryAction) {
                Button("Clear", role: .destructive) { model.clearActivity() }
            }
        }
    }
}
