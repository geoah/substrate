import Foundation
import SubstrateCore

// Suggested tasks: a `tasks/task` in `proposed`. Its options, always: make
// it a task, already done, dismiss, and the owner's own words ("Other",
// where the repository keeps them). Above a Save the assistant's answer is
// selected; nothing takes effect until Save. They are answered on a page
// of their own, grouped by project, each group with its own Save.

extension DayEngine {
    static let fadeRule = "Not answered, a suggestion fades after a week and is gone after two. What was said stays in memory."

    /// How many of a project's suggestions its group draws until it is
    /// asked for the rest.
    static let suggestedShown = 6

    /// When a suggestion was heard: when what it was made from was said,
    /// else when its record was written, which is all an older record says.
    func heard(_ task: TaskItem) -> Date { task.originAt ?? task.createdAt }

    /// How far a suggestion has faded, by its age in days: from when it
    /// was said, so one written late is as old as its words.
    func fade(_ task: TaskItem) -> SuggestedItem.Fade {
        let age = max(0, calendar.dateComponents([.day], from: calendar.startOfDay(for: heard(task)), to: day.startOfToday).day ?? 0)
        if age >= 14 { return .init(level: 3, label: (age == 14 ? "Two weeks old today." : "Past two weeks.") + " It is gone tonight unless you answer.") }
        if age >= 7 {
            let gone = calendar.date(byAdding: .day, value: 14 - age, to: day.startOfToday) ?? now
            return .init(level: 2, label: "Fading. Gone on \(Words.weekday(gone, calendar)).")
        }
        if age == 6 { return .init(level: 1, label: "Starts to fade tomorrow.") }
        return .init(level: 0, label: age == 0 ? "New today." : age == 1 ? "Since yesterday." : "\(Words.num(age)) days old. It fades after a week.")
    }

    /// Where a suggestion came from, in one phrase. "Heard" only where the
    /// record says when it was said: the time a suggestion was written is
    /// not when anyone said anything.
    func origin(_ task: TaskItem) -> String {
        var text = task.originAt.map { "Heard \(Words.when($0, now: now, calendar))" } ?? "Suggested \(Words.when(task.createdAt, now: now, calendar))"
        if let origin = task.origin, let reference = RecordReference(path: origin) {
            text += ", from \(article(kindDisplayName(reference.kind).lowercased()))"
        }
        if let person = task.people.first.flatMap(day.person) { text += ", with \(person.short)" }
        return text + "."
    }

    func article(_ noun: String) -> String {
        guard let first = noun.first else { return noun }
        return ("aeiou".contains(first) ? "an " : "a ") + noun
    }

    /// The line under "Make it a task".
    func taskEffect(_ task: TaskItem) -> String {
        var text = "A task"
        if let due = task.dueDay { text += " for \(Words.day(due.start(in: calendar, hour: 12), now: now, calendar))" }
        if let flag = task.flag { text += ", flagged \(flag.word)" }
        return text + "."
    }

    // MARK: Other: the owner's own words

    /// Whether the repository keeps what the owner says about a suggestion.
    var keepsCorrections: Bool { day.discovery.holds(TodayKinds.correction) }

    /// The owner's words about a suggested task, where they are newer than
    /// its last change: it is with the assistant and is not asked again.
    /// Once the task is changed (rewritten, say) it is a suggestion again.
    func correction(of task: TaskItem) -> Correction? {
        guard task.isProposed, let correction = day.corrections[task.path], correction.isAfter(task.updatedAt) else { return nil }
        return correction
    }

    /// A suggestion answered in words, drawn while its exchange is here:
    /// what was said and what the assistant answered, and no options.
    /// - Parameters:
    ///   - alone: on the task's own page, where it is drawn with or
    ///     without an exchange: the words were said either way.
    ///   - grouped: under its project's header, which names the project.
    func toldItem(_ task: TaskItem, alone: Bool = false, grouped: Bool = false) -> SuggestedItem? {
        guard let correction = correction(of: task) else { return nil }
        let exchange = day.exchanges.last { $0.subject == .task(task.id) }
        guard alone || exchange != nil else { return nil }
        return SuggestedItem(
            id: task.id, title: task.name, origin: origin(task), fade: fade(task),
            chips: Chips(project: grouped ? nil : chip(task.list), people: people(task)), stripe: stripe(task), options: .empty, pending: false,
            told: SuggestedItem.Told(words: correction.text, answer: exchange.map(answerCard)))
    }

    /// The owner's pick for a suggestion, as the draft has it. "Other" is
    /// no pick where the repository does not keep such words.
    func pick(_ task: TaskItem) -> SuggestedAnswer? {
        let pick = day.drafts.suggested[task.id]
        return pick == .other && !keepsCorrections ? nil : pick
    }

