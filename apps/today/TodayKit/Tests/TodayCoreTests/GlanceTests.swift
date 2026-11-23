import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing
import TodayGlance

@testable import TodayCore

/// A widget's day as indented text: what a reader checks a moment by.
func outline(_ day: GlanceDay) -> String {
    func item(_ item: GlanceItem) -> String {
        "\(item.glyph.rawValue) \(item.time.map { $0 + " " } ?? "")\(item.title) | \(item.line ?? "") | \(item.flag?.rawValue ?? "-")\(item.suggested ? "?" : "") | \(item.project ?? "") | \(item.count.map { _ in "counts " } ?? "")\(item.link)"
    }
    var out: [String] = ["\(day.dateShort) message: \(day.message ?? "-")"]
    out.append("now: \(day.now.map(item) ?? day.quiet ?? "-")")
    out.append("next: \(day.next.map { "\($0.time) \($0.title)\($0.counts ? " counts" : "")" } ?? "-")")
    out.append("left: \(day.left ?? "-")")
    out += day.upcoming.map { "  then: " + item($0) }
    out.append("tasks: \(day.tasks.standing ?? "-")")
    out += day.tasks.items.map { "  " + item($0) }
    out.append("meetings: \(day.meetings.standing ?? "-")")
    out += day.meetings.items.map {
        "  \($0.time) \($0.title) | \($0.note ?? "")\($0.live ? " live" : "")\($0.off ? " off" : "")\($0.pair.map { " pair:" + $0.rawValue } ?? "")\($0.counts ? " counts" : "")"
    }
    return out.joined(separator: "\n")
}

private func glance(_ clock: String, edit: (inout TodayApp.State) -> Void = { _ in }) -> GlanceDay { TodayFixtures.glance(at: clock, edit: edit) }

private func pane(_ clock: String) -> PanePresentation { TodayFixtures.present(TodayFixtures.state(at: clock), at: clock).pane }

private func timeline(_ clock: String, limit: Int = GlancePresenter.limit, edit: (inout TodayApp.State) -> Void = { _ in }) -> GlanceTimeline {
    GlancePresenter.timeline(TodayFixtures.state(at: clock, edit: edit), now: TodayFixtures.at(clock), calendar: TodayFixtures.calendar, limit: limit)
}

@Suite("The widgets' day", .timeLimit(.minutes(10)))
struct GlanceTests {
    @Test func dump() {
        guard ProcessInfo.processInfo.environment["TODAY_DUMP"] != nil else { return }
        for (name, day) in TodayFixtures.glances {
            print("==== \(name)")
            print(outline(day))
        }
    }

