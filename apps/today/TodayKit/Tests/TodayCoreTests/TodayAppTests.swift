import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing

@testable import TodayCore

/// The pane at a moment of the invented day.
private func pane(_ clock: String, urgent: Bool = true, held: Bool = true, reachable: Bool = true, edit: (inout TodayApp.State) -> Void = { _ in })
    -> PanePresentation
{
    TodayFixtures.present(TodayFixtures.state(at: clock, urgent: urgent, held: held, reachable: reachable, edit: edit), at: clock).pane
}

private func page(_ clock: String, _ route: TodayRoute, edit: (inout TodayApp.State) -> Void = { _ in }) -> PagePresentation {
    TodayFixtures.present(TodayFixtures.state(at: clock, edit: edit), at: clock).page(route)
}

private func rows(_ pane: PanePresentation) -> [Row] {
    pane.items.compactMap { if case let .row(row) = $0 { row } else { nil } }
}

private func row(_ pane: PanePresentation, _ id: String) -> Row? { rows(pane).first { $0.id == id } }

private func meetings(_ pane: PanePresentation) -> [MeetingRow] {
    pane.items.flatMap { item -> [MeetingRow] in
        switch item {
        case let .meeting(meeting): [meeting]
        case let .overlap(block): block.meetings
        default: []
        }
    }
}

@Suite("The order of the day", .timeLimit(.minutes(10)))
struct OrderTests {
    @Test func dump() throws {
        guard ProcessInfo.processInfo.environment["TODAY_DUMP"] != nil else { return }
        for fixture in TodayFixtures.all {
            print("==== \(fixture.name)")
            print(outline(fixture.app.pane))
        }
    }

