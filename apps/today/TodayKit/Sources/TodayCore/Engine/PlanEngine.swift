import Foundation
import SubstrateCore

// The plan: a page, and a draft until it is saved. Everything on the table
// today, grouped by project. What the assistant suggests for today starts
// ticked; a message is never ticked in advance.

extension DayEngine {
    /// Everything that could be for today: every open task not hidden past
    /// today, and every suggested task not answered.
    var candidates: [TaskItem] { day.tasks.filter { ($0.isOpen || $0.isProposed) && !hidden($0) } }

    /// Whether the assistant suggests a thing for today. Where it prepared
    /// the day (`today/plan.picked` with no `savedAt`), that; else what is
    /// due, what is flagged High and what is urgent.
    func suggestedForToday(_ task: TaskItem) -> Bool {
        if let plan = day.plan, plan.savedAt == nil, plan.preparedAt != nil { return plan.picked.contains(task.path) }
        if task.urgent || task.shownFlag == .high { return true }
        if let due = task.dueDay, due <= day.today { return true }
        if let due = task.dueAt, due < day.startOfTomorrow { return true }
        return day.notes.added.contains(task.id)
    }

    /// Whether a thing is ticked on the plan page: the owner's draft, else
    /// what was saved, else what the assistant suggests.
    func ticked(_ task: TaskItem) -> Bool {
        if let pick = day.drafts.plan?.picked[task.id] { return pick }
        if planSaved { return day.plan?.picked.contains(task.path) ?? false }
        return suggestedForToday(task)
    }

    func planFlag(_ task: TaskItem) -> Flag? { day.drafts.plan?.flags[task.id] ?? task.shownFlag }

    func planItem(_ task: TaskItem) -> PlanPage.Item {
        let own = day.drafts.plan?.flags[task.id]
        let flag = planFlag(task)
        let suggestedFlag = own == nil && task.isProposed
        var line: String?
        if task.isProposed { line = "A suggested task: ticked, it is a task." }
        // Above the Save, the flag it has is the one selected until the owner picks another.
        let control = FlagControl(
            stripe: Stripe(flag, suggested: suggestedFlag), word: flag?.word ?? "No flag yet",
            options: flagOptions(selected: flag, suggested: task.isProposed ? task.flag : nil, reason: nil) { .planFlag(task.id, $0) })
        return PlanPage.Item(
            id: task.id, title: task.name, why: why(task), line: line, ticked: ticked(task), suggested: !planSaved && suggestedForToday(task),
            flag: control)
    }

