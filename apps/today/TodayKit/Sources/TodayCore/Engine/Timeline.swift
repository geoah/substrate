import Foundation
import SubstrateCore

// The shape of the day: free stretches and runs of meetings, in order. A
// stretch has no header and no name on the screen; how long until the next
// fixed point is on that meeting's own row.

extension DayEngine {
    struct Run: Equatable {
        var from: Date
        var to: Date
        var meetings: [Meeting]
    }

    enum Phase: Equatable {
        case past, now, later
    }

    struct Stretch: Equatable {
        var index: Int
        var from: Date
        var to: Date
        var phase: Phase
        /// A fixed point is this close: the meeting leads and the rows wait.
        var near: Bool
        var next: Run?
    }

    /// Today's meetings, in order of their start.
    var todaysMeetings: [Meeting] {
        day.meetings.filter { $0.start < day.startOfTomorrow && $0.end > day.startOfToday }.sorted { ($0.start, $0.id) < ($1.start, $1.id) }
    }

    /// Meetings less than a quarter of an hour apart are one run.
    var runs: [Run] {
        var out: [Run] = []
        for meeting in todaysMeetings {
            if var last = out.last, meeting.start.timeIntervalSince(last.to) < 15 * 60 {
                last.meetings.append(meeting)
                last.to = max(last.to, meeting.end)
                out[out.count - 1] = last
            } else {
                out.append(Run(from: meeting.start, to: meeting.end, meetings: [meeting]))
            }
        }
        return out
    }

    func phase(_ from: Date, _ to: Date) -> Phase { to <= now ? .past : from <= now ? .now : .later }

    /// The free stretches: before the first run, between runs, and after
    /// the last one until the day ends. Always one more than the runs.
    var stretches: [Stretch] {
        let runs = runs
        var out: [Stretch] = []
        var cursor = day.startOfToday
        for (index, run) in runs.enumerated() {
            let phase = phase(cursor, run.from)
            out.append(
                Stretch(
                    index: index, from: cursor, to: run.from, phase: phase, near: phase == .now && run.from.timeIntervalSince(now) <= 10 * 60,
                    next: run))
            cursor = run.to
        }
        out.append(Stretch(index: runs.count, from: cursor, to: day.startOfTomorrow, phase: phase(cursor, day.startOfTomorrow), near: false, next: nil))
        return out
    }

    /// The stretch open rows are placed in when theirs has passed: the
    /// current one, or the one after the run the owner is in.
    var currentStretch: Int {
        let stretches = stretches
        if let current = stretches.first(where: { $0.phase == .now }) { return current.index }
        return stretches.first { $0.phase == .later }?.index ?? max(0, stretches.count - 1)
    }

    /// The stretch a moment falls in: its own, or the next when it is
    /// inside a run; never one that has passed.
    func stretch(at moment: Date) -> Int {
        let stretches = stretches
        let own = stretches.first { moment < $0.to }?.index ?? stretches.count - 1
        return max(own, currentStretch)
    }

    // MARK: Overlaps

    struct Overlap: Equatable {
        var id: String
        var meetings: [Meeting]
        /// The owner's choice: a meeting's path, or "both".
        var chosen: String?
        var chosenAt: Date?

        var first: Date { meetings.map(\.start).min() ?? .distantPast }
    }

    /// Meetings that share time, in pairs: each meeting with the next one
    /// that starts before it ends, and each meeting in one pair at most.
    /// Two that only touch do not overlap.
    var overlaps: [Overlap] {
        let meetings = todaysMeetings
        var paired: Set<String> = []
        var out: [Overlap] = []
        for (index, first) in meetings.enumerated() where !paired.contains(first.id) {
            guard let second = meetings[(index + 1)...].first(where: { !paired.contains($0.id) && $0.start < first.end && first.start < $0.end }) else {
                continue
            }
            paired.formUnion([first.id, second.id])
            let paths = [first.path, second.path]
            let record = day.plan?.overlaps.first { Set($0.events) == Set(paths) }
            out.append(Overlap(id: "ov-\(first.id)+\(second.id)", meetings: [first, second], chosen: record?.chosen, chosenAt: record?.at))
        }
        return out
    }

    func overlap(of meeting: Meeting) -> Overlap? { overlaps.first { $0.meetings.contains { $0.id == meeting.id } } }

    /// "The 15:00 overlap".
    func name(_ overlap: Overlap) -> String { "The \(clock(overlap.meetings.map(\.start).max() ?? overlap.first)) overlap" }

