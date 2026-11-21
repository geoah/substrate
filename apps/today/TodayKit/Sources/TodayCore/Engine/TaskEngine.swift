import Foundation
import SubstrateCore

// A task in the day, something urgent, "When", and the assistant's box.

extension DayEngine {
    // MARK: When

    struct WhenOption: Equatable {
        var id: String
        var name: String
        var line: String
        /// What it writes to `notBefore`; nil clears it.
        var notBefore: Date?
    }

    /// Where a task can go when it is not for now: the real gaps between
    /// today's meetings, tonight, tomorrow, the end of the week, next week.
    /// No option says how long anything takes.
    var whenOptions: [WhenOption] {
        var out = [WhenOption(id: "today", name: "Today, where it is", line: "It stays in today's order.", notBefore: nil)]
        let later = stretches.filter { $0.phase == .later }
        for stretch in later {
            if stretch.next == nil {
                out.append(
                    WhenOption(
                        id: "tonight", name: "Tonight, after \(clock(stretch.from))", line: "It moves to after the last meeting.",
                        notBefore: stretch.from))
            } else {
                out.append(
                    WhenOption(
                        id: "gap:" + Occurrence.stamp(stretch.from), name: "The \(clock(stretch.from)) gap",
                        line: "It moves to the gap at \(clock(stretch.from)).", notBefore: stretch.from))
            }
        }
        if later.isEmpty, let evening = calendar.date(bySettingHour: 20, minute: 0, second: 0, of: now), evening > now {
            out.append(WhenOption(id: "tonight", name: "Tonight, 20:00", line: "It leaves the order until 20:00.", notBefore: evening))
        }
        out.append(WhenOption(id: "tomorrow", name: "Tomorrow", line: "It leaves today and is in tomorrow's order.", notBefore: day.startOfTomorrow))
        let weekday = calendar.component(.weekday, from: now)  // 1 is Sunday
        if (2 ... 4).contains(weekday), let friday = calendar.date(byAdding: .day, value: 6 - weekday, to: day.startOfToday) {
            out.append(WhenOption(id: "friday", name: "Friday", line: "It leaves today and comes back on Friday.", notBefore: friday))
        }
        let toMonday = (9 - weekday) % 7 == 0 ? 7 : (9 - weekday) % 7
        if let monday = calendar.date(byAdding: .day, value: toMonday, to: day.startOfToday), toMonday > 1 {
            out.append(WhenOption(id: "nextweek", name: "Next week", line: "It leaves today and comes back on Monday.", notBefore: monday))
        }
        return out
    }

    /// The option a task stands at.
    func whenSelected(_ task: TaskItem) -> String {
        guard let moved = task.notBefore, moved > now else { return "today" }
        return whenOptions.first { $0.notBefore == moved }?.id ?? "other"
    }

    func whenDoor(_ task: TaskItem) -> Door {
        let selected = whenSelected(task)
        var rows = whenOptions.map { option in
            OptionRow(id: option.id, name: option.name, line: option.line, selected: option.id == selected, command: .setWhen(task: task.id, option: option.id))
        }
        if selected == "other", let moved = task.notBefore {
            rows.append(
                OptionRow(
                    id: "other", name: Words.cap(Words.when(moved, now: now, calendar)), line: "It is hidden until then.", selected: true,
                    command: .setWhen(task: task.id, option: "other")))
        }
        return Door(
            id: "when", label: "When", value: rows.first { $0.selected }?.name ?? "Today, where it is", options: OptionList(.round, rows),
            notes: task.deferrals >= 3 ? ["Put off \(Words.low(Words.num(task.deferrals))) times."] : [])
    }

    // MARK: A task's doors

    func flagDoor(_ task: TaskItem) -> Door {
        let control = flagControl(task)
        var pills: [Pill] = []
        var value = control.word
        if task.urgent {
            pills.append(.urgent)
            value = "Set by \(Words.low(setter(task)))"
        } else if control.stripe.suggested {
            pills.append(.suggested)
        }
        return Door(
            id: "flag", label: "Flag", value: value, stripe: task.urgent ? nil : control.stripe, pills: pills, options: control.options, notes: control.notes)
    }