    var planPage: PlanPage {
        let all = candidates
        let priorityIDs = priorities.map(\.id)
        let otherIDs = Set(all.compactMap(\.list)).subtracting(priorityIDs)
        let others = day.lists.filter { otherIDs.contains($0.id) }
        func group(_ list: ListItem?) -> PlanPage.Group {
            let mine = all.filter { $0.list == list?.id }.sorted { (rank(planFlag($0)), $0.createdAt) < (rank(planFlag($1)), $1.createdAt) }
            let items = mine.map(planItem)
            let first = items.filter(\.suggested), rest = items.filter { !$0.suggested }
            let count = items.filter(\.ticked).count
            let meetings = todaysMeetings.filter { $0.end > now }.filter { meeting in !Set(tasks(for: meeting).compactMap(\.list)).isDisjoint(with: [list?.id].compactMap { $0 }) }
            let restTicked = rest.filter(\.ticked).count
            return PlanPage.Group(
                id: list?.id ?? "none", tile: list.map { tile($0) }, name: list?.name ?? "On no project",
                count: count > 0 ? "\(count) today" : first.isEmpty ? "nothing today" : "\(first.count) suggested", stripe: Stripe(list?.flag),
                opens: list.map { .project($0.id) },
                meetings: meetings.isEmpty ? nil : "Meetings today: \(Words.list(meetings.map { "\($0.title) at \(clock($0.start))" })).",
                items: first, moreTitle: rest.isEmpty ? nil : "Not suggested for today (\(rest.count))" + (restTicked > 0 ? ", \(restTicked) ticked" : ""),
                more: rest, empty: items.isEmpty ? "Nothing of yours to pick here." : nil)
        }
        var groups = priorities.map { group($0) } + others.map { group($0) }
        if all.contains(where: { $0.list == nil || day.list($0.list) == nil }) { groups.append(group(nil)) }

        let picked = all.filter(ticked)
        let high = picked.filter { planFlag($0) == .high }.count
        let overlaps = overlaps.filter { ($0.meetings.map(\.end).max() ?? .distantPast) > now }
        let planOverlaps = overlaps.map { overlap in
            PlanPage.Overlap(
                id: overlap.id, name: name(overlap), text: text(overlap),
                options: options(overlap, selected: day.drafts.plan?.overlaps[overlap.id] ?? overlap.chosen) { .planOverlap(overlap.id, option: $0) })
        }
        let openOverlaps = overlaps.filter { (day.drafts.plan?.overlaps[$0.id] ?? $0.chosen) == nil }.count
        let sends = (day.plan?.sends ?? []).filter { $0.sentAt == nil }
        let sent = (day.plan?.sends ?? []).filter { $0.sentAt != nil }
        let sendRows = sends.map { send in
            OptionRow(
                id: send.id, name: "To \(send.toName ?? send.to.flatMap(day.person)?.short ?? "someone")",
                selected: day.drafts.plan?.sends[send.id] ?? false, quote: send.text, command: .planSend(send.id))
        }
        let going = sendRows.filter(\.selected).count
        let empty = priorities.filter { list in list.flag == .high && !picked.contains { $0.list == list.id } }
        var bar = picked.isEmpty ? "Nothing is ticked for today." : "Save \(picked.count) for today, \(high) of them High. "
        if !picked.isEmpty {
            bar += going > 0 ? "\(going) \(going == 1 ? "message goes" : "messages go")." : "No message goes."
            if openOverlaps > 0 { bar += " \(openOverlaps) \(openOverlaps == 1 ? "overlap is" : "overlaps are") left to choose later." }
        }
        return PlanPage(
            opening: shape, groups: groups, overlapsTitle: planOverlaps.isEmpty ? nil : "The \(Words.low(Words.things(planOverlaps.count, "overlap")))",
            overlaps: planOverlaps, sendsTitle: sendRows.isEmpty ? nil : "What Moss may send when you save",
            sendsHint: sendRows.isEmpty ? nil : "Moss sends only what you tick. Nothing is ticked for you.", sends: OptionList(.square, sendRows),
            sent: sent.map { "To \($0.toName ?? "someone"). Sent at \(clock($0.sentAt ?? now))." },
            note: picked.isEmpty || empty.isEmpty ? nil : empty.map { "\($0.name) is flagged High and has nothing ticked for today." }.joined(separator: " "),
            barLine: bar,
            save: ButtonPresentation("Save the plan", glyph: .done, role: .primary, wide: true, enabled: !picked.isEmpty, command: .planSave),
            reset: day.drafts.plan == nil ? nil : ButtonPresentation("Reset", role: .quiet, command: .planReset),
            saved: day.plan?.savedAt.map { "Plan saved at \(clock($0))." }, empty: all.isEmpty ? "Nothing is on the table today." : nil)
    }

    /// "Plan the day", in the day: one row that opens the page.
    func planRow(state: Row.State) -> Row? {
        guard !planSaved, !candidates.isEmpty || !overlaps.isEmpty else { return nil }
        let open = overlaps.filter { $0.chosen == nil }.count
        return Row(
            id: "plan", glyph: .plan, title: "Plan the day",
            line: "What is for today and how much each matters" + (open > 0 ? ", and \(Words.low(Words.things(open, "overlap"))) to choose." : "."),
            state: state,
            body: .plan(
                PlanRowCard(
                    line: "It opens as a page, and nothing changes until you save.",
                    open: ButtonPresentation("Open the plan", role: .primary, wide: true, opens: .plan))))
    }