    @Test func theDayIsThePanesOwnAtThatInstant() throws {
        let day = glance("11:30")
        let pane = pane("11:30")
        #expect(day.date == "Thursday 19 November" && day.dateShort == "Thu 19 Nov" && day.message == nil)
        // What leads is the pane's current item, with its flag, its project and what a tap opens.
        let now = try #require(day.now)
        #expect(pane.lead == now.id && now.id == "row:task:p-vendor")
        #expect(now.glyph == .task && now.title == "Verify the safety vendor endpoint claims" && now.flag == .high && !now.suggested)
        #expect(now.line == "A day late, and the vendors saw last night's errors." && now.project == "External API reliability")
        #expect(now.link == "substrate-today://task/p-vendor")
        // The next fixed point has its real instants, and no count hours ahead.
        let next = try #require(day.next)
        #expect(
            next.title == "Tomas" && next.time == "14:20" && !next.counts && next.start == TodayFixtures.at("14:20") && next.end == TodayFixtures.at("14:40"))
        #expect(day.left == "10 tasks and 12 meetings left")
        // The tasks are the pane's task rows, in its order, and the saved plan's row is no thing ahead.
        let rows = pane.items.compactMap { item -> String? in
            if case let .row(row) = item, row.id.hasPrefix("task:") { "row:" + row.id } else { nil }
        }
        #expect(day.tasks.items.map(\.id) == rows && rows.count == 10)
        #expect(day.tasks.title == "Today" && day.tasks.standing == "3 of 13 done")
        #expect(day.tasks.items.map(\.flag).prefix(4) == [.high, .high, .high, .medium])
        #expect(!day.upcoming.contains { $0.title == "Today's plan" } && !day.upcoming.contains { $0.id == now.id || $0.id == "meeting:" + next.id })
        #expect(day.tasks.more(after: 3) == "and 7 more" && day.tasks.more(after: 10) == nil)
        // What follows is rows and meetings as the pane orders them.
        let order = pane.items.compactMap { item -> String? in
            switch item {
            case let .row(row): row.id == "plan" ? nil : item.id
            case let .meeting(meeting): meeting.off ? nil : item.id
            default: nil
            }
        }.filter { $0 != now.id && $0 != "meeting:" + next.id }
        #expect(day.upcoming.map(\.id) == order)
        // The meetings of the rest of today: one the owner does not go to is struck through and not counted.
        #expect(day.meetings.title == "Meetings" && day.meetings.standing == "12 left" && day.meetings.items.count == 15)
        #expect(day.meetings.items.filter(\.off).map(\.title) == ["Karim", "Callum", "Cloud standup"])
        #expect(day.meetings.items[0].link == "substrate-today://meeting/tomas")
    }

    @Test func aRoutineSaysItsGroupAndACountAndNeverWhatIsInIt() throws {
        let day = glance("07:05")
        let now = try #require(day.now)
        #expect(now.glyph == .routine && now.title == "Morning medication" && now.line == "5 items." && now.link == GlanceLinks.day)
        #expect(day.next?.title == "Physiotherapy call" && day.next?.counts == true)
        // What was not logged yesterday is said, and not which.
        #expect(day.upcoming.first { $0.id == "row:yesterday" }?.line == nil)
        // A dose in no group is "Medication" and a count, as its reminder says it.
        let alone = glance("07:05") { state in
            state.linked.records[RecordFold.key(TodayFixtures.scheduleKind, "vitd")]?.properties["group"] = nil
        }
        let lines = [alone.now].compactMap { $0 } + alone.upcoming
        let dose = try #require(lines.first { $0.id.hasPrefix("row:routine:vitd") })
        #expect(dose.title == "Medication" && dose.line == "1 item.")
        // Nothing a widget is handed, from waking to the end of tomorrow, names a medicine or a dose.
        for state in [TodayFixtures.state(at: "07:05"), TodayFixtures.state(at: "19:20")] {
            let whole = GlancePresenter.timeline(state, now: state.windows.readAt ?? .distantPast, calendar: TodayFixtures.calendar, limit: 200)
            let text = String(decoding: try whole.encoded(), as: UTF8.self)
            for word in ["Magnesium", "Vitamin", "Omega", "Zinc", "Probiotic", "Iron", " mg", "capsule", "tablet", " iu", "mcg"] {
                #expect(!text.contains(word), "\(word)")
            }
        }
    }

    @Test func inAMeetingTheWidgetsSaySoAndCountToTheNext() throws {
        let day = glance("14:25", edit: TodayFixtures.unchosen)
        let now = try #require(day.now)
        #expect(now.id == "meeting:tomas" && now.glyph == .meeting && now.title == "Tomas" && now.line == "Now, until 14:40." && now.count == nil)
        #expect(now.link == "substrate-today://meeting/tomas")
        // The next fixed point starts within the hour: its count runs to its start.
        #expect(day.next?.id == "viktor" && day.next?.counts == true && day.next?.start == TodayFixtures.at("14:40"))
        let meetings = day.meetings.items
        #expect(meetings[0].live && meetings[0].note == "now, until 14:40" && !meetings[0].counts)
        #expect(meetings[1].id == "viktor" && meetings[1].counts && meetings.filter(\.counts).count == 1)
        // Two that share time and wait for a choice are a pair, and the first asks for one.
        #expect(meetings[2].title == "Henrik" && meetings[2].pair == .first && meetings[2].note == "Choose one")
        #expect(meetings[3].title == "Karim" && meetings[3].pair == .second && meetings[3].note == nil && !meetings[3].off)
        // Chosen, they are no pair: the one the owner does not go to is struck through.
        let chosen = glance("14:25").meetings.items
        #expect(chosen[2].pair == nil && chosen[3].pair == nil && chosen[3].off && !chosen[2].off)
    }

    @Test func aMeetingAboutToStartLeadsAndCountsAndTheTasksThatWaitAreStillListed() throws {
        let day = glance("14:12")
        let pane = pane("14:12")
        let now = try #require(day.now)
        #expect(pane.lead == "meeting:tomas" && now.id == "meeting:tomas" && now.line == "At 14:20." && now.count == TodayFixtures.at("14:20"))
        // The meeting that leads is not said again as what is next.
        #expect(day.next?.id == "viktor" && day.next?.counts == false)
        // The pane folds the rows that wait into one line; a widget still lists them, in their order.
        #expect(pane.items.contains { if case .parked = $0 { true } else { false } })
        #expect(
            day.tasks.items.map(\.title).prefix(2) == [
                "Verify the safety vendor endpoint claims", "Triage the Vigil findings and review the four lockdown PRs",
            ])
        #expect(day.tasks.items.count == 6 && day.tasks.standing == "8 of 14 done")
    }

    @Test func somethingUrgentLeadsAndIsATask() throws {
        let day = glance("11:40")
        let now = try #require(day.now)
        #expect(
            now.id == "row:urgent:inc-2" && now.glyph == .urgent && now.title == "External API is failing again" && now.link == "substrate-today://task/inc-2")
        #expect(now.line?.hasPrefix("Since 11:31") == true)
        #expect(day.tasks.items.first?.id == now.id)
    }

    @Test func aDayWithNothingToDrawSaysSoAndSignedOutSaysToOpenTheApp() throws {
        let empty = GlancePresenter.day(TodayApp.State(), now: TodayFixtures.at("11:30"), calendar: TodayFixtures.calendar)
        #expect(empty.message == "Nothing to draw a day from yet. Add the tasks, routines and today packages to this repository.")
        #expect(empty.now == nil && empty.tasks.items.isEmpty && empty.meetings.items.isEmpty)
        let closed = GlanceTimeline.closed(at: TodayFixtures.at("11:30"))
        #expect(closed.until == nil && closed.days(from: TodayFixtures.at("23:00")).map(\.message) == ["Open Substrate Today to see your day."])
        // A day with its tasks done and its meetings over says each in its own widget.
        let late = glance("23:40") { state in
            for (key, record) in state.linked.records where record.kind == TodayKinds.task && record["status"]?.text == "open" {
                state.linked.records[key]?.properties["status"] = "done"
            }
        }
        #expect(late.tasks.items.isEmpty && late.tasks.empty == "Nothing is left today.")
        #expect(late.meetings.items.isEmpty && late.meetings.empty == "No more meetings today." && late.meetings.standing == nil)
    }

    // MARK: The timeline

    /// The timeline is made at the day's turns alone. At every instant
    /// between them it must still say what the pane says then: from waking
    /// through the morning, where routines come due and nothing is chosen
    /// yet, and from the afternoon to the end of tomorrow.
    @Test(arguments: [("07:05", "13:00", 0), ("13:00", "00:00", 2)])
    func theTimelineSaysAtEveryInstantWhatThePaneSaysThen(from clock: String, to end: String, day offset: Int) throws {
        let state = TodayFixtures.state(at: clock)
        let from = TodayFixtures.at(clock)
        let last = TodayFixtures.at(end, day: offset)
        let whole = GlancePresenter.timeline(state, now: from, calendar: TodayFixtures.calendar, limit: 500)
        #expect(whole.until == TodayFixtures.at("00:00", day: 2), "made to the end of tomorrow")
        #expect(whole.entries.map(\.at) == whole.entries.map(\.at).sorted() && Set(whole.entries.map(\.at)).count == whole.entries.count)
        // Every five minutes to two in the night, then every twenty, off the round minutes.
        var instant = from.addingTimeInterval(61)
        while instant < last {
            let direct = GlancePresenter.day(state, now: instant, calendar: TodayFixtures.calendar)
            let held = whole.day(at: instant)
            guard held.says(sameAs: direct) else {
                Issue.record("at \(TodayFixtures.stamp(instant)) the timeline says\n\(outline(held))\nand the pane says\n\(outline(direct))")
                return
            }
            instant = instant.addingTimeInterval(instant < TodayFixtures.at("02:00", day: 1) ? 5 * 60 : 20 * 60)
        }
    }

    @Test func theCountComesOnAnHourBeforeWithoutAPaneOfItsOwn() throws {
        let whole = timeline("12:30")
        let on = try #require(whole.entries.first { $0.at == TodayFixtures.at("13:20") })
        let before = try #require(whole.entries.last { $0.at < TodayFixtures.at("13:20") })
        #expect(before.next?.id == "tomas" && before.next?.counts == false && on.next?.counts == true)
        #expect(on.meetings.items[0].counts && !before.meetings.items[0].counts)
        // Nothing else moved: it is the day before it with the count on.
        var same = before
        same.next?.counts = true
        same.meetings.items[0].counts = true
        #expect(same.says(sameAs: on))
    }

    @Test func aTimelineEndsWhereItWasMadeUpToAndSaysSoFromThere() throws {
        let short = timeline("14:25", limit: 3)
        // Three instants: now, the end of the meeting, and the next turn. The one after is where it ends.
        #expect(short.entries.map(\.at).prefix(2) == [TodayFixtures.at("14:25"), TodayFixtures.at("14:40")])
        let until = try #require(short.until)
        #expect(until > short.entries[short.entries.count - 1].at && until < TodayFixtures.at("16:00"))
        let days = short.days(from: TodayFixtures.at("14:45"))
        #expect(days[0].at == TodayFixtures.at("14:40") && days[0].now?.id == "meeting:viktor")
        #expect(days.last?.at == until && days.last?.message == "Open Substrate Today to bring your day up to date.")
        #expect(short.days(from: until.addingTimeInterval(3600)).map(\.message) == ["Open Substrate Today to bring your day up to date."])
        // Stopped, it ends at once: the day at the instant asked for, and no more.
        let stopped = GlancePresenter.timeline(TodayFixtures.state(at: "14:25"), now: TodayFixtures.at("14:25"), calendar: TodayFixtures.calendar) { true }
        #expect(stopped.entries.count == 1 && stopped.until == TodayFixtures.at("14:40"))
    }

    @Test func makingATimelineTakesAFractionOfASecond() {
        // The busiest moment of the invented day: thirteen tasks and seventeen meetings ahead.
        let state = TodayFixtures.state(at: "09:05")
        let started = Date()
        let whole = GlancePresenter.timeline(state, now: TodayFixtures.at("09:05"), calendar: TodayFixtures.calendar)
        let took = Date().timeIntervalSince(started)
        print("GLANCE a timeline of \(whole.entries.count) entries, to \(whole.until.map(TodayFixtures.stamp) ?? "no end"), took \(Int(took * 1000)) ms")
        #expect(whole.entries.count >= 12 && whole.entries.count <= GlancePresenter.limit + 2)
        #expect((whole.until ?? .distantPast) >= TodayFixtures.at("18:00"), "from the morning it reaches the evening")
        #expect(took < 5, "a bound for a loaded machine, not what it takes")
    }

    @Test func aCountIsSaidInMinutesAsTheAppSaysIt() {
        let from = TodayFixtures.at("14:25")
        #expect(GlanceWords.count(from: from, to: TodayFixtures.at("14:40")) == "in 15 min")
        #expect(GlanceWords.count(from: from, to: TodayFixtures.at("15:20")) == "in 55 min")
        // A minute that has started is counted, and at the start it is "now": never minutes and seconds.
        #expect(GlanceWords.count(from: from, to: from.addingTimeInterval(14 * 60 + 1)) == "in 15 min")
        #expect(GlanceWords.count(from: from, to: from.addingTimeInterval(1)) == "in 1 min")
        #expect(GlanceWords.count(from: from, to: from) == "now" && GlanceWords.count(from: from, to: from.addingTimeInterval(-5)) == "now")
        // Under an hour these are the pane's own words for the next fixed point.
        for seconds in stride(from: 0.0, to: 3541, by: 7) {
            let to = from.addingTimeInterval(seconds)
            #expect(GlanceWords.count(from: from, to: to) == Words.until(to, now: from), "\(seconds)")
        }
        #expect(GlanceWords.until == "in" && GlanceWords.now == "now" && GlanceWords.more(3) == "and 3 more" && GlanceWords.more(0) == nil)
    }

    @Test func theFileIsReadBackAsWrittenAndATimelineThatSaysTheSameIsNotWrittenAgain() throws {
        let whole = timeline("14:25", limit: 6)
        let read = try GlanceTimeline(data: whole.encoded())
        #expect(read == whole)
        #expect(try whole.encoded() == read.encoded())
        // Made again a moment later with nothing changed, it says the same from then on.
        let later = GlancePresenter.timeline(
            TodayFixtures.state(at: "14:25"), now: TodayFixtures.at("14:25").addingTimeInterval(20), calendar: TodayFixtures.calendar, limit: 6)
        #expect(later != whole && later.says(sameAs: whole, from: later.writtenAt))
        let changed = timeline("14:25", limit: 6, edit: TodayFixtures.unchosen)
        #expect(!changed.says(sameAs: whole, from: changed.writtenAt))
        #expect(!GlanceTimeline.closed(at: whole.writtenAt).says(sameAs: whole, from: whole.writtenAt))
    }

    // MARK: Links

    @Test func aLinkIsTheRouteOfThePageItOpensAndAnUnknownOneIsTheDay() throws {
        #expect(TodayLinks.link(nil) == "substrate-today://day" && GlanceLinks.day == "substrate-today://day")
        #expect(TodayLinks.link(.task("p-vendor")) == "substrate-today://task/p-vendor")
        #expect(TodayLinks.link(.meeting("tomas")) == "substrate-today://meeting/tomas")
        #expect(TodayLinks.link(.plan) == "substrate-today://plan" && TodayLinks.link(.suggested) == "substrate-today://suggested")
        // A page no widget line opens has no link of its own.
        #expect(TodayLinks.link(.priorities) == GlanceLinks.day)
        for route in [TodayRoute.task("p-vendor"), .meeting("tomas"), .plan, .suggested, .task("a b/c?d#e"), .meeting("7f3c1b1e_20261119T142000Z")] {
            #expect(TodayLinks.route(TodayLinks.link(route)) == route, "\(route)")
        }
        for link in [
            "substrate-today://day", "substrate-today://", "substrate-today://task", "substrate-today://task/", "substrate-today://elsewhere/x",
            "https://example.com/task/1", "not a link",
        ] {
            #expect(TodayLinks.route(link) == nil, "\(link)")
        }
        #expect(TodayLinks.route(try #require(URL(string: "SUBSTRATE-TODAY://task/t-1"))) == .task("t-1"))
        // Every line of every fixture day opens the day or a page that exists.
        for (name, day) in TodayFixtures.glances {
            let links = [day.now?.link].compactMap { $0 } + day.upcoming.map(\.link) + day.tasks.items.map(\.link) + day.meetings.items.map(\.link)
            for link in links where link != GlanceLinks.day { #expect(TodayLinks.route(link) != nil, "\(name) \(link)") }
        }
    }
}

