import Foundation
import SubstrateCore

// The pane: the strip, then the order of the day as one list of items.

extension DayEngine {
    // MARK: The strip

    /// How today stands on a project, in a few words.
    func stand(_ list: ListItem) -> String {
        let mine = day.tasks.filter { $0.list == list.id }
        let today = todaysTasks.filter { $0.list == list.id }
        let done = mine.filter { $0.status == .done && ($0.completedAt.map(isToday) ?? false) }
        let total = today.count + done.count
        if total > 0 { return "\(done.count) of \(total) done" }
        let suggested = mine.filter { $0.isOpen && !hidden($0) && suggestedForToday($0) }.count
        if !planSaved, suggested > 0 { return "\(suggested) suggested" }
        return "Nothing for today"
    }

    var strip: StripPresentation {
        var entries = priorities.map { list -> StripEntry in
            let change = flagSuggestion(list.path)?.flag
            return StripEntry(
                id: list.id, tile: tile(list), name: list.name, line: change == nil ? stand(list) : nil,
                suggestion: change.flatMap { $0 == list.flag ? nil : "to \($0.word)" }, stripe: Stripe(list.flag), opens: .project(list.id))
        }
        // A project the assistant suggests adding is an entry of its own:
        // a hollow stripe, an outlined tile, and one line of reason.
        for list in day.lists where list.active && list.flag == nil {
            guard let suggestion = flagSuggestion(list.path), let flag = suggestion.flag else { continue }
            entries.append(
                StripEntry(
                    id: list.id, tile: tile(list, suggested: true), name: list.name, line: nil, suggestion: "add as \(flag.word)",
                    stripe: Stripe(flag, suggested: true), opens: .priorities))
        }
        return StripPresentation(
            date: Words.longDate(now, calendar), dateShort: Words.shortDate(now, calendar), entries: entries,
            empty: entries.isEmpty ? (day.lists.isEmpty ? "No projects yet." : "No project is flagged. Flag the ones that matter.") : nil)
    }

    // MARK: The rows of a stretch

    /// The open rows of the day, each in the stretch it belongs to, in the
    /// order they are acted on.
    func rows(in stretch: Stretch, current: Int) -> [Row] {
        var out: [Row] = []
        let isCurrent = stretch.index == current
        let state: Row.State = stretch.phase == .later ? .later : .next
        let routines = todaysRoutines.filter { doneAt($0) == nil && self.stretch(at: $0.slot) == stretch.index }
        // A routine within the hour is first: on waking, the pane is the routine.
        let soon = routines.filter { $0.slot <= now.addingTimeInterval(60 * 60) }
        let later = routines.filter { $0.slot > now.addingTimeInterval(60 * 60) }
        // The day is closing: the wrap leads what is left of it.
        let closing = stretch.next == nil && stretch.phase == .now && now >= (calendar.date(bySettingHour: 21, minute: 0, second: 0, of: now) ?? now)
        if closing, let wrap = wrapRow(state: state, closing: true) { out.append(wrap) }
        out += soon.map { row($0, state: state) }
        if isCurrent {
            if let yesterday = yesterdayRow(yesterdayLeft, state: state) { out.append(yesterday) }
            if let suggested = suggestedRow(state: state) { out.append(suggested) }
            if let plan = planRow(state: state) { out.append(plan) }
            out += heard.map { heardRow($0, state: state) }
        }
        // An overlap the plan left open comes back in the last free stretch
        // before it is needed.
        if planSaved {
            for overlap in overlaps where overlap.chosen == nil && overlap.first > now {
                let before = stretches.last { $0.from <= overlap.first && $0.to <= overlap.first }?.index ?? 0
                guard max(before, current) == stretch.index else { continue }
                out.append(
                    Row(
                        id: "overlap:" + overlap.id, glyph: .meeting, title: name(overlap), line: "Choose before \(clock(overlap.first)).", state: state,
                        body: .overlap(card(overlap))))
            }
        }
        let interrupting = Set(interrupting.map(\.id))
        let tasks = todaysTasks.filter { task in
            guard !interrupting.contains(task.id) else { return false }
            let moved = task.notBefore.flatMap { $0 > now ? $0 : nil }
            return (moved.map { self.stretch(at: $0) } ?? current) == stretch.index
        }.sorted { (rank(orderFlag($0)), due($0), $0.createdAt) < (rank(orderFlag($1)), due($1), $1.createdAt) }
        out += tasks.map { taskRow($0, state: state) }
        out += later.map { row($0, state: state) }
        // Before the evening the wrap is the last thing ahead, never the
        // first thing of a day with nothing else in it.
        let evening = now >= (calendar.date(bySettingHour: 18, minute: 0, second: 0, of: now) ?? now)
        if stretch.next == nil, !closing, stretch.phase == .later || evening || !out.isEmpty, let wrap = wrapRow(state: state) { out.append(wrap) }
        return out
    }

