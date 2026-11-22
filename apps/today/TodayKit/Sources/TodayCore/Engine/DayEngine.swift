import Foundation
import SubstrateCore

/// The order of the day, and every rule of the design, as pure functions of
/// a `Day`: nothing here reads a clock, a device or the network, so a test
/// pins each rule by value and a fixture is the engine run at a fixed
/// instant.
public struct DayEngine: Sendable {
    public let day: Day
    /// Each project's initials, by the list's id: no two alike.
    let initials: [String: String]

    public init(_ day: Day) {
        self.day = day
        initials = DayEngine.initials(day.lists)
    }

    /// The initials each project is drawn by. Two projects never share
    /// them: the flagged ones choose first, in flag order, then the rest by
    /// name, and one whose initials are taken is drawn by the first two
    /// letters of its first word.
    static func initials(_ lists: [ListItem]) -> [String: String] {
        let order = lists.sorted {
            (!$0.active ? 1 : 0, $0.flag?.rank ?? 3, $0.name.lowercased(), $0.id) < (!$1.active ? 1 : 0, $1.flag?.rank ?? 3, $1.name.lowercased(), $1.id)
        }
        var taken: Set<String> = []
        var out: [String: String] = [:]
        for list in order {
            var mine = Words.initials(list.name, last: false)
            if taken.contains(mine) {
                let other = Words.firstTwo(list.name)
                if !taken.contains(other) { mine = other }
            }
            taken.insert(mine)
            out[list.id] = mine
        }
        return out
    }

    var now: Date { day.now }
    var calendar: Calendar { day.calendar }

    // MARK: Marks

    func tile(_ list: ListItem, suggested: Bool = false) -> Tile {
        Tile(initials: initials[list.id] ?? Words.initials(list.name, last: false), hue: Words.hue(list.id), suggested: suggested)
    }

    func chip(_ listID: String?, suggested: Bool = false) -> ProjectChip? {
        guard let list = day.list(listID) else { return nil }
        return ProjectChip(id: list.id, name: list.name, tile: tile(list, suggested: suggested), suggested: suggested)
    }

    func clock(_ date: Date) -> String { Words.clock(date, calendar) }

    func isToday(_ date: Date) -> Bool { date >= day.startOfToday && date < day.startOfTomorrow }

    /// The people a task involves, as chips: "for Tomas".
    func people(_ task: TaskItem) -> [PersonChip] {
        task.people.compactMap { path in day.person(path).map { PersonChip(id: $0.path, label: "for \($0.short)", badge: $0.badge) } }
    }

    // MARK: Suggestions on what exists

    /// The open request that suggests a flag for a record.
    func flagSuggestion(_ path: String) -> Suggestion? { day.suggestions.first { $0.target == path && $0.flag != nil } }

    /// The open request that suggests a project for a task.
    func listSuggestion(_ path: String) -> Suggestion? { day.suggestions.first { $0.target == path && $0.list != nil } }

    // MARK: Flags

    static let flagLines: [Flag: String] = [
        .high: "First in the order of the day.", .medium: "After what is flagged High.", .low: "Last, after what is flagged Medium.",
    ]
    static let priorityLines: [Flag: String] = [
        .high: "First in what is happening, and in what Moss suggests.", .medium: "After what is flagged High.",
        .low: "Last, after what is flagged Medium.",
    ]
    static let urgentRule =
        "Urgent is above High. Only Moss, an automation or the API can set it. You can take it off, and you cannot put it on."

    /// The three options of a flag. `selected` is drawn filled; `suggested`
    /// carries the pill and `reason`. Urgent is never one of them.
    func flagOptions(
        selected: Flag?, suggested: Flag?, reason: String?, lines: [Flag: String] = DayEngine.flagLines, command: (Flag) -> TodayCommand
    ) -> OptionList {
        OptionList(
            .round,
            Flag.allCases.map { flag in
                OptionRow(
                    id: flag.rawValue, name: flag.word, line: lines[flag], selected: selected == flag, suggested: suggested == flag,
                    reason: suggested == flag ? reason : nil, stripe: Stripe(flag), command: command(flag))
            })
    }

    /// The flag a task is ordered by: while its flag list is open, the one
    /// it had when the list was opened.
    func orderFlag(_ task: TaskItem) -> Flag? {
        if day.drafts.heldNone.contains(task.id) { return nil }
        return day.drafts.held[task.id] ?? task.shownFlag
    }

    func rank(_ flag: Flag?) -> Int { flag?.rank ?? 3 }

    /// A task's stripe: its flag, hollow while it is only what the
    /// assistant suggests (a suggested task's, or an open request's).
    func stripe(_ task: TaskItem) -> Stripe {
        if let flag = task.shownFlag { return Stripe(flag, suggested: task.isProposed) }
        if let suggested = flagSuggestion(task.path)?.flag { return Stripe(suggested, suggested: true) }
        return .none
    }