    /// What Save would apply to one suggestion: the owner's pick, else the
    /// assistant's answer; nil leaves it as it is.
    func pending(_ task: TaskItem) -> SuggestedAnswer? { pick(task) ?? day.advice[task.id]?.answer }

    /// - Parameters:
    ///   - own: drawn alone, with its own Save. Else it is in its project's
    ///     group on the page, whose header names the project and whose Save
    ///     is the group's.
    ///   - whole: on the task's own page, which shows all of what was said.
    func suggestedItem(_ task: TaskItem, own: Bool, whole: Bool = false) -> SuggestedItem {
        let advice = day.advice[task.id]
        let pick = pick(task)
        let selected = pick ?? advice?.answer
        let lines: [SuggestedAnswer: String] = [
            .task: taskEffect(task), .done: "Closed. Nothing more is asked.", .dismiss: "Not a task. What was said stays in memory.",
            .other: "Say what is wrong, or what to do instead.",
        ]
        let names: [SuggestedAnswer: String] = [.task: "Make it a task", .done: "Already done", .dismiss: "Dismiss", .other: "Other"]
        let answers = keepsCorrections ? SuggestedAnswer.allCases : SuggestedAnswer.advised
        let options = OptionList(
            .round,
            answers.map { answer in
                OptionRow(
                    id: answer.rawValue, name: names[answer] ?? answer.rawValue, line: lines[answer], selected: selected == answer,
                    suggested: advice?.answer == answer, reason: advice?.answer == answer ? advice?.reason : nil,
                    command: .suggest(task.id, answer))
            })
        // The assistant objects once, in place, when the owner picks Dismiss
        // on something it thinks matters.
        var objection: SuggestedItem.Objection?
        if pick == .dismiss, let advice, advice.answer != .dismiss, let line = advice.objection {
            objection = SuggestedItem.Objection(line: line, note: "It is said once. Dismiss stands if you save it.")
        }
        let todo = selected != nil
        return SuggestedItem(
            id: task.id, title: task.name, origin: origin(task), quote: task.details.map { whole ? Words.prose($0) : Words.excerpt($0) },
            fade: fade(task), chips: Chips(project: own ? chip(task.list) : nil, people: people(task)), stripe: stripe(task), options: options,
            other: selected == .other
                ? SuggestedItem.Other(
                    placeholder: "What is wrong, or what to do instead", note: "Kept for whoever suggests tasks, and sent to Moss with this one.")
                : nil,
            objection: objection,
            save: own
                ? SaveRow(
                    button: ButtonPresentation("Save", glyph: .done, role: .primary, enabled: todo, command: .saveSuggested([task.id], words: [:])))
                : nil,
            pending: todo)
    }

    /// A suggestion as a page's list draws it: as it was answered in words
    /// while that exchange is here, else with its options. Nil for one
    /// that is with the assistant and has nothing more to show.
    func listedSuggestion(_ task: TaskItem) -> SuggestedItem? {
        if correction(of: task) != nil { return toldItem(task) }
        return suggestedItem(task, own: true)
    }

    /// "Twelve made tasks, one already done, one dismissed and one told to Moss."
    func savedLine(_ counts: [Int]) -> String {
        func count(_ index: Int) -> Int { counts.indices.contains(index) ? counts[index] : 0 }
        let (made, done, dismissed, told) = (count(0), count(1), count(2), count(3))
        var parts: [String] = []
        if made > 0 { parts.append("\(Words.low(Words.num(made))) made \(made == 1 ? "a task" : "tasks")") }
        if done > 0 { parts.append("\(Words.low(Words.num(done))) already done") }
        if dismissed > 0 { parts.append("\(Words.low(Words.num(dismissed))) dismissed") }
        if told > 0 { parts.append("\(Words.low(Words.num(told))) told to Moss") }
        return parts.isEmpty ? "Nothing was changed." : Words.cap(Words.list(parts)) + "."
    }

    // MARK: The page

    /// A suggestion's place in its group: what the assistant advises making
    /// a task first, then what it has not looked at, then what it advises
    /// done or dismissed. A pick moves nothing, so a row never jumps under
    /// a tap.
    func suggestedRank(_ task: TaskItem) -> Int {
        switch day.advice[task.id]?.answer {
        case .task?: 0
        case nil: 1
        default: 2
        }
    }

