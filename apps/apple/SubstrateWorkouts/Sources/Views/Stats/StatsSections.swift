import Charts
import WorkoutsCore
import SwiftUI

/// The stats of a routine or of every session, as sections of a `List`:
/// the period picker, the totals against the period before, volume over
/// time, each exercise's progress, personal records, working sets per
/// muscle, and the sessions. The routine page and the Stats tab show the
/// same sections, so one number never reads two ways. Content, never glass
/// (§8). Exercises, records and sessions are value links: the stack that
/// shows them registers `statsDestinations()`.
struct StatsSections: View {
    let stats: StatsPresentation
    @Binding var period: StatsPeriod
    /// Sessions listed before "Show all".
    var sessionLimit = 5

    var body: some View {
        Section {
            Picker("Period", selection: $period) {
                ForEach(StatsPeriod.allCases) { period in
                    Text(period.rawValue)
                        .accessibilityLabel(period.spoken)
                        .tag(period)
                }
            }
            .pickerStyle(.segmented)
            .listRowSeparator(.hidden)
            if stats.hasSessions {
                VStack(alignment: .leading, spacing: 2) {
                    Text(stats.range)
                        .font(.subheadline.weight(.semibold))
                    Text(stats.comparison)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
            } else {
                ContentUnavailableView {
                    Label(StatsPresentation.noSessionsTitle, systemImage: "chart.bar.xaxis")
                } description: {
                    Text(stats.scope == .all ? StatsPresentation.noSessions : StatsPresentation.noRoutineSessions)
                }
            }
        } header: {
            Text("Progress")
        }
        if stats.hasSessions {
            Section {
                StatsTotalsGrid(totals: stats.totals)
                    .listRowInsets(EdgeInsets())
                    .listRowBackground(Color.clear)
            }
            Section {
                if stats.isEmpty {
                    Text(StatsPresentation.emptyPeriod)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                } else {
                    StatsVolumeChart(bars: stats.volume, unit: stats.volumeUnit, period: stats.period)
                        .padding(.vertical, 8)
                }
            } header: {
                Text(volumeTitle)
            }
            if !stats.exercises.isEmpty {
                Section("Exercises") {
                    ForEach(stats.exercises) { group in
                        if let title = group.title {
                            MovementHeader(title: title, detail: group.detail)
                        }
                        ForEach(group.exercises) { exercise in
                            NavigationLink(value: exercise.id) {
                                ExerciseProgressRow(exercise: exercise, grouped: group.title != nil)
                            }
                            .accessibilityHint("Shows the exercise's history")
                        }
                    }
                }
            }
            if !stats.records.isEmpty {
                Section("Personal records") {
                    ForEach(stats.records) { record in
                        NavigationLink(value: record.session) {
                            RecordRow(record: record)
                        }
                        .accessibilityHint("Shows the session")
                    }
                }
            }
            if !stats.muscles.isEmpty {
                Section {
                    BodyMapView(regions: stats.regions)
                        .padding(.vertical, 8)
                    MuscleChart(groups: stats.muscles)
                        .padding(.vertical, 8)
                    ForEach(stats.muscles.filter { !$0.muscles.isEmpty }) { group in
                        DisclosureGroup {
                            ForEach(group.muscles) { muscle in
                                LabeledContent(muscle.label, value: muscle.text)
                                    .monospacedDigit()
                            }
                        } label: {
                            LabeledContent(group.label, value: group.text)
                                .monospacedDigit()
                        }
                    }
                } header: {
                    Text("Working sets per muscle")
                } footer: {
                    Text(StatsPresentation.musclesFooter)
                }
            }
            Section {
                ForEach(stats.sessions.prefix(sessionLimit)) { row in
                    NavigationLink(value: row.id) {
                        if stats.scope == .all {
                            HistoryRowView(row: row)
                        } else {
                            RoutineSessionRow(row: row)
                        }
                    }
                }
                if stats.sessions.count > sessionLimit {
                    NavigationLink(value: StatsSessionsRoute(scope: stats.scope)) {
                        Text("Show all \(stats.sessions.count) sessions")
                    }
                }
            } header: {
                Text("History")
            }
        }
    }

    private var volumeTitle: String {
        let per = switch stats.period.bar {
        case .day: "day"
        case .week: "week"
        case .month: "month"
        }
        return "Volume per \(per), \(stats.volumeUnit.rawValue)"
    }
}

/// The period's totals as tiles, two to a row; one to a row at the
/// accessibility text sizes. Each tile's row keeps its neighbour's height.
private struct StatsTotalsGrid: View {
    let totals: [StatsTotal]
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        if typeSize.isAccessibilitySize {
            VStack(spacing: 12) {
                ForEach(totals) { StatsTile(total: $0) }
            }
        } else {
            Grid(horizontalSpacing: 12, verticalSpacing: 12) {
                ForEach(Array(stride(from: 0, to: totals.count, by: 2)), id: \.self) { index in
                    GridRow {
                        StatsTile(total: totals[index])
                        if index + 1 < totals.count {
                            StatsTile(total: totals[index + 1])
                        } else {
                            Color.clear
                                .accessibilityHidden(true)
                        }
                    }
                }
            }
        }
    }
}