    /// The project door: on a task with no project, or one typed today.
    func projectDoor(_ task: TaskItem) -> Door? {
        guard task.list == nil || day.notes.added.contains(task.id) else { return nil }
        let suggestion = listSuggestion(task.path)
        let lists = day.lists.filter(\.active).sorted { (priorityOrder($0.id), $0.name.lowercased()) < (priorityOrder($1.id), $1.name.lowercased()) }
        guard !lists.isEmpty else { return nil }
        let rows = lists.map { list in
            OptionRow(
                id: list.id, name: list.name, line: task.list == list.id ? "This task is on it." : nil, selected: task.list == list.id,
                suggested: task.list == nil && suggestion?.list == list.id, reason: suggestion?.list == list.id ? suggestion?.reason : nil,
                icon: .tile(tile(list)), command: .setProject(task: task.id, project: list.id))
        }
        let current = day.list(task.list)
        let suggested = current == nil ? day.list(suggestion?.list) : nil
        return Door(
            id: "project", label: "Project", value: current?.name ?? suggested?.name ?? "None yet",
            icon: (current ?? suggested).map { .tile(tile($0, suggested: current == nil)) }, pills: suggested == nil ? [] : [.suggested],
            options: OptionList(.round, rows))
    }

    // MARK: The assistant's box

    /// - Parameter label: how the thing is named after "about": a
    ///   project's, a person's or a meeting's own name, "this task" for a task.
    func mossBox(_ subject: MossSubject, about label: String, asks: [String]) -> MossBox {
        MossBox(
            subject: subject, title: "Moss, about \(label)",
            asks: day.reachable ? asks.map { ButtonPresentation($0, glyph: .moss, role: .secondary, command: .ask(subject, $0)) } : [],
            exchanges: day.exchanges.filter { $0.subject == subject }.map(answerCard), placeholder: "Ask or tell Moss about \(label)",
            unreachable: day.reachable ? nil : "Moss cannot be reached right now.")
    }

    func answerCard(_ exchange: Exchange) -> AnswerCard {
        let button: ButtonPresentation? =
            switch exchange.state {
            case .waiting, .writing: ButtonPresentation("Stop", role: .quiet, command: .stopAnswer)
            case .done, .failed: ButtonPresentation("Close", role: .quiet, command: .closeAnswer(exchange.id))
            }
        return AnswerCard(
            id: exchange.id, heading: exchange.question.hasSuffix("?") ? "You asked Moss" : "You said to Moss", question: exchange.question,
            text: exchange.text, state: exchange.state, button: button)
    }

    // MARK: A task, open

    /// Who set urgent, as the start of a sentence.
    func setter(_ task: TaskItem) -> String {
        switch task.setBy {
        case .assistant: "Moss"
        case .automation: "An automation"
        case .api: "The API"
        case .owner: "You, in another app"
        }
    }

    /// - Parameter reasonSaid: the line above the block already says why the
    ///   assistant thinks it can wait, so its option does not say it again.
    func markedBlock(_ task: TaskItem, reasonSaid: Bool = false) -> MarkedBlock {
        let unchecked = task.heldAt == nil && task.checkedAt == nil
        let chosen = day.notes.urgentLeft[task.id]
        var rows = [
            OptionRow(
                id: "takeNow", name: "Take it now",
                line: "It becomes what cannot wait: it leads, and what you are doing waits where you left it.",
                command: .urgent(task: task.id, .takeNow)),
            OptionRow(
                id: "leave", name: "Leave it here", line: "It stays in what is happening with the urgent pill. Nothing is parked.",
                selected: chosen != nil, suggested: !unchecked && day.reachable, reason: unchecked || reasonSaid ? nil : task.heldReason,
                command: .urgent(task: task.id, .leave)),
        ]
        rows.append(
            OptionRow(
                id: "notUrgent", name: "Not urgent",
                line: "The urgent pill comes off" + (task.was.map { " and it is flagged \($0.word) again" } ?? "") + ".",
                command: .urgent(task: task.id, .notUrgent)))
        return MarkedBlock(
            id: task.id, by: "\(setter(task)) marked it urgent at \(clock(task.updatedAt)).", options: OptionList(.round, rows),
            chosen: chosen.map { "Chosen at \(clock($0))." })
    }

    func taskChips(_ task: TaskItem, withProject: Bool = true) -> Chips {
        var pills: [Pill] = []
        if task.urgent, task.isOpen {
            pills.append(.urgent)
            pills.append(Pill(.neutral, setter(task)))
        }
        if let due = task.dueDay, due < day.today, task.isOpen {
            pills.append(Pill(.caution, "Due \(Words.day(due.start(in: calendar, hour: 12), now: now, calendar))"))
        }
        if let moved = task.notBefore, moved > now, isToday(moved) { pills.append(Pill(.neutral, "Moved: \(clock(moved))")) }
        var project = withProject ? chip(task.list) : nil
        if project == nil, withProject, let suggested = listSuggestion(task.path)?.list {
            project = chip(suggested, suggested: true)
            if project != nil { pills.append(.suggested) }
        }
        return Chips(project: project, people: people(task), pills: pills)
    }

