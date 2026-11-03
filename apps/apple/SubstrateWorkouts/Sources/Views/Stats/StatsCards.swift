import Charts
import WorkoutsCore
import SwiftUI

/// Progress over a period, for one routine or every session (the routine
/// page's Progress and the Stats tab show this one view, so one number never
/// reads two ways): the period picker, the totals as tiles, how regularly
/// the weeks were trained, volume over time, the last session against the
/// one before, each exercise's best and its trend, personal records, the
/// muscles worked and their balance, and the sessions. Cards on the grouped
/// background, never glass (§8); the caller scrolls. Exercises, records and
/// sessions are value links: the stack registers `statsDestinations()`.
///
/// The presentation is worked out off the main actor and kept against the
/// scope, the period, the day, the settings and `AppModel.dataRevision`:
/// it reads every session, and a render must not.
struct StatsProgressView: View {
    let scope: StatsScope
    /// Sessions listed before "Show all".
    var sessionLimit = 3

    @Environment(AppModel.self) private var app
    @AppStorage("stats.period") private var period: StatsPeriod = .threeMonths
    @State private var shown: StatsMemo.Entry?

    var body: some View {
        let key = StatsMemo.Key(scope: scope, period: period, revision: app.dataRevision, settings: app.presenterSettings,
                                day: app.presenterSettings.calendar.startOfDay(for: .now))
        let stats = StatsMemo.shared.presentation(for: key) ?? shown?.stats
        VStack(alignment: .leading, spacing: 12) {
            Picker("Period", selection: $period) {
                ForEach(StatsPeriod.allCases) { period in
                    Text(period.rawValue)
                        .accessibilityLabel(period.spoken)
                        .tag(period)
                }
            }
            .pickerStyle(.segmented)
            if let stats {
                StatsCards(stats: stats, sessionLimit: sessionLimit)
                    // A stale presentation while the next is worked out.
                    .opacity(stats.scope == scope && stats.period == period ? 1 : 0.5)
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, minHeight: 200)
            }
        }
        .task(id: key) {
            if let cached = StatsMemo.shared.presentation(for: key) {
                shown = StatsMemo.Entry(key: key, stats: cached)
                return
            }
            let (history, catalog, scope, period, settings) = (app.history, app.catalog ?? Catalog(), key.scope, key.period, key.settings)
            let stats = await Task.detached(priority: .userInitiated) {
                StatsPresenter.present(history, catalog: catalog, scope: scope, period: period, now: .now, settings: settings)
            }.value
            guard !Task.isCancelled else { return }
            StatsMemo.shared.store(stats, for: key)
            shown = StatsMemo.Entry(key: key, stats: stats)
        }
    }
}

/// The last few stats presentations, so a page shown again (a tab, the
/// routine page's Progress) does not work them out again.
@MainActor
final class StatsMemo {
    static let shared = StatsMemo()

    struct Key: Equatable {
        var scope: StatsScope
        var period: StatsPeriod
        var revision: Int
        var settings: PresenterSettings
        /// The period ends today.
        var day: Date
    }

    struct Entry: Equatable {
        var key: Key
        var stats: StatsPresentation
    }

    private var entries: [Entry] = []
    private let capacity = 8

    func presentation(for key: Key) -> StatsPresentation? {
        entries.first { $0.key == key }?.stats
    }

    func store(_ stats: StatsPresentation, for key: Key) {
        entries.removeAll { $0.key == key || $0.key.revision != key.revision }
        entries.insert(Entry(key: key, stats: stats), at: 0)
        if entries.count > capacity { entries.removeLast(entries.count - capacity) }
    }
}

