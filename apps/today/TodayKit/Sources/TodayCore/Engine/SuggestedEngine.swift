import Foundation
import SubstrateCore

// Suggested tasks: a `tasks/task` in `proposed`. Three options, always:
// make it a task, already done, dismiss. Above the Save the assistant's
// answer is selected; nothing takes effect until Save.

extension DayEngine {
    static let fadeRule = "Not answered, a suggestion fades after a week and is gone after two. What was said stays in memory."

    /// How far a suggestion has faded, by its age in days.
    func fade(_ task: TaskItem) -> SuggestedItem.Fade {
        let age = max(0, calendar.dateComponents([.day], from: calendar.startOfDay(for: task.createdAt), to: day.startOfToday).day ?? 0)
        if age >= 14 { return .init(level: 3, label: (age == 14 ? "Two weeks old today." : "Past two weeks.") + " It is gone tonight unless you answer.") }
        if age >= 7 {
            let gone = calendar.date(byAdding: .day, value: 14 - age, to: day.startOfToday) ?? now
            return .init(level: 2, label: "Fading. Gone on \(Words.weekday(gone, calendar)).")
        }
        if age == 6 { return .init(level: 1, label: "Starts to fade tomorrow.") }
        return .init(level: 0, label: age == 0 ? "New today." : age == 1 ? "Since yesterday." : "\(Words.num(age)) days old. It fades after a week.")
    }

    /// Where a suggestion came from, in one phrase.
    func origin(_ task: TaskItem) -> String {
        var text = "Heard \(Words.when(task.createdAt, now: now, calendar))"
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

    /// What Save would apply to one suggestion: the owner's pick, else the
    /// assistant's answer; nil leaves it as it is.
    func pending(_ task: TaskItem) -> SuggestedAnswer? { day.drafts.suggested[task.id] ?? day.advice[task.id]?.answer }

    /// - Parameters:
    ///   - own: drawn alone, with its own Save.
    ///   - whole: on the task's own page, which shows all of what was said.
    func suggestedItem(_ task: TaskItem, own: Bool, whole: Bool = false) -> SuggestedItem {
        let advice = day.advice[task.id]
        let pick = day.drafts.suggested[task.id]
        let selected = pick ?? advice?.answer
        let lines: [SuggestedAnswer: String] = [
            .task: taskEffect(task), .done: "Closed. Nothing more is asked.", .dismiss: "Not a task. What was said stays in memory.",
        ]
        let names: [SuggestedAnswer: String] = [.task: "Make it a task", .done: "Already done", .dismiss: "Dismiss"]
        let options = OptionList(
            .round,
            SuggestedAnswer.allCases.map { answer in
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
            id: task.id, title: task.name, origin: origin(task), quote: whole ? task.details : task.details.map { Words.excerpt($0) }, fade: fade(task),
            chips: Chips(project: chip(task.list), people: people(task)), stripe: stripe(task), options: options, objection: objection,
            save: own
                ? SaveRow(button: ButtonPresentation("Save", glyph: .done, role: .primary, enabled: todo, command: .saveSuggested(task.id)))
                : nil,
            pending: todo)
    }

    /// "Twelve made tasks, one already done and one dismissed."
    func savedLine(_ counts: [Int]) -> String {
        let (made, done, dismissed) = (counts.first ?? 0, counts.dropFirst().first ?? 0, counts.dropFirst(2).first ?? 0)
        var parts: [String] = []
        if made > 0 { parts.append("\(Words.low(Words.num(made))) made \(made == 1 ? "a task" : "tasks")") }
        if done > 0 { parts.append("\(Words.low(Words.num(done))) already done") }
        if dismissed > 0 { parts.append("\(Words.low(Words.num(dismissed))) dismissed") }
        return parts.isEmpty ? "Nothing was changed." : Words.cap(Words.list(parts)) + "."
    }

    /// The card draws twelve at a time, and its Save answers the ones it
    /// draws and no other: nothing the owner has not seen is answered.
    var suggestedCard: SuggestedCard {
        let items = shownSuggested.map { suggestedItem($0, own: false) }
        let todo = items.filter(\.pending).count
        return SuggestedCard(
            rule: DayEngine.fadeRule,
            batch: morningSuggested.count > items.count
                ? "Showing \(Words.low(Words.num(items.count))) at a time. The next take their place when you save." : nil,
            items: items,
            save: SaveRow(
                button: ButtonPresentation(
                    "Save \(todo) \(todo == 1 ? "answer" : "answers")", glyph: .done, role: .primary, wide: true, enabled: todo > 0,
                    command: .saveSuggestedCard(items.map(\.id)))))
    }

    func suggestedRow(state: Row.State) -> Row? {
        let count = morningSuggested.count
        guard count > 0 else { return nil }
        return Row(
            id: "suggested", glyph: .moss, title: "\(Words.things(count, "suggested task")) to answer",
            line: "From what you said and what people asked of you. Made a task, each keeps its flag and its project.", state: state,
            body: .suggested(suggestedCard))
    }

    func heardRow(_ task: TaskItem, state: Row.State) -> Row {
        let item = suggestedItem(task, own: true)
        return Row(
            id: "heard:" + task.id, glyph: .task, title: task.name, line: item.origin, stripe: item.stripe, state: state, chips: item.chips,
            body: .heard(item))
    }
}
