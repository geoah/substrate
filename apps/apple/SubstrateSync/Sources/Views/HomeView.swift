import Charts
import SwiftUI

/// The one page: each source's state and progress, what the device holds
/// against what was synced over time, the recent runs, and the settings.
struct HomeView: View {
    @EnvironmentObject private var model: AppModel
    @AppStorage(Settings.backfillKey) private var backfill = 14
    @AppStorage(Settings.frequencyKey) private var frequency = Settings.Frequency.hourly.rawValue
    @State private var signOutProblem: String?
    @State private var sampleNote: String?

    var body: some View {
        Form {
            ForEach(Source.allCases) { source in
                SourceSection(source: source)
            }

            Section {
                Button {
                    Task { await model.syncNow() }
                } label: {
                    HStack {
                        Label("Sync Now", systemImage: "arrow.triangle.2.circlepath")
                        if model.syncing { Spacer(); ProgressView() }
                    }
                }
                .disabled(model.syncing)
            }

            CoverageSection()

            HistorySection()

            Section {
                Picker("Sync in the background", selection: $frequency) {
                    ForEach(Settings.Frequency.allCases) { Text($0.title).tag($0.rawValue) }
                }
                .onChange(of: frequency) { _, raw in
                    if let f = Settings.Frequency(rawValue: raw) { model.setFrequency(f) }
                }
                Picker("Health backfill", selection: $backfill) {
                    ForEach(Settings.backfillChoices, id: \.self) { Text("\($0) days").tag($0) }
                }
                LabeledContent("Calendar window", value: "\(Settings.windowBack) days back, \(Settings.windowAhead) ahead")
            } header: {
                Text("Settings")
            } footer: {
                Text("iOS decides when a background sync runs; the frequency is the soonest it may. Every sync sends yesterday and today; older days go out through the backfill or from Coverage.")
            }

            if let session = model.session {
                Section("Signed in") {
                    LabeledContent("Repository", value: session.repository)
                    LabeledContent("Server", value: session.server.absoluteString)
                    LabeledContent("Health account", value: model.state.healthAccount)
                    Button("Sign Out", role: .destructive) {
                        Task { signOutProblem = await model.signOut() }
                    }
                }
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
                BuildFooter()
            }
        }
        .navigationTitle("Substrate Sync")
        .refreshable { await model.syncNow() }
        .task { await model.loadCoverage() }
        .alert("Signed out", isPresented: Binding(get: { signOutProblem != nil }, set: { if !$0 { signOutProblem = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(signOutProblem ?? "")
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
                LabeledContent("Now") {
                    HStack { ProgressView().controlSize(.small); Text(now) }
                }
            }
            LabeledContent("Access") {
                HStack {
                    Text(model.access[source] ?? "…")
                    if model.access[source] == "Not asked yet" {
                        Button("Ask") { Task { await model.requestAccess(source) } }.buttonStyle(.borderless)
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
                VStack(alignment: .leading, spacing: 4) {
                    Text("Last error").font(.footnote).foregroundStyle(.secondary)
                    Text(error).font(.footnote).foregroundStyle(.red).textSelection(.enabled)
                }
            }
        }
        .onAppear { enabled = Settings.enabled(source) }
    }

    /// Synced, held on the device, and what they count.
    private var totals: (Int, Int, String)? {
        let state = model.state
        switch source {
        case .health:
            guard let available = model.healthAvailable else { return nil }
            return (available.intersection(state.healthDays).count, available.count, "days with data")
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

/// What the device holds against what was synced, week by week, month by
/// month or year by year, and the gaps one tap queues.
private struct CoverageSection: View {
    @EnvironmentObject private var model: AppModel
    @AppStorage("coverage.source") private var sourceRaw = Source.health.rawValue
    @AppStorage("coverage.granularity") private var granularityRaw = Granularity.month.rawValue

    private var source: Source { Source(rawValue: sourceRaw) == .calendars ? .calendars : .health }
    private var granularity: Granularity { Granularity(rawValue: granularityRaw) ?? .month }

    var body: some View {
        Section {
            Picker("Source", selection: $sourceRaw) {
                Text("Health").tag(Source.health.rawValue)
                Text("Calendars").tag(Source.calendars.rawValue)
            }
            .pickerStyle(.segmented)
            Picker("Group by", selection: $granularityRaw) {
                ForEach(Granularity.allCases) { Text($0.title).tag($0.rawValue) }
            }
            .pickerStyle(.segmented)

            let buckets = self.buckets
            if model.coverageLoading, buckets.isEmpty {
                HStack { ProgressView(); Text("Reading what the device holds…").foregroundStyle(.secondary) }
            } else if buckets.isEmpty {
                Text(emptyText).font(.callout).foregroundStyle(.secondary)
            } else {
                summary(buckets)
                chart(buckets)
                ForEach(buckets.reversed()) { bucket in
                    BucketRow(bucket: bucket, unit: unit, canSend: source == .health)
                }
            }
            if let error = model.coverageError {
                Text(error).font(.footnote).foregroundStyle(.red)
            }
        } header: {
            HStack {
                Text("Coverage")
                Spacer()
                if model.coverageLoading { ProgressView().controlSize(.mini) }
            }
        } footer: {
            Text(source == .health
                ? "A day is available when Health holds steps, heart rate or sleep for it, looking back \(AppModel.coverageYears) years; it is synced once both of its deliveries were accepted."
                : "Occurrences in the calendar window on this device, and how many of them were pushed.")
        }
    }

    private var unit: String { source == .health ? "days" : "events" }

    private var emptyText: String {
        switch source {
        case .health: model.healthAvailable == nil ? "Allow Health access to see what the device holds." : "Health holds no steps, heart rate or sleep here."
        case .calendars: model.eventsAvailable == nil ? "Allow Calendars access to see what the device holds." : "No events in the window."
        default: ""
        }
    }

    private var buckets: [CoverageBucket] {
        var calendar = Calendar.current
        calendar.timeZone = .current
        switch source {
        case .health:
            guard let available = model.healthAvailable else { return [] }
            return Coverage.days(available: available, synced: model.state.healthDays, by: granularity, calendar: calendar)
        default:
            guard let available = model.eventsAvailable else { return [] }
            return Coverage.events(
                available: available, synced: model.state.events.values.compactMap(\.start), by: granularity, calendar: calendar)
        }
    }

    @ViewBuilder
    private func summary(_ buckets: [CoverageBucket]) -> some View {
        let available = buckets.map(\.available).reduce(0, +)
        let synced = buckets.map(\.synced).reduce(0, +)
        let missing = buckets.flatMap(\.missing)
        VStack(alignment: .leading, spacing: 6) {
            LabeledContent("Synced", value: "\(synced.formatted()) of \(available.formatted()) \(unit)")
            ProgressView(value: Double(synced), total: Double(max(available, 1)))
            if source == .health, !missing.isEmpty {
                Button("Send all \(missing.count.formatted()) missing days") {
                    Task { await model.send(days: missing) }
                }
                .disabled(model.syncing)
            }
        }
    }

    private func chart(_ buckets: [CoverageBucket]) -> some View {
        // The newest stretches, enough to read on a phone.
        let shown = Array(buckets.suffix(granularity == .week ? 26 : granularity == .month ? 24 : 15))
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
        .frame(height: 180)
        .padding(.vertical, 4)
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
                .buttonStyle(.borderless)
                .disabled(model.syncing)
            }
        }
    }
}

/// The recent runs, newest first.
private struct HistorySection: View {
    @EnvironmentObject private var model: AppModel
    @State private var all = false

    var body: some View {
        let runs = model.state.runs
        Section("History") {
            if runs.isEmpty {
                Text("No sync has run yet.").foregroundStyle(.secondary)
            }
            ForEach(all ? runs : Array(runs.prefix(8))) { run in
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
            if runs.count > 8 {
                Button(all ? "Show fewer" : "Show all \(runs.count)") { all.toggle() }
            }
        }
    }
}