// MARK: The root writes it

/// What the root handed its writer, in order.
private actor Written {
    var timelines: [GlanceTimeline] = []
    func take(_ timeline: GlanceTimeline) { timelines.append(timeline) }
}

private func app(_ written: Written? = nil) -> TodayApp {
    var write: (@Sendable (GlanceTimeline) async -> Void)?
    if let written { write = { await written.take($0) } }
    return TodayApp(
        cache: nil, outbox: nil, words: TodayApp.words(device: "this iPhone"), build: BuildFacts(version: "0.1.0 (1)"), glance: write)
}

/// A harness whose clock is the environment's, at a moment of the invented day in its time zone.
private func root(_ app: TodayApp, at clock: String, performsEffects: Bool = false) -> ControllerHarness<TodayApp> {
    let clock = TestClock(now: TodayFixtures.at(clock))
    var environment = AppEnvironment.testing(clock: clock)
    environment.timeZone = TodayFixtures.zone
    return ControllerHarness(app, clock: clock, environment: environment, performsEffects: performsEffects)
}

private let signedIn = LinkSnapshot(phase: .signedIn, server: TodayFixtures.server, online: true)

/// The root at a moment of the invented day, signed in, laid over through its own events.
private func load(_ harness: ControllerHarness<TodayApp>, _ clock: String) async {
    let state = TodayFixtures.state(at: clock)
    await harness.deliver(.records(.linked(signedIn)))
    await harness.deliver(.discovered(TodayFixtures.discovery))
    await harness.deliver(.records(.loaded(Array(state.linked.records.values), cursor: nil, queue: [])))
    await harness.deliver(
        .localRead(
            notes: state.notes, windows: state.windows, sources: state.sources, at: TodayFixtures.at(clock),
            today: CivilDay(year: 2026, month: 11, day: 19), zone: TodayFixtures.zone.identifier))
    await harness.deliver(.windowsRead(state.windows, requests: state.requests, stale: []))
    await harness.deliver(.contactsRead(Array(state.contacts.values), asked: []))
}