/// The cards of one presentation.
struct StatsCards: View {
    let stats: StatsPresentation
    var sessionLimit = 3

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(stats.range)
                    .font(.subheadline.weight(.semibold))
                Text(stats.comparison)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .padding(.horizontal, 4)
            .accessibilityElement(children: .combine)
            if !stats.hasSessions {
                StatsCard {
                    ContentUnavailableView {
                        Label(StatsPresentation.noSessionsTitle, systemImage: "chart.bar.xaxis")
                    } description: {
                        Text(stats.scope == .all ? StatsPresentation.noSessions : StatsPresentation.noRoutineSessions)
                    }
                }
            } else {
                StatsTilesGrid(totals: stats.totals)
                if let consistency = stats.consistency {
                    ConsistencyCard(consistency: consistency, weeks: stats.weeks)
                }
                StatsCard(title: volumeTitle) {
                    if stats.isEmpty {
                        Text(StatsPresentation.emptyPeriod)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    } else {
                        StatsVolumeChart(bars: stats.volume, unit: stats.volumeUnit, period: stats.period)
                    }
                }
                if let last = stats.lastSession {
                    LastSessionCard(comparison: last)
                }
                if !stats.exercises.isEmpty {
                    ExercisesCard(groups: stats.exercises)
                }
                if !stats.records.isEmpty {
                    RecordsCard(records: stats.records)
                }
                if !stats.muscles.isEmpty {
                    MusclesCard(stats: stats)
                }
                SessionsCard(stats: stats, limit: sessionLimit)
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

// MARK: - Cards

/// A card: an optional title (a header to VoiceOver) and its content.
struct StatsCard<Content: View>: View {
    var title: String?
    var detail: String?
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if let title {
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.headline)
                        .accessibilityAddTraits(.isHeader)
                    if let detail {
                        Text(detail)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            content
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RowsStyle.card, in: .rect(cornerRadius: 20))
    }
}

/// The period's totals as tiles, two to a row; one to a row at the
/// accessibility text sizes. Each tile's row keeps its neighbour's height.
private struct StatsTilesGrid: View {
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

/// A total: its label, its value large, the change under it, and what it comes to.
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
            HStack(spacing: 6) {
                if let change = total.change {
                    StatsChangeChip(change: change)
                }
                if let detail = total.detail {
                    Text(detail)
                        .font(.caption)
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .padding(14)
        .background(RowsStyle.card, in: .rect(cornerRadius: 20))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(total.label)
        .accessibilityValue([total.value, total.detail, total.change?.spoken].compactMap(\.self).joined(separator: ", "))
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
        .fixedSize()
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

/// Weeks trained, sessions a week and the run of weeks, over a bar per
/// week of its sessions; a week with none keeps its place as a grey stub.
private struct ConsistencyCard: View {
    let consistency: StatsConsistency
    let weeks: [StatsWeek]
    @ScaledMetric(relativeTo: .body) private var height = 96
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        StatsCard(title: "Consistency") {
            let layout = typeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
                : AnyLayout(HStackLayout(alignment: .top, spacing: 12))
            layout {
                fact("Weeks trained", consistency.value)
                fact("Per week", consistency.perWeek)
                if let streak = consistency.streak {
                    fact("In a row", streak)
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Consistency")
            .accessibilityValue(consistency.sentence)
            let most = max(1, weeks.map(\.sessions).max() ?? 1)
            Chart(weeks) { week in
                BarMark(x: .value("Week", week.start, unit: .weekOfYear),
                        y: .value("Sessions", week.sessions == 0 ? Double(most) * 0.04 : Double(week.sessions)))
                    .foregroundStyle(week.sessions == 0 ? AnyShapeStyle(Color(.tertiarySystemFill)) : AnyShapeStyle(Color.accentColor.gradient))
                    .cornerRadius(2)
                    .accessibilityLabel(week.label)
                    .accessibilityValue(week.text)
            }
            .chartYScale(domain: 0 ... Double(most))
            .chartYAxis {
                AxisMarks(values: .automatic(desiredCount: min(most, 3))) { value in
                    AxisGridLine()
                    AxisValueLabel {
                        if let number = value.as(Double.self) { Text(Format.number(number, decimals: 0)) }
                    }
                }
            }
            .chartXAxis {
                AxisMarks(values: .automatic(desiredCount: 4)) { _ in
                    AxisValueLabel(format: .dateTime.day().month(.abbreviated))
                }
            }
            .frame(height: height)
        }
    }

    private func fact(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(value)
                .font(.title3.weight(.semibold))
                .monospacedDigit()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// Volume per bar of the period. Each bar says its dates and its volume to
/// VoiceOver.
private struct StatsVolumeChart: View {
    let bars: [StatsVolumeBar]
    let unit: MassUnit
    let period: StatsPeriod
    @ScaledMetric(relativeTo: .body) private var height = 160

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
            AxisMarks(values: .automatic(desiredCount: 3)) { value in
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

/// The newest session against the one before it: its time, volume and
/// working sets, each with its change, and how many exercises went up. A
/// tap opens the session.
private struct LastSessionCard: View {
    let comparison: StatsSessionComparison
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        NavigationLink(value: comparison.session) {
            StatsCard(title: "Last session", detail: "\(comparison.title). \(comparison.against)") {
                let layout = typeSize.isAccessibilitySize
                    ? AnyLayout(VStackLayout(alignment: .leading, spacing: 10))
                    : AnyLayout(HStackLayout(alignment: .top, spacing: 12))
                layout {
                    ForEach(comparison.totals) { total in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(total.label)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            Text(total.value)
                                .font(.headline)
                                .monospacedDigit()
                                .lineLimit(1)
                                .minimumScaleFactor(0.8)
                            if let change = total.change {
                                StatsChangeChip(change: change)
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
                if let exercises = comparison.exercises {
                    Label(exercises, systemImage: "chart.line.uptrend.xyaxis")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            .overlay(alignment: .topTrailing) {
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .padding(18)
                    .accessibilityHidden(true)
            }
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Last session, \(comparison.title)")
        .accessibilityValue(spoken)
        .accessibilityHint("Shows the session")
        .accessibilityAddTraits(.isButton)
    }

    private var spoken: String {
        var parts = [comparison.against]
        parts += comparison.totals.map { total in
            [total.label, total.value, total.change?.spoken].compactMap(\.self).joined(separator: " ")
        }
        if let exercises = comparison.exercises { parts.append(exercises) }
        return parts.joined(separator: ". ")
    }
}

/// Each exercise's best over the period, its change and its trend; the
/// equipment versions of one movement under the movement's name.
private struct ExercisesCard: View {
    let groups: [StatsExerciseGroup]

    var body: some View {
        StatsCard(title: "Exercises") {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(groups.enumerated()), id: \.element.id) { index, group in
                    if index > 0 { Divider() }
                    if let title = group.title {
                        VStack(alignment: .leading, spacing: 1) {
                            Text(title)
                                .font(.subheadline.weight(.semibold))
                            if let detail = group.detail {
                                Text(detail)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                        .padding(.top, 10)
                        .accessibilityElement(children: .combine)
                        .accessibilityAddTraits(.isHeader)
                    }
                    ForEach(group.exercises) { exercise in
                        NavigationLink(value: exercise.id) {
                            ExerciseProgressRow(exercise: exercise, grouped: group.title != nil)
                        }
                        .buttonStyle(.plain)
                        .accessibilityHint("Shows the exercise's history")
                    }
                }
            }
        }
    }
}

/// One exercise: its name, the metric's best large with its change, the
/// sparkline beside it, and the best set and sets under it.
private struct ExerciseProgressRow: View {
    let exercise: StatsExercise
    /// An equipment version under its movement: indented under its name.
    let grouped: Bool
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        let layout = typeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
            : AnyLayout(HStackLayout(alignment: .center, spacing: 12))
        layout {
            VStack(alignment: .leading, spacing: 3) {
                Text(exercise.name)
                    .font(grouped ? .subheadline : .body.weight(.semibold))
                    .foregroundStyle(.primary)
                if let metric = exercise.metric, let best = exercise.best {
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(best)
                            .font(.title3.weight(.semibold))
                            .monospacedDigit()
                        Text(metric)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        if let change = exercise.change {
                            StatsChangeChip(change: change)
                        }
                    }
                }
                HStack(spacing: 12) {
                    ForEach(exercise.facts) { fact in
                        HStack(alignment: .firstTextBaseline, spacing: 4) {
                            Text(fact.label)
                                .foregroundStyle(.secondary)
                            Text(fact.value)
                                .monospacedDigit()
                        }
                        .font(.caption)
                    }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if exercise.points.count > 1 {
                StatsSparkline(points: exercise.points)
                    .frame(width: typeSize.isAccessibilitySize ? nil : 84, height: 36)
                    .frame(maxWidth: typeSize.isAccessibilitySize ? .infinity : nil)
            }
            Image(systemName: "chevron.right")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.tertiary)
                .accessibilityHidden(true)
        }
        .padding(.vertical, 10)
        .padding(.leading, grouped ? 12 : 0)
        .contentShape(.rect)
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

/// Bests set in the period above every earlier session's, the newest
/// first and the rest behind "Show all".
private struct RecordsCard: View {
    let records: [StatsRecord]
    @State private var showsAll = false
    private let limit = 5

    var body: some View {
        StatsCard(title: "Personal records", detail: Format.count(records.count, "record") + " in this period") {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array((showsAll ? records : Array(records.prefix(limit))).enumerated()), id: \.element.id) { index, record in
                    if index > 0 { Divider().padding(.leading, 36) }
                    NavigationLink(value: record.session) {
                        RecordRow(record: record)
                    }
                    .buttonStyle(.plain)
                    .accessibilityHint("Shows the session")
                }
                if records.count > limit {
                    Divider()
                    Button(showsAll ? "Show fewer" : "Show all \(records.count) records") {
                        withAnimation(.smooth(duration: 0.25)) { showsAll.toggle() }
                    }
                    .font(.subheadline.weight(.medium))
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                }
            }
        }
    }
}

/// A personal record: the exercise, what it was and when, the value and
/// the best it beat.
private struct RecordRow: View {
    let record: StatsRecord
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            Image(systemName: "trophy.fill")
                .font(.body)
                .foregroundStyle(.tint)
                .frame(width: 24)
                .accessibilityHidden(true)
            let layout = typeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: 4))
                : AnyLayout(HStackLayout(alignment: .firstTextBaseline, spacing: 12))
            layout {
                VStack(alignment: .leading, spacing: 2) {
                    Text(record.name)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.primary)
                    Text("\(record.kind), \(record.date)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                VStack(alignment: typeSize.isAccessibilitySize ? .leading : .trailing, spacing: 2) {
                    Text(record.value)
                        .font(.subheadline.weight(.semibold))
                        .monospacedDigit()
                        .foregroundStyle(.primary)
                    Text("was \(record.previous)")
                        .font(.caption)
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                }
            }
        }
        .padding(.vertical, 8)
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(record.name), \(record.kind)")
        .accessibilityValue("\(record.value) on \(record.date), previous best \(record.previous)")
    }
}

/// The body map, the balances, and the working sets per muscle group.
private struct MusclesCard: View {
    let stats: StatsPresentation
    @State private var showsAll = false
    /// Groups the chart shows before "Show all".
    private let limit = 6

    var body: some View {
        StatsCard(title: "Muscles worked", detail: "Working sets in this period") {
            BodyMapView(regions: stats.regions)
                .frame(maxHeight: 260)
            ForEach(stats.balance) { balance in
                BalanceBar(balance: balance)
            }
            VStack(alignment: .leading, spacing: 6) {
                Text("Sets per muscle group")
                    .font(.subheadline.weight(.semibold))
                    .accessibilityAddTraits(.isHeader)
                MuscleChart(groups: showsAll ? stats.muscles : Array(stats.muscles.prefix(limit)))
                if stats.muscles.count > limit {
                    Button(showsAll ? "Show fewer" : "Show all \(stats.muscles.count) groups") {
                        withAnimation(.smooth(duration: 0.25)) { showsAll.toggle() }
                    }
                    .font(.subheadline.weight(.medium))
                    .frame(minHeight: 44)
                }
                Text(StatsPresentation.musclesFooter)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }
}

/// Two or three sides of the period's sets as one split bar, each part
/// named under it with its share and sets. The parts are told apart by
/// their order and their words; the shades only echo them.
private struct BalanceBar: View {
    let balance: StatsBalance
    @ScaledMetric(relativeTo: .body) private var height: CGFloat = 12

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(balance.title)
                    .font(.subheadline.weight(.semibold))
                Spacer(minLength: 8)
                Text(balance.note)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.trailing)
            }
            GeometryReader { proxy in
                let gap: CGFloat = 2
                let width = max(0, proxy.size.width - gap * CGFloat(max(0, balance.parts.count - 1)))
                HStack(spacing: gap) {
                    ForEach(Array(balance.parts.enumerated()), id: \.element.id) { index, part in
                        Rectangle()
                            .fill(shade(index))
                            .frame(width: width * part.share)
                    }
                }
                .clipShape(.capsule)
            }
            .frame(height: height)
            HStack(alignment: .top, spacing: 12) {
                ForEach(Array(balance.parts.enumerated()), id: \.element.id) { index, part in
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Circle()
                            .fill(shade(index))
                            .frame(width: 8, height: 8)
                        VStack(alignment: .leading, spacing: 1) {
                            Text(part.label)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            Text(part.percent)
                                .font(.subheadline.weight(.semibold))
                                .monospacedDigit()
                            Text(part.text)
                                .font(.caption2)
                                .monospacedDigit()
                                .foregroundStyle(.secondary)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(balance.title)
        .accessibilityValue(balance.sentence)
    }

    private func shade(_ index: Int) -> Color {
        [Color.accentColor, Color.accentColor.opacity(0.55), Color.accentColor.opacity(0.25)][min(index, 2)]
    }
}

/// Working sets per muscle group, largest first, each bar labelled with its count.
private struct MuscleChart: View {
    let groups: [StatsMuscleGroup]
    @ScaledMetric(relativeTo: .caption) private var rowHeight = 30

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

/// The scope's newest sessions and "Show all".
private struct SessionsCard: View {
    let stats: StatsPresentation
    let limit: Int

    var body: some View {
        StatsCard(title: "Sessions", detail: Format.count(stats.sessions.count, "session") + " in all") {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(stats.sessions.prefix(limit).enumerated()), id: \.element.id) { index, row in
                    if index > 0 { Divider() }
                    NavigationLink(value: row.id) {
                        HStack {
                            if stats.scope == .all {
                                HistoryRowView(row: row)
                            } else {
                                RoutineSessionRow(row: row)
                            }
                            Image(systemName: "chevron.right")
                                .font(.footnote.weight(.semibold))
                                .foregroundStyle(.tertiary)
                                .accessibilityHidden(true)
                        }
                        .padding(.vertical, 8)
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                }
                if stats.sessions.count > limit {
                    Divider()
                    NavigationLink(value: StatsSessionsRoute(scope: stats.scope)) {
                        Text("Show all \(stats.sessions.count) sessions")
                            .font(.subheadline.weight(.medium))
                            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                            .contentShape(.rect)
                    }
                }
            }
        }
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
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.primary)
                Text(row.summary)
                    .font(.footnote.monospacedDigit())
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
    /// What the stats link to: an exercise, a session, and the whole list
    /// of the page's sessions.
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

#if DEBUG
#Preview("With history") {
    let app = PreviewData.app()
    NavigationStack {
        ScrollView {
            StatsProgressView(scope: .all)
                .padding(16)
        }
        .background(Color(.systemGroupedBackground))
        .statsDestinations()
    }
    .environment(app)
}

#Preview("No history") {
    let app = AppModel(catalog: PreviewData.catalog, history: [])
    NavigationStack {
        ScrollView {
            StatsProgressView(scope: .routine(SampleRoutine.id))
                .padding(16)
        }
        .background(Color(.systemGroupedBackground))
    }
    .environment(app)
}
#endif