/// A total: its label, its value large, and the change under it.
private struct StatsTile: View {
    let total: StatsTotal

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(total.label)
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Text(total.value)
                .font(.title2.weight(.semibold))
                .monospacedDigit()
                .lineLimit(2)
                .minimumScaleFactor(0.8)
            if let change = total.change {
                StatsChangeChip(change: change)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .padding(14)
        .cardBackground(cornerRadius: 16)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(total.label)
        .accessibilityValue([total.value, total.change?.spoken].compactMap(\.self).joined(separator: ", "))
    }
}

/// A change against the period before: an arrow and the signed amount, in
/// grey. Up is not better and down not worse (a deload week is meant to be
/// lighter), so no colour says so.
struct StatsChangeChip: View {
    let change: StatsChange

    var body: some View {
        HStack(spacing: 3) {
            Image(systemName: symbol)
                .imageScale(.small)
            Text(change.text)
                .monospacedDigit()
        }
        .font(.caption.weight(.semibold))
        .foregroundStyle(.secondary)
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
        .background(.fill.tertiary, in: .capsule)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(change.spoken)
    }

    private var symbol: String {
        switch change.direction {
        case .up: "arrow.up.right"
        case .down: "arrow.down.right"
        case .same: "equal"
        }
    }
}

/// Volume per bar of the period. Each bar says its dates and its volume to
/// VoiceOver.
private struct StatsVolumeChart: View {
    let bars: [StatsVolumeBar]
    let unit: MassUnit
    let period: StatsPeriod
    @ScaledMetric(relativeTo: .body) private var height = 180

    var body: some View {
        Chart(bars) { bar in
            BarMark(x: .value("Date", bar.start, unit: component), y: .value("Volume", bar.value))
                .foregroundStyle(Color.accentColor.gradient)
                .cornerRadius(3)
                .accessibilityLabel(bar.label)
                .accessibilityValue(bar.text)
        }
        .chartXAxis {
            AxisMarks(values: .stride(by: stride.component, count: stride.count)) { _ in
                AxisGridLine()
                AxisValueLabel(format: labelFormat, centered: period == .week || period == .year)
            }
        }
        .chartYAxis {
            AxisMarks { value in
                AxisGridLine()
                AxisValueLabel {
                    if let number = value.as(Double.self) { Text(Self.compact(number)) }
                }
            }
        }
        .frame(height: height)
    }

    private var component: Calendar.Component {
        switch period.bar {
        case .day: .day
        case .week: .weekOfYear
        case .month: .month
        }
    }

    private var stride: (component: Calendar.Component, count: Int) {
        switch period {
        case .week: (.day, 1)
        case .month: (.day, 7)
        case .threeMonths: (.weekOfYear, 3)
        case .sixMonths: (.month, 1)
        case .year: (.month, 2)
        }
    }

    private var labelFormat: Date.FormatStyle {
        switch period {
        case .week: .dateTime.weekday(.abbreviated)
        case .month, .threeMonths: .dateTime.day().month(.abbreviated)
        case .sixMonths, .year: .dateTime.month(.abbreviated)
        }
    }