    func text(_ overlap: Overlap) -> String {
        let parts = overlap.meetings.map { "\($0.title) at \(clock($0.start))" }
        return Words.list(parts) + " share time."
    }

    /// An overlap's options: each meeting, and both where one starts after
    /// the other. `selected` is the owner's choice, or a draft's above the
    /// plan's Save. The assistant has no option here: nothing in the
    /// records says which it would choose.
    func options(_ overlap: Overlap, selected: String?, command: (String) -> TodayCommand) -> OptionList {
        var rows = overlap.meetings.map { meeting in
            let others = overlap.meetings.filter { $0.id != meeting.id }.map(\.title)
            return OptionRow(
                id: meeting.path, name: meeting.title, line: "You go to \(meeting.title). \(Words.list(others)) \(others.count == 1 ? "is" : "are") left.",
                selected: selected == meeting.path, command: command(meeting.path))
        }
        if overlap.meetings.count == 2, overlap.meetings[0].start < overlap.meetings[1].start {
            let (first, second) = (overlap.meetings[0], overlap.meetings[1])
            rows.append(
                OptionRow(
                    id: "both", name: "Both: \(first.title) until \(clock(second.start)), then \(second.title)",
                    line: "No message. \(second.title) starts when you leave \(first.title).", selected: selected == "both",
                    command: command("both")))
        }
        return OptionList(.round, rows)
    }

    func card(_ overlap: Overlap) -> OverlapCard {
        OverlapCard(
            id: overlap.id, name: name(overlap), text: text(overlap),
            pill: overlap.chosen == nil ? Pill(.caution, "To choose") : Pill(.neutral, "Chosen"),
            options: options(overlap, selected: overlap.chosen) { .decideOverlap(overlap.id, option: $0) },
            chosen: overlap.chosen.flatMap { _ in overlap.chosenAt.map { "Chosen at \(clock($0)). No message is needed." } })
    }

    /// Whether the owner goes to a meeting of an overlap that is chosen.
    func attends(_ meeting: Meeting) -> Bool? {
        guard let overlap = overlap(of: meeting), let chosen = overlap.chosen else { return nil }
        return chosen == "both" || chosen == meeting.path
    }

    // MARK: Meetings

    /// The next fixed point: the first meeting that has not started.
    var nextMeeting: Meeting? { todaysMeetings.first { $0.start > now && attends($0) != false } }

    func people(_ meeting: Meeting) -> [(name: String, person: Person?)] {
        meeting.people.compactMap { attendee in
            if let path = attendee.contact, let person = day.person(path) { return (person.name, person) }
            guard !attendee.name.isEmpty else { return nil }
            // An attendee named as a contact the day already holds is that contact.
            let known = day.people.values.first { $0.name.caseInsensitiveCompare(attendee.name) == .orderedSame }
            return (attendee.name, known)
        }
    }

    func badge(name: String, person: Person?) -> Badge {
        person?.badge ?? Badge(initials: Words.initials(name, last: true), hue: Words.hue(name.lowercased()))
    }

    /// The tasks that involve the people in a meeting, and the ones its
    /// dossier names.
    func tasks(for meeting: Meeting) -> [TaskItem] {
        let paths = Set(people(meeting).compactMap { $0.person?.path })
        let named = Set(day.dossier(meeting)?.tasks ?? [])
        return day.tasks.filter { task in
            (task.isOpen || task.isProposed) && !hidden(task) && (named.contains(task.id) || !paths.isDisjoint(with: task.people))
        }
    }

    func taskLine(_ task: TaskItem) -> TaskLine {
        let line: String
        switch task.status {
        case .done: line = task.completedAt.map { "Done \(isToday($0) ? "at \(clock($0))" : Words.when($0, now: now, calendar))." } ?? "Done."
        case .abandoned: line = "Dropped."
        case .proposed: line = "A suggestion, not answered yet."
        case .open:
            if todaysTasks.contains(where: { $0.id == task.id }) {
                line = "Today."
            } else if let moved = task.notBefore, moved > now {
                line = "\(Words.cap(Words.day(moved, now: now, calendar)))."
            } else if let due = task.dueDay {
                line = "Due \(Words.day(due.start(in: calendar, hour: 12), now: now, calendar))."
            } else {
                line = "Not today."
            }
        }
        let pill: Pill? = task.urgent && task.isOpen ? .urgent : task.isProposed ? .suggested : nil
        return TaskLine(
            id: task.id, glyph: task.status == .done ? .done : .task, title: task.name, line: line, pill: pill, stripe: stripe(task),
            opens: .task(task.id))
    }