    /// When a task is needed, among tasks of one flag: its time, else the
    /// start of its day when that is today or past, else never.
    func due(_ task: TaskItem) -> Date {
        if let at = task.dueAt { return at }
        if let day = task.dueDay, day <= self.day.today { return day.start(in: calendar) }
        return .distantFuture
    }

    // MARK: The pane

    public var pane: PanePresentation {
        let stretches = stretches
        let runs = runs
        let current = currentStretch
        let urgent = interrupting
        var items: [PaneItem] = []
        var lead: String?
        var ground = Ground.working
        let happening = happening
        var placedHappening = false

        func place() {
            guard !placedHappening, let happening else { return }
            items.append(.happening(happening))
            placedHappening = true
        }

        let inRun = runs.contains { $0.from <= now && $0.to > now }
        let currentRows = stretches.first { $0.index == current }.map { rows(in: $0, current: current) } ?? []
        // The night leads on waking: unread, with something in it, and
        // nothing due before it.
        let dueFirst = currentRows.first.map { if case .routine = $0.body { true } else { false } } ?? false
        let nightLeads = nightWaits && urgent.isEmpty && !inRun && !dueFirst && !(stretches.first { $0.index == current }?.near ?? false)
        items.append(.night(night(leads: nightLeads)))
        if let earlier { items.append(.earlier(earlier)) }
        // The saved plan keeps a row, under what is done: it opens the page and never leads.
        if let plan = savedPlanRow { items.append(.row(plan)) }
        if nightLeads {
            lead = "night"
            ground = .planning
        }

        // What cannot wait takes its place ahead of everything, and parks
        // what was leading.
        if !urgent.isEmpty {
            let parked = currentRows.first { if case .task = $0.body { true } else { false } }?.title
            place()
            for task in urgent { items.append(.row(urgentRow(task, parked: parked))) }
            lead = "row:urgent:" + urgent[0].id
            ground = .urgent
        }

        for (index, stretch) in stretches.enumerated() {
            var rows = stretch.phase == .past ? [] : stretch.index == current ? currentRows : rows(in: stretch, current: current)
            if stretch.phase == .now {
                if stretch.near, !rows.isEmpty {
                    // This close to a fixed point, the meeting leads and the rows wait.
                    let names = rows.prefix(4).map { Words.low($0.title) }
                    items.append(
                        .parked(
                            ParkedLine(
                                id: "s\(stretch.index)",
                                what: Words.cap(Words.list(names + (rows.count > 4 ? ["\(Words.low(Words.num(rows.count - 4))) more"] : []))),
                                rest: "\(rows.count == 1 ? "waits" : "wait") until you are out.")))
                    rows = []
                } else if lead == nil, let first = rows.first {
                    rows[0].state = .now
                    lead = "row:" + first.id
                    ground = self.ground(for: first)
                    place()
                } else if rows.isEmpty, stretch.next == nil, lead == nil {
                    items.append(.note(NoteLine(id: "clear", text: "Nothing is left today.")))
                    ground = .closing
                }
            }
            items += rows.map(PaneItem.row)
            if stretch.phase == .now, !stretch.near, urgent.isEmpty { items += unfinished(in: stretch).map(PaneItem.unfinished) }

            guard index < runs.count else { continue }
            let run = runs[index]
            guard run.to > now else { continue }
            let live = run.meetings.first { $0.start <= now && $0.end > now && attends($0) != false }
            let upcoming = run.meetings.first { $0.start > now && attends($0) != false }
            // The meeting in progress leads; else the one about to start,
            // when the stretch before it is nearly over or holds nothing.
            var leading: String?
            if lead == nil {
                if let live {
                    leading = live.id
                } else if stretch.phase == .now, stretch.near || currentRows.isEmpty {
                    leading = upcoming?.id
                }
                if let leading {
                    lead = "meeting:" + leading
                    ground = .meeting
                }
            }
            var seen: Set<String> = []
            for meeting in run.meetings where meeting.end > now {
                if let overlap = overlap(of: meeting) {
                    guard !seen.contains(overlap.id) else { continue }
                    seen.insert(overlap.id)
                    let members = overlap.meetings.filter { $0.end > now }
                    if members.contains(where: { $0.id == leading }) { place() }
                    items.append(.overlap(OverlapBlock(id: overlap.id, card: card(overlap), meetings: members.map { meetingRow($0, leads: $0.id == leading) })))
                } else {
                    if meeting.id == leading { place() }
                    items.append(.meeting(meetingRow(meeting, leads: meeting.id == leading)))
                }
            }
        }
        if !placedHappening, let happening {
            // Nothing leads: it sits under what is done.
            let index = items.firstIndex { if case .row = $0 { true } else if case .meeting = $0 { true } else { false } } ?? items.count
            items.insert(.happening(happening), at: index)
        }
        let asked = day.exchanges.last { $0.subject == .day }
        return PanePresentation(
            strip: strip, items: items, ground: ground,
            // Named once its row is drawn: the write reaches the records a
            // moment after the note of it, and a view scrolls when this changes.
            composer: ComposerPresentation(added: day.notes.added.last.flatMap { day.task($0) == nil ? nil : "task:" + $0 }),
            answer: asked.map(answerCard), lead: lead,
            empty: day.discovery.present.isEmpty && day.discovery.series.isEmpty
                ? "Nothing to draw a day from yet. Add the tasks, routines and today packages to this repository." : nil)
    }

