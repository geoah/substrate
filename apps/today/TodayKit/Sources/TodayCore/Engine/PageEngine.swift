import Foundation
import SubstrateCore

// The pages: the priorities, the list of projects, a project, a person, a
// meeting, a task and the plan. Each is made on demand from the day.

extension DayEngine: PageMaking {
    public func page(_ route: TodayRoute) -> PagePresentation {
        switch route {
        case .priorities: return .priorities(prioritiesPage)
        case .projects: return .projects(projectsPage)
        case .plan: return .plan(planPage)
        case .suggested: return .suggested(suggestedPage)
        case let .project(id): return day.list(id).map { .project(projectPage($0)) } ?? .missing(MissingPage())
        case let .person(id): return day.person(id).map { .person(personPage($0)) } ?? .missing(MissingPage())
        case let .meeting(id): return day.meeting(id).map { .meeting(meetingPage($0)) } ?? .missing(MissingPage())
        case let .task(id): return day.task(id).map { .task(taskPage($0)) } ?? .missing(MissingPage())
        }
    }

    // MARK: Priorities

    func priorityControl(_ list: ListItem) -> FlagControl {
        let suggestion = flagSuggestion(list.path)
        let changes = suggestion?.flag != nil && suggestion?.flag != list.flag
        return FlagControl(
            stripe: list.flag == nil ? Stripe(suggestion?.flag, suggested: true) : Stripe(list.flag), word: list.flag?.word ?? "Not a priority",
            options: flagOptions(
                selected: list.flag, suggested: changes ? suggestion?.flag : nil, reason: suggestion?.reason, lines: DayEngine.priorityLines
            ) { .setPriority(project: list.id, $0) }, open: changes)
    }

    var prioritiesPage: PrioritiesPage {
        let adds = day.lists.filter { $0.active && $0.flag == nil && flagSuggestion($0.path)?.flag != nil }
        let rest = day.lists.filter { list in list.active && list.flag == nil && !adds.contains { $0.id == list.id } }
        return PrioritiesPage(
            note: "Yours to set, as many as you like, each with a flag. Moss suggests a change when it sees a reason, and nothing changes until you choose.",
            suggestedTitle: adds.isEmpty ? nil : "Moss suggests adding",
            suggested: adds.map { PriorityEntry(id: $0.id, tile: tile($0, suggested: true), name: $0.name, flag: priorityControl($0), opens: .project($0.id)) },
            entries: priorities.map { list in
                PriorityEntry(
                    id: list.id, tile: tile(list), name: list.name, line: stand(list), flag: priorityControl(list),
                    remove: ButtonPresentation("Remove", role: .destructive, command: .removePriority(project: list.id)), opens: .project(list.id))
            },
            add: OptionList(
                .square,
                rest.map { list in
                    OptionRow(id: list.id, name: list.name, line: stand(list), icon: .tile(tile(list)), command: .togglePriority(project: list.id))
                }), addEmpty: rest.isEmpty ? (day.lists.isEmpty ? "No projects yet." : "Every project is on your list.") : nil)
    }

    // MARK: All projects

    func projectLine(_ list: ListItem) -> ProjectLine {
        let digest = day.digests.first { $0.key == list.id }
        let fresh = digest.map { digest in dayItems(digest).filter { isNew($0, in: digest) } } ?? []
        let urgent = day.tasks.contains { $0.list == list.id && $0.urgent && $0.isOpen }
        return ProjectLine(
            id: list.id, tile: tile(list), name: list.name, line: stand(list),
            news: fresh.first.map { "New at \(clock($0.at)). \($0.text)" + (fresh.count > 1 ? " And \(Words.low(Words.num(fresh.count - 1))) more." : "") },
            pill: urgent ? .urgent : nil, stripe: Stripe(list.flag))
    }

    var projectsPage: ProjectsPage {
        let others = day.lists.filter { $0.active && $0.flag == nil }
        return ProjectsPage(
            priorities: priorities.map(projectLine), others: others.map(projectLine), empty: day.lists.isEmpty ? "No projects yet." : nil)
    }

    // MARK: A project