    /// Why a task matters now, where a record says so: its reason, when it
    /// was typed, when it is due.
    func reason(_ task: TaskItem) -> String? {
        if let reason = task.reason { return reason }
        // A task still on its way to the server has no stamp of its own yet.
        if day.notes.added.contains(task.id) { return "You added it at \(clock(task.createdAt == .distantPast ? now : task.createdAt))." }
        if let due = task.dueAt, isToday(due) { return "Due at \(clock(due))." }
        if let due = task.dueDay {
            return due == day.today ? "Due today." : "Due \(Words.day(due.start(in: calendar, hour: 12), now: now, calendar))."
        }
        return nil
    }

    /// A task's one line in a row: why it matters now, else the opening
    /// words of its description.
    func why(_ task: TaskItem) -> String? { reason(task) ?? task.details.map { Words.excerpt($0, limit: Words.lineExcerptLimit) } }

    /// - Parameter whole: on the task's own page, which shows all of its
    ///   description. In the day a card shows its opening words, and none
    ///   where the row's line above it is those words already.
    func taskCard(_ task: TaskItem, whole: Bool = false) -> TaskCard {
        var doors: [Door] = []
        if task.isOpen {
            doors.append(flagDoor(task))
            if let project = projectDoor(task) { doors.append(project) }
            doors.append(whenDoor(task))
        }
        let left = day.returns[task.path].map { TaskCard.Left(heading: "Where you left it, \(clock($0.at))", text: $0.text) }
        let held = urgentState(task) == .held || urgentState(task) == .unchecked(interrupts: false)
        return TaskCard(
            id: task.id, chips: taskChips(task),
            quote: whole ? task.details : reason(task) == nil ? nil : task.details.map { Words.excerpt($0) }, left: left,
            deferred: task.deferrals >= 3 ? "Put off \(Words.low(Words.num(task.deferrals))) times." : nil,
            markedTitle: held ? "Marked urgent, and waiting" : nil, marked: held ? markedBlock(task) : nil, doors: doors,
            done: task.isOpen ? ButtonPresentation("Done", glyph: .done, role: .primary, command: .done(task.id)) : nil,
            said: task.status == .done ? (task.completedAt.map { "Done at \(clock($0))." } ?? "Done.") : nil,
            moss: mossBox(.task(task.id), about: "this task", asks: ["Break it into steps", "Who else could take this?"]))
    }

    func taskRow(_ task: TaskItem, state: Row.State) -> Row {
        Row(
            id: "task:" + task.id, glyph: .task, title: task.name, line: why(task), stripe: stripe(task), state: state, chips: taskChips(task),
            trailing: .flag(flagControl(task)), body: .task(taskCard(task)))
    }

    // MARK: What cannot wait

    func urgentCard(_ task: TaskItem, parked: String?) -> UrgentCard {
        let unchecked = task.setBy != .assistant && task.setBy != .owner && task.checkedAt == nil && day.notes.urgentTaken[task.id] == nil
        var voices: [UrgentCard.Voice] = []
        if task.setBy != .assistant, task.checkedAt != nil { voices.append(.init(who: "Moss", says: "agrees it cannot wait.")) }
        if let taken = day.notes.urgentTaken[task.id] { voices.append(.init(who: "You", says: "took it at \(clock(taken)).")) }
        let rows = [
            OptionRow(
                id: "leave", name: "Not now", line: "It waits in what is happening with the urgent pill, and your task leads again.",
                command: .urgent(task: task.id, .leave)),
            OptionRow(
                id: "notUrgent", name: "Not urgent",
                line: "The urgent pill comes off" + (task.was.map { " and it is flagged \($0.word) again" } ?? "") + ".",
                command: .urgent(task: task.id, .notUrgent)),
        ]
        return UrgentCard(
            id: task.id, setBy: "\(setter(task)) marked it urgent at \(clock(task.updatedAt)).",
            text: task.reason ?? task.details.map { Words.excerpt($0) }, voices: voices,
            unchecked: unchecked ? (day.reachable ? "Nobody has checked this yet." : "Moss cannot be reached, so nobody has checked this.") : nil,
            took: parked.map { "It takes the place of \(Words.low($0)), which waits where you left it." } ?? "It takes the place of what you were doing.",
            options: OptionList(.round, rows), project: chip(task.list),
            done: ButtonPresentation("Done", glyph: .done, role: .primary, command: .done(task.id)),
            moss: day.reachable ? mossBox(.task(task.id), about: "this task", asks: ["What do I need to know?", "Who else could take this?"]) : nil)
    }

    func urgentRow(_ task: TaskItem, parked: String?) -> Row {
        Row(
            id: "urgent:" + task.id, glyph: .urgent, title: task.name, line: nil, stripe: Stripe(task.was), state: .now,
            body: .urgent(urgentCard(task, parked: parked)))
    }
}
