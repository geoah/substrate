import Foundation
import SubstrateCore

/// Where an open task falls on Needs you: waiting on a decision, due
/// within the week, or later.
public enum TaskBand: String, Bridged, Hashable, Sendable, CaseIterable {
    /// Past its day or its moment, or urgent with no day to be due on.
    case needsYou
    /// Due today or within the next seven days.
    case upcoming
    case later
}

public extension TasksModel {
    /// Where to draw the needs-you line under a priority: `high` and
    /// `urgent` with no date wait on a day.
    static let pressing = TaskPriority.high

    /// How far Upcoming reaches past today.
    static let horizonDays = 7

    /// An open task's band at `now`.
    static func band(of task: TaskItem, now: Date, today: CivilDay, calendar: Calendar) -> TaskBand {
        if let due = task.due {
            let overdue = due.instant.map { $0 < now } ?? (due.day < today)
            if overdue { return .needsYou }
            return due.day <= today.adding(days: horizonDays, in: calendar) ? .upcoming : .later
        }
        return task.priority >= pressing ? .needsYou : .later
    }

    /// Every open task, narrowed to `filter` where there is one, in its
    /// band, ranked: what is late by how late, then what has no day by
    /// how urgent; what is coming by when; the rest by when, the undated
    /// after by how urgent, each tie in the owner's order.
    func banded(filter: ListSelection?, now: Date, today: CivilDay, calendar: Calendar) -> [(task: TaskItem, band: TaskBand)] {
        let open = tasks.values.filter { task in
            guard task.isOpen else { return false }
            switch filter {
            case nil: return true
            case .needsYou?: return true
            case .inbox?: return task.list == nil
            case let .list(id)?: return task.list == id
            }
        }
        let ranked = open.map { (task: $0, band: Self.band(of: $0, now: now, today: today, calendar: calendar)) }
        return ranked.sorted { a, b in
            if a.band != b.band { return a.band.rank < b.band.rank }
            switch (a.task.due, b.task.due) {
            case let (x?, y?):
                let (kx, ky) = (x.sortKey(in: calendar), y.sortKey(in: calendar))
                return kx != ky ? kx < ky : TaskOrder.manual(a.task, b.task)
            case (.some, nil): return true
            case (nil, .some): return false
            case (nil, nil):
                return a.task.priority != b.task.priority ? a.task.priority > b.task.priority : TaskOrder.manual(a.task, b.task)
            }
        }
    }

    /// How many open tasks wait on a decision, across every list.
    func needsYouCount(now: Date, today: CivilDay, calendar: Calendar) -> Int {
        tasks.values.filter { $0.isOpen && Self.band(of: $0, now: now, today: today, calendar: calendar) == .needsYou }.count
    }
}

extension TaskBand {
    var rank: Int { Self.allCases.firstIndex(of: self) ?? 0 }
}