    func projectPage(_ list: ListItem) -> ProjectPage {
        let mine = day.tasks.filter { $0.list == list.id }
        let today = Set(todaysTasks.map(\.id))
        let open = mine.filter { $0.isOpen && today.contains($0.id) }.sorted { rank($0.shownFlag) < rank($1.shownFlag) }
        let notToday = mine.filter { $0.isOpen && !today.contains($0.id) }.sorted { rank($0.shownFlag) < rank($1.shownFlag) }
        let done = mine.filter { $0.status == .done && ($0.completedAt.map(isToday) ?? false) }
        let suggested = mine.filter(\.isProposed)
        let coming = todaysMeetings.filter { $0.end > now && tasks(for: $0).contains { $0.list == list.id } }
        let timeline = timeline(list, done: done, coming: coming)
        let urgent = mine.first { $0.urgent && $0.isOpen }
        return ProjectPage(
            id: list.id, tile: tile(list), name: list.name, flag: list.flag == nil && flagSuggestion(list.path) == nil ? nil : priorityControl(list),
            makePriority: list.flag == nil && flagSuggestion(list.path) == nil
                ? ButtonPresentation("Make it a priority", role: .secondary, command: .togglePriority(project: list.id)) : nil,
            summary: list.summary, urgent: urgent.map(taskLine),
            moss: mossBox(.project(list.id), about: list.name, asks: ["What should I do next here?", "What changed this week?"]),
            tasks: open.map(taskLine), tasksEmpty: open.isEmpty && done.isEmpty ? "No task of yours here today." : nil, done: done.map(taskLine),
            // Under the label "Not today" a line that only says so again is left out.
            notToday: notToday.map { task in
                var line = taskLine(task)
                if line.line == "Not today." { line.line = nil }
                return line
            }, suggested: suggested.compactMap(listedSuggestion),
            suggestedEmpty: suggested.allSatisfy { listedSuggestion($0) == nil } ? "Nothing suggested here that you have not answered." : nil,
            timeline: timeline,
            timelineEmpty: timeline.isEmpty ? "Nothing is recorded for this project in the sources checked." : nil,
            meetings: coming.map {
                MeetingLine(id: $0.id, title: $0.title, line: "\(clock($0.start)) to \(clock($0.end))" + ($0.start <= now ? ", on now." : "."))
            })
    }

    // MARK: A project's timeline

    /// The day a line of a project's timeline stands under. Today is two:
    /// what came in since waking, and the night before it.
    enum TimelineDay: Hashable {
        case today
        case night
        /// An earlier day, by where it starts.
        case day(Date)
    }

    /// Where the night starts on a project's timeline: at the last look of
    /// the day before, where that was in its evening. A last look after
    /// midnight leaves the night starting at midnight, and one longer ago
    /// than the evening leaves it starting where a night with no look
    /// before it does, so every line under "Overnight" is of one night and
    /// its clock time says when.
    var timelineNightFrom: Date { min(max(nightFrom, day.startOfToday.addingTimeInterval(-8 * 3600)), day.startOfToday) }

    func timelineDay(_ at: Date) -> TimelineDay {
        if at > wokeAt { return .today }
        if at > timelineNightFrom || at >= day.startOfToday { return .night }
        return .day(calendar.startOfDay(for: at))
    }