    /// `12k` for 12,000: an axis has no room for the grouped form.
    static func compact(_ value: Double) -> String {
        value >= 1000 ? "\(Format.number(value / 1000, decimals: 1))k" : Format.number(value, decimals: 0)
    }
}

/// The name of a movement done in several equipment versions, above them.
private struct MovementHeader: View {
    let title: String
    let detail: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title)
                .font(.subheadline.weight(.semibold))
            if let detail {
                Text(detail)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }
}

/// One exercise's progress: its name, the metric's best and its change,
/// the sparkline, then the best set, volume and working sets.
private struct ExerciseProgressRow: View {
    let exercise: StatsExercise
    /// An equipment version under its movement: drawn with the group's bar.
    let grouped: Bool
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            if grouped {
                RoundedRectangle(cornerRadius: 2)
                    .fill(GroupPalette.color(nil))
                    .frame(width: 4)
                    .accessibilityHidden(true)
            }
            VStack(alignment: .leading, spacing: 10) {
                let layout = typeSize.isAccessibilitySize
                    ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
                    : AnyLayout(HStackLayout(alignment: .top, spacing: 12))
                layout {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(exercise.name)
                            .font(.headline)
                        if let metric = exercise.metric, let best = exercise.best {
                            Text(metric)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                Text(best)
                                    .font(.title3.weight(.semibold))
                                    .monospacedDigit()
                                if let change = exercise.change {
                                    StatsChangeChip(change: change)
                                }
                            }
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    if exercise.points.count > 1 {
                        StatsSparkline(points: exercise.points)
                            .frame(width: typeSize.isAccessibilitySize ? nil : 88, height: 40)
                            .frame(maxWidth: typeSize.isAccessibilitySize ? .infinity : nil)
                    }
                }
                let facts = typeSize.isAccessibilitySize
                    ? AnyLayout(VStackLayout(alignment: .leading, spacing: 6))
                    : AnyLayout(HStackLayout(alignment: .top, spacing: 16))
                facts {
                    ForEach(exercise.facts) { fact in
                        VStack(alignment: .leading, spacing: 1) {
                            Text(fact.label)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            Text(fact.value)
                                .font(.subheadline)
                                .monospacedDigit()
                        }
                    }
                }
            }
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(exercise.name)
        .accessibilityValue(spoken)
    }

    private var spoken: String {
        var parts: [String] = []
        if let metric = exercise.metric, let best = exercise.best {
            parts.append("\(metric) \(best)")
            if let change = exercise.change { parts.append(change.spoken) }
        }
        if exercise.points.count > 1, let summary = exercise.trendSummary { parts.append(summary) }
        parts += exercise.facts.map { "\($0.label) \($0.value)" }
        return parts.joined(separator: ". ")
    }
}

/// A trend at a glance: one point per session, straight segments (as the
/// exercise's chart draws them), the latest marked. Hidden from VoiceOver:
/// its row says what it shows.
struct StatsSparkline: View {
    let points: [TrendPoint]

    var body: some View {
        let values = points.map(\.value)
        let low = values.min() ?? 0
        let high = values.max() ?? 1
        let pad = max((high - low) * 0.15, 0.5)
        Chart(points) { point in
            AreaMark(x: .value("Date", point.date), yStart: .value("Low", low - pad), yEnd: .value("Value", point.value))
                .foregroundStyle(LinearGradient(colors: [Color.accentColor.opacity(0.25), Color.accentColor.opacity(0)],
                                                startPoint: .top, endPoint: .bottom))
                .interpolationMethod(.linear)
            LineMark(x: .value("Date", point.date), y: .value("Value", point.value))
                .foregroundStyle(Color.accentColor)
                .lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
                .interpolationMethod(.linear)
            if point.id == points.last?.id {
                PointMark(x: .value("Date", point.date), y: .value("Value", point.value))
                    .foregroundStyle(Color.accentColor)
                    .symbolSize(30)
            }
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .chartLegend(.hidden)
        .chartYScale(domain: (low - pad) ... (high + pad))
        .accessibilityHidden(true)
    }
}

/// A personal record: the exercise, what it was, when, and the best it beat.
private struct RecordRow: View {
    let record: StatsRecord
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: "trophy.fill")
                .font(.title3)
                .foregroundStyle(.tint)
                .accessibilityHidden(true)
            let layout = typeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: 4))
                : AnyLayout(HStackLayout(alignment: .top, spacing: 12))
            layout {
                VStack(alignment: .leading, spacing: 2) {
                    Text(record.name)
                        .font(.headline)
                    Text(record.kind)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Text(record.date)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                VStack(alignment: typeSize.isAccessibilitySize ? .leading : .trailing, spacing: 2) {
                    Text(record.value)
                        .font(.headline)
                        .monospacedDigit()
                    Text("Previous best \(record.previous)")
                        .font(.footnote)
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(typeSize.isAccessibilitySize ? .leading : .trailing)
                }
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(record.name), \(record.kind)")
        .accessibilityValue("\(record.value) on \(record.date), previous best \(record.previous)")
    }
}

