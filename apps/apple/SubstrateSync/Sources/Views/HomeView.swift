import SubstrateKit
import SwiftUI

/// The overview: what is happening now, one row per source with its state,
/// and the way to the activity. Each source's details and settings are on its
/// own page, and the account and the app's settings behind the gear. Above
/// them, what the server says about Sync's access, where it says anything.
struct HomeView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.openURL) private var openURL
    @State private var reviewing = false

    var body: some View {
        List {
            if !model.homeIssues.isEmpty {
                Section {
                    AppStatusBanner(issues: model.homeIssues, appName: SyncManifest.appName, host: host, actions: AppStatusActions(
                        review: { reviewing = true },
                        signInAgain: { Task { await model.authorize() } },
                        moveToAppAccess: { Task { await model.authorize() } },
                        retry: { Task { await model.status.refresh(force: true) } },
                        openFix: model.session.map { session in { path in ServerAddress.page(session.server, path: path).map { openURL($0) } } }))
                        .disabled(model.authorizing)
                }
                .listRowInsets(EdgeInsets())
                .listRowBackground(Color.clear)
            }
            if let problem = model.authorizeError {
                Section {
                    ProblemBox(title: model.session?.isApp == true ? "The review didn't finish" : "Couldn't move to app access",
                               message: problem)
                }
            }
            if !model.unrevoked.isEmpty {
                Section {
                    UnrevokedNotices(appName: SyncManifest.appName, unrevoked: model.unrevoked)
                }
            }

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
                            Text("Last: \(run.source.title), \(run.finished.shortAgo)")
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
        .sheet(isPresented: $reviewing) {
            ReviewChangesSheet(appName: SyncManifest.appName, host: host, changes: model.reviewChanges,
                               firstSignIn: false,
                               onContinue: {
                                   reviewing = false
                                   Task { await model.authorize() }
                               },
                               onCancel: { reviewing = false })
        }
    }

    private var host: String { model.session.map { ServerAddress.display($0.server) } ?? "Your server" }
}

