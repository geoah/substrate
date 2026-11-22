import Foundation
import SubstrateCore

// Routines: everything that binds the series trait, by the occurrences the
// window read gave. The occurrences of one local day whose series share a
// group are one row and one tap, whatever their slots: the steps of one
// routine sit a minute apart to keep their order.

extension DayEngine {
    struct RoutineRow: Equatable {
        var id: String
        var title: String
        /// The first slot of the row: where it sits in the day.
        var slot: Date
        /// In slot order, as the steps are done.
        var occurrences: [Occurrence]
        var isDose: Bool
    }

    /// The rows whose first slot is in a stretch of time, in slot order:
    /// active series only. A group is whole or absent: its row is made from
    /// every occurrence of its local day, then kept if it starts in the
    /// stretch.
    func routineRows(from start: Date, to end: Date) -> [RoutineRow] {
        var rows: [String: RoutineRow] = [:]
        let first = calendar.startOfDay(for: start)
        let last = calendar.date(byAdding: .day, value: 1, to: calendar.startOfDay(for: end)) ?? end.addingTimeInterval(86400)
        for occurrence in day.occurrences where occurrence.slot >= first && occurrence.slot < last {
            guard let series = day.series[occurrence.series], series.active else { continue }
            let id =
                if let group = series.group {
                    "routine:g:\(group):\(CivilDay(occurrence.slot, in: calendar).description)"
                } else {
                    "routine:\(series.id):\(Occurrence.stamp(occurrence.slot))"
                }
            var row = rows[id] ?? RoutineRow(id: id, title: series.group ?? series.name, slot: occurrence.slot, occurrences: [], isDose: false)
            row.occurrences.append(occurrence)
            row.slot = min(row.slot, occurrence.slot)
            row.isDose = row.isDose || series.isDose
            rows[id] = row
        }
        return rows.values.filter { $0.slot >= start && $0.slot < end }.map { row in
            var sorted = row
            sorted.occurrences.sort { ($0.slot, day.series[$0.series]?.name ?? "", $0.id) < ($1.slot, day.series[$1.series]?.name ?? "", $1.id) }
            return sorted
        }.sorted { ($0.slot, $0.id) < ($1.slot, $1.id) }
    }

    /// What a series' line says beside its name: a dose's strength where
    /// its medication's record gives one ("200 mg"), else the dose itself
    /// ("1 capsule").
    func detail(_ series: SeriesItem) -> String? { series.medication.flatMap { day.strengths[$0] } ?? series.dose }

    var todaysRoutines: [RoutineRow] { routineRows(from: day.startOfToday, to: day.startOfTomorrow) }
    var yesterdaysRoutines: [RoutineRow] { routineRows(from: day.startOfYesterday, to: day.startOfToday) }

    func open(_ row: RoutineRow) -> [Occurrence] { row.occurrences.filter { day.logs[$0.id] == nil } }

    /// When the row was finished: its last log, once every occurrence has one.
    func doneAt(_ row: RoutineRow) -> Date? {
        guard open(row).isEmpty else { return nil }
        return row.occurrences.compactMap { day.logs[$0.id]?.at }.max()
    }

    /// When anything was last logged, where that is more than a week ago:
    /// logging has stopped, and the rows say so instead of asking after
    /// every day that passed.
    var loggingStopped: Date? {
        guard let last = day.logs.values.map(\.at).max(), now.timeIntervalSince(last) > 7 * 86400 else { return nil }
        return last
    }

    /// One plain line, when nothing has been logged for a week.
    func notLogged(_ row: RoutineRow) -> String? {
        loggingStopped.map { "Not logged since \(Words.dayMonth($0, calendar))." }
    }

    /// What logs a whole row: "Taken" or "Done" for one thing, "All taken"
    /// or "All done" for a group of several.
    func allWord(_ row: RoutineRow) -> String {
        let word = row.isDose ? "taken" : "done"
        return row.occurrences.count > 1 ? "All \(word)" : Words.cap(word)
    }