/// Working sets per muscle group, largest first, each bar labelled with its
/// count.
private struct MuscleChart: View {
    let groups: [StatsMuscleGroup]
    /// A band holds its label above its bar.
    @ScaledMetric(relativeTo: .caption) private var rowHeight = 36

    var body: some View {
        let most = groups.map(\.sets).max() ?? 1
        Chart(groups) { group in
            BarMark(x: .value("Working sets", group.sets), y: .value("Muscle group", group.label))
                .foregroundStyle(Color.accentColor.gradient)
                .cornerRadius(3)
                .annotation(position: .trailing, alignment: .leading, spacing: 4) {
                    Text(group.text)
                        .font(.caption)
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                }
                .accessibilityLabel(group.label)
                .accessibilityValue(group.text)
        }
        .chartXScale(domain: 0 ... most * 1.45)
        .chartXAxis(.hidden)
        .chartYAxis {
            AxisMarks(position: .leading) { _ in
                AxisValueLabel()
                    .font(.caption)
            }
        }
        .frame(height: CGFloat(groups.count) * rowHeight)
    }
}

/// A session on its routine's page, where the routine's name would repeat
/// on every row: when, then the summary, the Health glyph trailing.
private struct RoutineSessionRow: View {
    let row: HistoryRow

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(row.date)
                    .font(.headline)
                Text(row.summary)
                    .font(.subheadline.monospacedDigit())
                    .foregroundStyle(.secondary)
                if row.isDeload {
                    Text("Deload")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if let symbol = row.health.symbol {
                Image(systemName: symbol)
                    .foregroundStyle(row.health.tint)
                    .accessibilityLabel(row.health.accessibilityText)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// "Show all" of a stats page's sessions.
struct StatsSessionsRoute: Hashable {
    let scope: StatsScope
}

extension View {
    /// What the stats sections link to: an exercise, a session, and the
    /// whole list of the page's sessions.
    func statsDestinations() -> some View {
        navigationDestination(for: ExerciseID.self) { id in
            ExerciseDetailView(exerciseID: id)
        }
        .navigationDestination(for: SessionID.self) { id in
            SessionDetailView(sessionID: id)
        }
        .navigationDestination(for: StatsSessionsRoute.self) { route in
            StatsSessionsView(scope: route.scope)
        }
    }
}

/// Every session of a stats page, newest first, by month as History lists them.
struct StatsSessionsView: View {
    let scope: StatsScope
    @Environment(AppModel.self) private var app

    var body: some View {
        let entries = StatsPresenter.entries(app.history, scope: scope)
        let sections = HistoryPresenter.sections(entries, exercises: app.exercises, settings: app.presenterSettings)
        List {
            ForEach(sections) { section in
                Section(section.title) {
                    ForEach(section.rows) { row in
                        NavigationLink(value: row.id) {
                            HistoryRowView(row: row)
                        }
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
        .navigationTitle("Sessions")
        .navigationSubtitle(subtitle)
        .navigationBarTitleDisplayMode(.inline)
    }

    private var subtitle: String {
        switch scope {
        case .all: StatsPresentation.allRoutines
        case let .routine(id): app.catalog?.routines[id]?.name
            ?? StatsPresenter.entries(app.history, scope: scope).first.map { HistoryPresenter.routineTitle(of: $0.records.session) } ?? ""
        }
    }
}
