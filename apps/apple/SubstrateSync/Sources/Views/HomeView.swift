import SwiftUI

/// The overview: what is happening now, one row per source with its state,
/// and the way to the activity. Each source's details and settings are on its
/// own page, and the account and the app's settings behind the gear.
struct HomeView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        List {
            Section {
                StatusCard()
            }

            Section("Sources") {
                ForEach(Source.allCases) { source in
                    NavigationLink {
                        SourceView(source: source)
                    } label: {
                        SourceRow(source: source)
                    }
                    .accessibilityIdentifier("source.\(source.rawValue)")
                }
            }

            Section {
                NavigationLink {
                    ActivityView()
                } label: {
                    VStack(alignment: .leading, spacing: 2) {
                        Label("Activity", systemImage: "list.bullet.rectangle")
                        if let run = model.state.runs.first {
                            Text("Last: \(run.source.title), \(run.finished.formatted(.relative(presentation: .named)))")
                                .font(.footnote).foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
        .navigationTitle("Substrate Sync")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                NavigationLink {
                    SettingsView()
                } label: {
                    Label("Settings", systemImage: "gearshape")
                }
            }
        }
        .refreshable { await model.syncNow() }
        .task { await model.loadCoverage() }
    }
}

/// What the app is doing now, and the one button: while a sync runs, the
/// source and step it is on and a way to stop it; when idle, Sync Now and how
/// the last sync ended.
private struct StatusCard: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if model.syncing {
                let running = Source.allCases.filter { model.progress[$0] != nil }
                if running.isEmpty {
                    HStack(spacing: 10) { ProgressView(); Text("Starting…").foregroundStyle(.secondary) }
                }
                ForEach(running) { source in
                    VStack(alignment: .leading, spacing: 6) {
                        Label("Syncing \(source.title)", systemImage: source.symbol).font(.headline)
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
                if let summary {
                    if summary.failed {
                        ErrorBox(title: "The last sync did not finish", message: summary.text)
                    } else {
                        Label(summary.text, systemImage: "checkmark.circle.fill")
                            .font(.callout)
                            .foregroundStyle(.green)
                    }
                }
                Button {
                    Task { await model.syncNow() }
                } label: {
                    WideLabel(title: "Sync Now", systemImage: "arrow.triangle.2.circlepath")
                }
                .primaryAction()
            }
        }
        .padding(.vertical, 4)
    }

    /// How the last run of each source ended, in one line.
    private var summary: (text: String, failed: Bool)? {
        let enabled = Source.allCases.filter(Settings.enabled)
        let failed = enabled.filter { model.overview($0).tone == .failed }.map(\.title)
        if !failed.isEmpty {
            return ("\(failed.joined(separator: " and ")) failed. Open \(failed.count == 1 ? "it" : "each") below for the error.", true)
        }
        guard let last = enabled.compactMap({ model.state.status[$0]?.lastSync }).max() else { return nil }
        return ("Everything synced \(last.formatted(.relative(presentation: .named))).", false)
    }
}

/// One source in the overview: its name, one line of where it stands, and
/// its state as a badge.
private struct SourceRow: View {
    @EnvironmentObject private var model: AppModel
    let source: Source

    var body: some View {
        let overview = model.overview(source)
        HStack(spacing: 12) {
            Image(systemName: source.symbol)
                .font(.body.weight(.semibold))
                .foregroundStyle(.white)
                .frame(width: 32, height: 32)
                .background(source.tint, in: RoundedRectangle(cornerRadius: 8))
            VStack(alignment: .leading, spacing: 2) {
                Text(source.title).font(.body.weight(.medium))
                Text(line).font(.footnote).foregroundStyle(.secondary).lineLimit(2)
            }
            Spacer(minLength: 8)
            StatusBadge(text: overview.badge, color: overview.tone.color)
        }
        .padding(.vertical, 2)
    }

    private var line: String {
        if let p = model.progress[source] {
            return p.total > 0 ? "\(p.step) · \(p.done.formatted()) of \(p.total.formatted())" : p.step
        }
        guard Settings.enabled(source) else { return "Not synced" }
        var parts: [String] = []
        if let coverage = model.coverageLine(source) { parts.append(coverage) }
        if let last = model.state.status[source]?.lastSync {
            parts.append(last.formatted(.relative(presentation: .named)))
        }
        return parts.isEmpty ? "Never synced" : parts.joined(separator: " · ")
    }
}

/// One source's step, with a bar and counts where the step can count.
struct ProgressRow: View {
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

extension Source {
    var tint: Color {
        switch self {
        case .health: .pink
        case .contacts: .gray
        case .calendars: .red
        }
    }
}

extension SourceOverview.Tone {
    var color: Color {
        switch self {
        case .good: .green
        case .busy: .blue
        case .attention: .orange
        case .failed: .red
        case .off: .secondary
        }
    }
}