    @Test func onWakingThePaneIsTheRoutineAndTheNightIsOneLine() {
        let pane = pane("07:05")
        #expect(pane.ground == .waking)
        #expect(pane.lead?.hasPrefix("row:routine:g:Morning medication") == true)
        guard case let .night(night) = pane.items[0] else { return #expect(Bool(false), "the night is always first") }
        #expect(night.unread && !night.leads)
        #expect(night.line == "Overnight: twelve things on seven projects. One may be yours.")
        // One row and one tap for a group: five items, named inside the app only.
        let routine = rows(pane)[0]
        #expect(routine.title == "Morning medication" && routine.line == "5 items.")
        guard case let .routine(card) = routine.body else { return #expect(Bool(false)) }
        #expect(card.items.map(\.name).sorted() == ["Magnesium", "Omega-3", "Probiotic", "Vitamin D", "Zinc"])
        #expect(card.done?.title == "All taken" && card.done?.command == .log(row: routine.id, done: true))
        #expect(card.skip?.title == "Skip" && card.skip?.command == .log(row: routine.id, done: false))
        // Each item has a tick of its own, which logs that one.
        #expect(card.items.allSatisfy { $0.tick?.iconOnly == true && $0.tick?.title == "Taken" && $0.tick?.command == .logOccurrence($0.id, done: true) })
        if case let .tick(all) = routine.trailing { #expect(all.title == "All taken") } else { #expect(Bool(false), "a closed routine ends with its tick") }
        #expect(card.notLogged == nil, "yesterday's were logged")
    }

    @Test func whereLoggingStoppedTheRoutineSaysSoAndYesterdayIsNotAskedAfter() throws {
        let pane = try #require(TodayFixtures.all.first { $0.name == "today-wake-stale" }).app.pane
        guard case let .routine(card) = rows(pane)[0].body else { return #expect(Bool(false)) }
        #expect(card.notLogged == "Not logged since 17 October.")
        // Not a month of doses: nothing of yesterday's routines is asked after, and the task that was due is on the plan page.
        #expect(row(pane, "yesterday") == nil)
        #expect(row(pane, "plan")?.line?.hasSuffix("One is left from yesterday.") == true)
    }

    @Test func theMorningIsNightThenSuggestedAndThePlanInThatOrder() throws {
        let pane = pane("08:20")
        #expect(pane.lead == "night" && pane.ground == .planning)
        #expect(rows(pane).prefix(2).map(\.id) == ["suggested", "plan"])
        // Both open a page, so both end with the right chevron and a tap opens the page.
        #expect(rows(pane).prefix(2).map(\.trailing) == [.page(.suggested), .page(.plan)])
        // On waking, only a routine yesterday left with nothing logged is asked after in the day, by name.
        let waking = try #require(row(TodayCoreTests.pane("07:05"), "yesterday"))
        #expect(waking.title == "Not logged yesterday" && waking.line == "Weekly medication." && waking.glyph == .routine)
        #expect(row(TodayCoreTests.pane("07:38"), "yesterday") == nil, "logged at 07:33")
        let saved = TodayCoreTests.pane("08:40")
        #expect(saved.lead == "row:plan")
        #expect(row(saved, "suggested") == nil)
    }

    @Test func aSavedPlanKeepsARowThatOpensItAndNeverLeads() throws {
        // Before it is saved the plan's row says what yesterday left.
        let before = try #require(row(pane("08:40"), "plan"))
        #expect(
            before.title == "Plan the day"
                && before.line == "What is for today and how much each matters, and four overlaps to choose. One is left from yesterday.")
        let pane = pane("11:30")
        let plan = try #require(row(pane, "plan"))
        #expect(plan.title == "Today's plan" && plan.line == "Saved at 08:44. Thirteen tasks." && plan.trailing == .page(.plan))
        guard case let .plan(card) = plan.body else { return #expect(Bool(false)) }
        #expect(card.open.opens == .plan && card.saved == "Plan saved at 08:44.")
        // It stands under what is done, above what is happening and what leads.
        let ids = pane.items.map(\.id)
        #expect(ids.firstIndex(of: "row:plan") == ids.firstIndex(of: "earlier").map { $0 + 1 })
        #expect(pane.lead == "row:task:p-vendor")
        // "Earlier today" keeps its line, and what yesterday left is named there with its answer.
        guard case let .earlier(earlier)? = pane.items.first(where: { $0.id == "earlier" }) else { return #expect(Bool(false)) }
        #expect(earlier.lines.contains { $0.text == "Plan saved." && $0.button == nil })
        #expect(earlier.lines.contains { $0.text == "From yesterday: verify the safety vendor endpoint claims, moved to today." })
        // The page says it is saved, and has nothing to save until something changes.
        guard case let .plan(page) = page("11:30", .plan) else { return #expect(Bool(false)) }
        #expect(page.saved == "Plan saved at 08:44." && !page.save.enabled && page.left.isEmpty)
    }

    @Test func aMeetingWithinTenMinutesLeadsAndTheRowsWait() {
        let pane = pane("07:52")
        #expect(pane.lead == "meeting:physio" && pane.ground == .meeting)
        #expect(rows(pane).allSatisfy { $0.state == .later })
        #expect(pane.items.contains { if case .parked = $0 { true } else { false } })
        // Only the next fixed point says how long until it.
        let next = meetings(pane).filter { $0.until != nil }
        #expect(next.map(\.id) == ["physio"] && next[0].until == "in 8 min")
    }

    @Test func tasksAreInFlagOrderAndNeverMoveByHand() {
        let pane = pane("11:30")
        #expect(pane.ground == .working && pane.lead == "row:task:p-vendor")
        let tasks = rows(pane).filter { $0.id.hasPrefix("task:") && $0.state != .later }
        #expect(tasks.map(\.stripe.flag) == [.high, .high, .high, .medium, .medium, .medium, .low])
        #expect(tasks[0].state == .now && tasks.dropFirst().allSatisfy { $0.state == .next })
        // A task's closed row ends with its tick, as a routine's does, and no flag control.
        guard case let .tick(tick) = tasks[0].trailing else { return #expect(Bool(false)) }
        #expect(tick.iconOnly && tick.title == "Done" && tick.command == .done("p-vendor") && tick.command?.finishing == "task:p-vendor")
        // The flag is changed from the opened row, and its list never offers Urgent.
        guard case let .task(card) = tasks[0].body, let door = card.doors.first(where: { $0.id == "flag" }) else { return #expect(Bool(false)) }
        #expect(door.options.rows.map(\.name) == ["High", "Medium", "Low"])
        #expect(door.options.selected?.id == "high")
    }

    @Test func aFlagReordersOnlyWhenItsListIsClosed() async throws {
        let harness = ControllerHarness(TodayFixtures.app, performsEffects: false)
        // The state is laid over through the root's own events.
        await harness.deliver(.discovered(TodayFixtures.discovery))
        await harness.deliver(.records(.loaded(TodayFixtures.records(at: TodayFixtures.at("11:30")), cursor: nil, queue: [])))
        let before = await harness.state
        var state = before
        state.windows.meetings = TodayFixtures.meetings
        let engine = DayEngine(TodayPresenter.day(state, now: TodayFixtures.at("11:30"), calendar: TodayFixtures.calendar))
        let first = try #require(engine.todaysTasks.first { $0.id == "t-pharmacy" })
        #expect(engine.orderFlag(first) == .low)
        var held = state
        held.drafts.held["t-pharmacy"] = .low
        held.linked.records[RecordFold.key(TodayKinds.task, "t-pharmacy")]?.properties["priority"] = "high"
        let open = DayEngine(TodayPresenter.day(held, now: TodayFixtures.at("11:30"), calendar: TodayFixtures.calendar))
        let moved = try #require(open.todaysTasks.first { $0.id == "t-pharmacy" })
        #expect(moved.flag == .high && open.orderFlag(moved) == .low)
        held.drafts.held = [:]
        let closed = DayEngine(TodayPresenter.day(held, now: TodayFixtures.at("11:30"), calendar: TodayFixtures.calendar))
        let settled = try #require(closed.todaysTasks.first { $0.id == "t-pharmacy" })
        #expect(closed.orderFlag(settled) == .high)
    }

    @Test func whatIsHappeningSitsDirectlyAboveTheItemThatLeads() throws {
        let pane = pane("11:30")
        let index = try #require(pane.items.firstIndex { if case .happening = $0 { true } else { false } })
        #expect(pane.items[index + 1].id == pane.lead)
        #expect(index > 0, "not at the top of the page")
        guard case let .happening(happening) = pane.items[index] else { return }
        // Grouped by project in flag order.
        #expect(happening.groups.map(\.name) == ["Lantern", "Security hardening", "Hiring"])
        #expect(happening.time == "09:10" && happening.gotIt?.command == .gotIt(project: nil))
        #expect(happening.line == "Callum accepted Friday at 16:30. The 17:20 slot is free. And three more on two projects.")
    }

    @Test func whatIsHappeningListsEveryProjectWithNothingFoldedInside() throws {
        // Something came in on a project with no flag, and something on no project.
        let pane = pane("18:00") { state in
            state.linked.records[RecordFold.key(TodayKinds.digest, "northfield")] = TodayFixtures.digest(
                "northfield", line: "Stefan gave the go-ahead on one node.",
                [("06:23", "Stefan gave the go-ahead on one node."), ("10:40", "Stefan asked when the cable swap can start.")])
        }
        guard case let .happening(happening)? = pane.items.first(where: { if case .happening = $0 { true } else { false } }) else {
            return #expect(Bool(false), "something came in today")
        }
        // The flagged projects in flag order, then the others by name, then what is on no project: each with its own lines.
        #expect(
            happening.groups.map(\.name) == [
                "EU AI Act", "External API reliability", "Lantern", "Security hardening", "Hiring", "Northfield bring-up", "On no project",
            ])
        let flagged = happening.groups.map { $0.stripe.flag != nil }
        #expect(flagged == flagged.sorted { $0 && !$1 }, "no flagged project after one with no flag")
        let northfield = try #require(happening.groups.first { $0.id == "northfield" })
        #expect(northfield.stripe.flag == nil && northfield.opens == .project("northfield"))
        #expect(northfield.items.map(\.text) == ["Stefan asked when the cable swap can start."])
        let unfiled = try #require(happening.groups.last)
        #expect(unfiled.name == "On no project" && unfiled.tile == nil && unfiled.opens == nil)
        #expect(unfiled.items.map(\.text) == ["The Product Review moved to next Thursday."])
        #expect(happening.gotIt?.title == "Got it")
    }

    @Test func anEventOnACalendarThatIsNotTheOwnersIsDrawnNowhere() throws {
        let kind = "providers.substrate.reamde.dev/google/calendar"
        func calendar(_ id: String, _ properties: [String: JSONValue]) -> Record { Record(id: id, kind: kind, properties: properties, version: 1) }
        func on(_ calendar: String, _ event: Record) -> Record {
            var event = event
            event.properties["calendar"] = .object(["ref": .string("\(kind)/\(calendar)")])
            return event
        }
        // Whose a calendar is, by what its record says.
        let mine = calendar("mine", ["primary": true, "accessRole": "owner"])
        let second = calendar("second", ["accessRole": "owner", "dataOwner": .object(["ref": "providers.substrate.reamde.dev/google/emailaddress/me"])])
        let colleague = calendar("colleague", ["accessRole": "reader"])
        #expect(Meeting.isOwn(calendar: mine) && Meeting.isOwn(calendar: second))
        #expect(!Meeting.isOwn(calendar: colleague) && !Meeting.isOwn(calendar: calendar("team", ["accessRole": "writer"])))
        #expect(!Meeting.isOwn(calendar: calendar("partner", ["accessRole": "owner"])), "another person's calendar, with an owner's rights on it")
        #expect(!Meeting.isOwn(calendar: calendar("put-away", ["primary": true, "accessRole": "owner", "hidden": true])))
        #expect(Meeting.isOwn(calendar: calendar("home", ["title": "Home"])) && !Meeting.isOwn(calendar: calendar("feed", ["isSubscribed": true])))

        // A colleague's calendar the account can see holds two events today, one past and one across a meeting of the owner's.
        func state(at clock: String) -> TodayApp.State {
            TodayFixtures.state(at: clock) { state in
                state.windows.calendars = [mine, second, colleague]
                state.windows.meetings = state.windows.meetings.map { on("mine", $0) }
                state.windows.meetings += [
                    on("colleague", TodayFixtures.event("x-early", "09:00", "09:30", "Their standup", people: ["Stefan"])),
                    on("colleague", TodayFixtures.event("x-across", "14:20", "14:50", "Their vendor call", people: ["Stefan"])),
                    on("second", TodayFixtures.event("x-mine", "21:30", "21:45", "Collect the parcel", meet: false)),
                    on("never-read", TodayFixtures.event("x-unknown", "21:50", "21:55", "On a calendar that was not read")),
                ]
            }
        }
        let engine = DayEngine(TodayPresenter.day(state(at: "11:30"), now: TodayFixtures.at("11:30"), calendar: TodayFixtures.calendar))
        let titles = Set(engine.todaysMeetings.map(\.title))
        #expect(titles.contains("Collect the parcel"), "a second calendar of the owner's own")
        #expect(titles.isDisjoint(with: ["Their standup", "Their vendor call", "On a calendar that was not read"]))
        // And what the fixture holds is all there: its events are on the owner's calendar.
        let plain = DayEngine(TodayPresenter.day(TodayFixtures.state(at: "11:30"), now: TodayFixtures.at("11:30"), calendar: TodayFixtures.calendar))
        #expect(titles.subtracting(["Collect the parcel"]) == Set(plain.todaysMeetings.map(\.title)))
        #expect(engine.overlaps.map { $0.meetings.map(\.id) } == plain.overlaps.map { $0.meetings.map(\.id) }, "no overlap with what is not theirs")
        // Nowhere in the day or on a page, before it or after it.
        for clock in ["11:30", "18:00"] {
            let app = TodayFixtures.present(state(at: clock), at: clock)
            var words: [String] = []
            collect(String.self, in: app.pane, into: &words)
            for route in [TodayRoute.plan, .priorities, .projects] { collect(String.self, in: app.page(route), into: &words) }
            #expect(!words.contains { $0.contains("Their standup") || $0.contains("Their vendor call") || $0.contains("was not read") }, "at \(clock)")
            #expect(words.contains { $0.contains("Collect the parcel") }, "at \(clock)")
        }
        // What an earlier build kept holds no calendars, and is read.
        let kept = try JSONDecoder().decode(Windows.self, from: Data(#"{"meetings":[],"occurrences":[]}"#.utf8))
        #expect(kept.calendars.isEmpty && kept.ownMeetings.isEmpty)
    }

    @Test func overlapsArePairsAndAChoiceMakesTheOtherMeetingOff() {
        let morning = pane("08:30")
        let blocks = morning.items.compactMap { if case let .overlap(block) = $0 { block } else { nil } }
        #expect(blocks.map(\.card.name) == ["The 15:00 overlap", "The 17:20 overlap", "The 17:40 overlap", "The 21:00 overlap"])
        #expect(blocks.allSatisfy { $0.card.pill.word == "To choose" && $0.card.options.selected == nil })
        #expect(blocks[1].card.options.rows.map(\.id).last == "both")
        let planned = pane("11:30")
        let chosen = planned.items.compactMap { if case let .overlap(block) = $0 { block } else { nil } }
        #expect(chosen[0].card.pill.word == "Chosen")
        #expect(chosen[0].meetings.map(\.off) == [false, true])
    }

    @Test func theLiveMeetingLeads() {
        let pane = pane("14:25")
        #expect(pane.lead == "meeting:tomas" && pane.ground == .meeting)
        let live = meetings(pane).first { $0.id == "tomas" }
        #expect(live?.live == true && live?.leads == true)
        #expect(live?.card.purpose?.hasPrefix("Weekly 1:1.") == true)
        #expect(live?.card.tasksTitle == "Your tasks with Tomas")
        #expect(live?.card.raise == ["Tomas sent core PR 4127 with no words. It reads as a review request."])
    }

    @Test func theSixGroundsFollowTheMoment() {
        #expect(pane("07:05").ground == .waking)
        #expect(pane("08:30").ground == .planning)
        #expect(pane("11:30").ground == .working)
        #expect(pane("14:25").ground == .meeting)
        #expect(pane("11:40").ground == .urgent)
        #expect(pane("22:15").ground == .closing)
    }

    @Test func nothingInThePaneAssumesADuration() throws {
        // No sizes, no meter, no "may not fit": no word of minutes that is not a real time until a fixed point.
        for fixture in TodayFixtures.all {
            let text = String(decoding: try JSONEncoder().encode(fixture.app.pane), as: UTF8.self)
            #expect(!text.contains("minutes"), "\(fixture.name)")
            #expect(!text.contains("may not fit"), "\(fixture.name)")
        }
    }
}

@Suite("Suggested tasks, the plan and the wrap", .timeLimit(.minutes(10)))
struct SaveCardTests {
    /// The suggested tasks' page at a moment of the invented day.
    private func suggestedPage(_ clock: String, edit: (inout TodayApp.State) -> Void = { _ in }) throws -> SuggestedPage {
        guard case let .suggested(page) = page(clock, .suggested, edit: edit) else { throw CancellationError() }
        return page
    }

    @Test func theSuggestedTasksOpenAsAPageGroupedByProject() throws {
        let pane = pane("08:30")
        let suggested = try #require(row(pane, "suggested"))
        // The row says how many wait and what matters about them, and opens the page: it lists none of them.
        #expect(suggested.title == "Thirteen suggested tasks to answer" && suggested.state == .now && suggested.trailing == .page(.suggested))
        #expect(suggested.line == "Four on your priorities. Moss would make twelve tasks.")
        guard case let .suggested(door) = suggested.body else { return #expect(Bool(false)) }
        #expect(door.open.opens == .suggested)
        var listed: [SuggestedItem] = []
        collect(SuggestedItem.self, in: pane, into: &listed)
        #expect(listed.isEmpty, "the day lists no suggested task")

        let page = try suggestedPage("08:30")
        // The flagged projects first in flag order, then the others by name, then what is on no project.
        #expect(page.groups.map(\.name) == ["EU AI Act", "Lantern", "Hiring", "Northfield bring-up", "Security hardening", "On no project"])
        #expect(page.groups.map(\.stripe.flag) == [.high, .high, .medium, nil, nil, nil])
        #expect(page.groups.map(\.count) == ["1 suggested", "2 suggested", "1 suggested", "1 suggested", "3 suggested", "5 suggested"])
        #expect(page.line == suggested.line && page.rule == DayEngine.fadeRule)
        // Within one, the newest first.
        #expect(page.groups[1].items.map(\.id) == ["t-wei", "p-marcus"])
        #expect(page.groups[4].items.map(\.id) == ["p-kenji", "p-ticket", "p-origin"])
        #expect(page.groups[5].items.map(\.id) == ["p-marco", "p-casey", "p-tomas", "o-farid", "o-ben"])
        // Each group is a save card of its own.
        #expect(page.groups[1].save?.button?.title == "Save 2 answers")
        #expect(page.groups[1].save?.button?.command == .saveSuggested(["t-wei", "p-marcus"], words: [:]))
        for item in page.groups.flatMap(\.items) {
            #expect(item.options.rows.map(\.name) == ["Make it a task", "Already done", "Dismiss", "Other"])
            let selected = try #require(item.options.selected)
            #expect(selected.suggested, "the assistant's option starts selected, and carries the pill")
            #expect(item.save == nil && item.other == nil)
        }
        let maya = try #require(page.groups[2].items.first)
        #expect(maya.options.selected?.id == "dismiss" && maya.fade.level == 3)
        let ben = try #require(page.groups[5].items.last)
        #expect(ben.fade == .init(level: 2, label: "Fading. Gone on Tuesday."))
    }

    @Test func aPickIsADraftAndTheAssistantObjectsOnceInPlace() throws {
        let page = try suggestedPage("08:30") { $0.drafts.suggested["p-marcus"] = .dismiss }
        let marcus = try #require(page.groups.flatMap(\.items).first { $0.id == "p-marcus" })
        #expect(marcus.options.selected?.id == "dismiss")
        // The pill stays on the assistant's option.
        #expect(marcus.options.rows.first { $0.suggested }?.id == "task")
        #expect(marcus.objection?.heading == "Moss objects, once")
    }

    @Test func savingAGroupWritesItsAnswersAndNoOthers() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("08:30"), performsEffects: false)
        await load(harness, "08:30")
        await harness.send(.suggest("p-kenji", .done))
        #expect(writes(harness).isEmpty, "a pick writes nothing")
        guard case let .suggested(page) = await harness.presentation.page(.suggested) else { return #expect(Bool(false)) }
        let security = try #require(page.groups.first { $0.name == "Security hardening" })
        await harness.send(try #require(security.save?.button?.command))
        let written = writes(harness).flatMap(\.entries)
        // The three of that project, and none of the ten on the others.
        #expect(Set(written.map(\.recordID)) == ["p-kenji", "p-ticket", "p-origin"])
        // The one answered "Already done" is finished, but it is not a row of what was done today,
        // even once the server has stamped when it was completed. A task marked done by hand is.
        var state = await harness.state
        let stamp = JSONValue.string("2026-11-19T08:30:00Z")
        state.linked.records[RecordFold.key(TodayKinds.task, "p-kenji")]?.properties["completedAt"] = stamp
        state.linked.records[RecordFold.key(TodayKinds.task, "p-ticket")]?.properties["status"] = "done"
        state.linked.records[RecordFold.key(TodayKinds.task, "p-ticket")]?.properties["completedAt"] = stamp
        state.notes?.suggestedDone?.remove("p-ticket")
        let rows = DayEngine(TodayPresenter.day(state, now: TodayFixtures.at("08:31"), calendar: TodayFixtures.calendar)).earlier?.rows ?? []
        #expect(!rows.contains { $0.id == "task:p-kenji" } && rows.contains { $0.id == "task:p-ticket" })
        let byID = Dictionary(uniqueKeysWithValues: written.map { ($0.recordID, $0.properties) })
        #expect(byID["p-kenji"] == ["status": "done"])
        #expect(byID["p-ticket"] == ["status": "open", "prioritySetBy": "owner"])
        let notes = try #require(await harness.state.notes)
        #expect(notes.suggestedSaved == [2, 1, 0, 0] && notes.suggestedSavedAt == TodayFixtures.at("08:30"))
        // The answered group leaves the page, and the row counts what is left.
        guard case let .suggested(after) = await harness.presentation.page(.suggested) else { return #expect(Bool(false)) }
        #expect(!after.groups.contains { $0.name == "Security hardening" } && after.groups.count == 5)
        #expect(row(await harness.presentation.pane, "suggested")?.title == "Ten suggested tasks to answer")
    }

    @Test func aLargeBacklogIsGroupedAndAGroupDrawsSixUntilAskedForTheRest() async throws {
        let pane = pane("08:30", edit: TodayFixtures.crowd)
        #expect(row(pane, "suggested")?.title == "113 suggested tasks to answer")
        #expect(row(pane, "suggested")?.line == "Four on your priorities. Moss would make two tasks.")
        let page = try suggestedPage("08:30", edit: TodayFixtures.crowd)
        #expect(page.groups.map(\.name) == ["EU AI Act", "Lantern", "Hiring", "Northfield bring-up", "Security hardening", "On no project"])
        let none = try #require(page.groups.last)
        // What the assistant would make a task first, then the newest it has not looked at; what it would
        // close or dismiss is last, among the ones not drawn yet.
        #expect(none.count == "105 suggested" && none.items.map(\.id) == ["b-001", "b-091", "b-078", "b-065", "b-052", "b-039"])
        #expect(none.more?.title == "Show the other 99" && none.more?.command == .showSuggested(group: "none"))
        // With no advice nothing is selected, and Save counts only what is answered, of what is drawn.
        #expect(none.items[0].options.selected?.suggested == true && none.items[0].pending)
        #expect(none.items.dropFirst().allSatisfy { $0.options.selected == nil && !$0.options.rows.contains { $0.suggested } && !$0.pending })
        #expect(none.save?.button?.title == "Save 1 answer" && none.save?.button?.enabled == true)
        // Advice or not, the security group has its own order: the one Moss would make a task first.
        #expect(page.groups[4].items.map(\.id) == ["p-kenji", "p-ticket", "p-origin"])

        let harness = ControllerHarness(TodayFixtures.app, environment: environment("08:30"), performsEffects: false)
        await load(harness, "08:30", edit: TodayFixtures.crowd)
        await harness.send(.suggest("b-091", .dismiss))
        await harness.send(.suggest("b-078", .task))
        guard case let .suggested(drawn) = await harness.presentation.page(.suggested) else { return #expect(Bool(false)) }
        let picked = try #require(drawn.groups.last)
        #expect(picked.items.map(\.id) == none.items.map(\.id), "a pick moves nothing")
        #expect(picked.save?.button?.title == "Save 3 answers")
        await harness.send(try #require(picked.save?.button?.command))
        // Exactly the three answered among the six drawn are written, of 105: b-002 and b-003 have the
        // assistant's answer and were never drawn.
        let written = writes(harness).flatMap(\.entries)
        #expect(Set(written.map(\.recordID)) == ["b-001", "b-091", "b-078"])
        #expect(written.first { $0.recordID == "b-091" }?.properties == ["status": "abandoned"])
        // The next take their place, and the rest are drawn when asked for.
        guard case let .suggested(next) = await harness.presentation.page(.suggested) else { return #expect(Bool(false)) }
        #expect(next.groups.last?.items.map(\.id) == ["b-065", "b-052", "b-039", "b-026", "b-013", "p-marco"])
        #expect(next.groups.last?.save?.button?.enabled == false)
        await harness.send(.showSuggested(group: "none"))
        guard case let .suggested(all) = await harness.presentation.page(.suggested) else { return #expect(Bool(false)) }
        let whole = try #require(all.groups.last)
        #expect(whole.items.count == 102 && whole.more == nil && whole.items.suffix(2).map(\.id) == ["b-002", "b-003"])
        #expect(whole.save?.button?.title == "Save 2 answers")
        #expect(row(await harness.presentation.pane, "suggested")?.title == "110 suggested tasks to answer")
    }

    @Test func otherKeepsTheOwnersWordsAndSendsThemToTheAssistantAboutThatTask() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("08:30"), performsEffects: false)
        await load(harness, "08:30")
        await harness.send(.suggest("p-marco", .other))
        guard case let .suggested(page) = await harness.presentation.page(.suggested) else { return #expect(Bool(false)) }
        let none = try #require(page.groups.last)
        let marco = try #require(none.items.first { $0.id == "p-marco" })
        // Chosen, it shows its field; the pill stays on the assistant's option.
        #expect(marco.options.selected?.id == "other" && marco.other?.placeholder == "What is wrong, or what to do instead")
        #expect(marco.options.rows.first { $0.suggested }?.id == "task" && marco.pending)
        #expect(writes(harness).isEmpty, "nothing is written before the Save")
        // With no words it is left as it is, and its pick stays.
        await harness.send(.saveSuggested(["p-marco"], words: ["p-marco": "  "]))
        let kept = await harness.state.drafts.suggested["p-marco"]
        #expect(writes(harness).isEmpty && kept == .other)

        guard case let .saveSuggested(ids, _)? = none.save?.button?.command else { return #expect(Bool(false)) }
        await harness.send(.saveSuggested(ids, words: ["p-marco": " Marco asked about the offsite, not what I drink. Ask him the dates. "]))
        let written = writes(harness).flatMap(\.entries)
        let correction = try #require(written.first { $0.kind == TodayKinds.correction })
        let path = "\(TodayKinds.task)/p-marco"
        #expect(correction.method == .put)
        #expect(
            correction.properties == [
                "subject": ["ref": .string(path)], "text": "Marco asked about the offsite, not what I drink. Ask him the dates.",
            ])
        #expect(!written.contains { $0.recordID == "p-marco" }, "the task itself is not changed: Moss acts on it")
        #expect(Set(written.filter { $0.kind == TodayKinds.task }.map(\.recordID)) == ["p-casey", "p-tomas", "o-farid", "o-ben"])
        // The words go to the assistant about that task, with the task as the turn's context.
        guard case let .ask(exchange, message, context)? = harness.effects.last(where: { if case .ask = $0 { true } else { false } }) else {
            return #expect(Bool(false))
        }
        #expect(exchange.subject == .task("p-marco") && context == [path])
        #expect(message == "Marco asked about the offsite, not what I drink. Ask him the dates.")
        let counts = await harness.state.notes?.suggestedSaved
        #expect(counts == [4, 0, 0, 1])

        // It is with Moss: not asked again, not counted, and drawn with what was said while the exchange is here.
        guard case let .suggested(after) = await harness.presentation.page(.suggested) else { return #expect(Bool(false)) }
        let told = try #require(after.groups.last)
        #expect(told.items.isEmpty && told.save == nil && told.count == "With Moss" && told.told.map(\.id) == ["p-marco"])
        #expect(told.told[0].told?.words == "Marco asked about the offsite, not what I drink. Ask him the dates.")
        #expect(told.told[0].told?.answer?.state == .waiting && told.told[0].options.isEmpty)
        #expect(row(await harness.presentation.pane, "suggested")?.title == "Eight suggested tasks to answer")
        await harness.deliver(.answer(exchange.id, .done("I asked Marco for the dates and dismissed the suggestion.")))
        guard case let .suggested(answered) = await harness.presentation.page(.suggested) else { return #expect(Bool(false)) }
        #expect(answered.groups.last?.told.first?.told?.answer?.text == "I asked Marco for the dates and dismissed the suggestion.")
        // On its own page it says the same, once: the box under it does not hold that exchange a second time.
        guard case let .task(own) = await harness.presentation.page(.task("p-marco")) else { return #expect(Bool(false)) }
        #expect(own.suggestion?.told?.answer?.state == .done && own.suggestion?.told?.words.hasPrefix("Marco asked") == true)
        #expect(own.suggestion?.options.isEmpty == true && own.card.moss?.exchanges.isEmpty == true)
        // Closed, it leaves the page; the record stays for whoever suggests tasks.
        await harness.send(.closeAnswer(exchange.id))
        guard case let .suggested(closed) = await harness.presentation.page(.suggested) else { return #expect(Bool(false)) }
        #expect(!closed.groups.contains { $0.id == "none" })
    }

    @Test func aCorrectedSuggestionIsAskedAgainOnlyOnceItChanges() throws {
        func correction(_ clock: String) -> Record {
            Record(
                id: "c1", kind: TodayKinds.correction,
                properties: ["subject": ["ref": .string("\(TodayKinds.task)/p-marco")], "text": "Not what he asked."], version: 1,
                createdAt: TodayFixtures.stamp(TodayFixtures.at(clock)))
        }
        func ids(_ edit: @escaping (inout TodayApp.State) -> Void) throws -> [String] {
            try suggestedPage("08:30", edit: edit).groups.flatMap(\.items).map(\.id)
        }
        // The suggestion was written at 00:40: words said after that stand, and it is with Moss.
        #expect(try !ids { $0.linked.records[RecordFold.key(TodayKinds.correction, "c1")] = correction("08:00") }.contains("p-marco"))
        // Rewritten since, it is a suggestion again.
        let rewritten = try ids { state in
            state.linked.records[RecordFold.key(TodayKinds.correction, "c1")] = correction("08:00")
            state.linked.records[RecordFold.key(TodayKinds.task, "p-marco")]?.updatedAt = TodayFixtures.stamp(TodayFixtures.at("08:10"))
        }
        #expect(rewritten.contains("p-marco"))
        // A repository that does not keep such words offers the three answers and no other.
        let without = try suggestedPage("08:30") { state in
            state.discovery?.present.removeAll { $0 == TodayKinds.correction }
            state.drafts.suggested["p-marco"] = .other
        }
        #expect(without.groups.flatMap(\.items).allSatisfy { $0.options.rows.map(\.id) == ["task", "done", "dismiss"] && $0.other == nil })
    }

    @Test func otherIsNotWrittenWhereTheRepositoryDoesNotKeepIt() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("08:30"), performsEffects: false)
        await load(harness, "08:30")
        var discovery = TodayFixtures.discovery
        discovery.present.removeAll { $0 == TodayKinds.correction }
        await harness.deliver(.discovered(discovery))
        await harness.send(.suggest("p-marco", .other))
        harness.clearTrace()
        // The pick is no pick there: the list shows the assistant's option selected, and Save applies that.
        guard case let .task(page) = await harness.presentation.page(.task("p-marco")) else { return #expect(Bool(false)) }
        #expect(page.suggestion?.options.rows.map(\.id) == ["task", "done", "dismiss"] && page.suggestion?.options.selected?.id == "task")
        await harness.send(.saveSuggested(["p-marco"], words: ["p-marco": "Not what he asked."]))
        let written = writes(harness).flatMap(\.entries)
        #expect(written.map(\.kind) == [TodayKinds.task] && written.first?.properties?["status"] == "open")
        #expect(!harness.effects.contains { if case .ask = $0 { true } else { false } })
    }

    @Test func aSuggestionSaysWhenItWasHeardOnlyWhereItsRecordSaysSo() throws {
        let page = try suggestedPage("08:30", edit: TodayFixtures.crowd)
        let items = page.groups.flatMap(\.items)
        // The record names when the words were said: that, and not when the suggestion was written four minutes later.
        #expect(items.first { $0.id == "p-kenji" }?.origin == "Heard yesterday at 21:37, with Kenji.")
        #expect(items.first { $0.id == "p-marcus" }?.origin == "Heard on 22 October, with Marcus.")
        // An older record says only when it was written, and is not called heard.
        #expect(items.first { $0.id == "b-091" }?.origin == "Suggested today at 07:31.")
        // It fades by when it was said: one written yesterday from words two weeks old is gone tonight.
        let late = try suggestedPage("08:30") { state in
            let key = RecordFold.key(TodayKinds.task, "p-marco")
            state.linked.records[key]?.createdAt = TodayFixtures.stamp(TodayFixtures.at("22:59", day: -1))
            state.linked.records[key]?.properties["originAt"] = .string(TodayFixtures.stamp(TodayFixtures.at("15:06", day: -15)))
        }
        let old = try #require(late.groups.flatMap(\.items).first { $0.id == "p-marco" })
        #expect(old.origin == "Heard on 4 November, with Marco." && old.fade.level == 3)
    }

    @Test func thePlanListsNoSuggestedTaskAndHasADoorToTheirPage() throws {
        guard case let .plan(plan) = page("08:30", .plan, edit: TodayFixtures.crowd) else { return #expect(Bool(false)) }
        let listed = Set(plan.groups.flatMap { $0.items + $0.more }.map(\.id))
        #expect(!listed.contains { $0.hasPrefix("b-") } && !listed.contains("p-kenji"))
        #expect(plan.suggested == "113 suggested tasks wait for an answer, on a page of their own." && plan.suggestedOpen?.opens == .suggested)
        guard case let .plan(none) = page("11:30", .plan) else { return #expect(Bool(false)) }
        #expect(none.suggested == nil && none.suggestedOpen == nil)
    }

    @Test func oneHeardInTheDayIsARowOfItsOwnOnlyWhileFewWait() throws {
        // At 16:35 one suggestion was heard after a group was saved: it is its own row, with its own Save.
        let few = pane("16:35")
        let heard = try #require(row(few, "heard:h-tomas-pr"))
        #expect(row(few, "suggested") == nil && heard.line == "Heard today at 14:28, with Tomas.")
        guard case let .heard(item) = heard.body else { return #expect(Bool(false)) }
        #expect(item.save?.button?.command == .saveSuggested(["h-tomas-pr"], words: [:]))
        // With a hundred waiting, everything goes through the page.
        let many = pane("16:35", edit: TodayFixtures.crowd)
        #expect(!rows(many).contains { $0.id.hasPrefix("heard:") })
        #expect(row(many, "suggested")?.title == "101 suggested tasks to answer")
    }

    @Test func theWrapSaysHowManySuggestedTasksWaitAndListsNone() throws {
        guard case let .wrap(card) = try #require(row(pane("22:15", edit: TodayFixtures.crowd), "wrap")).body else { return #expect(Bool(false)) }
        #expect(card.unanswered == "101 suggested tasks are still not answered." && card.answer?.opens == .suggested)
        var items: [SuggestedItem] = []
        collect(SuggestedItem.self, in: card, into: &items)
        #expect(items.isEmpty, "the wrap never lists a suggested task")
        guard case let .wrap(one) = try #require(row(pane("22:15"), "wrap")).body else { return #expect(Bool(false)) }
        #expect(one.unanswered == "One suggested task is still not answered.")
    }

    @Test func aDescriptionIsDrawnAsMarkdownWithoutABracketedRecordPath() throws {
        let said = """
            Agreed on the call: you would send everyone the answers.

            **Question:** do the drafts need the same sign-off? (source: providers.example.dev/chat/message/chat-733f1dd2ad0a) Seen again on 21 November.
            """
        // Where the words came from is the task's origin already: a record's path in brackets is not shown.
        #expect(
            Words.prose(said)
                == "Agreed on the call: you would send everyone the answers.\n\n**Question:** do the drafts need the same sign-off? Seen again on 21 November.")
        #expect(
            Words.prose("Both asked (sources: a.example.dev/chat/message/m1, a.example.dev/chat/message/m2 and a.example.dev/mail/message/m3).")
                == "Both asked.")
        #expect(Words.prose("Asked twice (see (a.example.dev/chat/message/m1)).") == "Asked twice (see).")
        // Brackets that say something stay, and so does a record named in the sentence, which is drawn as its pill.
        #expect(Words.prose("He agreed (twice, by Friday).") == "He agreed (twice, by Friday).")
        #expect(Words.prose("See a.example.dev/chat/message/m1 for the rest.") == "See a.example.dev/chat/message/m1 for the rest.")
        #expect(Words.prose("> But yes.\n> > Fixing it") == "But yes.\nFixing it")
        // A cut never leaves a mark or a link open.
        #expect(
            Words.excerpt("Intro words here and **a bold run that is cut in the middle of it** end.", limit: 40)
                == "Intro words here and a bold run that\u{2026}")
        #expect(Words.excerpt("**Whole:** kept as it was written.", limit: 40) == "**Whole:** kept as it was written.")
        #expect(
            Words.excerpt("Read [the proposal for the new bastion](https://example.com/a/long/address) today", limit: 30)
                == "Read the proposal for the new\u{2026}")
        #expect(Words.excerpt("Run `make check` and then `make all the things` twice", limit: 34) == "Run `make check` and then make\u{2026}")
        #expect(Words.closed("one *two") == "one two" && Words.closed("**one** *two* three") == "**one** *two* three")
        // A row's line draws no Markdown: there the same words are plain.
        #expect(Words.plain("**Question:** see [the doc](https://example.com/x) and `the script`") == "Question: see the doc and the script")

        // A suggested task's description on the page, whole on its own page, and a task's line in the day.
        let key = RecordFold.key(TodayKinds.task, "p-marco")
        func lay(_ state: inout TodayApp.State) { state.linked.records[key]?.properties["description"] = .string(said) }
        let item = try #require(try suggestedPage("08:30", edit: lay).groups.flatMap(\.items).first { $0.id == "p-marco" })
        // A text a little over a card's cut is shown whole, with its marks; cut, it would keep them closed.
        #expect(item.quote == Words.prose(said) && item.brief == nil)
        #expect(
            Words.excerpt(said)
                == "Agreed on the call: you would send everyone the answers. **Question:** do the drafts need the same sign-off? Seen again\u{2026}")
        guard case let .task(own) = page("08:30", .task("p-marco"), edit: lay) else { return #expect(Bool(false)) }
        #expect(own.suggestion?.quote == Words.prose(said) && own.suggestion?.brief == nil)
        let day = pane("11:30") { state in
            state.linked.records[RecordFold.key(TodayKinds.task, "t-leo")]?.properties["priorityReason"] = nil
            state.linked.records[RecordFold.key(TodayKinds.task, "t-leo")]?.properties["description"] =
                "**Question:** which of the decisions are [ours](https://example.com/x)?"
            state.linked.records[RecordFold.key(TodayKinds.task, "t-leo")]?.properties["dueDay"] = nil
            state.linked.records[RecordFold.key(TodayKinds.task, "t-leo")]?.properties["notBefore"] = TodayFixtures.ts("10:00")
        }
        #expect(row(day, "task:t-leo")?.opening == "Question: which of the decisions are ours?")
    }

    @Test func anOpenTaskShowsAllOfItsDescriptionAndTheCurrentItemItsOpeningWordsUntilAsked() throws {
        let long = TodayFixtures.longDescription
        let short = "Asked in the finance channel whether the October ledger was checked against the bank export, and you said you would look\u{2026}"
        #expect(Words.excerpt(long) == short && short.count <= Words.excerptLimit + 1)
        #expect(Words.excerpt("One line.\n\nAnother.") == "One line. Another.", "a short text is one paragraph, not cut")
        #expect(Words.excerpt("> But yes.\n> > Fixing it") == "But yes. Fixing it", "a quote's marks are not words")
        #expect(Words.excerpt(String(repeating: "a", count: 300)).count == Words.excerptLimit + 1, "a run with no word to cut at is still cut")
        // A suggested task on the page, and on a project's or a person's page.
        let suggested = try suggestedPage("08:30", edit: TodayFixtures.crowd)
        let crowded = try #require(suggested.groups.flatMap(\.items).first { $0.id == "b-091" })
        #expect(crowded.brief?.text == short && crowded.quote == long)
        #expect(suggested.groups.flatMap(\.items).first { $0.quote == "I will get to it this week." }?.brief == nil, "a short one has no button")
        // A text a little over the cut is whole: no button for a dozen words.
        let some = String(repeating: "word ", count: 30) + "end."
        func lay(_ state: inout TodayApp.State) {
            TodayFixtures.crowd(&state)
            state.linked.records[RecordFold.key(TodayKinds.task, "b-091")]?.properties["description"] = .string(some)
        }
        let near = try #require(try suggestedPage("08:30", edit: lay).groups.flatMap(\.items).first { $0.id == "b-091" })
        #expect(some.count > Words.excerptLimit && some.count <= Words.excerptWholeLimit && near.quote == some && near.brief == nil)
        // The lead is open without being asked: its reason above, and under it the opening
        // words of a long description with the button that shows all of it, which the card holds.
        let day = pane("11:30", edit: TodayFixtures.longTasks)
        let lead = try #require(row(day, "task:t-ledger"))
        guard case let .task(open) = lead.body else { return #expect(Bool(false)) }
        let brief = try #require(open.brief)
        #expect(lead.state == .now && lead.line == "Due today." && lead.opening == nil && open.details == long)
        #expect(brief.text.hasPrefix("Asked in the finance channel") && brief.text.hasSuffix("\u{2026}"))
        #expect(brief.text.count <= Words.leadExcerptLimit + 1 && brief.more == "Show all" && brief.less == "Show less")
        // A row with no other reason: closed, the opening words are its line, cut once and shorter
        // than a card's, a row being narrower. Opened, it shows all of the description, and the
        // head does not say its opening again.
        let moved = try #require(row(day, "task:t-archive"))
        guard case let .task(under) = moved.body else { return #expect(Bool(false)) }
        let line = "Asked in the finance channel whether the October ledger was checked against the\u{2026}"
        #expect(moved.line == nil && moved.opening == line && line.count <= Words.lineExcerptLimit + 1)
        #expect(under.details == long && under.brief == nil)
        // A short description is whole where it leads too, and one of blank lines alone is none.
        func lay(_ text: String) -> (inout TodayApp.State) -> Void {
            { state in
                TodayFixtures.longTasks(&state)
                state.linked.records[RecordFold.key(TodayKinds.task, "t-ledger")]?.properties["description"] = .string(text)
            }
        }
        guard case let .task(few) = try #require(row(pane("11:30", edit: lay("Two accounts.\n\nBoth small.")), "task:t-ledger")).body else {
            return #expect(Bool(false))
        }
        #expect(few.details == "Two accounts.\n\nBoth small." && few.brief == nil)
        guard case let .task(none) = try #require(row(pane("11:30", edit: lay(" \n\n ")), "task:t-ledger")).body else {
            return #expect(Bool(false))
        }
        #expect(none.details == nil && none.brief == nil)
        // The task's own page shows all of it as well, and its line does not say its opening.
        guard case let .task(own) = page("11:30", .task("t-archive"), edit: TodayFixtures.longTasks) else { return #expect(Bool(false)) }
        #expect(own.card.details == long && own.card.brief == nil && own.line == nil)
        guard case let .task(suggestion) = page("08:30", .task("b-091"), edit: TodayFixtures.crowd) else { return #expect(Bool(false)) }
        #expect(suggestion.suggestion?.quote == long && suggestion.suggestion?.brief == nil)
    }

    @Test func thePlanPageTicksWhatIsSuggestedAndNeverAMessage() throws {
        guard case let .plan(plan) = page("08:40", .plan) else { return #expect(Bool(false)) }
        #expect(plan.groups.prefix(5).map(\.name) == ["EU AI Act", "External API reliability", "Lantern", "GitHub orgs", "Hiring"])
        let all = plan.groups.flatMap { $0.items + $0.more }
        #expect(all.filter(\.suggested).allSatisfy { $0.ticked })
        #expect(all.first { $0.id == "t-proxy" }?.ticked == true)
        #expect(all.first { $0.id == "t-keys" }?.ticked == false)
        // What yesterday left is the first section: each task by name with its own list, and nothing chosen for the owner.
        #expect(plan.leftTitle == "Left from yesterday" && plan.left.map(\.id) == ["p-vendor"] && !all.contains { $0.id == "p-vendor" })
        let left = try #require(plan.left.first)
        #expect(left.title == "Verify the safety vendor endpoint claims" && left.chips.project?.name == "External API reliability")
        #expect(left.line == "It was due yesterday. A day late, and the vendors saw last night's errors." && left.chips.pills.isEmpty)
        #expect(left.options.rows.map(\.name) == ["Today", "Tomorrow", "Drop it"] && left.options.selected == nil)
        #expect(plan.barLine.hasSuffix("One from yesterday has no answer yet."))
        #expect(plan.overlaps.count == 4 && plan.overlaps.allSatisfy { $0.options.selected == nil })
        #expect(plan.sends.rows.allSatisfy { !$0.selected })
        #expect(plan.save.title == "Save the plan" && plan.save.enabled)
        #expect(plan.barLine.hasPrefix("Save ") && plan.barLine.contains("No message goes."))
        #expect(plan.opening == "Free until Tomas at 14:20, then meetings until 21:30, with gaps at 16:30, 18:15 and 20:30.")
    }

    @Test func savingThePlanKeepsOnlyWhatWasTicked() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("08:40"), performsEffects: false)
        await load(harness, "08:40")
        await harness.send(.planPick("t-leo"))
        await harness.send(.planFlag("t-leo", .medium))
        await harness.send(.planOverlap("ov-henrik+karim", option: "\(TodayFixtures.eventKind)/henrik"))
        // What yesterday left: a choice is a draft too, and the option chosen again takes it back.
        await harness.send(.planLeft("p-vendor", option: "drop"))
        await harness.send(.planLeft("p-vendor", option: "drop"))
        let taken = await harness.state.drafts.plan?.left
        #expect(taken?.isEmpty == true)
        await harness.send(.planLeft("p-vendor", option: "today"))
        guard case let .plan(draft) = await harness.presentation.page(.plan) else { return #expect(Bool(false)) }
        #expect(draft.left.first?.options.selected?.id == "today" && !draft.barLine.contains("from yesterday"))
        #expect(writes(harness).isEmpty, "the plan is a draft until it is saved")
        await harness.send(.planSave)
        let written = writes(harness).flatMap(\.entries)
        // The one left from yesterday is for today: its day moves, and the plan holds it.
        #expect(written.first { $0.recordID == "p-vendor" }?.properties == ["dueDay": "2026-11-19"])
        let noted = await harness.state.notes?.yesterday["y-task:p-vendor"]
        #expect(noted == "today")
        let flag = try #require(written.first { $0.recordID == "t-leo" })
        #expect(flag.properties?["priority"] == "medium" && flag.properties?["prioritySetBy"] == "owner")
        let plan = written.filter { $0.kind == TodayKinds.plan }
        #expect(plan.map(\.recordID) == ["d20261119", "d20261119"] && plan.map(\.method) == [.put, .patch])
        #expect(plan[0].properties == ["day": "2026-11-19"])
        let picked = (plan[1].properties?["picked"]?.array ?? []).compactMap(\.text)
        #expect(picked.contains("\(TodayKinds.task)/t-leo") && !picked.contains("\(TodayKinds.task)/t-keys"))
        #expect(picked.contains("\(TodayKinds.task)/p-vendor"))
        #expect(plan[1].properties?["savedAt"] == .string("2026-11-19T08:40:00Z"))
        #expect(plan[1].properties?["overlaps"]?.array?.first?["chosen"]?.text == "\(TodayFixtures.eventKind)/henrik")
        #expect(await harness.state.drafts.plan == nil)
    }

    @Test func whatYesterdayLeftCanLeaveTodayOrBeDroppedFromThePlan() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("08:40"), performsEffects: false)
        await load(harness, "08:40")
        await harness.send(.planLeft("p-vendor", option: "tomorrow"))
        guard case let .plan(draft) = await harness.presentation.page(.plan) else { return #expect(Bool(false)) }
        #expect(draft.barLine.contains("One from yesterday leaves today."))
        await harness.send(.planSave)
        let moved = try #require(writes(harness).flatMap(\.entries).first { $0.recordID == "p-vendor" })
        #expect(
            moved.properties == ["dueDay": "2026-11-20", "notBefore": "2026-11-20T00:00:00Z", "deferrals": 1])
        let picked = (writes(harness).flatMap(\.entries).last?.properties?["picked"]?.array ?? []).compactMap(\.text)
        #expect(!picked.contains("\(TodayKinds.task)/p-vendor"))

        let other = ControllerHarness(TodayFixtures.app, environment: environment("08:40"), performsEffects: false)
        await load(other, "08:40")
        await other.send(.planLeft("p-vendor", option: "drop"))
        await other.send(.planSave)
        #expect(writes(other).flatMap(\.entries).first { $0.recordID == "p-vendor" }?.properties == ["status": "abandoned"])
        // Answered, it is named with its answer in what is done, and is asked after no more.
        let lines = await other.presentation.pane.items.compactMap { item -> [String]? in
            if case let .earlier(earlier) = item { earlier.lines.map(\.text) } else { nil }
        }.flatMap { $0 }
        #expect(lines.contains("From yesterday: verify the safety vendor endpoint claims, dropped."))
        guard case let .plan(after) = await other.presentation.page(.plan) else { return #expect(Bool(false)) }
        #expect(after.left.isEmpty && after.leftTitle == nil)
    }

    @Test func aTaskPutOnTodayIsCountedAndTickedWhereverTodayIsDrawn() async throws {
        // What Moss adds when the owner asks: an open task in a project, `notBefore` now, no flag and no due day.
        let added = TodayFixtures.task(
            "m-caddies", "Test the drive caddies", list: "hiring", created: TodayFixtures.at("08:35"), extra: ["notBefore": TodayFixtures.ts("08:35")])
        func lay(_ state: inout TodayApp.State) { state.linked.records[RecordFold.key(added.kind, added.id)] = added }
        // Before the plan is saved: the strip counts it, the day draws it, and the plan has it ticked.
        let before = pane("08:40", edit: lay)
        #expect(before.strip.entries.first { $0.id == "hiring" }?.line == "0 of 1 done")
        #expect(row(before, "task:m-caddies") != nil)
        guard case let .plan(plan) = page("08:40", .plan, edit: lay) else { return #expect(Bool(false)) }
        let hiring = try #require(plan.groups.first { $0.id == "hiring" })
        #expect(hiring.count == "1 today" && hiring.items.map(\.id) == ["m-caddies"] && hiring.items[0].ticked && hiring.items[0].suggested)
        // Added after the plan was saved, it is today's all the same.
        let after = pane("11:30", edit: lay)
        #expect(after.strip.entries.first { $0.id == "hiring" }?.line == "0 of 2 done" && row(after, "task:m-caddies") != nil)
        guard case let .plan(saved) = page("11:30", .plan, edit: lay) else { return #expect(Bool(false)) }
        #expect(saved.groups.flatMap { $0.items + $0.more }.first { $0.id == "m-caddies" }?.ticked == true)
        // Unticked and saved, it leaves today: its place is taken back.
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("11:30"), performsEffects: false)
        await load(harness, "11:30", edit: lay)
        await harness.send(.planPick("m-caddies"))
        await harness.send(.planSave)
        #expect(writes(harness).flatMap(\.entries).first { $0.recordID == "m-caddies" }?.properties == ["notBefore": .null])
    }

    @Test func aFlagSuggestedOnATaskStartsSelectedInThePlanAndIsTakenOnSave() async throws {
        let request = Record(
            id: "req-keys", kind: TodayKinds.patchRequest,
            properties: [
                "target": .string("\(TodayKinds.task)/t-keys"), "decision": "proposed", "diff": ["properties": ["priority": "high"]],
                "rationale": "The order closes on Friday.",
            ], version: 1)
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("08:40"), performsEffects: false)
        await load(harness, "08:40") { $0.requests.append(request) }
        guard case let .plan(plan) = await harness.presentation.page(.plan) else { return #expect(Bool(false)) }
        let item = try #require(plan.groups.flatMap { $0.items + $0.more }.first { $0.id == "t-keys" })
        // Above the Save the assistant's option starts selected, with its pill and its reason, and the stripe is hollow.
        #expect(item.flag.stripe == Stripe(.high, suggested: true))
        let high = try #require(item.flag.options.rows.first { $0.id == "high" })
        #expect(high.selected && high.suggested && high.reason == "The order closes on Friday.")
        await harness.send(.planPick("t-keys"))
        #expect(writes(harness).isEmpty, "nothing above the Save takes effect before it")
        await harness.send(.planSave)
        let entries = writes(harness).flatMap(\.entries)
        let decision = try #require(entries.first { $0.kind == TodayKinds.patchRequest })
        #expect(decision.recordID == "req-keys" && decision.properties?["decision"]?.text == "accepted" && decision.baseVersion == 1)
        #expect(decision.properties?["adjustedDiff"]?["properties"]?["prioritySetBy"]?.text == "owner")
        #expect(entries.contains { $0.recordID == "t-keys" && $0.properties?["priority"] == "high" })
        #expect(await !harness.state.requests.contains { $0.id == "req-keys" }, "a request decided here is hidden until it is read again")
    }

    @Test func theWrapLeadsTheClosingDayAndSavesWhereEachThingGoes() async throws {
        let pane = pane("22:15")
        #expect(pane.lead == "row:wrap")
        guard case let .wrap(card) = try #require(row(pane, "wrap")).body else { return #expect(Bool(false)) }
        #expect(card.carry.map(\.id).contains("p-vendor"))
        #expect(card.carry.allSatisfy { $0.options.selected?.id == "tomorrow" && $0.options.selected?.suggested == true })
        #expect(card.save.button?.title == "Save and close the day")
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("22:15"), performsEffects: false)
        await load(harness, "22:15")
        await harness.send(.carry(task: "t-pharmacy", option: "drop"))
        #expect(writes(harness).isEmpty)
        await harness.send(.saveWrap)
        let written = writes(harness).flatMap(\.entries)
        #expect(written.first { $0.recordID == "t-pharmacy" }?.properties == ["status": "abandoned"])
        let vendor = try #require(written.first { $0.recordID == "p-vendor" })
        #expect(vendor.properties?["notBefore"] == .string("2026-11-20T00:00:00Z") && vendor.properties?["deferrals"] == 1)
        #expect(written.last?.properties?["wrapSavedAt"] == .string("2026-11-19T22:15:00Z"))
    }
}

@Suite("Urgent", .timeLimit(.minutes(10)))
struct UrgentTests {
    @Test func somethingUrgentTheAssistantCheckedInterruptsAndParksTheTask() throws {
        let pane = pane("11:40")
        #expect(pane.lead == "row:urgent:inc-2" && pane.ground == .urgent)
        let urgent = try #require(row(pane, "urgent:inc-2"))
        guard case let .urgent(card) = urgent.body else { return #expect(Bool(false)) }
        #expect(card.setBy == "An automation marked it urgent at 11:38.")
        #expect(card.voices == [.init(who: "Moss", says: "agrees it cannot wait.")])
        #expect(card.unchecked == nil)
        #expect(card.took == "It takes the place of verify the safety vendor endpoint claims, which waits where you left it.")
        // No option is selected in advance, and the owner can only take urgent off.
        #expect(card.options.selected == nil && card.options.rows.map(\.name) == ["Not now", "Not urgent"])
        // What is happening never repeats the thing that leads.
        guard case let .happening(happening)? = pane.items.first(where: { if case .happening = $0 { true } else { false } }) else { return }
        #expect(!happening.groups.flatMap(\.items).contains { $0.id.contains("inc-2") })
    }

    @Test func somethingTheAssistantHoldsBackWaitsInWhatIsHappening() throws {
        let pane = pane("16:35")
        #expect(pane.ground != .urgent)
        guard case let .happening(happening)? = pane.items.first(where: { if case .happening = $0 { true } else { false } }) else {
            return #expect(Bool(false))
        }
        #expect(happening.pill == .urgent && happening.line.hasPrefix("One thing is marked urgent and waits here."))
        let lantern = try #require(happening.groups.first { $0.name == "Lantern" })
        let item = try #require(lantern.items.first)
        #expect(item.pill == .urgent, "first in its project's group, with the urgent pill")
        let marked = try #require(item.marked)
        #expect(marked.options.rows.map(\.name) == ["Take it now", "Leave it here", "Not urgent"])
        // A tap takes effect at once: the assistant's option has the pill and nothing is selected.
        #expect(marked.options.selected == nil && marked.options.rows[1].suggested)
        // Why it can wait is said once, on the thing itself, and not again under the option.
        #expect(item.source == "Moss thinks it can wait. It is a task for Friday morning, and Wei is here until Monday.")
        #expect(marked.options.rows[1].reason == nil)
    }

    @Test func anUncheckedMarkWaitsTwoMinutesThenInterruptsAndSaysSo() throws {
        // Set at 16:22 by an automation; nobody has written a check.
        let waiting = pane("16:23", held: false, reachable: false)
        #expect(waiting.ground != .urgent)
        let pane = pane("16:35", held: false, reachable: false)
        #expect(pane.lead == "row:urgent:t-wei" && pane.ground == .urgent)
        guard case let .urgent(card) = try #require(row(pane, "urgent:t-wei")).body else { return #expect(Bool(false)) }
        #expect(card.unchecked == "Moss cannot be reached, so nobody has checked this.")
        let engine = DayEngine(
            TodayPresenter.day(TodayFixtures.state(at: "16:23", held: false), now: TodayFixtures.at("16:23"), calendar: TodayFixtures.calendar))
        #expect(engine.nextChange == TodayFixtures.at("16:24"), "the pane is drawn again when the wait ends")
    }

    @Test func notUrgentPutsBackTheFlagItHadAndTheAppNeverWritesUrgent() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("16:35"), performsEffects: false)
        await load(harness, "16:35")
        await harness.send(.urgent(task: "t-wei", .notUrgent))
        let entry = try #require(writes(harness).flatMap(\.entries).first)
        #expect(
            entry.properties == [
                "priority": "medium", "prioritySetBy": "owner", "priorityReason": .null, "priorityWas": .null, "urgentCheckedAt": .null,
                "urgentHeldAt": .null, "urgentHeldReason": .null,
            ])
        await harness.send(.setFlag(task: "p-vendor", .low))
        for write in writes(harness).flatMap(\.entries) { #expect(write.properties?["priority"] != "urgent") }
        // "Take it now" is the owner's choice, kept on the device: it writes no record.
        let other = ControllerHarness(TodayFixtures.app, environment: environment("16:35"), performsEffects: false)
        await load(other, "16:35")
        await other.send(.urgent(task: "t-wei", .takeNow))
        #expect(writes(other).isEmpty)
        #expect(await other.state.notes?.urgentTaken["t-wei"] == TodayFixtures.at("16:35"))
        #expect(await other.presentation.pane.lead == "row:urgent:t-wei")
    }
}

@Suite("Writes", .timeLimit(.minutes(10)))
struct WriteTests {
    @Test func aRoutineRowLogsEveryOccurrenceUnderTheOccurrencesOwnID() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("07:05"), performsEffects: false)
        await load(harness, "07:05")
        let pane = await harness.presentation.pane
        let routine = try #require(rows(pane).first)
        await harness.send(.log(row: routine.id, done: true))
        let entries = writes(harness).flatMap(\.entries)
        #expect(entries.count == 5 && entries.allSatisfy { $0.method == .put && $0.kind == TodayKinds.log && $0.baseVersion == 0 })
        // The steps of the group sit a minute apart: each is logged at its own slot's id.
        #expect(Set(entries.map(\.recordID)).count == 5)
        let vitd = try #require(entries.first { $0.recordID == "vitd_20261119T073300Z" })
        #expect(
            vitd.properties == [
                "series": "substrate.reamde.dev/health/medicationschedule/vitd", "scheduledAt": "2026-11-19T07:33:00Z", "at": "2026-11-19T07:05:00Z",
            ])
        // The row is done at once, laid over before the server answers.
        #expect(rows(await harness.presentation.pane).first?.id != routine.id)
    }

    @Test func aFinishWaitsAndCanBeTakenBackAndIsNeverLost() {
        let start = Date(timeIntervalSince1970: 1_800_000_000)
        func at(_ seconds: TimeInterval) -> Date { start.addingTimeInterval(seconds) }
        var finishing = Finishing()
        // A command that finishes nothing does not wait.
        #expect(!finishing.begin(.reopen("a"), at: start) && finishing.isEmpty && TodayCommand.unlog("x").finishing == nil)
        #expect(finishing.begin(.done("a"), at: start) && finishing.command("task:a") == .done("a"))
        // For three seconds the control counts down, then for two more the thing is drawn finished; nothing is due.
        #expect(finishing.phase("task:a", at: at(0)) == .counting && finishing.left("task:a", at: at(1.5)) == 0.5)
        #expect(finishing.phase("task:a", at: at(2.9)) == .counting && finishing.phase("task:a", at: at(3)) == .settled)
        #expect(finishing.next(after: at(0)) == at(3) && finishing.next(after: at(3)) == at(5))
        #expect(finishing.due(at: at(4.9)).isEmpty && finishing.phase("task:a", at: at(4.9)) == .settled)
        // Then it is sent, once.
        #expect(finishing.due(at: at(5)) == [.done("a")] && finishing.isEmpty && finishing.due(at: at(9)).isEmpty)
        #expect(finishing.next(after: at(5)) == nil && finishing.phase("task:a", at: at(5)) == nil)

        // Taken back while it counts, or while it is drawn finished: nothing is ever sent.
        finishing.begin(.done("b"), at: start)
        finishing.cancel("task:b")
        finishing.begin(.log(row: "r", done: true), at: start)
        #expect(finishing.phase("r", at: at(4)) == .settled)
        finishing.cancel("r")
        #expect(finishing.isEmpty && finishing.due(at: at(60)).isEmpty)

        // One item ticked, then the whole group: both are sent, the item first, and what was begun later waits its own time.
        finishing.begin(.logOccurrence("o1", done: true), at: at(0))
        finishing.begin(.log(row: "r", done: true), at: at(2))
        #expect(finishing.due(at: at(5)) == [.logOccurrence("o1", done: true)] && finishing.command("r") != nil)
        #expect(finishing.due(at: at(7)) == [.log(row: "r", done: true)])
        // A second way of finishing the same thing takes the place of the first.
        finishing.begin(.log(row: "r", done: true), at: at(0))
        finishing.begin(.log(row: "r", done: false), at: at(1))
        #expect(finishing.pending.count == 1 && finishing.command("r") == .log(row: "r", done: false))
        // Leaving the app sends everything that waits, in order, at once.
        finishing.begin(.done("c"), at: at(1.5))
        #expect(finishing.flush() == [.log(row: "r", done: false), .done("c")] && finishing.isEmpty)
    }

    @Test func whatIsFinishedStaysInTheDayAsARowThatTakesItBack() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("07:05"), performsEffects: false)
        await load(harness, "07:05")
        let routine = try #require(rows(await harness.presentation.pane).first)
        let task = try #require(rows(await harness.presentation.pane).first { $0.id.hasPrefix("task:") })
        func earlier() async -> EarlierPresentation? {
            for item in await harness.presentation.pane.items { if case let .earlier(earlier) = item { return earlier } }
            return nil
        }
        #expect(await earlier()?.rows.contains { $0.id == routine.id || $0.id == task.id } != true)
        await harness.send(.log(row: routine.id, done: true))
        await harness.send(.done(String(task.id.dropFirst("task:".count))))
        let done = try #require(await earlier())
        // Each is a closed row of what is done, with when, and a filled tick that takes it back.
        let taken = try #require(done.rows.first { $0.id == routine.id })
        #expect(taken.state == .done && taken.title == routine.title && taken.line == "Taken at 07:05.")
        guard case let .tick(back) = taken.trailing, case .routine = taken.body else { return #expect(Bool(false)) }
        #expect(back.role == .primary && back.command == .unlogRow(routine.id) && back.command?.finishing == nil)
        let finished = try #require(done.rows.first { $0.id == task.id })
        #expect(
            finished.state == .done && finished.line == "Done at 07:05."
                && finished.trailing
                    == .tick(
                        ButtonPresentation(
                            "Open again", glyph: .done, iconOnly: true, role: .primary, command: .reopen(String(task.id.dropFirst("task:".count)))))
        )
        #expect(done.count == "\(done.entries.count) done")
        // Taking the routine back removes every log it has, and it is a row of the day again.
        let before = writes(harness).flatMap(\.entries).count
        await harness.send(.unlogRow(routine.id))
        let removed = writes(harness).flatMap(\.entries).dropFirst(before)
        #expect(removed.count == 5 && removed.allSatisfy { $0.method == .delete && $0.kind == TodayKinds.log })
        #expect(rows(await harness.presentation.pane).first?.id == routine.id)
        #expect(await earlier()?.rows.contains { $0.id == routine.id } != true)
        // A row skipped says so.
        await harness.send(.log(row: routine.id, done: false))
        #expect(await earlier()?.rows.first { $0.id == routine.id }?.line == "Skipped at 07:05.")
    }

    @Test func oneItemOfAGroupIsTickedByItselfAndTheGroupsButtonLogsTheRest() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("07:05"), performsEffects: false)
        await load(harness, "07:05")
        let routine = try #require(rows(await harness.presentation.pane).first)
        func card() async throws -> RoutineCard {
            let row = try #require(rows(await harness.presentation.pane).first { $0.id == routine.id })
            guard case let .routine(card) = row.body else { throw CancellationError() }
            return card
        }
        let item = try #require(try await card().items.first { $0.name == "Vitamin D" })
        await harness.send(try #require(item.tick?.command))
        let first = writes(harness).flatMap(\.entries)
        #expect(first.count == 1 && first[0].recordID == item.id && first[0].method == .put && first[0].kind == TodayKinds.log)
        // Its line shows as taken with a filled tick that takes the log back; the row counts it and stays open.
        #expect(rows(await harness.presentation.pane).first { $0.id == routine.id }?.line == "1 of 5 logged.")
        let ticked = try #require(try await card().items.first { $0.id == item.id })
        #expect(ticked.logged == "Taken at 07:05." && ticked.tick?.role == .primary && ticked.tick?.command == .unlog(item.id))
        #expect(try await card().done?.title == "All taken")
        await harness.send(.unlog(item.id))
        #expect(writes(harness).flatMap(\.entries).last?.method == .delete)
        #expect(rows(await harness.presentation.pane).first { $0.id == routine.id }?.line == "5 items.")
        // The group's button logs what is not ticked yet, and the row is done.
        await harness.send(.logOccurrence(item.id, done: true))
        let before = writes(harness).flatMap(\.entries).count
        await harness.send(.log(row: routine.id, done: true))
        let rest = writes(harness).flatMap(\.entries).dropFirst(before)
        #expect(rest.count == 4 && !rest.contains { $0.recordID == item.id })
        #expect(rows(await harness.presentation.pane).first?.id != routine.id)
    }

    @Test func anAnswerToWhatYesterdayLeftStaysWithItsChoiceAndTheChoiceMoves() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("07:05"), performsEffects: false)
        await load(harness, "07:05")
        func card() async -> YesterdayCard? {
            for row in rows(await harness.presentation.pane) { if case let .yesterday(card) = row.body { return card } }
            return nil
        }
        let before = try #require(await card())
        let dose = try #require(before.items.first { $0.title == "Weekly medication" })
        #expect(dose.options.rows.allSatisfy { !$0.selected } && dose.chosen == nil && dose.text == "It was due yesterday at 07:30. Not logged.")
        await harness.send(.answerYesterday(item: dose.id, option: "took"))
        let first = writes(harness).flatMap(\.entries)
        #expect(first.map(\.method) == [.put] && first[0].recordID == "b12_20261118T073000Z")
        // Choosing hides nothing: while another thing waits, the answered one stays in the card with its choice filled.
        if let after = await card() {
            let answered = try #require(after.items.first { $0.id == dose.id })
            #expect(answered.options.rows.first { $0.id == "took" }?.selected == true && answered.chosen == "Chosen at 07:05.")
            #expect(answered.text == "It was due yesterday at 07:30.")
        }
        // What is done names the thing and its answer: the same words for each answered thing would say nothing.
        func done() async -> [String] {
            for item in await harness.presentation.pane.items {
                if case let .earlier(earlier) = item { return earlier.lines.map(\.text).filter { $0.hasPrefix("From yesterday") } }
            }
            return []
        }
        #expect(await done() == ["From yesterday: weekly medication, taken."])
        // Another tap moves the choice: the log that answer made is changed, not made again.
        harness.clearTrace()
        await harness.deliver(.records(.enqueued(Set(first.map(\.id)), queue: first, landed: [])))
        await harness.send(.answerYesterday(item: dose.id, option: "skipped"))
        let second = writes(harness).flatMap(\.entries)
        #expect(second.map(\.method) == [.patch] && second[0].recordID == "b12_20261118T073000Z" && second[0].properties == ["status": "skipped"])
        #expect(await harness.state.notes?.yesterday[dose.id] == "skipped")
        #expect(await done() == ["From yesterday: weekly medication, skipped."])
    }

    @Test func aSkipIsACreateThenAPatchOfItsState() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("07:38"), performsEffects: false)
        await load(harness, "07:38")
        await harness.send(.logOccurrence("skin_20261119T073500Z", done: false))
        let entries = writes(harness).flatMap(\.entries)
        #expect(entries.map(\.method) == [.put, .patch])
        #expect(entries[1].properties == ["status": "skipped"])
    }

    @Test func aFlagWriteIsTheOwnersAndClearsWhatUrgentLeft() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("11:30"), performsEffects: false)
        await load(harness, "11:30")
        await harness.send(.setFlag(task: "t-lin", .high))
        let entry = try #require(writes(harness).flatMap(\.entries).first)
        #expect(entry.method == .patch && entry.guarded && entry.properties?["priority"] == "high" && entry.properties?["prioritySetBy"] == "owner")
        #expect(entry.properties?["urgentHeldAt"] == .null)
        // The order holds until the list is closed.
        #expect(await harness.state.drafts.held["t-lin"] == .medium)
        await harness.send(.settleFlags)
        #expect(await harness.state.drafts.held.isEmpty)
    }

    @Test func whenWritesNotBeforeAndCountsADeferral() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("11:30"), performsEffects: false)
        await load(harness, "11:30")
        guard case let .task(task) = await harness.presentation.page(.task("p-vendor")) else { return #expect(Bool(false)) }
        let when = try #require(task.card.doors.first { $0.id == "when" })
        #expect(
            when.options.rows.map(\.name) == [
                "Today, where it is", "The 16:30 gap", "The 18:15 gap", "The 20:30 gap", "Tonight, after 21:30", "Tomorrow", "Next week",
            ])
        #expect(when.value == "Today, where it is")
        await harness.send(.setWhen(task: "p-vendor", option: "tomorrow"))
        let entry = try #require(writes(harness).flatMap(\.entries).first)
        #expect(entry.properties == ["notBefore": "2026-11-20T00:00:00Z", "deferrals": 1])
        // From the third time, the row says how many times.
        guard case let .task(passport) = await harness.presentation.page(.task("t-passport")) else { return #expect(Bool(false)) }
        #expect(passport.card.deferred == "Put off three times.")
    }

    @Test func choosingTheSuggestedFlagAcceptsTheRequestAndAnotherRejectsIt() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("08:30"), performsEffects: false)
        await load(harness, "08:30")
        // Moss suggests Low for GitHub orgs, and adding Security hardening as High.
        let strip = await harness.presentation.pane.strip
        #expect(strip.entries.first { $0.id == "ghorgs" }?.suggestion == "to Low")
        let add = try #require(strip.entries.first { $0.id == "security" })
        #expect(add.suggestion == "add as High" && add.stripe == Stripe(.high, suggested: true) && add.tile.suggested && add.opens == .priorities)
        await harness.send(.setPriority(project: "ghorgs", .low))
        await harness.send(.setPriority(project: "security", .medium))
        let entries = writes(harness).flatMap(\.entries)
        let decisions = entries.filter { $0.kind == TodayKinds.patchRequest }
        #expect(decisions.map(\.recordID) == ["req-ghorgs", "req-security"])
        #expect(decisions.map { $0.properties?["decision"]?.text } == ["accepted", "rejected"])
        #expect(decisions.allSatisfy { $0.baseVersion == 1 }, "a decision carries the version the owner saw")
        #expect(entries.filter { $0.kind == TodayKinds.list }.map { $0.properties?["priority"]?.text } == ["low", "medium"])
        #expect(await harness.state.requests.isEmpty)
    }

    @Test func gotItWritesTheReviewMarkAndTheNightIsReadInThePreference() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("11:30"), performsEffects: false)
        await load(harness, "11:30")
        await harness.send(.gotIt(project: nil))
        let marks = writes(harness).flatMap(\.entries).filter { $0.kind == TodayKinds.reviewmark }
        #expect(Set(marks.map(\.recordID)) == ["lantern", "security", "hiring"])
        #expect(marks.first { $0.recordID == "lantern" }?.properties == ["through": "2026-11-19T11:30:00Z"])
        let night = ControllerHarness(TodayFixtures.app, environment: environment("08:20"), performsEffects: false)
        await load(night, "08:20")
        await night.send(.readNight)
        #expect(writes(night).flatMap(\.entries).last?.properties == ["nightReadAt": "2026-11-19T08:20:00Z"])
    }

    @Test func whatIsTypedGoesToTheAssistantAndIsKeptAsATaskWhereItCannotBeSent() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("11:30"), performsEffects: false)
        await load(harness, "11:30")
        // A task's name is sent as it was typed, lines and all, and nothing is written here.
        await harness.send(.addOrAsk(" send Callum the snapshot restore numbers\nand the runbook "))
        guard case let .ask(told, words, _)? = harness.effects.last(where: { if case .ask = $0 { true } else { false } }) else {
            return #expect(Bool(false))
        }
        #expect(told.subject == .day && words == "send Callum the snapshot restore numbers\nand the runbook" && writes(harness).isEmpty)
        #expect(await harness.presentation.pane.answer?.heading == "You said to Moss")
        #expect(await harness.presentation.pane.answer?.keep == nil, "nothing is kept while the assistant has the words")
        // Where the assistant could not take them, they are kept as a task the owner asks for.
        await harness.deliver(.answer(told.id, .failed("Moss could not be reached. This iPhone is offline.")))
        let failed = try #require(await harness.presentation.pane.answer)
        #expect(failed.state == .failed && failed.keep?.title == "Add as a task" && failed.keep?.command == .addUnanswered(told.id))
        await harness.send(.addUnanswered(told.id))
        let entry = try #require(writes(harness).flatMap(\.entries).first)
        #expect(
            entry.kind == TodayKinds.task && entry.method == .put
                && entry.properties == ["name": "Send Callum the snapshot restore numbers and the runbook"])
        let pane = await harness.presentation.pane
        let added = try #require(row(pane, "task:" + entry.recordID))
        #expect(added.stripe == .none && added.line == "You added it at 11:30.")
        #expect(pane.composer.added == "task:" + entry.recordID, "the day scrolls to the row")
        #expect(pane.answer?.state == .done && pane.answer?.text == "Added as a task. Moss did not see it." && pane.answer?.keep == nil)
        await harness.send(.addUnanswered(told.id))
        #expect(writes(harness).flatMap(\.entries).count == 1, "an exchange is kept once")

        await harness.send(.addOrAsk("What can I drop this afternoon?"))
        guard case let .ask(exchange, message, context)? = harness.effects.last(where: { if case .ask = $0 { true } else { false } }) else {
            return #expect(Bool(false))
        }
        #expect(exchange.subject == .day && message == "What can I drop this afternoon?" && context.isEmpty)
        #expect(await harness.presentation.pane.answer?.state == .waiting)
        // What it says before a step of work is a paragraph of its own.
        await harness.deliver(.answer(exchange.id, .step))
        await harness.deliver(.answer(exchange.id, .delta("Reading the afternoon.")))
        await harness.deliver(.answer(exchange.id, .step))
        await harness.deliver(.answer(exchange.id, .step))
        await harness.deliver(.answer(exchange.id, .delta("Callum is at the standup too.")))
        await harness.deliver(.answer(exchange.id, .done(nil)))
        let answer = try #require(await harness.presentation.pane.answer)
        #expect(answer.text == "Reading the afternoon.\n\nCallum is at the standup too." && answer.state == .done && answer.keep == nil)
    }

    @Test func aQuestionAboutATaskCarriesItsRecordAndAFailureIsSaidInPlace() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("11:30"), performsEffects: false)
        await load(harness, "11:30")
        await harness.send(.ask(.task("p-vendor"), "Break it into steps"))
        guard case let .ask(exchange, _, context)? = harness.effects.last(where: { if case .ask = $0 { true } else { false } }) else {
            return #expect(Bool(false))
        }
        #expect(context == ["\(TodayKinds.task)/p-vendor"])
        await harness.deliver(.answer(exchange.id, .failed("Moss could not be reached. The server did not answer.")))
        guard case let .task(page) = await harness.presentation.page(.task("p-vendor")) else { return #expect(Bool(false)) }
        #expect(page.card.moss?.exchanges.first?.state == .failed)
        #expect(page.card.moss?.unreachable == "Moss cannot be reached right now.")
    }

    @Test func theAssistantsStreamIsReadLineByLine() {
        #expect(Assistant.parse(#"{"kind":"thread","thread":"t1"}"#) == .thread("t1"))
        #expect(Assistant.parse(#"{"kind":"delta","turn":1,"text":"Two "}"#) == .delta("Two "))
        #expect(Assistant.parse("{}") == nil)
        #expect(Assistant.parse(#"{"kind":"toolStarted","name":"query"}"#) == .step)
        #expect(Assistant.parse(#"{"kind":"toolFinished","name":"query"}"#) == nil)
        #expect(Assistant.parse(#"{"kind":"done","result":{"reply":"Two things.","status":"completed"}}"#) == .done("Two things."))
        #expect(Assistant.parse(#"{"kind":"error","text":"The provider refused."}"#) == .failed("The provider refused."))
        let body = Assistant.body(message: "Why?", context: (0 ..< 12).map { "a.b/c/d/\($0)" }, timezone: "Europe/Lisbon", messageID: "m1")
        #expect(body["context"]?.array?.count == 8, "at most eight records go with a turn")
        #expect(body["messageId"] == "m1" && body["thread"] == "")
    }
}