    /// A project's timeline: one group for each day, said once ("Today",
    /// "Overnight", "Yesterday", then the date), and inside a day one
    /// section for each place its lines came from: first what was written
    /// from no record, then the places in order of each one's newest line.
    /// A line's time is its clock time alone.
    func timeline(_ list: ListItem, done: [TaskItem], coming: [Meeting]) -> [ProjectPage.TimelineGroup] {
        var out: [ProjectPage.TimelineGroup] = []
        if !coming.isEmpty {
            let lines = coming.map {
                TimelineLine(id: "m-" + $0.id, time: clock($0.start), text: $0.title + ($0.start <= now ? ", on now." : "."), opens: .meeting($0.id))
            }
            out.append(.init(id: "coming", label: "Coming up", sections: [.init(id: "coming:", lines: lines)]))
        }
        var entries: [(at: Date, source: String?, line: TimelineLine)] = []
        // A line is named by when it happened, so it keeps its place when a newer one arrives above it.
        var taken: Set<String> = []
        for item in day.digests.first(where: { $0.key == list.id })?.items ?? [] where item.at <= now {
            var id = "d-\(Int(item.at.timeIntervalSince1970))"
            while !taken.insert(id).inserted { id += "+" }
            // Its section says where it came from, so its button does not.
            let source = day.source(of: origin(item))
            entries.append((item.at, source, line(item, id: id, placeSaid: source != nil)))
        }
        for task in done {
            guard let at = task.completedAt else { continue }
            entries.append((at, nil, TimelineLine(id: "t-" + task.id, time: clock(at), who: "You", text: "finished: \(Words.low(task.name)).")))
        }
        let days = Dictionary(grouping: entries) { timelineDay($0.at) }
        func order(_ day: TimelineDay) -> Date {
            switch day {
            case .today: .distantFuture
            case .night: wokeAt
            case let .day(start): start
            }
        }
        for key in days.keys.sorted(by: { order($0) > order($1) }) {
            let id: String
            let label: String
            switch key {
            case .today: (id, label) = ("today", "Today")
            case .night: (id, label) = ("night", "Overnight")
            case let .day(start):
                id = "day-" + CivilDay(start, in: calendar).description
                label = start == day.startOfYesterday ? "Yesterday" : Words.longDate(start, calendar)
            }
            // All of one place's lines in a day are one section, the newest line first. What was written from no
            // record has no label, so it stands first, under the day's name: after a labelled section it would
            // read as more of that one.
            let sections = Dictionary(grouping: days[key] ?? []) { $0.source ?? "" }.map { source, entries in
                (
                    newest: source.isEmpty ? Date.distantFuture : entries.map(\.at).max() ?? .distantPast, source: source,
                    lines: entries.sorted { ($0.at, $0.line.id) > ($1.at, $1.line.id) }.map(\.line)
                )
            }
            out.append(
                .init(
                    id: id, label: label,
                    sections: sections.sorted { ($0.newest, $1.source) > ($1.newest, $0.source) }.map {
                        .init(id: "\(id):\($0.source)", source: $0.source.isEmpty ? nil : $0.source, lines: $0.lines)
                    }))
        }
        return out
    }

    // MARK: A person

    func personPage(_ person: Person) -> PersonPage {
        let mine = day.tasks.filter { $0.people.contains(person.path) }
        let meetings = todaysMeetings.filter { meeting in meeting.end > now && people(meeting).contains { $0.person?.path == person.path } }
        let open = mine.filter(\.isOpen)
        return PersonPage(
            id: person.path, badge: person.badge, name: person.name, sees: meetings.first.map { "You see \(person.short) at \(clock($0.start))." },
            tasksTitle: "Tasks with \(person.short)", tasks: open.map(taskLine), tasksEmpty: open.isEmpty ? "No open task involves \(person.short)." : nil,
            suggested: mine.filter(\.isProposed).compactMap(listedSuggestion),
            meetings: meetings.map { MeetingLine(id: $0.id, title: $0.title, line: "\(clock($0.start)) to \(clock($0.end)).") },
            moss: mossBox(.person(person.path), about: person.short, asks: ["What is open between us?", "Draft a message to \(person.short)"]))
    }

    // MARK: A meeting and a task

    func meetingPage(_ meeting: Meeting) -> MeetingPage {
        let when =
            meeting.end <= now
            ? "over"
            : meeting.start <= now ? "now" : isToday(meeting.start) ? Words.until(meeting.start, now: now) : Words.day(meeting.start, now: now, calendar)
        return MeetingPage(
            id: meeting.id, eyebrow: "\(clock(meeting.start)) to \(clock(meeting.end)), \(when)", title: meeting.title, card: meetingCard(meeting))
    }

    func taskPage(_ task: TaskItem) -> TaskPage {
        var eyebrow: String?
        if task.status == .done { eyebrow = task.completedAt.map { "Done at \(clock($0))" } ?? "Done" }
        if task.status == .abandoned { eyebrow = "Dropped" }
        var card = taskCard(task)
        // The line above the title already says when it was done.
        if task.status == .done { card.said = nil }
        let told = task.isProposed ? toldItem(task, alone: true) : nil
        // What was said about the suggestion is drawn with its answer once: not again in the box under it.
        if let answered = told?.told?.answer { card.moss?.exchanges.removeAll { $0.id == answered.id } }
        return TaskPage(
            id: task.id, eyebrow: eyebrow, glyph: task.status == .done ? .done : .task, title: task.name, line: reason(task), stripe: stripe(task),
            card: card, suggestion: task.isProposed ? told ?? suggestedItem(task, own: true, whole: true) : nil)
    }
}
