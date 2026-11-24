import Foundation
import SubstrateCore

// The night and what is happening: both read the digests. The night is the
// items between the owner's last look of the day before and the first look
// today; what is happening is the items after that, new while they are past
// the project's review mark.

extension DayEngine {
    var wokeAt: Date { day.notes.wokeAt ?? now }
    var nightFrom: Date { day.notes.nightFrom ?? day.startOfToday.addingTimeInterval(-8 * 3600) }

    func name(of key: String) -> String { day.list(key)?.name ?? "On no project" }

    func nightItems(_ digest: Digest) -> [Digest.Item] { digest.items.filter { $0.at > nightFrom && $0.at <= wokeAt } }

    func dayItems(_ digest: Digest) -> [Digest.Item] { digest.items.filter { $0.at > wokeAt && $0.at <= now } }

    func isNew(_ item: Digest.Item, in digest: Digest) -> Bool { item.at > (day.marks[digest.key] ?? wokeAt) }

    /// When the night was read today.
    var nightReadAt: Date? {
        guard let read = day.preference.nightReadAt, read >= day.startOfToday else { return nil }
        return read
    }

    /// What a digest's item was written from: the first record it names,
    /// which is its thread's opener. Its links and its source are that
    /// record's.
    func origin(_ item: Digest.Item) -> String? { item.sources.first }

    func line(_ item: Digest.Item, id: String) -> TimelineLine {
        TimelineLine(id: id, time: clock(item.at), text: item.text, links: day.links(for: origin(item)))
    }

    func nightItem(_ digest: Digest) -> NightItem? {
        let items = nightItems(digest)
        guard !items.isEmpty else { return nil }
        let list = day.list(digest.key)
        let seen = (day.marks[digest.key] ?? .distantPast) >= (items.first?.at ?? .distantFuture) || nightReadAt != nil
        let mine = digest.needsOwner && day.notes.night[digest.key] == nil
        var decision: NightDecision?
        if digest.needsOwner {
            let chosen = day.notes.night[digest.key]
            decision = NightDecision(
                question: digest.needsOwnerReason ?? "This may be yours.",
                options: OptionList(
                    .round,
                    [
                        OptionRow(
                            id: "take", name: "Take it", line: "A task on \(list?.name ?? "no project"): \(items[0].text)",
                            selected: chosen == "take", command: .answerNight(project: digest.key, option: "take")),
                        OptionRow(
                            id: "no", name: "Not mine", line: "Nothing is made. It stays in the timeline as what happened.", selected: chosen == "no",
                            command: .answerNight(project: digest.key, option: "no")),
                    ]), chosen: chosen == nil ? nil : "Chosen.")
        }
        return NightItem(
            id: digest.key, tile: list.map { tile($0) }, name: name(of: digest.key), stripe: Stripe(list?.flag),
            count: seen ? "read" : "\(items.count) new", pill: mine ? Pill(.caution, "Yours?") : nil, text: digest.line ?? items[0].text,
            lines: items.enumerated().map { line($1, id: "\(digest.key)-\($0)") }, decision: decision,
            open: list.map { ButtonPresentation("Open the project", role: .quiet, opens: .project($0.id)) },
            gotIt: seen ? nil : ButtonPresentation("Got it", glyph: .done, role: .secondary, command: .gotIt(project: digest.key)), seen: seen)
    }