@Suite("Reads, pages and reminders", .timeLimit(.minutes(10)))
struct ReadTests {
    @Test func theCodecReadsEveryKindTolerantly() throws {
        var day = Day(now: TodayFixtures.at("11:30"), calendar: TodayFixtures.calendar)
        day.discovery = TodayFixtures.discovery
        day.take(TodayFixtures.records(at: TodayFixtures.at("11:30")))
        #expect(day.lists.count == 11 && day.tasks.count == 28)
        let vendor = try #require(day.task("p-vendor"))
        #expect(vendor.flag == .high && vendor.list == "extapi" && vendor.dueDay == CivilDay("2026-11-19") && vendor.setBy == .owner)
        #expect(day.series.count == 14 && day.series.values.filter(\.isDose).count == 10)
        #expect(day.series["\(TodayFixtures.scheduleKind)/vitd"]?.dose == "1 tablet")
        #expect(day.strengths["\(TodayKinds.medication)/vitd"] == "1,000 IU" && day.strengths["\(TodayKinds.medication)/omega"] == nil)
        #expect(day.plan?.picked.count == 13 && day.plan?.overlaps.count == 4)
        #expect(day.preference.urgentWait == 120 && day.preference.nightReadAt == TodayFixtures.at("08:27"))
        let occurrence = try #require(Occurrence(TodayFixtures.occurrences[0]))
        #expect(occurrence.id == "mag_20261118T073000Z" && occurrence.series == "\(TodayFixtures.scheduleKind)/mag")
        #expect(Occurrence.slot("20261118T073000Z") == TodayFixtures.at("07:30", day: -1) && Occurrence.stamp(occurrence.slot) == "20261118T073000Z")
        // A property of another shape reads as absent, never as a crash.
        let odd = Record(id: "x", kind: TodayKinds.task, properties: ["name": 3, "priority": ["a"], "people": "nobody", "dueDay": "soon"])
        #expect(TaskItem(odd)?.name == "Untitled task" && TaskItem(odd)?.flag == nil && TaskItem(odd)?.people == [])
        #expect(
            Meeting(Record(id: "m", kind: "a.b/c/d", properties: ["at": "2026-11-19T10:00:00Z", "endsAt": "2026-11-19T10:30:00Z", "status": "cancelled"]))
                == nil)
    }

    @Test func aRepositoryWithoutThePackagesDrawsAnEmptyPaneAndWritesNothing() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("11:30"), performsEffects: false)
        await harness.deliver(.discovered(Discovery(taskMoves: false, listPriority: false)))
        let pane = await harness.presentation.pane
        #expect(pane.empty != nil && rows(pane).isEmpty)
        await harness.send(.addOrAsk("Buy milk"))
        await harness.send(.readNight)
        #expect(writes(harness).isEmpty, "a kind the repository lacks is never written")
        // And a read over no kinds is never asked for.
        #expect(!harness.effects.contains { if case .records(.read) = $0 { true } else { false } })
    }

    @Test func discoveryOpensTheLinkOverTheKindsFoundAndKeepsThem() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("11:30"), performsEffects: false)
        await harness.deliver(.records(.loaded([], cursor: nil, queue: [])))
        await harness.deliver(.discovered(TodayFixtures.discovery))
        #expect(await harness.state.linked.kinds == TodayFixtures.discovery.linked)
        #expect(harness.effects.contains(.saveDiscovery(TodayFixtures.discovery)) && harness.effects.contains(.readWindows(TodayFixtures.discovery)))
        #expect(TodayFixtures.discovery.linked.contains(TodayFixtures.scheduleKind) && !TodayFixtures.discovery.linked.contains(TodayFixtures.eventKind))
    }

    @Test func thePagesAnswerTheirRoutes() throws {
        guard case let .priorities(priorities) = page("08:30", .priorities) else { return #expect(Bool(false)) }
        #expect(priorities.entries.map(\.name) == ["EU AI Act", "External API reliability", "Lantern", "GitHub orgs", "Hiring"])
        #expect(priorities.suggested.map(\.name) == ["Security hardening"] && priorities.suggestedTitle == "Moss suggests adding")
        let orgs = try #require(priorities.entries.first { $0.id == "ghorgs" })
        #expect(orgs.flag.open && orgs.flag.options.selected?.id == "medium" && orgs.flag.options.rows.first { $0.suggested }?.id == "low")
        #expect(orgs.remove?.command == .removePriority(project: "ghorgs"))
        #expect(priorities.add.mark == .square && !priorities.add.rows.contains { $0.id == "security" })

        guard case let .projects(projects) = page("11:30", .projects) else { return #expect(Bool(false)) }
        #expect(projects.priorities.count == 6 && projects.others.map(\.name) == ["Bastion", "Home", "Northfield bring-up", "Paperwork", "Tally"])

        guard case let .project(project) = page("11:30", .project("security")) else { return #expect(Bool(false)) }
        #expect(project.tasks.map(\.id) == ["t-vigil", "p-ticket"] && project.done.map(\.id).sorted() == ["p-kenji", "t-proxy"])
        #expect(project.notToday.map(\.id) == ["p-origin"])
        #expect(project.timeline.map(\.label) == ["Today", "Overnight"])

        guard case let .person(person) = page("11:30", .person(TodayFixtures.person("tomas"))) else { return #expect(Bool(false)) }
        #expect(person.name == "Tomas Varga" && person.sees == "You see Tomas at 14:20." && person.tasks.map(\.id) == ["p-tomas"])
        #expect(person.badge.initials == "TV")

        guard case let .meeting(meeting) = page("11:30", .meeting("tomas")) else { return #expect(Bool(false)) }
        #expect(meeting.eyebrow == "14:20 to 14:40, in 2 h 50" && meeting.card.meta == "Google Meet, 2 people")

        guard case let .task(heard) = page("16:35", .task("h-tomas-pr")) else { return #expect(Bool(false)) }
        #expect(heard.suggestion?.save?.button?.command == .saveSuggested(["h-tomas-pr"], words: [:]) && heard.card.doors.isEmpty)
        guard case .missing = page("11:30", .task("gone")) else { return #expect(Bool(false)) }
    }

    @Test func aPersonIsNamedFromWhatTheContactHolds() throws {
        func name(_ properties: [String: JSONValue]) -> Person { Person(Record(id: "c", kind: TodayKinds.contact, properties: properties)) }
        // `name` when it has a capital.
        #expect(name(["name": "Tomas Varga", "givenName": "Tom", "title": "tvarga"]).name == "Tomas Varga")
        // A `name` in lower case is a handle: the given and family names are the name.
        let handled = name(["name": "odilem", "givenName": "Odile", "familyName": "Marchetti", "title": "odilem"])
        #expect(handled.name == "Odile Marchetti" && handled.short == "Odile" && handled.badge.initials == "OM")
        #expect(name(["givenName": "Odile", "nickname": "odi"]).name == "Odile")
        // No `name` and no parts: the title, when it is written as a name.
        #expect(name(["title": "Pilar Quint"]).name == "Pilar Quint" && name(["title": "Pilar Quint"]).short == "Pilar")
        // Else the nickname or the handle, with its first letter in upper case.
        #expect(name(["name": "rsato", "title": "rsato"]).name == "Rsato" && name(["name": "rsato"]).short == "Rsato")
        #expect(name(["nickname": "odi", "title": "odi"]).name == "Odi" && name(["title": "rsato@example.com"]).name == "Rsato")
        #expect(name([:]).name == "Someone")
        // A given name a source filled with a handle or an address is read as one: never "for christopher".
        let handle = name(["givenName": "odile"])
        #expect(handle.name == "Odile" && handle.short == "Odile")
        let address = name(["name": "pilar@example.com", "givenName": "pilar@example.com"])
        #expect(address.name == "Pilar" && address.short == "Pilar")
        #expect(
            name(["givenName": "ines.castel@example.com"]).name == "Ines Castel"
                && name(["givenName": "odile", "familyName": "marchetti"]).name == "Odile Marchetti")
        // The short name is the given name where there is one, else the first word.
        #expect(name(["name": "Dr Ines Castel", "givenName": "Ines"]).short == "Ines" && name(["name": "Ines Castel"]).short == "Ines")

        // Wherever a person is named: "for …", "with …", the circle, the page.
        guard case let .suggested(suggested) = page("08:30", .suggested, edit: TodayFixtures.crowd) else { return #expect(Bool(false)) }
        let items = suggested.groups.flatMap(\.items)
        let odile = try #require(items.first { $0.id == "b-078" })
        #expect(odile.chips.people.map(\.label) == ["for Odile"] && odile.chips.people.first?.badge.initials == "OM")
        #expect(odile.origin.hasSuffix(", with Odile."))
        let sato = try #require(items.first { $0.id == "b-065" })
        #expect(sato.chips.people.map(\.label) == ["for Rsato"] && sato.origin.hasSuffix(", with Rsato."))
        guard case let .person(person) = page("08:30", .person(TodayFixtures.person("odile")), edit: TodayFixtures.crowd) else {
            return #expect(Bool(false))
        }
        #expect(person.name == "Odile Marchetti" && person.tasksTitle == "Tasks with Odile" && person.moss?.title == "Moss, about Odile")
        // An attendee with no name written out is named by the handle of the address.
        let event = Record(
            id: "m", kind: TodayFixtures.eventKind,
            properties: [
                "summary": "Sync", "at": "2026-11-19T10:00:00Z", "endsAt": "2026-11-19T10:30:00Z",
                "attendees": [["value": "rsato@example.com"], ["displayName": "odilem", "value": "odile@example.com"]],
            ])
        #expect(Meeting(event)?.people.map(\.name) == ["Rsato", "Odilem"])
    }

    @Test func oneEventFromTwoCalendarsIsOneMeeting() throws {
        // The invented day holds the 08:00 call twice, from two accounts, and a "Busy" block over the 14:20 1:1.
        #expect(TodayFixtures.meetings.filter { ["physio", "physio-second", "tomas", "tomas-busy"].contains($0.id) }.count == 4)
        let day = TodayPresenter.day(TodayFixtures.state(at: "11:30"), now: TodayFixtures.at("11:30"), calendar: TodayFixtures.calendar)
        let call = try #require(day.meetings.first { $0.id == "physio" })
        #expect(day.meetings.filter { $0.start == call.start }.map(\.id) == ["physio"], "same start, end and title, without case or space")
        // Every attendee of each, once; where it is and how to join from whichever has them.
        #expect(call.people.map(\.name) == ["Dana Whitlock", "Ines Castel"] && call.count == 3 && call.also == ["physio-second"])
        #expect(call.place == "Room 2" && call.join == "https://meet.example.com/physio-second")
        // "Earlier today" lists it once, and nothing overlaps itself.
        let pane = pane("11:30")
        guard case let .earlier(earlier)? = pane.items.first(where: { if case .earlier = $0 { true } else { false } }) else { return #expect(Bool(false)) }
        let ended = earlier.entries.compactMap { entry -> MeetingRow? in if case let .meeting(row) = entry { row } else { nil } }
        #expect(ended.filter { $0.title.lowercased().contains("physiotherapy") }.map(\.id) == ["physio"] && ended.allSatisfy(\.ended))
        let waking = TodayCoreTests.pane("07:52")
        #expect(meetings(waking).filter { $0.start == "08:00" }.map(\.id) == ["physio"])
        #expect(!waking.items.contains { if case let .overlap(block) = $0 { block.meetings.contains { $0.start == "08:00" } } else { false } })
        // One page, under either id.
        guard case let .meeting(first) = page("11:30", .meeting("physio")), case let .meeting(second) = page("11:30", .meeting("physio-second")) else {
            return #expect(Bool(false))
        }
        #expect(first.id == "physio" && second.id == "physio" && first.card.meta == "Room 2, 3 people")
        // A "Busy" block at the time of a named meeting is its shadow: not drawn, and not an overlap.
        #expect(day.meetings.filter { $0.start == TodayFixtures.at("14:20") }.map(\.title) == ["Tomas"])
        #expect(day.meeting("tomas-busy")?.id == "tomas" && day.meeting("tomas")?.count == 2)
        #expect(!meetings(pane).contains { $0.title == "Busy" })
        let blocks = pane.items.compactMap { if case let .overlap(block) = $0 { block.card.name } else { nil } }
        #expect(blocks == ["The 15:00 overlap", "The 17:20 overlap", "The 17:40 overlap", "The 21:00 overlap"])
        // A "Busy" block with nothing named at its time is still drawn, and two of them are one.
        func busy(_ id: String) -> Record {
            Record(id: id, kind: TodayFixtures.eventKind, properties: ["summary": "Busy", "at": "2026-11-19T11:00:00Z", "endsAt": "2026-11-19T11:30:00Z"])
        }
        #expect(Meeting.merged([busy("b"), busy("a")].compactMap(Meeting.init)).map(\.id) == ["a"])
    }

    @Test func remindersNameNoMedicineAndStayUnderTheSystemsLimit() {
        let state = TodayFixtures.state(at: "07:05")
        let plan = TodayFixtures.present(state, at: "07:05").reminders
        #expect(plan.count <= 60 && plan.count == 18)
        let first = plan[0]
        #expect(first.title == "Morning medication" && first.body == "5 items" && first.fireAt == TodayFixtures.at("07:30"))
        let medicines = (TodayFixtures.morning.values.map(\.0) + TodayFixtures.evening.values.map(\.0))
        for reminder in plan { for medicine in medicines { #expect(!reminder.title.contains(medicine) && !reminder.body.contains(medicine)) } }
        let request = TodayReminders.request(first)
        #expect(request.category == TodayReminders.category && request.userInfo["row"] == first.row)
        #expect(TodayReminders.categoryValue.actions.map(\.title) == ["Taken", "Skip", "Later"])
        let response = NotificationResponse(id: first.id, category: TodayReminders.category, action: .named(TodayReminders.skip), userInfo: ["row": first.row])
        #expect(TodayReminders.command(for: response) == .log(row: first.row, done: false))
    }

    @Test func aGroupIsOneRowADayWhateverItsSlotsInSlotOrder() throws {
        // The morning doses sit a minute apart, 07:30 to 07:34, to keep their order.
        let pane = pane("07:05")
        let routine = rows(pane)[0]
        #expect(routine.id == "routine:g:Morning medication:2026-11-19" && routine.line == "5 items.")
        guard case let .routine(card) = routine.body else { return #expect(Bool(false)) }
        #expect(card.items.map(\.name) == ["Magnesium", "Omega-3", "Probiotic", "Vitamin D", "Zinc"])
        // A strength where the medication's record gives one, else the dose, written as people write them.
        #expect(card.items.map(\.detail) == ["200 mg", "1 capsule", "1 capsule", "1,000 IU", "15 mg"])
        // One reminder a day for the group, at its first slot.
        let plan = TodayFixtures.present(TodayFixtures.state(at: "07:05"), at: "07:05").reminders
        let morning = plan.filter { $0.title == "Morning medication" }
        #expect(morning.map(\.fireAt) == [TodayFixtures.at("07:30"), TodayFixtures.at("07:30", day: 1), TodayFixtures.at("07:30", day: 2)])
        #expect(morning.allSatisfy { $0.body == "5 items" })
        // Part of the way through the group's minutes it is still one row, and no reminder is planned for a slot gone by.
        let midway = TodayFixtures.present(TodayFixtures.state(at: "07:32"), at: "07:32")
        #expect(rows(midway.pane).first?.id == routine.id)
        #expect(midway.reminders.filter { $0.title == "Morning medication" }.count == 2)
    }

    @Test func aRowOfOneThingListsNoItems() throws {
        let pane = pane("07:38")
        let skin = try #require(rows(pane).first { $0.title == "Skincare" })
        guard case let .routine(card) = skin.body else { return #expect(Bool(false)) }
        #expect(card.items.isEmpty && skin.line == "At 07:35.")
    }

    @Test func amountsAreWrittenAsPeopleWriteThem() {
        #expect(Words.amount(1000, "iu") == "1,000 IU" && Words.amount(200, "mg") == "200 mg" && Words.amount(100, "mcg") == "100 mcg")
        #expect(Words.amount(5, "percent") == "5%" && Words.amount(Decimal(string: "0.5")!, "ml") == "0.5 ml" && Words.amount(12500, "units") == "12,500 units")
        #expect(Words.amount(1, "capsule") == "1 capsule" && Words.amount(2, "capsule") == "2 capsules" && Words.amount(2, "gummy") == "2 gummies")
        #expect(Words.amount(3, nil) == "3")
    }

    @Test func noTwoProjectsShareInitials() {
        #expect(Words.initials("EU AI Act", last: false) == "EU" && Words.initials("External API reliability", last: false) == "EA")
        #expect(Words.initials("GitHub orgs", last: false) == "GO" && Words.initials("Tomas Varga", last: true) == "TV")
        let strip = pane("11:30").strip.entries
        #expect(strip.first { $0.name == "EU AI Act" }?.tile.initials == "EU")
        #expect(strip.first { $0.name == "External API reliability" }?.tile.initials == "EA")
        #expect(Set(strip.map(\.tile.initials)).count == strip.count)
        // One whose initials are taken is drawn by the first two letters of its first word.
        let lists = ["Personal; Health", "Personal; Home", "Personal; Other", "Platform hiring"].enumerated().map { index, name in
            ListItem(Record(id: "l\(index)", kind: TodayKinds.list, properties: ["name": .string(name), "status": "active"], version: 1))!
        }
        let drawn = DayEngine.initials(lists)
        #expect(drawn["l0"] == "PH" && drawn["l1"] == "PE" && drawn["l2"] == "PO")
        #expect(Set(drawn.values).count >= 3)
    }

    @Test func theAssistantsBoxKeepsItsSubjectsCapitals() throws {
        guard case let .project(project) = page("11:30", .project("extapi")) else { return #expect(Bool(false)) }
        #expect(project.moss?.title == "Moss, about External API reliability")
        guard case let .meeting(meeting) = page("11:30", .meeting("tomas")) else { return #expect(Bool(false)) }
        #expect(meeting.card.moss?.title.hasPrefix("Moss, about Tomas") == true)
    }

    @Test func settingsListTheAssistantsInstructionsAndWriteGuardedPatches() async throws {
        let instruction = Record(
            id: "today-suggest", kind: TodayKinds.instruction,
            properties: [
                "agent": .string("substrate.reamde.dev/core/agent/\(TodayKinds.assistant)"), "text": "Suggest, never act.", "active": true,
                "source": "import",
            ], version: 3, createdAt: TodayFixtures.stamp(TodayFixtures.at("09:00", day: -3)))
        let other = Record(
            id: "someone-elses", kind: TodayKinds.instruction,
            properties: ["agent": "substrate.reamde.dev/core/agent/example.com/helpers/other", "text": "Not Moss's."], version: 1)
        let edit: (inout TodayApp.State) -> Void = { state in
            for record in [instruction, other] { state.linked.records["\(record.kind)/\(record.id)"] = record }
        }
        let shown = TodayFixtures.present(TodayFixtures.state(at: "11:30", edit: edit), at: "11:30")
        #expect(shown.moss.rows == [MossSettings.Row(id: "today-suggest", text: "Suggest, never act.", on: true, source: "Came with a package")])
        #expect(shown.settings.first?.rows.map(\.id) == [.app("assistantPrepares"), .app("prepareAt"), .app("urgentWait")])

        let harness = ControllerHarness(TodayFixtures.app, environment: environment("11:30"), performsEffects: false)
        await load(harness, "11:30", edit: edit)
        await harness.send(.instruction("today-suggest", on: false))
        await harness.send(.rewriteInstruction("today-suggest", text: "  Suggest, and never act.  "))
        await harness.send(.settings(.choose(row: .app("urgentWait"), option: "300")))
        await harness.send(.settings(.toggle(row: .app("assistantPrepares"), on: false)))
        let entries = writes(harness).flatMap(\.entries)
        #expect(entries.count == 4 && entries.allSatisfy { $0.method == .patch })
        #expect(entries[0].kind == TodayKinds.instruction && entries[0].recordID == "today-suggest" && entries[0].properties == ["active": false])
        #expect(entries[0].baseVersion == 3)
        #expect(entries[1].properties == ["text": "Suggest, and never act."])
        #expect(entries[2].kind == TodayKinds.preference && entries[2].properties == ["urgentWait": "PT300S"])
        #expect(entries[3].properties == ["assistantPrepares": false])
    }

    @Test func theManifestIsTheRequestAppJSONRecords() throws {
        struct Recorded: Decodable {
            struct Ask: Decodable, Equatable {
                var package: String
                var atLeast: Int
            }
            var name: String
            var scopes: [String]
            var asks: [Ask]
            var publishes: [String]
            var apple: [String: JSONValue]
        }
        let file = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("../../../app.json").standardized
        let recorded = try JSONDecoder().decode(Recorded.self, from: Data(contentsOf: file))
        let manifest = TodayManifest.manifest(clientID: TodayManifest.localClientID)
        #expect(recorded.name == manifest.appName)
        #expect(recorded.scopes == manifest.scopes && manifest.scopes == ["full"])
        #expect(recorded.asks.isEmpty)
        #expect(recorded.publishes.isEmpty)
        #expect(manifest.details.isEmpty)
        #expect(recorded.apple["clientIDKey"] == .string(TodayManifest.clientIDKey))
        #expect(recorded.apple["redirectKey"] == .string(TodayManifest.redirectKey))
    }

    @Test func everyFixtureEncodesAndEveryRouteItNamesIsAPage() throws {
        #expect(TodayFixtures.all.count >= 18)
        for fixture in TodayFixtures.all {
            _ = try JSONEncoder().encode(fixture.app)
            for route in fixture.routes {
                if case .missing = fixture.app.page(route) { #expect(Bool(false), "\(fixture.name) \(route)") }
            }
        }
    }

    @Test func aNewDayStartsTheNightAtTheLastLookOfTheDayBefore() {
        var old = DayNotes(day: CivilDay(year: 2026, month: 11, day: 18))
        old.wokeAt = TodayFixtures.at("07:10", day: -1)
        old.lastLook = TodayFixtures.at("01:57")
        let fresh = TodayApp.rolled(old, at: TodayFixtures.at("07:05"), today: CivilDay(year: 2026, month: 11, day: 19))
        #expect(fresh.nightFrom == TodayFixtures.at("01:57") && fresh.wokeAt == TodayFixtures.at("07:05") && fresh.suggestedSavedAt == nil)
        let same = TodayApp.rolled(fresh, at: TodayFixtures.at("09:00"), today: fresh.day)
        #expect(same.wokeAt == TodayFixtures.at("07:05") && same.lastLook == TodayFixtures.at("09:00"))
    }
}

// MARK: Harness

private func environment(_ clock: String) -> AppEnvironment {
    var environment = AppEnvironment.testing(clock: TestClock(now: TodayFixtures.at(clock)))
    environment.timeZone = TodayFixtures.zone
    return environment
}

/// The root at a moment of the invented day, laid over through its own events.
private func load(_ harness: ControllerHarness<TodayApp>, _ clock: String, edit: (inout TodayApp.State) -> Void = { _ in }) async {
    let state = TodayFixtures.state(at: clock, edit: edit)
    await harness.deliver(.discovered(TodayFixtures.discovery))
    await harness.deliver(.records(.loaded(Array(state.linked.records.values), cursor: nil, queue: [])))
    await harness.deliver(
        .localRead(
            notes: state.notes, windows: state.windows, at: TodayFixtures.at(clock), today: CivilDay(year: 2026, month: 11, day: 19),
            zone: TodayFixtures.zone.identifier))
    await harness.deliver(.windowsRead(state.windows, requests: state.requests, stale: []))
    await harness.deliver(.contactsRead(Array(state.contacts.values), asked: []))
    await harness.deliver(.permission(.on))
    harness.clearTrace()
}

private func writes(_ harness: ControllerHarness<TodayApp>) -> [LocalWrite] {
    harness.effects.compactMap { effect -> LocalWrite? in
        if case let .records(.enqueue(write)) = effect { return write }
        return nil
    }
}

/// Every value of one type anywhere inside another, by reflection.
private func collect<T>(_ type: T.Type, in value: Any, into out: inout [T]) {
    if let found = value as? T {
        out.append(found)
        return
    }
    for child in Mirror(reflecting: value).children { collect(type, in: child.value, into: &out) }
}

@Suite("The one control for a choice", .timeLimit(.minutes(10)))
struct OptionListTests {
    /// Every option list of every scene, and of every page each scene names.
    static var lists: [(String, OptionList)] {
        var out: [(String, OptionList)] = []
        for fixture in TodayFixtures.all {
            var found: [OptionList] = []
            collect(OptionList.self, in: fixture.app.pane, into: &found)
            for route in fixture.routes + [.plan, .priorities, .projects, .suggested] { collect(OptionList.self, in: fixture.app.page(route), into: &found) }
            out += found.map { (fixture.name, $0) }
        }
        return out
    }

    @Test func atMostOneOptionOfARoundListIsSelectedAndNoneOffersUrgent() {
        let lists = Self.lists
        #expect(lists.count > 200)
        for (name, list) in lists {
            if list.mark == .round { #expect(list.rows.filter { $0.selected }.count <= 1, "\(name)") }
            #expect(!list.rows.contains { $0.name.lowercased() == "urgent" || $0.id == "urgent" }, "\(name)")
            #expect(Set(list.rows.map(\.id)).count == list.rows.count, "\(name)")
        }
    }

    @Test func nothingThatSendsIsSelectedInAdvanceAndASquareRowIsNeverSuggested() {
        for (name, list) in Self.lists where list.mark == .square {
            #expect(!list.rows.contains { $0.quote != nil && $0.selected }, "\(name)")
        }
    }

    @Test func whereATapTakesEffectAtOnceNothingIsSelectedThatWasNotChosen() throws {
        // A flag, on a task the assistant has a suggestion for: the pill, and the owner's own flag filled.
        guard case let .priorities(page) = TodayFixtures.present(TodayFixtures.state(at: "08:30"), at: "08:30").page(.priorities) else { return }
        let suggested = try #require(page.suggested.first)
        #expect(suggested.flag.options.selected == nil && suggested.flag.options.rows.first { $0.suggested }?.id == "high")
        // Above a Save, the assistant's option is the one selected.
        guard case let .suggested(answers) = TodayFixtures.present(TodayFixtures.state(at: "08:30"), at: "08:30").page(.suggested) else { return }
        #expect(answers.groups.flatMap(\.items).allSatisfy { $0.options.selected?.suggested == true })
    }
}