/// What the app is doing now, and the one button. Every state has the same
/// shape (a symbol, a headline, two lines of detail, a bar that shows only
/// while a sync runs, one button), so starting or ending a sync moves
/// nothing below it.
private struct StatusCard: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        let state = self.state
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: state.symbol)
                    .font(.title3)
                    .foregroundStyle(state.color)
                    .frame(width: 28, height: 28)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text(state.headline).font(.headline)
                    // Two lines, always: the detail, or one line of it and
                    // the bar while a sync runs.
                    ZStack(alignment: .topLeading) {
                        Text(" \n ").font(.subheadline).hidden()
                        if model.syncing {
                            VStack(alignment: .leading, spacing: 8) {
                                Text(state.detail).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                                ProgressView(value: state.fraction)
                            }
                        } else {
                            Text(state.detail).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                        }
                    }
                }
                .accessibilityElement(children: .combine)
            }
            if model.syncing {
                Button(role: .destructive) {
                    model.cancelSync()
                } label: {
                    WideLabel(title: "Stop", systemImage: "stop.fill")
                }
                .destructiveAction()
            } else {
                Button {
                    Task { await model.syncNow() }
                } label: {
                    WideLabel(title: "Sync Now", systemImage: "arrow.triangle.2.circlepath")
                }
                .primaryAction()
                .disabled(!Source.allCases.contains(where: Settings.enabled))
            }
        }
        .padding(.vertical, 6)
    }

    private struct Shown {
        var symbol: String
        var color: Color
        var headline: String
        var detail: String
        var fraction: Double?
    }

    private var state: Shown {
        if model.syncing {
            guard let (source, p) = model.current else {
                return Shown(symbol: "arrow.triangle.2.circlepath", color: .blue, headline: "Starting…", detail: "")
            }
            return Shown(symbol: source.symbol, color: .blue, headline: "Syncing \(source.title)", detail: p.detail, fraction: p.fraction)
        }
        if model.serverUnreachable != nil {
            let host = model.session?.server.host() ?? "Your server"
            return Shown(symbol: "wifi.exclamationmark", color: .orange, headline: "Can't reach the server",
                         detail: "\(host) didn't answer. Sync Now tries again.")
        }
        let enabled = Source.allCases.filter(Settings.enabled)
        if enabled.isEmpty {
            return Shown(symbol: "power", color: .secondary, headline: "Nothing is turned on",
                         detail: "Open a source below and turn it on to start.")
        }
        let overviews = enabled.map { ($0, model.overview($0)) }
        func named(_ sources: [Source]) -> String { sources.map(\.title).formatted(.list(type: .and)) }
        let failed = overviews.filter { $0.1.tone == .failed }
        if !failed.isEmpty {
            let errors = Set(failed.compactMap(\.1.error))
            let headline = failed.count == 1 ? "\(failed[0].0.title) didn't sync" : "\(failed.count) sources didn't sync"
            let detail = errors.count == 1 ? errors.first! : "Open each one below to see what went wrong."
            return Shown(symbol: "exclamationmark.triangle.fill", color: .red, headline: headline, detail: detail)
        }
        let blocked = overviews.filter { $0.1.badge == "No access" }.map(\.0)
        if !blocked.isEmpty {
            return Shown(symbol: "hand.raised.fill", color: .orange, headline: "\(named(blocked)) \(blocked.count == 1 ? "needs" : "need") access",
                         detail: "Open \(blocked.count == 1 ? "it" : "each") below to allow it, or turn \(blocked.count == 1 ? "it" : "them") off.")
        }
        if overviews.contains(where: { $0.1.badge == "Stopped" }) {
            return Shown(symbol: "stop.circle.fill", color: .orange, headline: "Sync stopped", detail: "Sync again to carry on where it left off.")
        }
        if overviews.contains(where: { $0.1.badge == "Behind" }), let unsent = model.healthUnsent {
            return Shown(symbol: "clock.arrow.circlepath", color: .orange, headline: "Health is behind",
                         detail: "\(unsent.formatted()) \(unsent == 1 ? "day" : "days") of your Health history \(unsent == 1 ? "is" : "are") not on the server yet. Sync Now carries on.")
        }
        let never = overviews.filter { model.state.status[$0.0]?.lastSync == nil }.map(\.0)
        if never.count == enabled.count {
            return Shown(symbol: "arrow.triangle.2.circlepath", color: .secondary, headline: "Not synced yet",
                         detail: "Sync Now sends everything that is turned on below.")
        }
        if !never.isEmpty {
            return Shown(symbol: "arrow.triangle.2.circlepath", color: .secondary, headline: "\(named(never)) not synced yet",
                         detail: "Sync Now sends \(never.count == 1 ? "it" : "them") with the rest.")
        }
        if overviews.contains(where: { $0.1.badge == "Processing" }) {
            return Shown(symbol: "clock.fill", color: .blue, headline: "Health is being processed",
                         detail: "\(model.healthWaiting.count) days are waiting for the server. The next sync checks them.")
        }
        // Every source synced: as of the one that synced longest ago.
        let oldest = enabled.compactMap { model.state.status[$0]?.lastSync }.min() ?? Date()
        return Shown(symbol: "checkmark.circle.fill", color: .green, headline: "Up to date",
                     detail: "Everything synced \(oldest.formatted(.relative(presentation: .named))).")
    }
}

/// One source in the overview: its name, one line of where it stands, and
/// its state as a badge.
private struct SourceRow: View {
    @EnvironmentObject private var model: AppModel
    let source: Source

    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        let overview = model.overview(source)
        let icon = Image(systemName: source.symbol)
            .font(.body.weight(.semibold))
            .foregroundStyle(.white)
            .frame(width: 32, height: 32)
            .background(source.tint, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
            .accessibilityHidden(true)
        let subtitle = Text(line(overview))
            .font(.subheadline)
            .foregroundStyle(.secondary)
        Group {
            // At the largest text sizes the badge goes under the name, rather
            // than squeezing it.
            if typeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: 6) {
                    HStack(spacing: 12) { icon; Text(source.title).font(.body.weight(.medium)) }
                    subtitle.lineLimit(3)
                    StatusBadge(text: overview.badge, color: overview.tone.color)
                }
            } else {
                HStack(spacing: 12) {
                    icon
                    VStack(alignment: .leading, spacing: 2) {
                        Text(source.title).font(.body.weight(.medium))
                        subtitle.lineLimit(1)
                    }
                    Spacer(minLength: 8)
                    StatusBadge(text: overview.badge, color: overview.tone.color)
                }
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }

    /// One line: what it is doing, what went wrong, or what it holds and
    /// when it last ran; what it syncs where it is off or new.
    private func line(_ overview: SourceOverview) -> String {
        if let p = model.progress[source] { return p.detail }
        if overview.tone == .off { return source.summary }
        // The badge says it failed and the card above says why: the row says
        // what is still there.
        if overview.error != nil {
            return model.state.status[source]?.lastSync.map { "Last synced \($0.shortAgo)" } ?? "Never synced"
        }
        guard let last = model.lastRun(source) else { return source.summary }
        return [model.coverageLine(source), last.shortAgo].compactMap { $0 }.joined(separator: " · ")
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
        case .reminders: .orange
        case .places: .blue
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
