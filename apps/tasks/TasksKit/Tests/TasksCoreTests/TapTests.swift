import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateScreens
import SubstrateTesting
import Testing

@testable import TasksCore

@Suite("A reminder's tap", .timeLimit(.minutes(10)))
struct TapTests {
    func rig() throws -> (Rig, ControllerHarness<TasksApp>) {
        let rig = try Rig()
        return (rig, rig.harness(performsEffects: false))
    }

    @Test func aTapOnlyAReminderMakesIsACommand() {
        let tap = NotificationResponse(id: Reminders.id("t3"), category: Reminders.category, action: .open, userInfo: ["task": "t3"])
        #expect(Reminders.command(for: tap) == .reminderTapped("t3"))
        var dismissed = tap
        dismissed.action = .dismiss
        #expect(Reminders.command(for: dismissed) == nil)
        #expect(Reminders.command(for: NotificationResponse(id: "x", category: "chat.reply", action: .open, userInfo: ["task": "t3"])) == nil)
    }

    @Test func aTapAtAColdLaunchOpensTheTaskOnceItsRecordIsRead() async throws {
        let (_, harness) = try rig()
        // The tap reaches the root before the device's copy is read.
        await harness.send(.reminderTapped("t3"))
        #expect(await harness.state.openTask == nil)
        #expect(await harness.state.tapped == "t3")
        await harness.deliver(.records(.loaded([listRecord("home", "Home"), taskRecord("t3", "Pay rent", list: "home")], cursor: nil, queue: [])))
        let state = await harness.state
        #expect(state.section == TasksMenu.lists)
        #expect(state.selection == .list("home"))
        #expect(state.openTask == "t3")
        #expect(state.tapped == nil)
        let presented = await harness.presentation
        #expect(presented.menu.selected == TasksMenu.lists)
        #expect(presented.detail?.title == "Pay rent")
        #expect(presented.list?.selection == .list("home"))
    }

    @Test func aViewTellingTheStateItHoldsForgetsNoTap() async throws {
        let (_, harness) = try rig()
        await harness.send(.reminderTapped("t3"))
        // Drawn again at launch: the tab already open, no task open.
        await harness.send(.select(section: TasksMenu.needsYou))
        await harness.send(.open(task: nil))
        await harness.deliver(.records(.loaded([taskRecord("t3", "Pay rent")], cursor: nil, queue: [])))
        #expect(await harness.state.openTask == "t3")
        await harness.send(.open(task: "t3"))
        #expect(await harness.state.openTask == "t3")
    }

    @Test func goingElsewhereBeforeTheRecordComesForgetsTheTap() async throws {
        let (_, harness) = try rig()
        await harness.send(.reminderTapped("t3"))
        await harness.send(.select(section: TasksMenu.settings))
        await harness.deliver(.records(.loaded([taskRecord("t3", "Pay rent")], cursor: nil, queue: [])))
        #expect(await harness.state.openTask == nil)
        #expect(await harness.state.section == TasksMenu.settings)
    }

    @Test func navigationIsThePresentations() async throws {
        let (_, harness) = try rig()
        await harness.deliver(.records(.loaded([listRecord("home", "Home"), taskRecord("a", "A", list: "home")], cursor: nil, queue: [])))
        // A phone opens on Needs you; Lists shows the lists until one is chosen.
        #expect(await harness.presentation.list?.selection == .needsYou)
        await harness.send(.select(section: TasksMenu.lists))
        #expect(await harness.presentation.list == nil)
        await harness.send(.show(.list("home")))
        await harness.send(.open(task: "a"))
        #expect(await harness.presentation.detail?.id == "a")
        // Back, then back again.
        await harness.send(.open(task: nil))
        #expect(await harness.presentation.detail == nil)
        await harness.send(.show(nil))
        #expect(await harness.presentation.list == nil)
    }

    @Test func aDaysReminderComesAtTheHourOfTheDeviceTimeZone() throws {
        let model = TasksModel(tasks: ["t": TaskItem(id: "t", name: "Rent", due: .day(CivilDay(year: 2026, month: 11, day: 4)))])
        func fire(_ zone: String) -> Date? {
            var calendar = Calendar(identifier: .gregorian)
            calendar.timeZone = TimeZone(identifier: zone)!
            return Reminders.plan(
                model, settings: SettingsValues(toggles: [TasksSettings.remind: true]), permission: .on, now: now, calendar: calendar,
                locale: Locale(identifier: "en_US")
            ).first?.fireAt
        }
        // 9:00 in each zone: a move of the zone is a new plan, which the
        // next presentation makes and the effects schedule.
        #expect(fire("UTC") == Date(timeIntervalSince1970: 1_793_782_800))
        #expect(fire("Europe/Berlin") == Date(timeIntervalSince1970: 1_793_782_800 - 3600))
    }
}