    func meetingCard(_ meeting: Meeting) -> MeetingCard {
        let dossier = day.dossier(meeting)
        let who = people(meeting)
        let tasks = tasks(for: meeting)
        let soon = meeting.start.timeIntervalSince(now) <= 10 * 60 && meeting.end > now
        var meta: [String] = []
        if let place = meeting.place { meta.append(place) }
        if meeting.count > 1 { meta.append("\(meeting.count) people") }
        let chips = Chips(
            people: who.compactMap { entry in entry.person.map { PersonChip(id: $0.path, label: $0.short, badge: $0.badge) } })
        var card = MeetingCard(
            meta: meta.isEmpty ? nil : Words.cap(meta.joined(separator: ", ")), chips: chips, purpose: dossier?.purpose,
            overlap: overlap(of: meeting).map(card),
            tasksTitle: tasks.isEmpty ? nil : "Your tasks with \(who.count == 1 ? (who[0].person?.short ?? who[0].name) : "the room")",
            tasks: tasks.map(taskLine), since: dossier?.since, raise: dossier?.raise ?? [],
            readFirst: (dossier?.readFirst ?? []).enumerated().map { index, path in
                // What is read first opens at its source where that is known, else as the record itself.
                MeetingCard.Reading(
                    id: "\(index)", title: RecordReference(path: path).map(fallbackName) ?? path, kind: nil,
                    url: day.sources[path]?.url ?? SourcePages.console(day.server, path: path))
            },
            join: meeting.join.flatMap { url in
                soon
                    ? ButtonPresentation("Join", glyph: .join, role: .primary, wide: true, url: url)
                    : ButtonPresentation("Join link", glyph: .join, role: .quiet, url: url)
            }, after: dossier?.after,
            moss: mossBox(.meeting(meeting.id), about: meeting.title, asks: ["Prepare me in three lines", "What is open with the people in it?"]))
        if let page = meeting.page, let url = URL(string: page), url.scheme?.hasPrefix("http") == true {
            let name = SourcePages.name(of: url)
            card.links = [SourceLink(id: "source", title: name, label: "Open in \(name)", url: page)]
        }
        return card
    }

    /// - Parameter tie: set where the meeting it shares time with is drawn
    ///   next to it.
    func meetingRow(_ meeting: Meeting, leads: Bool, tie: MeetingTie? = nil) -> MeetingRow {
        let who = people(meeting)
        let tasks = tasks(for: meeting)
        let attends = attends(meeting)
        var line = day.dossier(meeting)?.purpose
        if let first = tasks.first {
            line = (first.isProposed ? "Suggested: " : "Your task: ") + Words.low(first.name) + ". " + (taskLine(first).line ?? "")
        }
        // Hours ahead the time says enough: the count is for the hour before.
        let isNext = nextMeeting?.id == meeting.id && meeting.start.timeIntervalSince(now) <= 60 * 60
        // While the two wait for a choice they are their times and titles, and one line that asks.
        if tie?.toChoose == true { line = nil }
        var row = MeetingRow(
            id: meeting.id, start: clock(meeting.start), end: clock(meeting.end), title: meeting.title,
            people: who.prefix(3).map { badge(name: $0.name, person: $0.person) },
            attendance: attends.map { $0 ? "Going." : "Not going." }, line: attends == false ? nil : line,
            until: isNext ? Words.until(meeting.start, now: now) : nil, live: meeting.start <= now && meeting.end > now, leads: leads,
            off: attends == false, card: meetingCard(meeting))
        row.tie = tie
        return row
    }

    /// The shape of the day in real times, for the plan's opening line.
    var shape: String? {
        let coming = runs.filter { $0.to > now }
        guard let first = coming.first else { return todaysMeetings.isEmpty ? "No meetings today." : "No more meetings today." }
        let last = coming[coming.count - 1]
        var text = first.from <= now ? "In meetings now" : "Free until \(first.meetings[0].title) at \(clock(first.from))"
        text += coming.count == 1 && first.from > now ? ", which ends at \(clock(last.to))" : ", then meetings until \(clock(last.to))"
        let gaps = zip(coming, coming.dropFirst()).map { clock($0.0.to) }
        if !gaps.isEmpty { text += ", with \(gaps.count == 1 ? "a gap" : "gaps") at \(Words.list(gaps))" }
        return text + "."
    }
}
