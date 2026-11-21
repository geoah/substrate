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
        case let .project(id): return day.list(id).map { .project(projectPage($0)) } ?? .missing(MissingPage())
        case let .person(id): return day.person(id).map { .person(personPage($0)) } ?? .missing(MissingPage())
        case let .meeting(id): return day.meetings.first { $0.id == id }.map { .meeting(meetingPage($0)) } ?? .missing(MissingPage())
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
        let digest = day.digests.first { $0.key == list.id }
        var timeline: [ProjectPage.TimelineGroup] = []
        let coming = todaysMeetings.filter { $0.end > now && tasks(for: $0).contains { $0.list == list.id } }
        if !coming.isEmpty {
            timeline.append(
                .init(
                    id: "coming", label: "Coming up",
                    lines: coming.map { TimelineLine(id: "m-" + $0.id, time: clock($0.start), text: $0.title + ($0.start <= now ? ", on now." : "."), opens: .meeting($0.id)) }))
        }
        var todayLines = (digest.map(dayItems) ?? []).enumerated().map { line($1, id: "d-\($0)") }
        todayLines += done.compactMap { task in task.completedAt.map { TimelineLine(id: "t-" + task.id, time: clock($0), who: "You", text: "finished: \(Words.low(task.name)).") } }
        if !todayLines.isEmpty { timeline.append(.init(id: "today", label: "Today", lines: todayLines.sorted { $0.time > $1.time })) }
        let night = (digest.map(nightItems) ?? []).enumerated().map { line($1, id: "n-\($0)") }
        if !night.isEmpty { timeline.append(.init(id: "night", label: "Overnight", lines: night)) }
        let older = (digest?.items ?? []).filter { $0.at <= nightFrom }.enumerated().map { index, item in
            TimelineLine(id: "e-\(index)", time: Words.cap(Words.day(item.at, now: now, calendar)), text: item.text)
        }
        if !older.isEmpty { timeline.append(.init(id: "earlier", label: "Earlier", lines: older)) }
        let urgent = mine.first { $0.urgent && $0.isOpen }
        return ProjectPage(
            id: list.id, tile: tile(list), name: list.name, flag: list.flag == nil && flagSuggestion(list.path) == nil ? nil : priorityControl(list),
            makePriority: list.flag == nil && flagSuggestion(list.path) == nil
                ? ButtonPresentation("Make it a priority", role: .secondary, command: .togglePriority(project: list.id)) : nil,
            summary: list.summary, urgent: urgent.map(taskLine),
            moss: mossBox(.project(list.id), about: list.name, asks: ["What should I do next here?", "What changed this week?"]),
            tasks: open.map(taskLine), tasksEmpty: open.isEmpty && done.isEmpty ? "No task of yours here today." : nil, done: done.map(taskLine),
            notToday: notToday.map(taskLine), suggested: suggested.map { suggestedItem($0, own: true) },
            suggestedEmpty: suggested.isEmpty ? "Nothing suggested here that you have not answered." : nil, timeline: timeline,
            timelineEmpty: timeline.isEmpty ? "Nothing is recorded for this project in the sources checked." : nil,
            meetings: coming.map { MeetingLine(id: $0.id, title: $0.title, line: "\(clock($0.start)) to \(clock($0.end))" + ($0.start <= now ? ", on now." : ".")) })
    }

    // MARK: A person

    func personPage(_ person: Person) -> PersonPage {
        let mine = day.tasks.filter { $0.people.contains(person.path) }
        let meetings = todaysMeetings.filter { meeting in meeting.end > now && people(meeting).contains { $0.person?.path == person.path } }
        let open = mine.filter(\.isOpen)
        return PersonPage(
            id: person.path, badge: person.badge, name: person.name, sees: meetings.first.map { "You see \(person.short) at \(clock($0.start))." },
            tasksTitle: "Tasks with \(person.short)", tasks: open.map(taskLine), tasksEmpty: open.isEmpty ? "No open task involves \(person.short)." : nil,
            suggested: mine.filter(\.isProposed).map { suggestedItem($0, own: true) },
            meetings: meetings.map { MeetingLine(id: $0.id, title: $0.title, line: "\(clock($0.start)) to \(clock($0.end)).") },
            moss: mossBox(.person(person.path), about: person.short, asks: ["What is open between us?", "Draft a message to \(person.short)"]))
    }

    // MARK: A meeting and a task

    func meetingPage(_ meeting: Meeting) -> MeetingPage {
        let when = meeting.end <= now ? "over" : meeting.start <= now ? "now" : isToday(meeting.start) ? Words.until(meeting.start, now: now) : Words.day(meeting.start, now: now, calendar)
        return MeetingPage(id: meeting.id, eyebrow: "\(clock(meeting.start)) to \(clock(meeting.end)), \(when)", title: meeting.title, card: meetingCard(meeting))
    }

    func taskPage(_ task: TaskItem) -> TaskPage {
        var eyebrow: String?
        if task.status == .done { eyebrow = task.completedAt.map { "Done at \(clock($0))" } ?? "Done" }
        if task.status == .abandoned { eyebrow = "Dropped" }
        return TaskPage(
            id: task.id, eyebrow: eyebrow, glyph: task.status == .done ? .done : .task, title: task.name, line: why(task), stripe: stripe(task),
            card: taskCard(task), suggestion: task.isProposed ? suggestedItem(task, own: true) : nil)
    }
}
