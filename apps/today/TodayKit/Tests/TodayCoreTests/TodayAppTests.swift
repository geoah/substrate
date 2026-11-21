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
private func pane(_ clock: String, urgent: Bool = true, held: Bool = true, reachable: Bool = true, edit: (inout TodayApp.State) -> Void = { _ in }) -> PanePresentation {
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
        #expect(card.done?.title == "Taken" && card.done?.command == .log(row: routine.id, done: true))
        #expect(card.stale == nil, "yesterday's were logged")
    }

    @Test func whereLoggingStoppedTheRoutineSaysSoAndYesterdayIsNotAskedAfter() throws {
        let pane = try #require(TodayFixtures.all.first { $0.name == "today-wake-stale" }).app.pane
        guard case let .routine(card) = rows(pane)[0].body else { return #expect(Bool(false)) }
        #expect(card.stale?.pill == .stale)
        #expect(card.stale?.text == "Nothing has been logged since 17 October. A tick here is the log.")
        guard case let .yesterday(left) = try #require(row(pane, "yesterday")).body else { return #expect(Bool(false)) }
        #expect(left.items.map(\.glyph) == [.task], "only the task that was due, not a month of doses")
    }

    @Test func theMorningIsNightYesterdaySuggestedPlanInThatOrder() {
        let pane = pane("08:20")
        #expect(pane.lead == "night" && pane.ground == .planning)
        #expect(rows(pane).prefix(2).map(\.id) == ["suggested", "plan"])
        // What yesterday left came first, and was answered at 08:19 and on waking.
        #expect(rows(TodayCoreTests.pane("07:38")).map(\.id).contains("yesterday"))
        let saved = TodayCoreTests.pane("08:40")
        #expect(saved.lead == "row:plan")
        #expect(row(saved, "suggested") == nil)
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
        // A task's row ends with its flag control, and the list never offers Urgent.
        guard case let .flag(control) = tasks[0].trailing else { return #expect(Bool(false)) }
        #expect(control.options.rows.map(\.name) == ["High", "Medium", "Low"])
        #expect(control.options.selected?.id == "high")
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
        // Grouped by project in flag order; the rest is one count.
        #expect(happening.groups.map(\.name) == ["Lantern", "Security hardening", "Hiring"])
        #expect(happening.time == "09:10" && happening.gotIt?.command == .gotIt(project: nil))
        #expect(happening.line == "Callum accepted Friday at 16:30. The 17:20 slot is free. And three more on two projects.")
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
    @Test func everySuggestedListIsOpenWithTheAssistantsOptionSelected() throws {
        let pane = pane("08:30")
        let suggested = try #require(row(pane, "suggested"))
        #expect(suggested.title == "Thirteen suggested tasks to answer" && suggested.state == .now)
        guard case let .suggested(card) = suggested.body else { return #expect(Bool(false)) }
        #expect(card.items.count == 13)
        #expect(card.save.button?.title == "Save 13 answers" && card.save.button?.enabled == true)
        for item in card.items {
            #expect(item.options.rows.map(\.name) == ["Make it a task", "Already done", "Dismiss"])
            let selected = try #require(item.options.selected)
            #expect(selected.suggested, "the assistant's option starts selected, and carries the pill")
        }
        let maya = try #require(card.items.first { $0.id == "o-maya" })
        #expect(maya.options.selected?.id == "dismiss" && maya.fade.level == 3)
        let ben = try #require(card.items.first { $0.id == "o-ben" })
        #expect(ben.fade == .init(level: 2, label: "Fading. Gone on Tuesday."))
    }

    @Test func aPickIsADraftAndTheAssistantObjectsOnceInPlace() throws {
        let pane = pane("08:30") { $0.drafts.suggested["p-marcus"] = .dismiss }
        guard case let .suggested(card) = try #require(row(pane, "suggested")).body else { return #expect(Bool(false)) }
        let marcus = try #require(card.items.first { $0.id == "p-marcus" })
        #expect(marcus.options.selected?.id == "dismiss")
        // The pill stays on the assistant's option.
        #expect(marcus.options.rows.first { $0.suggested }?.id == "task")
        #expect(marcus.objection?.heading == "Moss objects, once")
    }

    @Test func savingTheSuggestedTasksWritesEachAnswerAndNothingBefore() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("08:30"), performsEffects: false)
        await load(harness, "08:30")
        await harness.send(.suggest("p-kenji", .done))
        #expect(writes(harness).isEmpty, "a pick writes nothing")
        await harness.send(.saveSuggested(nil))
        let written = writes(harness).flatMap(\.entries)
        #expect(written.count == 13)
        let byID = Dictionary(uniqueKeysWithValues: written.map { ($0.recordID, $0.properties) })
        #expect(byID["p-kenji"] == ["status": "done"])
        #expect(byID["o-maya"] == ["status": "abandoned"])
        #expect(byID["p-tomas"] == ["status": "open", "prioritySetBy": "owner"])
        let notes = try #require(await harness.state.notes)
        #expect(notes.suggestedSaved == [11, 1, 1] && notes.suggestedSavedAt == TodayFixtures.at("08:30"))
    }

    @Test func thePlanPageTicksWhatIsSuggestedAndNeverAMessage() throws {
        guard case let .plan(plan) = page("08:40", .plan) else { return #expect(Bool(false)) }
        #expect(plan.groups.prefix(5).map(\.name) == ["EU AI Act", "External API reliability", "Lantern", "GitHub orgs", "Hiring"])
        let all = plan.groups.flatMap { $0.items + $0.more }
        #expect(all.filter(\.suggested).allSatisfy { $0.ticked })
        #expect(all.first { $0.id == "p-vendor" }?.ticked == true)
        #expect(all.first { $0.id == "t-keys" }?.ticked == false)
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
        #expect(writes(harness).isEmpty, "the plan is a draft until it is saved")
        await harness.send(.planSave)
        let written = writes(harness).flatMap(\.entries)
        let flag = try #require(written.first { $0.recordID == "t-leo" })
        #expect(flag.properties?["priority"] == "medium" && flag.properties?["prioritySetBy"] == "owner")
        let plan = written.filter { $0.kind == TodayKinds.plan }
        #expect(plan.map(\.recordID) == ["d20261119", "d20261119"] && plan.map(\.method) == [.put, .patch])
        #expect(plan[0].properties == ["day": "2026-11-19"])
        let picked = (plan[1].properties?["picked"]?.array ?? []).compactMap(\.text)
        #expect(picked.contains("\(TodayKinds.task)/t-leo") && !picked.contains("\(TodayKinds.task)/t-keys"))
        #expect(plan[1].properties?["savedAt"] == .string("2026-11-19T08:40:00Z"))
        #expect(plan[1].properties?["overlaps"]?.array?.first?["chosen"]?.text == "\(TodayFixtures.eventKind)/henrik")
        #expect(await harness.state.drafts.plan == nil)
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
        guard case let .happening(happening)? = pane.items.first(where: { if case .happening = $0 { true } else { false } }) else { return #expect(Bool(false)) }
        #expect(happening.pill == .urgent && happening.line.hasPrefix("One thing is marked urgent and waits here."))
        let lantern = try #require(happening.groups.first { $0.name == "Lantern" })
        let item = try #require(lantern.items.first)
        #expect(item.pill == .urgent, "first in its project's group, with the urgent pill")
        let marked = try #require(item.marked)
        #expect(marked.options.rows.map(\.name) == ["Take it now", "Leave it here", "Not urgent"])
        // A tap takes effect at once: the assistant's option has the pill and nothing is selected.
        #expect(marked.options.selected == nil && marked.options.rows[1].suggested)
        #expect(marked.options.rows[1].reason == "It is a task for Friday morning, and Wei is here until Monday.")
    }

    @Test func anUncheckedMarkWaitsTwoMinutesThenInterruptsAndSaysSo() throws {
        // Set at 16:22 by an automation; nobody has written a check.
        let waiting = pane("16:23", held: false, reachable: false)
        #expect(waiting.ground != .urgent)
        let pane = pane("16:35", held: false, reachable: false)
        #expect(pane.lead == "row:urgent:t-wei" && pane.ground == .urgent)
        guard case let .urgent(card) = try #require(row(pane, "urgent:t-wei")).body else { return #expect(Bool(false)) }
        #expect(card.unchecked == "Moss cannot be reached, so nobody has checked this.")
        let engine = DayEngine(TodayPresenter.day(TodayFixtures.state(at: "16:23", held: false), now: TodayFixtures.at("16:23"), calendar: TodayFixtures.calendar))
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
        let vitd = try #require(entries.first { $0.recordID == "vitd_20261119T073000Z" })
        #expect(
            vitd.properties == [
                "series": "substrate.reamde.dev/health/medicationschedule/vitd", "scheduledAt": "2026-11-19T07:30:00Z", "at": "2026-11-19T07:05:00Z",
            ])
        // The row is done at once, laid over before the server answers.
        #expect(rows(await harness.presentation.pane).first?.id != routine.id)
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
        #expect(when.options.rows.map(\.name) == ["Today, where it is", "The 16:30 gap", "The 18:15 gap", "The 20:30 gap", "Tonight, after 21:30", "Tomorrow", "Next week"])
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

    @Test func whatIsTypedIsATaskAtOnceAndAQuestionGoesToTheAssistant() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("11:30"), performsEffects: false)
        await load(harness, "11:30")
        await harness.send(.addOrAsk(" send Callum the snapshot restore numbers "))
        let entry = try #require(writes(harness).flatMap(\.entries).first)
        #expect(entry.kind == TodayKinds.task && entry.method == .put && entry.properties == ["name": "Send Callum the snapshot restore numbers"])
        let pane = await harness.presentation.pane
        let added = try #require(row(pane, "task:" + entry.recordID))
        #expect(added.stripe == .none && added.line == "You added it at 11:30.")
        await harness.send(.addOrAsk("What can I drop this afternoon?"))
        guard case let .ask(exchange, message, context)? = harness.effects.last(where: { if case .ask = $0 { true } else { false } }) else { return #expect(Bool(false)) }
        #expect(exchange.subject == .day && message == "What can I drop this afternoon?" && context.isEmpty)
        #expect(await harness.presentation.pane.answer?.state == .waiting)
        await harness.deliver(.answer(exchange.id, .delta("Callum is at the standup too.")))
        await harness.deliver(.answer(exchange.id, .done(nil)))
        let answer = try #require(await harness.presentation.pane.answer)
        #expect(answer.text == "Callum is at the standup too." && answer.state == .done)
    }

    @Test func aQuestionAboutATaskCarriesItsRecordAndAFailureIsSaidInPlace() async throws {
        let harness = ControllerHarness(TodayFixtures.app, environment: environment("11:30"), performsEffects: false)
        await load(harness, "11:30")
        await harness.send(.ask(.task("p-vendor"), "Break it into steps"))
        guard case let .ask(exchange, _, context)? = harness.effects.last(where: { if case .ask = $0 { true } else { false } }) else { return #expect(Bool(false)) }
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
        #expect(Assistant.parse(#"{"kind":"toolStarted","name":"query"}"#) == nil)
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
        #expect(day.series["\(TodayFixtures.scheduleKind)/vitd"]?.dose == "1000 iu")
        #expect(day.plan?.picked.count == 13 && day.plan?.overlaps.count == 4)
        #expect(day.preference.urgentWait == 120 && day.preference.nightReadAt == TodayFixtures.at("08:27"))
        let occurrence = try #require(Occurrence(TodayFixtures.occurrences[0]))
        #expect(occurrence.id == "mag_20261118T073000Z" && occurrence.series == "\(TodayFixtures.scheduleKind)/mag")
        #expect(Occurrence.slot("20261118T073000Z") == TodayFixtures.at("07:30", day: -1) && Occurrence.stamp(occurrence.slot) == "20261118T073000Z")
        // A property of another shape reads as absent, never as a crash.
        let odd = Record(id: "x", kind: TodayKinds.task, properties: ["name": 3, "priority": ["a"], "people": "nobody", "dueDay": "soon"])
        #expect(TaskItem(odd)?.name == "Untitled task" && TaskItem(odd)?.flag == nil && TaskItem(odd)?.people == [])
        #expect(Meeting(Record(id: "m", kind: "a.b/c/d", properties: ["at": "2026-11-19T10:00:00Z", "endsAt": "2026-11-19T10:30:00Z", "status": "cancelled"])) == nil)
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
        #expect(heard.suggestion?.save?.button?.command == .saveSuggested("h-tomas-pr") && heard.card.doors.isEmpty)
        guard case .missing = page("11:30", .task("gone")) else { return #expect(Bool(false)) }
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
private func load(_ harness: ControllerHarness<TodayApp>, _ clock: String) async {
    let state = TodayFixtures.state(at: clock)
    await harness.deliver(.discovered(TodayFixtures.discovery))
    await harness.deliver(.records(.loaded(Array(state.linked.records.values), cursor: nil, queue: [])))
    await harness.deliver(.localRead(notes: state.notes, windows: state.windows, at: TodayFixtures.at(clock), today: CivilDay(year: 2026, month: 11, day: 19), zone: TodayFixtures.zone.identifier))
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
            for route in fixture.routes + [.plan, .priorities, .projects] { collect(OptionList.self, in: fixture.app.page(route), into: &found) }
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
        guard case let .suggested(card) = try #require(rows(pane("08:30")).first { $0.id == "suggested" }).body else { return }
        #expect(card.items.allSatisfy { $0.options.selected?.suggested == true })
    }
}
