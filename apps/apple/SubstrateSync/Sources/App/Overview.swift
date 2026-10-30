import Foundation
import SwiftUI

/// One source's state as the overview shows it: a word, a tone, and whether
/// the last run's error still describes the source. A source that is running
/// or waiting its turn shows that, never the error of a run it is replacing.
struct SourceOverview: Equatable {
    enum Tone: Equatable { case good, busy, attention, failed, off }

    var badge: String
    var tone: Tone
    /// The last run's error, when it is still the news.
    var error: String?

    /// `unsent` counts what the source holds and never sent: a run that ended
    /// well is not up to date while history is left behind it.
    static func of(
        enabled: Bool, access: String?, running: Bool, queued: Bool, status: SourceStatus?, waiting: Int, unsent: Int = 0
    ) -> SourceOverview {
        if !enabled { return SourceOverview(badge: "Off", tone: .off) }
        if running { return SourceOverview(badge: "Syncing", tone: .busy) }
        if queued { return SourceOverview(badge: "Queued", tone: .busy) }
        if let access, Self.blocked.contains(where: access.hasPrefix) {
            return SourceOverview(badge: "No access", tone: .attention, error: status?.lastError)
        }
        if status?.stopped == true, let reason = status?.lastError {
            return SourceOverview(badge: "Stopped", tone: .attention, error: reason)
        }
        if let error = status?.lastError { return SourceOverview(badge: "Failed", tone: .failed, error: error) }
        if status?.lastSync == nil { return SourceOverview(badge: "Not synced yet", tone: .attention) }
        if unsent > 0 { return SourceOverview(badge: "Behind", tone: .attention) }
        if waiting > 0 { return SourceOverview(badge: "Processing", tone: .busy) }
        return SourceOverview(badge: "Up to date", tone: .good)
    }

    /// The access words (AppModel.refreshAccess) under which a sync cannot read.
    private static let blocked = ["Not allowed", "Add only", "Not available", "While using"]
}

/// One calendar on the phone, as the picker lists it.
struct CalendarChoice: Identifiable, Equatable {
    let id: String
    let title: String
    /// The account it belongs to: iCloud, an Exchange or Google account,
    /// Subscribed Calendars, Other.
    let account: String
    let note: String?
    let color: Color
}

/// The activity as one timeline: each finished run with the log lines it
/// wrote, and the lines no run wrote (a sign-in, a stop), newest first.
enum TimelineItem: Identifiable, Equatable {
    case run(SyncRun, [ActivityEntry])
    case entry(ActivityEntry)

    var id: String {
        switch self {
        case let .run(run, _): "run-\(run.source.rawValue)-\(run.started.timeIntervalSince1970)"
        case let .entry(entry): entry.id.uuidString
        }
    }

    var at: Date {
        switch self {
        case let .run(run, _): run.finished
        case let .entry(entry): entry.at
        }
    }

    /// `runs` and `entries` newest first, as the state and the log hold them;
    /// `only` keeps one source's.
    static func build(runs: [SyncRun], entries: [ActivityEntry], only: Source? = nil) -> [TimelineItem] {
        let runs = runs.filter { only == nil || $0.source == only }
        var lines: [Int: [ActivityEntry]] = [:]
        var loose: [ActivityEntry] = []
        for entry in entries where only == nil || entry.source == only {
            // A second either side: a run's first line is logged as it starts,
            // its last as it records the run.
            if let i = runs.firstIndex(where: {
                $0.source == entry.source && entry.at >= $0.started.addingTimeInterval(-1) && entry.at <= $0.finished.addingTimeInterval(1)
            }) {
                lines[i, default: []].append(entry)
            } else {
                loose.append(entry)
            }
        }
        let items = runs.enumerated().map { TimelineItem.run($0.element, lines[$0.offset] ?? []) } + loose.map(TimelineItem.entry)
        return items.sorted { $0.at > $1.at }
    }
}