    /// - Parameter leads: the night is the current item.
    func night(leads: Bool) -> NightPresentation {
        let items = day.digests.compactMap(nightItem)
        let mine = items.filter { $0.pill != nil }
        let flagged = items.filter { $0.pill == nil && $0.stripe.flag != nil }.sorted { priorityOrder($0.id) < priorityOrder($1.id) }
        let rest = items.filter { $0.pill == nil && $0.stripe.flag == nil }
        let read = nightReadAt
        let count = day.digests.map { nightItems($0).count }.reduce(0, +)
        var line: String
        if let read {
            line = "The night, read at \(clock(read))."
        } else if count == 0 {
            line = "Nothing came in overnight."
        } else {
            line = "Overnight: \(Words.low(Words.things(count, "thing"))) on \(Words.low(Words.things(items.count, "project")))."
            line += mine.isEmpty ? " Nothing needs you." : " \(Words.num(mine.count)) may be yours."
        }
        var groups: [NightPresentation.Group] = []
        if !mine.isEmpty { groups.append(.init(id: "mine", label: "Yours to decide", items: mine)) }
        if !flagged.isEmpty { groups.append(.init(id: "priorities", label: "On your priorities", items: flagged)) }
        if !rest.isEmpty { groups.append(.init(id: "rest", label: "The rest", items: rest)) }
        let quiet = priorities.filter { list in !items.contains { $0.id == list.id } }
        let stale = day.stale.first.map { source in
            StaleNote("\(source.name) has not synced" + (source.since.map { " since \(Words.dayMonth($0, calendar))" } ?? "") + ".")
        }
        return NightPresentation(
            line: line, unread: read == nil && count > 0, leads: leads, title: leads ? "Read the night" : "The night",
            headline: count == 0 ? nil : mine.isEmpty ? "Nothing needs you." : "\(Words.num(mine.count)) may be yours.",
            coverage: "Since \(Words.when(nightFrom, now: now, calendar)).", stale: stale, groups: groups,
            quiet: quiet.isEmpty || count == 0 ? nil : "\(Words.num(quiet.count)) had nothing: \(Words.list(quiet.map(\.name))).",
            gotIt: read == nil && count > 0 ? ButtonPresentation("Got it, all read", glyph: .done, role: .primary, wide: true, command: .readNight) : nil,
            read: read.map { "Read at \(clock($0))." }, empty: count == 0 ? "Nothing came in since you stopped." : nil)
    }

    /// Whether the night would lead: unread, with something in it.
    var nightWaits: Bool { nightReadAt == nil && day.digests.contains { !nightItems($0).isEmpty } }

    // MARK: What is happening

    func happeningGroup(_ key: String, items: [HappeningItem]) -> HappeningGroup {
        let list = day.list(key)
        return HappeningGroup(
            id: key, tile: list.map { tile($0) }, name: name(of: key), stripe: Stripe(list?.flag), opens: list.map { .project($0.id) }, items: items)
    }

    /// Nil when nothing has come in today and nothing marked urgent waits.
    var happening: HappeningPresentation? {
        var byKey: [String: [HappeningItem]] = [:]
        var fresh: [(at: Date, key: String, text: String)] = []
        for digest in day.digests {
            for (index, item) in dayItems(digest).enumerated() {
                let new = isNew(item, in: digest)
                byKey[digest.key, default: []].append(
                    HappeningItem(
                        id: "\(digest.key)-\(index)", time: clock(item.at), text: item.text, source: day.source(of: origin(item)).map { $0 + "." },
                        isNew: new, links: day.links(for: origin(item))))
                if new { fresh.append((item.at, digest.key, item.text)) }
            }
        }
        // What is marked urgent and waits is first in its project's group.
        let waiting = waitingUrgent
        for task in waiting {
            let key = task.list ?? TodayKinds.unfiled
            let item = HappeningItem(
                id: "urgent-" + task.id, time: clock(task.heldAt ?? task.updatedAt), text: task.name + ".",
                source: task.heldReason.map { "Moss thinks it can wait. \($0)" } ?? "Nobody has checked it yet.", pill: .urgent,
                marked: markedBlock(task, reasonSaid: task.heldReason != nil))
            byKey[key, default: []].insert(item, at: 0)
        }
        guard !byKey.isEmpty else { return nil }
        // Every project with something in it is listed whole, with nothing
        // folded inside: the flagged ones in flag order, then the others by
        // name, then what is on no project.
        let flaggedKeys = priorities.map(\.id).filter { byKey[$0] != nil }
        let restKeys = byKey.keys.filter { !flaggedKeys.contains($0) }.sorted {
            (day.list($0) == nil ? 1 : 0, name(of: $0), $0) < (day.list($1) == nil ? 1 : 0, name(of: $1), $1)
        }
        let groups = (flaggedKeys + restKeys).map { happeningGroup($0, items: byKey[$0] ?? []) }
        // The latest new thing leads the closed line, a flagged project's first.
        fresh.sort { (priorityOrder($0.key), $1.at) < (priorityOrder($1.key), $0.at) }
        let lead = fresh.first
        let others = max(0, fresh.count - 1)
        let otherProjects = Set(fresh.dropFirst().map(\.key)).count
        var line =
            waiting.isEmpty
            ? "" : (waiting.count == 1 ? "One thing is marked urgent and waits here. " : "\(Words.num(waiting.count)) things are marked urgent and wait here. ")
        if let lead {
            line += lead.text
            if others > 0 { line += " And \(Words.low(Words.num(others))) more on \(Words.low(Words.things(otherProjects, "project")))." }
        } else {
            line += waiting.isEmpty ? "Nothing new since you last looked." : ""
        }
        return HappeningPresentation(
            line: line.trimmingCharacters(in: .whitespaces), time: waiting.isEmpty ? lead.map { clock($0.at) } : nil,
            tile: waiting.isEmpty ? lead.flatMap { day.list($0.key) }.map { tile($0) } : nil, pill: waiting.isEmpty ? nil : .urgent, quiet: fresh.isEmpty,
            groups: groups,
            gotIt: fresh.isEmpty ? nil : ButtonPresentation("Got it", glyph: .done, role: .primary, command: .gotIt(project: nil)),
            // Said once: the closed line already says it when nothing urgent waits there.
            nothingNew: fresh.isEmpty && !waiting.isEmpty ? "Nothing new since you last looked." : nil)
    }