private func glances(_ harness: ControllerHarness<TodayApp>) -> [TodayApp.State?] {
    harness.effects.compactMap { if case let .writeGlance(state) = $0 { .some(state) } else { nil } }
}

@Suite("The root writes the widgets' day", .timeLimit(.minutes(10)))
struct GlanceWritingTests {
    @Test func theDayIsMadeAgainEachTimeTheStateMovesOnceItsRecordsAreRead() async throws {
        let harness = root(app(Written()), at: "11:30")
        let state = TodayFixtures.state(at: "11:30")
        // Signed in, and nothing read yet: there is no day to write.
        await harness.deliver(.records(.linked(signedIn)))
        await harness.deliver(.discovered(TodayFixtures.discovery))
        #expect(glances(harness).isEmpty)
        await harness.deliver(.records(.loaded(Array(state.linked.records.values), cursor: nil, queue: [])))
        #expect(glances(harness).count == 1)
        #expect(glances(harness)[0]?.linked.records.count == state.linked.records.count)
        // An event that leaves the state as it was asks for nothing.
        harness.clearTrace()
        await harness.deliver(.problemDismissed)
        #expect(glances(harness).isEmpty)
        // One that moves it asks for the day again, from the state as it now stands, under one key.
        await harness.deliver(.windowsRead(state.windows, requests: state.requests, stale: []))
        let asked = glances(harness)
        #expect(asked.count == 1 && asked[0]?.windows == state.windows)
        #expect(TodayFixtures.app.ownKey(of: .writeGlance(nil)) == "glance")
    }