    /// Every suggestion waiting for an answer, by project: the flagged
    /// projects first in flag order, then the others by name, then what
    /// is on no project. Within one, by `suggestedRank`, the newest first.
    var suggestedGroups: [(list: ListItem?, tasks: [TaskItem])] {
        let waiting = proposed
        var byList: [String: [TaskItem]] = [:]
        var none: [TaskItem] = []
        for task in waiting {
            if let list = day.list(task.list) { byList[list.id, default: []].append(task) } else { none.append(task) }
        }
        func ordered(_ tasks: [TaskItem]) -> [TaskItem] {
            tasks.sorted { (suggestedRank($0), heard($1), $1.id) < (suggestedRank($1), heard($0), $0.id) }
        }
        let flagged = priorities.filter { byList[$0.id] != nil }
        let others = day.lists.filter { list in byList[list.id] != nil && !flagged.contains { $0.id == list.id } }
        var out: [(list: ListItem?, tasks: [TaskItem])] = (flagged + others).map { ($0, ordered(byList[$0.id] ?? [])) }
        if !none.isEmpty { out.append((nil, ordered(none))) }
        return out
    }

    var suggestedPage: SuggestedPage {
        // What was answered in words stays under its project while its exchange is here.
        let told = day.tasks.compactMap { task in toldItem(task, grouped: true).map { (task, $0) } }
        var groups = suggestedGroups.map { group -> SuggestedPage.Group in
            let id = group.list?.id ?? "none"
            let all = day.drafts.suggestedShown.contains(id) || group.tasks.count <= DayEngine.suggestedShown
            let drawn = all ? group.tasks : Array(group.tasks.prefix(DayEngine.suggestedShown))
            let items = drawn.map { suggestedItem($0, own: false) }
            let todo = items.filter(\.pending).count
            let rest = group.tasks.count - drawn.count
            return SuggestedPage.Group(
                id: id, tile: group.list.map { tile($0) }, name: group.list?.name ?? "On no project", count: "\(group.tasks.count) suggested",
                stripe: Stripe(group.list?.flag), opens: group.list.map { .project($0.id) }, items: items,
                more: rest > 0 ? ButtonPresentation("Show the other \(rest)", role: .secondary, command: .showSuggested(group: id)) : nil,
                told: told.filter { (day.list($0.0.list)?.id ?? "none") == id }.map(\.1),
                save: SaveRow(
                    button: ButtonPresentation(
                        "Save \(todo) \(todo == 1 ? "answer" : "answers")", glyph: .done, role: .primary, wide: true, enabled: todo > 0,
                        command: .saveSuggested(drawn.map(\.id), words: [:]))))
        }
        // A project whose every suggestion was answered in words keeps its card while an exchange is here.
        for (task, item) in told where !groups.contains(where: { $0.told.contains { $0.id == item.id } }) {
            let list = day.list(task.list)
            let id = list?.id ?? "none"
            if let index = groups.firstIndex(where: { $0.id == id }) {
                groups[index].told.append(item)
            } else {
                groups.append(
                    SuggestedPage.Group(
                        id: id, tile: list.map { tile($0) }, name: list?.name ?? "On no project", count: "With Moss", stripe: Stripe(list?.flag),
                        opens: list.map { .project($0.id) }, items: [], told: [item]))
            }
        }
        let count = proposed.count
        return SuggestedPage(
            line: count == 0 ? nil : suggestedLine(proposed), rule: DayEngine.fadeRule, groups: groups,
            empty: groups.isEmpty ? "Nothing suggested is waiting for an answer." : nil)
    }

    /// What matters about the ones waiting, in one line: how many are on
    /// the owner's priorities, and how many the assistant would make tasks.
    func suggestedLine(_ waiting: [TaskItem]) -> String {
        let flagged = Set(priorities.map(\.id))
        let onPriorities = waiting.filter { $0.list.map(flagged.contains) ?? false }.count
        let tasks = waiting.filter { day.advice[$0.id]?.answer == .task }.count
        var line =
            flagged.isEmpty
            ? "Grouped by project." : onPriorities == 0 ? "None is on your priorities." : "\(Words.num(onPriorities)) on your priorities."
        if tasks > 0 { line += " Moss would make \(Words.low(Words.num(tasks))) \(tasks == 1 ? "a task" : "tasks")." }
        return line
    }

    // MARK: In the day

    /// The suggested tasks, in the day: one row that opens their page.
    func suggestedRow(state: Row.State) -> Row? {
        let waiting = morningSuggested
        guard !waiting.isEmpty else { return nil }
        return Row(
            id: "suggested", glyph: .moss, title: "\(Words.things(waiting.count, "suggested task")) to answer", line: suggestedLine(waiting), state: state,
            trailing: .page(.suggested),
            body: .suggested(
                PageRowCard(
                    line: "They open as a page, grouped by project. Each project has its own Save, and nothing changes until you press it.",
                    open: ButtonPresentation("Open the suggested tasks", role: .primary, wide: true, opens: .suggested))))
    }

    func heardRow(_ task: TaskItem, state: Row.State) -> Row {
        let item = toldItem(task) ?? suggestedItem(task, own: true)
        return Row(
            id: "heard:" + task.id, glyph: .task, title: task.name, line: item.origin, stripe: item.stripe, state: state, chips: item.chips,
            body: .heard(item))
    }
}