    // MARK: The end of the day

    /// What carries over: today's tasks still open.
    var carrying: [TaskItem] { todaysTasks.filter { !$0.urgent } }

    static let carryOptions: [(id: String, name: String, line: String)] = [
        ("tomorrow", "Tomorrow", "It carries to tomorrow."), ("nextweek", "Next week", "It leaves this week and comes back on Monday."),
        ("drop", "Drop it", "Closed as not done."),
    ]

    var wrapCard: WrapCard {
        let doneToday = day.tasks.filter { $0.status == .done && ($0.completedAt.map(isToday) ?? false) }
        let high = doneToday.filter { $0.flag == .high } + carrying.filter { $0.shownFlag == .high }
        let others = doneToday.filter { $0.flag != .high }
        let saved = day.plan?.wrapSavedAt
        let carry = carrying.map { task -> WrapCard.Carry in
            let pick = day.drafts.carry[task.id] ?? day.notes.carried[task.id] ?? "tomorrow"
            return WrapCard.Carry(
                id: task.id, title: task.name, why: why(task) ?? "It was not done today.",
                options: OptionList(
                    .round,
                    DayEngine.carryOptions.map { option in
                        OptionRow(
                            id: option.id, name: option.name, line: option.line, selected: pick == option.id, suggested: option.id == "tomorrow",
                            command: .carry(task: task.id, option: option.id))
                    }))
        }
        let unanswered = proposed.map { suggestedItem($0, own: true) }
        let first = day.tasks.filter { $0.isOpen && ($0.dueDay == day.today.adding(days: 1, in: calendar) || ($0.notBefore.map { $0 >= day.startOfTomorrow && $0 < day.startOfTomorrow.addingTimeInterval(86400) } ?? false)) }
        let meetingsTomorrow = day.meetings.filter { $0.start >= day.startOfTomorrow && $0.start < day.startOfTomorrow.addingTimeInterval(86400) }.sorted { $0.start < $1.start }
        var tomorrow: [String] = []
        if let meeting = meetingsTomorrow.first { tomorrow.append("\(meeting.title) at \(clock(meeting.start))") }
        tomorrow += first.prefix(3).map(\.name)
        return WrapCard(
            done: high.map { task in
                WrapCard.Done(
                    id: task.id, title: task.name, time: task.completedAt.map(clock) ?? "open", done: task.status == .done, stripe: Stripe(task.shownFlag))
            },
            others: others.isEmpty ? nil : "\(Words.things(others.count, "other")): \(Words.list(others.map { Words.low($0.name) })).",
            suggested: day.notes.suggestedSavedAt == nil ? nil : savedLine(day.notes.suggestedSaved),
            carry: carry, unanswered: unanswered, tomorrow: tomorrow.isEmpty ? nil : "First tomorrow: \(Words.list(tomorrow)).",
            save: SaveRow(
                button: saved == nil || !day.drafts.carry.isEmpty
                    ? ButtonPresentation(
                        carry.isEmpty ? "Close the day" : "Save and close the day", glyph: .done, role: .primary, wide: true, command: .saveWrap)
                    : nil,
                saved: saved.map { "Day closed at \(clock($0))." }))
    }

    var wrapLine: String {
        let doneToday = day.tasks.filter { $0.status == .done && ($0.completedAt.map(isToday) ?? false) }
        let carry = carrying.count
        return "\(Words.things(doneToday.count, "thing")) done. "
            + (carry > 0 ? "\(Words.num(carry)) \(carry == 1 ? "thing carries" : "things carry") over." : "Nothing carries over.")
    }

    /// - Parameter closing: the day is closing, so the row says how it stands.
    func wrapRow(state: Row.State, closing: Bool = false) -> Row? {
        guard day.plan?.wrapSavedAt == nil else { return nil }
        return Row(
            id: "wrap", glyph: .night, title: "Close the day",
            line: closing ? wrapLine : "What got done, what carries over, and what is first tomorrow.", state: state, body: .wrap(wrapCard))
    }
}