    @Test func aRootWithNoWriterAsksForNoDay() async throws {
        let harness = root(app(), at: "11:30")
        await load(harness, "11:30")
        await harness.deliver(.records(.cleared))
        #expect(glances(harness).isEmpty)
    }

    @Test func signingOutAndClearingWhatTheDeviceKeptCloseTheDay() async throws {
        let harness = root(app(Written()), at: "11:30")
        await load(harness, "11:30")
        harness.clearTrace()
        // Clear Saved Copies: what the widgets showed is taken back, and the records read again bring the day back.
        await harness.deliver(.records(.cleared))
        #expect(glances(harness).count == 1 && glances(harness)[0] == nil)
        harness.clearTrace()
        await harness.deliver(.records(.linked(LinkSnapshot(phase: .signedOut, server: TodayFixtures.server, generation: 1))))
        #expect(glances(harness).count == 1 && glances(harness)[0] == nil)
        // Signed out, the state may still move: nothing of the day is written.
        harness.clearTrace()
        await harness.deliver(.windowsRead(Windows(), requests: [], stale: []))
        await harness.deliver(.records(.linked(LinkSnapshot(phase: .signedOut, server: TodayFixtures.server, generation: 1, online: true))))
        #expect(glances(harness).isEmpty)
        // A launch that finds no session says so once.
        let fresh = root(app(Written()), at: "11:30")
        await fresh.deliver(.records(.linked(LinkSnapshot(phase: .signedOut))))
        #expect(glances(fresh).count == 1 && glances(fresh)[0] == nil)
    }