    func routineCard(_ row: RoutineRow) -> RoutineCard {
        let word = row.isDose ? "Taken" : "Done"
        // A row of one thing says it in its title and its line: listing it again would repeat both.
        let items =
            row.occurrences.count < 2
            ? []
            : row.occurrences.map { occurrence -> RoutineCard.Item in
                let series = day.series[occurrence.series]
                let log = day.logs[occurrence.id]
                // Each line is ticked by itself: a tick logs that one, and a filled tick takes its log back.
                let tick =
                    log?.done == true
                    ? ButtonPresentation("Undo", glyph: .done, iconOnly: true, role: .primary, command: .unlog(occurrence.id))
                    : ButtonPresentation(word, glyph: .done, iconOnly: true, role: .secondary, command: .logOccurrence(occurrence.id, done: true))
                return RoutineCard.Item(
                    id: occurrence.id, name: series?.name ?? "Untitled routine", detail: series.flatMap(detail),
                    logged: log.map { $0.done ? "\(word) at \(clock($0.at))." : "Skipped." }, tick: tick)
            }
        let done = doneAt(row)
        return RoutineCard(
            items: items,
            done: done == nil
                ? ButtonPresentation(allWord(row), glyph: .done, role: .primary, wide: true, command: .log(row: row.id, done: true)) : nil,
            skip: done == nil ? ButtonPresentation("Skip", role: .quiet, command: .log(row: row.id, done: false)) : nil,
            said: done.map { "\(word) at \(clock($0))." }, notLogged: done == nil ? notLogged(row) : nil)
    }

    func row(_ routine: RoutineRow, state: Row.State) -> Row {
        let left = open(routine).count
        let count = routine.occurrences.count
        let line: String
        if count > 1 {
            line = left < count ? "\(count - left) of \(count) logged." : "\(count) items."
        } else {
            line = day.series[routine.occurrences[0].series].flatMap(detail) ?? "At \(clock(routine.slot))."
        }
        return Row(
            id: routine.id, glyph: .routine, title: routine.title, line: line, state: state,
            trailing: .tick(
                ButtonPresentation(allWord(routine), glyph: .done, iconOnly: true, role: .secondary, command: .log(row: routine.id, done: true))),
            body: .routine(routineCard(routine)))
    }

    /// The routine row an id names, among yesterday's and today's.
    func routine(_ id: String) -> RoutineRow? { (todaysRoutines + yesterdaysRoutines).first { $0.id == id } }

    // MARK: What yesterday left

    struct Left: Equatable {
        enum Kind: Equatable {
            case routine(RoutineRow)
            case task(TaskItem)
        }

        var id: String
        var kind: Kind
        /// The option the owner chose for it today, and when: it stays in
        /// the card with its choice filled, and another tap moves the choice.
        var answered: String?
        var at: Date?
    }

    /// What yesterday left, as "Earlier today" and the day's row read it: a
    /// routine row with nothing logged, and each thing answered today,
    /// which keeps its choice. A task that was due before today and is
    /// still open is asked after on the plan page (`leftTasks`), and is
    /// here only once the plan's Save has answered it.
    var yesterdayLeft: [Left] {
        var out: [Left] = []
        let answered = day.notes.yesterday
        for row in yesterdaysRoutines {
            let id = "y-" + row.id
            if let option = answered[id] {
                out.append(Left(id: id, kind: .routine(row), answered: option, at: day.notes.yesterdayAt[id]))
            } else if loggingStopped == nil, !open(row).isEmpty {
                // Where logging has stopped, yesterday's routines are not asked after: today's rows say since when.
                out.append(Left(id: id, kind: .routine(row)))
            }
        }
        for task in day.tasks {
            let id = DayEngine.leftID(task)
            if let option = answered[id] { out.append(Left(id: id, kind: .task(task), answered: option, at: day.notes.yesterdayAt[id])) }
        }
        return out
    }

    /// The id a task yesterday left is noted under once it is answered.
    static func leftID(_ task: TaskItem) -> String { "y-task:" + task.id }

    /// The occurrences of a row an answer of today logged: the ones a
    /// moved choice changes.
    func answeredOccurrences(_ row: RoutineRow, since: Date?) -> [Occurrence] {
        guard let since else { return [] }
        return row.occurrences.filter { (day.logs[$0.id]?.at ?? .distantPast) >= since.addingTimeInterval(-1) }
    }