    func flagControl(_ task: TaskItem) -> FlagControl {
        let suggestion = flagSuggestion(task.path)
        var notes: [String] = []
        if task.urgent { notes.append(DayEngine.urgentRule + " Choosing a flag here takes urgent off.") }
        if day.drafts.held[task.id] != nil || day.drafts.heldNone.contains(task.id) { notes.append("The order follows when you close this.") }
        let own = task.urgent || task.isProposed ? nil : task.flag
        return FlagControl(
            stripe: stripe(task), word: task.urgent ? "Urgent" : task.shownFlag?.word ?? "No flag yet",
            options: flagOptions(selected: own, suggested: suggestion?.flag ?? (task.isProposed ? task.flag : nil), reason: suggestion?.reason) {
                .setFlag(task: task.id, $0)
            }, notes: notes)
    }

    // MARK: The priorities

    /// The projects the owner has flagged, in flag order, then by name.
    var priorities: [ListItem] {
        day.lists.filter { $0.active && $0.flag != nil }.sorted { (rank($0.flag), $0.name.lowercased()) < (rank($1.flag), $1.name.lowercased()) }
    }

    /// A project's place among the priorities, for sorting; unflagged last.
    func priorityOrder(_ listID: String?) -> Int { priorities.firstIndex { $0.id == listID } ?? Int.max }

    // MARK: Which tasks are today's

    /// Whether the plan of today was saved.
    var planSaved: Bool { day.plan?.savedAt != nil }

    /// A task hidden until later: its `notBefore` is after today.
    func hidden(_ task: TaskItem) -> Bool { (task.notBefore ?? .distantPast) >= day.startOfTomorrow }

    /// The tasks in the order of the day: what the saved plan ticked, what
    /// is due today, what the owner typed today, what was moved to today,
    /// and anything urgent. Before a plan is saved, what is due and what was
    /// typed.
    var todaysTasks: [TaskItem] {
        let picked = Set(planSaved ? day.plan?.picked ?? [] : [])
        let added = Set(day.notes.added)
        return day.tasks.filter { task in
            guard task.isOpen, !hidden(task) else { return false }
            if picked.contains(task.path) || added.contains(task.id) || task.urgent { return true }
            if let due = task.dueDay, due == day.today { return true }
            if let due = task.dueAt, isToday(due) { return true }
            if let moved = task.notBefore, isToday(moved) { return true }
            return false
        }
    }

    /// The suggested tasks still waiting for an answer. One the owner
    /// answered in words is with the assistant, and is not asked again.
    var proposed: [TaskItem] { day.tasks.filter { $0.isProposed && correction(of: $0) == nil } }

    /// While more than this many wait, a new suggestion is not a row of
    /// its own: every one is answered on the page.
    static let heardLimit = 12

    var suggestedCrowded: Bool { proposed.count > DayEngine.heardLimit }

    /// The ones the day's row counts: every one waiting, until a group of
    /// the page is saved; after that, a new one is a row of its own while
    /// few wait.
    var morningSuggested: [TaskItem] {
        guard let saved = day.notes.suggestedSavedAt, !suggestedCrowded else { return proposed }
        return proposed.filter { $0.createdAt <= saved }
    }

    /// The ones that arrived since, each a row of its own with its own
    /// Save; and one of them answered in words, while that exchange is here.
    var heard: [TaskItem] {
        guard let saved = day.notes.suggestedSavedAt, !suggestedCrowded else { return [] }
        return day.tasks.filter { $0.isProposed && $0.createdAt > saved && (correction(of: $0) == nil || toldItem($0) != nil) }
    }

    // MARK: Urgent

    enum UrgentState: Equatable {
        /// It interrupts.
        case checked
        /// It waits in what is happening.
        case held
        /// Nobody checked it: it waits, then interrupts.
        case unchecked(interrupts: Bool)
    }

    func urgentState(_ task: TaskItem) -> UrgentState? {
        guard task.urgent, task.isOpen else { return nil }
        if day.notes.urgentTaken[task.id] != nil { return .checked }
        if day.notes.urgentLeft[task.id] != nil { return .held }
        if task.setBy == .assistant || task.setBy == .owner || task.checkedAt != nil { return .checked }
        if task.heldAt != nil { return .held }
        return .unchecked(interrupts: now.timeIntervalSince(task.updatedAt) >= day.preference.urgentWait)
    }

    /// The urgent things that lead the pane now.
    var interrupting: [TaskItem] {
        day.tasks.filter { task in
            switch urgentState(task) {
            case .checked?, .unchecked(interrupts: true)?: true
            default: false
            }
        }.sorted { $0.updatedAt < $1.updatedAt }
    }

    /// The urgent things that wait in what is happening.
    var waitingUrgent: [TaskItem] {
        day.tasks.filter { task in
            switch urgentState(task) {
            case .held?, .unchecked(interrupts: false)?: true
            default: false
            }
        }
    }

    /// The next instant the pane changes without a record changing: a
    /// minute boundary while something counts down, or the moment an
    /// unchecked urgent mark starts to interrupt.
    public var nextChange: Date {
        var next = calendar.date(byAdding: .minute, value: 1, to: calendar.date(bySetting: .second, value: 0, of: now) ?? now) ?? now.addingTimeInterval(60)
        if next <= now { next = now.addingTimeInterval(60) }
        for task in day.tasks {
            if case .unchecked(interrupts: false)? = urgentState(task) {
                next = min(next, task.updatedAt.addingTimeInterval(day.preference.urgentWait))
            }
        }
        return next
    }
}