    @Test func aRunOfChangesIsWrittenOnceAfterAMomentAsTheDayAtThatInstant() async throws {
        let written = Written()
        let harness = root(app(written), at: "11:30", performsEffects: true)
        // What the root reads as it opens (nothing, with no folder) has answered before the day is laid over it.
        try await harness.until { harness in
            let state = await harness.state
            return state.notesRead && state.linked.loaded
        }
        try await harness.untilIdle()
        await load(harness, "11:30")
        // Six events moved the state, each asking for the day: none is written before its moment has passed.
        await harness.settle()
        #expect(await written.timelines.isEmpty)
        try await harness.until(advancing: 1) { _ in await written.timelines.count == 1 }
        let first = try #require(await written.timelines.first)
        #expect(first.writtenAt == TodayFixtures.at("11:30").addingTimeInterval(0.4))
        #expect(first.entries[0].now?.id == "row:task:p-vendor" && first.entries[0].at == first.writtenAt)
        #expect(first.entries.count > 8 && first.until != nil)
        // Another change, another day, once.
        var notes = try #require(await harness.state.notes)
        notes.added.append("typed")
        await harness.deliver(.noted(notes))
        try await harness.until(advancing: 1) { _ in await written.timelines.count == 2 }
        #expect(await written.timelines.count == 2)
        // Signing out takes it back without waiting.
        await harness.deliver(.records(.linked(LinkSnapshot(phase: .signedOut, server: TodayFixtures.server, generation: 1))))
        try await harness.until { _ in await written.timelines.count == 3 }
        let last = try #require(await written.timelines.last)
        #expect(last == .closed(at: last.writtenAt) && last.days(from: last.writtenAt).map(\.message) == [GlanceWords.closed])
    }
}
