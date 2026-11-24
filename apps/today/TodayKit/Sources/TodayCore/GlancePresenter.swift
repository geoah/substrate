import Foundation
import SubstrateCore
import TodayGlance

/// The widgets' presenter: the day as a widget draws it at an instant, made
/// from the pane the engine makes at that instant and never worked out a
/// second time, and the timeline of such days ahead, so a widget stays
/// right while the app is not running.
///
/// A routine is its group and a count here, as in a reminder: the names and
/// doses of what is in it are shown inside the app only.
public enum GlancePresenter {
    /// The most instants a timeline is made at. Each is a whole pane, which
    /// takes the engine about twelve milliseconds on a day of eighteen
    /// meetings, so this many is about a third of a second, off the main
    /// actor. It reaches the evening from the morning of such a day, and
    /// the end of tomorrow on a quiet one.
    public static let limit = 24

    // MARK: A day

    public static func day(_ state: TodayApp.State, now: Date, calendar: Calendar) -> GlanceDay {
        day(DayEngine(TodayPresenter.day(state, now: now, calendar: calendar)))
    }

    static func day(_ engine: DayEngine) -> GlanceDay {
        let pane = engine.pane
        var out = GlanceDay(at: engine.now, date: pane.strip.date, dateShort: pane.strip.dateShort)
        if let empty = pane.empty {
            out.message = empty
            return out
        }
        var done = 0
        var open: [GlanceItem] = []
        var meetings: [GlanceMeeting] = []

        func take(_ row: Row, leads: Bool) {
            // What is done, and the saved plan's row, which is the way back into the plan, are not things ahead.
            guard row.state != .done, !(row.id == "plan" && engine.planSaved) else { return }
            let item = item(row, engine)
            if leads { out.now = item } else { out.upcoming.append(item) }
            if row.id.hasPrefix("task:") || row.id.hasPrefix("urgent:") { open.append(item) }
        }

        for item in pane.items {
            let leads = item.id == pane.lead
            switch item {
            case let .night(night):
                if leads { out.now = GlanceItem(id: item.id, glyph: .night, title: night.title, line: night.headline ?? night.line) }
            case let .earlier(earlier):
                done = earlier.rows.filter { $0.id.hasPrefix("task:") }.count
            case let .row(row):
                take(row, leads: leads)
            case let .parked(line):
                // The rows that wait while a meeting leads are still things of the day, in their order.
                guard let stretch = engine.stretches.first(where: { "s\($0.index)" == line.id }) else { continue }
                for row in engine.rows(in: stretch, current: engine.currentStretch) { take(row, leads: false) }
            case let .meeting(row):
                guard let meeting = engine.day.meeting(row.id) else { continue }
                let glance = self.meeting(row, meeting)
                meetings.append(glance)
                guard !row.off else { continue }
                if leads {
                    out.now = self.item(row, meeting)
                } else {
                    if out.next == nil, !row.live { out.next = glance }
                    out.upcoming.append(self.item(row, meeting))
                }
            case let .note(note):
                if out.quiet == nil { out.quiet = note.text }
            case .happening, .unfinished:
                continue
            }
        }
        if out.now != nil { out.quiet = nil }
        let going = meetings.filter { !$0.off }.count
        out.left = left(tasks: open.count, meetings: going)
        out.tasks = GlanceTasks(standing: done + open.count > 0 ? "\(done) of \(done + open.count) tasks done" : nil, items: open)
        out.meetings = GlanceMeetings(
            standing: left(tasks: 0, meetings: going), items: meetings,
            empty: engine.todaysMeetings.isEmpty ? "No meetings today." : "No more meetings today.")
        return out
    }

    /// A row of the day as one line. A routine says what a reminder says,
    /// and what was not logged yesterday says no more than that.
    static func item(_ row: Row, _ engine: DayEngine) -> GlanceItem {
        var title = row.title
        var line = row.line ?? row.opening
        var route: TodayRoute?
        switch row.body {
        case .routine:
            if let routine = engine.todaysRoutines.first(where: { $0.id == row.id }) {
                title = engine.outsideTitle(routine)
                if routine.isDose, routine.occurrences.count < 2 { line = "1 item." }
            }
        case .yesterday:
            line = nil
        case let .urgent(card):
            line = line ?? card.text
            route = .task(card.id)
        case let .task(card):
            route = .task(card.id)
        case let .heard(suggested):
            route = .task(suggested.id)
        case .suggested, .plan, .overlap, .wrap:
            break
        }
        if case let .page(page) = row.trailing { route = page }
        return GlanceItem(
            id: "row:" + row.id, glyph: GlanceGlyph(rawValue: row.glyph.rawValue) ?? .task, title: title, line: line,
            flag: row.stripe.flag.flatMap { GlanceFlag(rawValue: $0.rawValue) }, suggested: row.stripe.suggested, project: row.chips.project?.name,
            link: TodayLinks.link(route))
    }