    /// The ground a leading row gives the pane.
    func ground(for row: Row) -> Ground {
        switch row.body {
        case .routine: .waking
        case .yesterday: .waking
        case .suggested, .heard, .plan, .overlap: .planning
        case .urgent: .urgent
        case .task: .working
        case .wrap: .closing
        }
    }

    /// Something flagged High that will not be finished before the next
    /// fixed point: the assistant says so and asks where it goes. Only
    /// when the next meeting is within half an hour, and at most two.
    func unfinished(in stretch: Stretch) -> [UnfinishedCard] {
        guard let next = stretch.next, next.from.timeIntervalSince(now) <= 30 * 60 else { return [] }
        // What was moved into this stretch was placed here on purpose.
        let late = todaysTasks.filter { orderFlag($0) == .high && !$0.urgent && ($0.notBefore ?? .distantPast) < stretch.from }.prefix(2)
        let options = whenOptions.filter { $0.id != "today" }
        return late.map { task in
            let selected = whenSelected(task)
            var rows = options.map { option in
                OptionRow(
                    id: option.id, name: option.name, line: option.line, selected: selected == option.id, command: .moveTo(task: task.id, option: option.id))
            }
            rows.append(
                OptionRow(
                    id: "today", name: "Leave it where it is", line: "It stays open where it is. The evening asks again.",
                    command: .moveTo(task: task.id, option: "today")))
            return UnfinishedCard(
                id: task.id,
                line: "\(Words.cap(Words.low(task.name))) will not be finished before \(next.meetings[0].title) at \(clock(next.from)). Where does it go?",
                left: day.returns[task.path].map { "Left at \(clock($0.at)): \(Words.low($0.text))" }, options: OptionList(.round, rows),
                project: chip(task.list))
        }
    }
}