    // MARK: Earlier today

    /// What is done today, in one order by when it happened: what was
    /// finished as rows, a meeting that ended as its own row, and what is
    /// only bookkeeping as small lines.
    var earlier: EarlierPresentation? {
        var entries: [(at: Date, entry: EarlierPresentation.Entry)] = []
        var lines: [(at: Date, line: EarlierPresentation.Line)] = []
        // A suggestion answered "Already done" is finished, but not something done today.
        let answered = day.notes.suggestedDone ?? []
        for task in day.tasks where task.status == .done && !answered.contains(task.id) {
            // Marked here a moment ago: the server's stamp has not come back yet.
            guard let at = task.completedAt ?? day.notes.doneAt?[task.id], isToday(at), at <= now else { continue }
            entries.append((at, .row(doneRow(task, at: at))))
        }
        for row in todaysRoutines {
            guard let at = doneAt(row) else { continue }
            entries.append((at, .row(doneRow(row, at: at))))
        }
        for meeting in todaysMeetings where meeting.end <= now && attends(meeting) != false {
            var row = meetingRow(meeting, leads: false)
            row.ended = true
            row.until = nil
            entries.append((meeting.end, .meeting(row)))
        }
        if let read = nightReadAt { lines.append((read, .init(id: "night", time: clock(read), text: "Read the night."))) }
        if let saved = day.notes.suggestedSavedAt {
            lines.append((saved, .init(id: "suggested", time: clock(saved), text: "Suggested tasks saved: " + Words.low(savedLine(day.notes.suggestedSaved)))))
        }
        // The plan's own row, under this, is the way back into it.
        if let saved = day.plan?.savedAt, isToday(saved) { lines.append((saved, .init(id: "plan", time: clock(saved), text: "Plan saved."))) }
        // Each thing yesterday left, by name and with its answer: the same words for each would say nothing.
        for item in yesterdayLeft {
            guard let at = item.at, let line = yesterdayAnswer(item) else { continue }
            lines.append((at, .init(id: "y:" + item.id, time: clock(at), text: line)))
        }
        if let closed = day.plan?.wrapSavedAt { lines.append((closed, .init(id: "wrap", time: clock(closed), text: "Day closed."))) }
        entries += lines.map { ($0.at, .line($0.line)) }
        guard !entries.isEmpty else { return nil }
        entries.sort { ($0.at, $0.entry.id) < ($1.at, $1.entry.id) }
        return EarlierPresentation(count: "\(entries.count) done", entries: entries.map(\.entry))
    }
}
