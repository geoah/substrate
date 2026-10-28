import SwiftUI

/// Everything the syncs did, as one timeline: each run with what it ended
/// with, its log lines folded inside it, and the lines no run wrote. One
/// source's, or all of them.
struct ActivityView: View {
    @EnvironmentObject private var model: AppModel
    @State private var only: Source?

    init(only: Source? = nil) {
        _only = State(initialValue: only)
    }

    var body: some View {
        let items = TimelineItem.build(runs: model.state.runs, entries: model.activity, only: only)
        List {
            Section {
                Picker("Source", selection: $only) {
                    Text("All").tag(Source?.none)
                    ForEach(Source.allCases) { Text($0.title).tag(Source?.some($0)) }
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            }
            Section {
                if items.isEmpty {
                    Text("Nothing has run yet.").foregroundStyle(.secondary)
                }
                ForEach(items) { item in
                    switch item {
                    case let .run(run, lines):
                        if lines.isEmpty {
                            RunRow(run: run, showsSource: only == nil)
                        } else {
                            DisclosureGroup {
                                ForEach(lines) { ActivityRow(entry: $0, showsSource: false) }
                            } label: {
                                RunRow(run: run, showsSource: only == nil)
                            }
                        }
                    case let .entry(entry):
                        ActivityRow(entry: entry, showsSource: only == nil)
                    }
                }
            }
        }
        .navigationTitle(only.map { "\($0.title) Activity" } ?? "Activity")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                ShareLink(item: Activity.shared.text, preview: SharePreview("Substrate Sync activity"))
            }
            ToolbarItem(placement: .secondaryAction) {
                Button("Clear the Log", role: .destructive) { model.clearActivity() }
            }
        }
    }
}

/// One finished run: its source, when, how it ended, and how long it took.
struct RunRow: View {
    let run: SyncRun
    var showsSource = true

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                Image(systemName: run.error == nil ? "checkmark.circle.fill" : "exclamationmark.triangle.fill")
                    .foregroundStyle(run.error == nil ? .green : .red)
                if showsSource {
                    Text(run.source.title).font(.subheadline.weight(.semibold))
                }
                Text(run.finished, format: .dateTime.day().month().hour().minute())
                    .font(.subheadline).foregroundStyle(showsSource ? .secondary : .primary)
                Spacer()
                Text(Duration.seconds(run.finished.timeIntervalSince(run.started)).formatted(.units(allowed: [.minutes, .seconds], width: .abbreviated)))
                    .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
            }
            Text(run.error ?? run.summary ?? "")
                .font(.footnote)
                .foregroundStyle(run.error == nil ? Color.secondary : Color.red)
        }
        .padding(.vertical, 2)
    }
}

/// One log line.
struct ActivityRow: View {
    let entry: ActivityEntry
    var showsSource = true

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 6) {
                Image(systemName: icon).foregroundStyle(color).font(.caption)
                if showsSource {
                    Text(entry.source?.title ?? "Sync").font(.caption.weight(.semibold))
                }
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
