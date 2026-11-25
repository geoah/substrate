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
            said: done.map { "\(settled(row)) at \(clock($0))." }, notLogged: done == nil ? notLogged(row) : nil)
    }

    /// How a finished row was answered, as its line begins: "Taken", "Done",
    /// "Skipped", and where some of it was skipped, how many of each
    /// ("3 of 6 taken, 3 skipped,"). A row is never said to be taken while
    /// part of it was skipped.
    func settled(_ row: RoutineRow) -> String {
        let logs = row.occurrences.compactMap { day.logs[$0.id] }
        let done = logs.filter(\.done).count
        let word = row.isDose ? "taken" : "done"
        if done == logs.count { return Words.cap(word) }
        if done == 0 { return "Skipped" }
        return "\(done) of \(logs.count) \(word), \(logs.count - done) skipped,"
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

    /// A routine row finished today, as a row of what is done: crossed out,
    /// with when, and a filled tick that takes every log of it back.
    func doneRow(_ routine: RoutineRow, at: Date) -> Row {
        return Row(
            id: routine.id, glyph: .routine, title: routine.title, line: "\(settled(routine)) at \(clock(at)).", state: .done,
            trailing: .tick(ButtonPresentation("Take it back", glyph: .done, iconOnly: true, role: .primary, command: .unlogRow(routine.id))),
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

    /// The occurrences of a row that yesterday left: the ones with no log,
    /// and the ones logged today, which is how they were answered. One
    /// logged on its own day is not among them.
    func late(_ row: RoutineRow) -> [Occurrence] {
        row.occurrences.filter { (day.logs[$0.id]?.at ?? .distantFuture) >= day.startOfToday }
    }

    /// What yesterday left, as "Earlier today" and the day's row read it: a
    /// routine row with something not logged, and each one answered today,
    /// which keeps its answer. An answer is read from the logs, so it is the
    /// same whether the owner chose an option for the whole row or ticked
    /// its lines one by one: "took", "skipped", or "mixed" where the lines
    /// differ. A task that was due before today and is still open is asked
    /// after on the plan page (`leftTasks`), and is here only once the
    /// plan's Save has answered it.
    var yesterdayLeft: [Left] {
        var out: [Left] = []
        let answered = day.notes.yesterday
        for row in yesterdaysRoutines {
            let id = "y-" + row.id
            let left = late(row)
            guard !left.isEmpty else { continue }
            let logs = left.compactMap { day.logs[$0.id] }
            if logs.count == left.count {
                let option = logs.allSatisfy(\.done) ? "took" : logs.contains(where: \.done) ? "mixed" : "skipped"
                out.append(Left(id: id, kind: .routine(row), answered: option, at: day.notes.yesterdayAt[id] ?? logs.map(\.at).max()))
            } else if loggingStopped == nil {
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

    /// The row of yesterday an occurrence belongs to.
    func yesterdaysRow(of occurrence: String) -> RoutineRow? {
        yesterdaysRoutines.first { $0.occurrences.contains { $0.id == occurrence } }
    }

    /// What a row of several things left, line by line, each with a tick of
    /// its own: open, it logs that one as taken or done; filled, a tap
    /// takes its log back. A row of one thing has no lines: its title names
    /// it.
    func yesterdayLines(_ row: RoutineRow) -> [RoutineCard.Item] {
        guard row.occurrences.count > 1 else { return [] }
        let word = row.isDose ? "Taken" : "Done"
        return late(row).map { occurrence in
            let series = day.series[occurrence.series]
            let log = day.logs[occurrence.id]
            let tick =
                log?.done == true
                ? ButtonPresentation("Undo", glyph: .done, iconOnly: true, role: .primary, command: .unlog(occurrence.id))
                : ButtonPresentation(word, glyph: .done, iconOnly: true, role: .secondary, command: .logOccurrence(occurrence.id, done: true))
            return RoutineCard.Item(
                id: occurrence.id, name: series?.name ?? "Untitled routine", detail: series.flatMap(detail),
                logged: log.map { $0.done ? "\(word)." : "Skipped." }, tick: tick)
        }
    }

    /// The routine rows yesterday left, each with its lines and its two
    /// options: the ones still not logged, and the ones answered today with
    /// their answer. An option answers what is still open; once nothing is,
    /// it moves the answer of all of it.
    func yesterdayCard(_ left: [Left]) -> YesterdayCard {
        let items = left.compactMap { item -> YesterdayCard.Item? in
            guard case let .routine(row) = item.kind else { return nil }
            let all = late(row)
            let open = all.filter { day.logs[$0.id] == nil }
            let count = all.count
            // Some lines have their answer and some do not: an option is for the rest.
            let rest = item.answered == nil && open.count < count
            let did = row.isDose ? "Took" : "Did"
            var text = "\(count == 1 ? "It was" : "\(Words.num(count)) were") due yesterday at \(clock(row.slot))."
            if item.answered == nil {
                text += rest ? " \(Words.num(open.count)) \(open.count == 1 ? "is" : "are") not logged." : " Not logged."
            }
            return YesterdayCard.Item(
                id: item.id, glyph: .routine, title: row.title, text: text, lines: yesterdayLines(row),
                options: OptionList(
                    .round,
                    [
                        OptionRow(
                            id: "took", name: rest ? "\(did) the rest yesterday" : count > 1 ? "\(did) them all yesterday" : "\(did) it yesterday",
                            line: "Logged for yesterday.", selected: item.answered == "took",
                            command: .answerYesterday(item: item.id, option: "took")),
                        OptionRow(
                            id: "skipped", name: rest ? "Skipped the rest" : count > 1 ? "Skipped them all" : "Skipped",
                            line: "Logged as skipped.", selected: item.answered == "skipped",
                            command: .answerYesterday(item: item.id, option: "skipped")),
                    ]),
                // An option's time where one was chosen; else when the last line was ticked.
                chosen: item.at.map { "\(day.notes.yesterdayAt[item.id] == nil ? "Logged" : "Chosen") at \(clock($0))." })
        }
        return YesterdayCard(items: items)
    }

    /// How an answered thing of yesterday's is said: "taken", "moved to
    /// today", and for a row whose lines differ, how many of each.
    func yesterdayWords(_ item: Left) -> String? {
        switch item.kind {
        case let .routine(row):
            let word = row.isDose ? "taken" : "done"
            guard item.answered == "mixed" else { return ["took": word, "skipped": "skipped"][item.answered ?? ""] }
            let logs = late(row).compactMap { day.logs[$0.id] }
            let done = logs.filter(\.done).count
            return "\(Words.low(Words.num(done))) \(word) and \(Words.low(Words.num(logs.count - done))) skipped"
        case .task:
            return ["today": "moved to today", "tomorrow": "moved to tomorrow", "drop": "dropped"][item.answered ?? ""]
        }
    }

    /// One answered thing as a line of what is done: "From yesterday:
    /// vitamin B12, taken."
    func yesterdayAnswer(_ item: Left) -> String? {
        let name: String
        switch item.kind {
        case let .routine(row): name = row.title
        case let .task(task): name = task.name
        }
        guard let answer = yesterdayWords(item) else { return nil }
        return "From yesterday: \(Words.low(name)), \(answer)."
    }

    /// A routine yesterday left, once it is answered, as a row of what is
    /// done: by name, with its answer, and opening to its lines and its
    /// options, so an answer given by mistake is changed there.
    func yesterdayDoneRow(_ item: Left) -> Row? {
        guard case let .routine(row) = item.kind, let answer = yesterdayWords(item) else { return nil }
        return Row(
            id: item.id, glyph: .routine, title: row.title, line: "From yesterday: \(answer).", state: .done,
            body: .yesterday(yesterdayCard([item])))
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

    // MARK: Outside the app

    /// What a routine row is called outside the app, where no medicine is
    /// named: its group, or its own name where it is not a dose, else
    /// "Medication".
    func outsideTitle(_ row: RoutineRow) -> String {
        let grouped = day.series[row.occurrences[0].series]?.group != nil
        return grouped || !row.isDose ? row.title : "Medication"
    }

    // MARK: Reminders

    /// One reminder for each routine row still open in the next three days,
    /// at its first slot: a group is one reminder a day, however many
    /// steps it has. Its words name no medicine: the group, or
    /// "Medication", and a count.
    var reminders: [ReminderPlan] {
        let end = calendar.date(byAdding: .day, value: 3, to: day.startOfToday) ?? now.addingTimeInterval(3 * 86400)
        return routineRows(from: now, to: end).filter { !open($0).isEmpty }.prefix(48).map { row in
            let count = open(row).count
            return ReminderPlan(
                id: "today.routine." + row.id, title: outsideTitle(row), body: count == 1 ? "1 item" : "\(count) items", fireAt: row.slot, row: row.id)
        }
    }
}