    /// A meeting as a thing of the day: its start and its end, "now" while
    /// it is on, and the count to its start where it is the next fixed
    /// point within the hour.
    static func item(_ row: MeetingRow, _ meeting: Meeting) -> GlanceItem {
        GlanceItem(
            id: "meeting:" + row.id, glyph: .meeting, title: row.title, line: row.live ? GlanceWords.now : nil, time: row.start, until: row.end,
            count: !row.live && row.until != nil ? meeting.start : nil, link: TodayLinks.link(.meeting(row.id)))
    }

    static func meeting(_ row: MeetingRow, _ meeting: Meeting) -> GlanceMeeting {
        let live = row.live && !row.off
        let pair: GlanceMeeting.Pair? = row.tie.flatMap { $0.toChoose ? ($0.second ? .second : .first) : nil }
        return GlanceMeeting(
            id: row.id, time: row.start, until: row.end, title: row.title, start: meeting.start, end: meeting.end,
            // The first of a pair asks: where a widget has room for one of the two, it is the one it draws.
            note: live ? GlanceWords.now : pair == .first ? "Choose one" : nil, live: live, off: row.off, pair: pair,
            counts: !row.off && row.until != nil, link: TodayLinks.link(.meeting(row.id)))
    }

    /// "3 tasks and 2 meetings left".
    static func left(tasks: Int, meetings: Int) -> String? {
        var parts: [String] = []
        if tasks > 0 { parts.append("\(tasks) \(tasks == 1 ? "task" : "tasks")") }
        if meetings > 0 { parts.append("\(meetings) \(meetings == 1 ? "meeting" : "meetings")") }
        return parts.isEmpty ? nil : parts.joined(separator: " and ") + " left"
    }

    // MARK: The timeline

    /// The days from `now` on: one at `now`, then one at each turn of the
    /// day (`DayEngine.turns`) that changes what a widget draws, up to
    /// `limit` instants or the end of tomorrow. The instant the count to
    /// the next fixed point starts to be said needs no pane of its own: it
    /// is the day before it with the count on.
    /// - Parameter stop: asked between instants; true ends the timeline
    ///   where it stands, for work that was cancelled.
    public static func timeline(
        _ state: TodayApp.State, now: Date, calendar: Calendar, limit: Int = limit, stop: () -> Bool = { false }
    ) -> GlanceTimeline {
        let today = calendar.startOfDay(for: now)
        let horizon = calendar.date(byAdding: .day, value: 2, to: today) ?? today.addingTimeInterval(2 * 86400)
        var entries: [GlanceDay] = []
        var instant = now
        var until = horizon
        var made = 0
        var records = TodayPresenter.day(state, now: now, calendar: calendar)
        while true {
            // The records are read into a day once for each day the timeline reaches, and the instant moved within it.
            if CivilDay(instant, in: calendar) == records.today {
                records.now = instant
            } else {
                records = TodayPresenter.day(state, now: instant, calendar: calendar)
            }
            let engine = DayEngine(records)
            let glance = day(engine)
            made += 1
            if entries.last.map({ !$0.says(sameAs: glance) }) ?? true { entries.append(glance) }
            guard let turn = engine.turns.first, turn < horizon else { break }
            if let counting = counting(glance, engine, before: turn) { entries.append(counting) }
            if made >= limit || stop() {
                until = turn
                break
            }
            instant = turn
        }
        return GlanceTimeline(writtenAt: now, entries: entries, until: until)
    }

    /// The day with the count to the next fixed point on, from the instant
    /// the pane starts to say it, where that comes before the next turn.
    static func counting(_ glance: GlanceDay, _ engine: DayEngine, before turn: Date) -> GlanceDay? {
        guard let next = engine.nextMeeting else { return nil }
        let from = next.start.addingTimeInterval(-DayEngine.countWindow)
        guard from > engine.now, from < turn else { return nil }
        var out = glance
        out.at = from
        if out.now?.id == "meeting:" + next.id { out.now?.count = next.start }
        if let index = out.upcoming.firstIndex(where: { $0.id == "meeting:" + next.id }) { out.upcoming[index].count = next.start }
        if out.next?.id == next.id { out.next?.counts = true }
        if let index = out.meetings.items.firstIndex(where: { $0.id == next.id }) { out.meetings.items[index].counts = true }
        return out
    }
}
