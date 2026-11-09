import Foundation
import SubstrateCore

/// Where an open task falls on Needs you (apps/tasks/contract/README.md,
/// "A record"): late or urgent, due within the week, due later, or with no
/// date at all.
public enum TaskBand: String, Bridged, Hashable, Sendable, CaseIterable {
    /// Past its moment or its day, or pressing with no date.
    case lateOrUrgent
    /// Due today or within the next seven days.
    case comingUp
    case later
    /// No date, and not pressing: counted, never listed, on Needs you.
    case noDate
}

public extension TasksModel {
    /// How far Coming up reaches past today.
    static let horizonDays = 7

    /// An open task's band at `now`; nil for any other.
    static func band(of task: TaskItem, now: Date, today: CivilDay, calendar: Calendar) -> TaskBand? {
        guard task.isOpen else { return nil }
        if let due = task.due {
            let late = due.instant.map { $0 < now } ?? (due.day < today)
            if late { return .lateOrUrgent }
            return due.day <= today.adding(days: horizonDays, in: calendar) ? .comingUp : .later
        }
        return task.pressing ? .lateOrUrgent : .noDate
    }

    /// Every open task, narrowed to `filter` where there is one, in its
    /// band, in the contract's order: what is late by when it was due, then
    /// what has no date by how pressing; what is coming and what is later by
    /// when; each tie by the owner's order, then by when it was written.
    func banded(filter: ListSelection?, now: Date, today: CivilDay) -> [(task: TaskItem, band: TaskBand)] {
        let open = tasks.values.filter { task in task.isOpen && (filter.map { holds(task, in: $0) } ?? true) }
        let ranked = open.compactMap { task in Self.band(of: task, now: now, today: today, calendar: calendar).map { (task: task, band: $0) } }
        return ranked.sorted { a, b in
            if a.band != b.band { return a.band.rank < b.band.rank }
            switch (a.task.due, b.task.due) {
            case let (x?, y?):
                let (kx, ky) = (x.sortKey(in: calendar), y.sortKey(in: calendar))
                return kx != ky ? kx < ky : Self.tie(a.task, b.task)
            case (.some, nil): return true
            case (nil, .some): return false
            case (nil, nil):
                let (ra, rb) = (fromTop(a.task), fromTop(b.task))
                return ra != rb ? ra < rb : Self.tie(a.task, b.task)
            }
        }
    }

    /// How many open tasks are late or urgent, across every collection:
    /// Needs you's badge.
    func lateOrUrgentCount(now: Date, today: CivilDay) -> Int {
        tasks.values.filter { Self.band(of: $0, now: now, today: today, calendar: calendar) == .lateOrUrgent }.count
    }

    /// A tie in any band: the owner's order where both are of one kind and
    /// hold one, then when each was written, then the path.
    static func tie(_ a: TaskItem, _ b: TaskItem) -> Bool {
        if a.kind == b.kind, let x = a.position, let y = b.position, x != y { return x < y }
        let (ca, cb) = (a.createdAt ?? .distantPast, b.createdAt ?? .distantPast)
        if ca != cb { return ca < cb }
        return a.id < b.id
    }

    /// How far a priority is from the top of its own collection's values (the
    /// top is 0), so ladders of any length rank alike; none is last.
    func fromTop(_ task: TaskItem) -> Int {
        guard let collection = collection(task.kind), let value = task.priorityValue, let index = collection.priorities.firstIndex(of: value)
        else { return Int.max }
        return collection.priorities.count - 1 - index
    }
}

extension TaskBand {
    var rank: Int { Self.allCases.firstIndex(of: self) ?? 0 }
}