    /// The routine rows yesterday left, each with its two options: the
    /// ones still not logged, and the ones answered today with their choice.
    func yesterdayCard(_ left: [Left]) -> YesterdayCard {
        let items = left.compactMap { item -> YesterdayCard.Item? in
            guard case let .routine(row) = item.kind else { return nil }
            let verb = row.isDose ? "Took it yesterday" : "Did it yesterday"
            let count = item.answered == nil ? open(row).count : answeredOccurrences(row, since: item.at).count
            return YesterdayCard.Item(
                id: item.id, glyph: .routine, title: row.title,
                text: "\(count == 1 ? "It was" : "\(Words.num(count)) were") due yesterday at \(clock(row.slot))."
                    + (item.answered == nil ? " Not logged." : ""),
                options: OptionList(
                    .round,
                    [
                        OptionRow(
                            id: "took", name: verb, line: "Logged for yesterday.", selected: item.answered == "took",
                            command: .answerYesterday(item: item.id, option: "took")),
                        OptionRow(
                            id: "skipped", name: "Skipped", line: "Logged as skipped.", selected: item.answered == "skipped",
                            command: .answerYesterday(item: item.id, option: "skipped")),
                    ]), chosen: item.at.map { "Chosen at \(clock($0))." })
        }
        return YesterdayCard(items: items)
    }

    /// One answered thing as a line of what is done: "From yesterday:
    /// vitamin B12, taken."
    func yesterdayAnswer(_ item: Left) -> String? {
        let name: String
        let answer: String?
        switch item.kind {
        case let .routine(row):
            name = row.title
            answer = ["took": row.isDose ? "taken" : "done", "skipped": "skipped"][item.answered ?? ""]
        case let .task(task):
            name = task.name
            answer = ["today": "moved to today", "tomorrow": "moved to tomorrow", "drop": "dropped"][item.answered ?? ""]
        }
        guard let answer else { return nil }
        return "From yesterday: \(Words.low(name)), \(answer)."
    }

    /// "yesterday", "on Tuesday": the day a task was due.
    func dueWord(_ task: TaskItem) -> String {
        guard let due = task.dueDay else { return "yesterday" }
        let named = Words.day(due.start(in: calendar, hour: 12), now: now, calendar)
        return Words.weekdays.contains(named) || named.first?.isNumber == true ? "on \(named)" : named
    }

    /// The row of the routines yesterday left with nothing logged: there
    /// while one is unanswered, and it names them. Its card holds each by
    /// name with its options, and the ones answered today too.
    func yesterdayRow(_ left: [Left], state: Row.State) -> Row? {
        let routines = left.filter { if case .routine = $0.kind { true } else { false } }
        let named = routines.filter { $0.answered == nil }.compactMap { item -> String? in
            if case let .routine(row) = item.kind { row.title } else { nil }
        }
        guard !named.isEmpty else { return nil }
        return Row(
            id: "yesterday", glyph: .routine, title: "Not logged yesterday", line: Words.cap(Words.list(named.map(Words.low))) + ".", state: state,
            body: .yesterday(yesterdayCard(routines)))
    }

    // MARK: Reminders

    /// One reminder for each routine row still open in the next three days,
    /// at its first slot: a group is one reminder a day, however many
    /// steps it has. Its words name no medicine: the group, or
    /// "Medication", and a count.
    var reminders: [ReminderPlan] {
        let end = calendar.date(byAdding: .day, value: 3, to: day.startOfToday) ?? now.addingTimeInterval(3 * 86400)
        return routineRows(from: now, to: end).filter { !open($0).isEmpty }.prefix(48).map { row in
            let grouped = day.series[row.occurrences[0].series]?.group != nil
            let count = open(row).count
            return ReminderPlan(
                id: "today.routine." + row.id, title: grouped || !row.isDose ? row.title : "Medication",
                body: count == 1 ? "1 item" : "\(count) items", fireAt: row.slot, row: row.id)
        }
    }
}
