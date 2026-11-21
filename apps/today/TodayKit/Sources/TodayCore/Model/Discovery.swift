import Foundation
import SubstrateCore

/// What the repository holds of what the pane reads, as the server last
/// said: the fixed kinds that exist, the kinds that bind the series trait,
/// and the kinds whose records are meetings. Kept on the device, so the
/// pane opens offline over the same kinds.
public struct Discovery: Codable, Equatable, Sendable {
    /// Of `TodayKinds.linkable`, the ones the repository declares.
    public var present: [String]
    /// The kinds that bind `routines/series`.
    public var series: [String]
    /// The kinds that bind core `temporal(range)` with `recurring` or
    /// `override`: a calendar's event and its series.
    public var calendars: [String]

    /// The task kind declares what version 4 of the tasks package added
    /// (`notBefore`, `prioritySetBy` and the rest): where it does not, the
    /// app writes none of them.
    public var taskMoves: Bool
    /// The list kind declares `priority`: where it does not, no project
    /// can be flagged.
    public var listPriority: Bool

    public init(present: [String] = [], series: [String] = [], calendars: [String] = [], taskMoves: Bool = true, listPriority: Bool = true) {
        self.present = present
        self.series = series
        self.calendars = calendars
        self.taskMoves = taskMoves
        self.listPriority = listPriority
    }

    /// The kinds the data link reads whole and follows.
    public var linked: [String] {
        var out = TodayKinds.linkable.filter(present.contains)
        for kind in series.sorted() where !out.contains(kind) { out.append(kind) }
        return out
    }

    public func holds(_ kind: String) -> Bool { present.contains(kind) || series.contains(kind) }

    /// Whether a kind's declaration binds core `temporal` as a range.
    static func isRange(_ definition: [String: JSONValue]) -> Bool {
        (definition["traits"]?.array ?? []).contains { ($0.text ?? "").hasPrefix("substrate.reamde.dev/core/temporal(range") }
    }
}
