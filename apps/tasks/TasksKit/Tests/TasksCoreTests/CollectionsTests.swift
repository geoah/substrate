import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing

@testable import TasksCore

private let chore = "ada.example.com/work/chore"
private let area = "ada.example.com/work/area"
private func c(_ id: String) -> String { "\(chore)/\(id)" }

private func choreRecord(_ id: String, _ summary: String, state: String, area areaID: String? = nil, version: Int = 1) -> Record {
    var properties: [String: JSONValue] = ["summary": .string(summary), "title": .string(summary), "state": .string(state)]
    if let areaID { properties["area"] = .string("\(area)/\(areaID)") }
    return Record(id: id, kind: chore, properties: properties, version: version, createdAt: "2026-11-02T10:00:00Z")
}

private func collectionRecord(_ kind: String, enabled: Bool) -> Record {
    Record(
        id: DerivedIDs.collection(kind), kind: TasksKinds.collection, properties: ["kind": .string(kind), "enabled": .bool(enabled)],
        version: 1, createdAt: "2026-11-01T10:00:00Z")
}

@Suite("Collections, suggestions and decisions", .timeLimit(.minutes(10)))
struct CollectionsTests {
    /// A root with the chores declared, the records given, the settings
    /// given, and a link holding `scopes`.
    func loaded(
        _ records: [Record], settings: [Record] = [collectionRecord(chore, enabled: true)],
        scopes: [String] = TasksManifest.scopes + ["write:\(chore)", "read:\(area)"], selection: ListSelection? = .needsYou
    ) async throws -> ControllerHarness<TasksApp> {
        let rig = try Rig()
        let harness = rig.harness(performsEffects: false)
        var link = signedIn
        link.scopes = scopes
        await harness.deliver(.records(.loaded(records, cursor: nil, queue: [])))
        await harness.deliver(.settings(.loaded(settings, cursor: nil, queue: [])))
        await harness.deliver(.records(.linked(link)))
        await harness.deliver(.discovered(TasksFixtures.declarations))
        // Read again over the kinds the collections now name.
        await harness.deliver(.records(.listed(records, cursor: WatchCursor(seq: 1, generation: "g"))))
        await harness.deliver(.settingsRead(SettingsValues(), usage: []))
        await harness.deliver(.shown(selection))
        harness.clearTrace()
        return harness
    }

    func enqueued(_ harness: ControllerHarness<TasksApp>) -> [OutboxEntry] {
        harness.effects.flatMap { effect -> [OutboxEntry] in
            switch effect {
            case let .records(.enqueue(write)), let .settings(.enqueue(write)): write.entries
            default: []
            }
        }
    }

    func settle(_ harness: ControllerHarness<TasksApp>, _ path: String) async throws {
        let write = try #require(await harness.state.pending[path]?.write)
        await harness.deliver(.records(.wrote(write)))
    }

    @Test func discoveryAndTheSettingsOpenTheDataLinkOverEveryCollectionAndItsGroups() async throws {
        let harness = try await loaded([])
        let state = await harness.state
        #expect(Fixtures.app.linked.kinds(in: state.linked) == [TasksKinds.task, TasksKinds.list, chore, area])
        #expect(state.linked.phase == .following)
        // Discovery runs at every new approval.
        var approved = signedIn
        approved.scopes = TasksManifest.scopes
        await harness.deliver(.records(.linked(approved)))
        #expect(harness.effects.contains(.discover))
        // The areas out of reach now: the chores are read ungrouped, and the
        // link closes over the rest and opens again.
        #expect(Fixtures.app.linked.kinds(in: await harness.state.linked) == [TasksKinds.task, TasksKinds.list, chore])
        #expect(harness.effects.contains(.records(.read(over: [TasksKinds.task, TasksKinds.list, chore]))))
    }

    @Test func aSuggestionIsAcceptedWithItsEditsInOnePatchAfterItsWindowAndUndoWritesNothing() async throws {
        let maya = taskRecord("maya", "Reply to Maya", status: "proposed", version: 3)
        let harness = try await loaded([maya, listRecord("home", "Home")])
        let queue = try #require(await harness.presentation.list?.queue)
        #expect(queue == CountedRow(title: "1 suggestion", detail: "Tasks waiting for you to accept or dismiss", count: 1, selection: .suggestions))
        // A suggestion is no open work.
        #expect(await harness.presentation.menu.sections.first?.badge == nil)

        await harness.send(.accept(t("maya")))
        #expect(await harness.state.pending[t("maya")]?.decision == .accept)
        await harness.send(.undo(t("maya")))
        #expect(await harness.state.pending.isEmpty)
        #expect(enqueued(harness).isEmpty)
        #expect(harness.effects.contains(.unsettle(t("maya"))))

        await harness.send(.setSuggestion(t("maya"), property: "list", to: .string(ListSelection.list("home").listPath)))
        await harness.send(.setSuggestion(t("maya"), property: "priority", to: "high"))
        await harness.send(.show(.suggestions))
        await harness.send(.open(task: t("maya")))
        let page = try #require(await harness.presentation.suggestion)
        #expect(page.fields.map(\.label) == ["List", "Due", "Priority"])
        #expect(page.fields.map(\.value) == ["Home", "None", "High"])
        #expect(page.actions.map(\.title) == ["Dismiss", "Accept"])
        await harness.send(.accept(t("maya")))
        #expect(await harness.presentation.queue?.rows[t("maya")]?.pendingWords == "Accepted")
        try await settle(harness, t("maya"))
        let entry = try #require(enqueued(harness).last)
        #expect(entry.method == .patch)
        #expect(entry.properties?.keys == ["status", "list", "priority"])
        #expect(entry.properties == ["status": "open", "list": .string(ListSelection.list("home").listPath), "priority": "high"])
        #expect(entry.guarded)
        #expect(await harness.state.edits.isEmpty)
    }

    @Test func severalDestinationsAreAChoiceByTheirLabels() async throws {
        let harness = try await loaded([choreRecord("fence", "Fix the fence", state: "suggested", area: "a1")], selection: .suggestions)
        let row = try #require(await harness.presentation.queue?.rows.rows.first)
        #expect(row.decisions.map(\.title) == ["Dismiss", "Accept"])
        #expect(row.written == "Chores · yesterday")
        await harness.send(.accept(c("fence")))
        let choice = try #require(await harness.presentation.choice)
        #expect(choice.title == "Accept as")
        #expect(choice.options.map(\.title) == ["Todo", "Doing"])
        #expect(choice.cancel == "Cancel")
        #expect(await harness.state.pending.isEmpty)
        await harness.send(.decide(c("fence"), .accept, to: "doing"))
        #expect(await harness.presentation.choice == nil)
        try await settle(harness, c("fence"))
        #expect(enqueued(harness).last?.properties == ["state": "doing"])
        // A value that is not one of the move's writes nothing.
        await harness.send(.decide(c("fence"), .accept, to: "closed"))
        #expect(enqueued(harness).count == 1)
    }

    @Test func dismissAndWontDoWaitTheirWindowsToo() async throws {
        let harness = try await loaded(
            [
                choreRecord("fence", "Fix the fence", state: "suggested"), choreRecord("gutters", "Clean the gutters", state: "todo"),
            ], selection: .ungrouped(chore))
        await harness.send(.dismiss(c("fence")))
        await harness.send(.abandon(c("gutters")))
        #expect(enqueued(harness).isEmpty)
        let rows = try #require(await harness.presentation.list?.open.rows)
        #expect(rows.first { $0.id == c("gutters") }?.pendingWords == "Won't do")
        try await settle(harness, c("fence"))
        try await settle(harness, c("gutters"))
        #expect(enqueued(harness).map(\.properties) == [["state": "skipped"], ["state": "skipped"]])
    }

    @Test func pickedSuggestionsAreDecidedTogether() async throws {
        let harness = try await loaded(
            [taskRecord("a", "A", status: "proposed"), choreRecord("fence", "Fix the fence", state: "suggested")], selection: .suggestions)
        await harness.send(.pick(true))
        await harness.send(.picked(t("a")))
        await harness.send(.picked(c("fence")))
        let queue = try #require(await harness.presentation.queue)
        #expect(queue.select == "Cancel")
        #expect(queue.picked == "2 selected")
        #expect(queue.acceptPicked == "Accept")
        #expect(queue.dismissPicked == "Dismiss")
        #expect(queue.rows.rows.map(\.picked) == [true, true])
        await harness.send(.dismissPicked)
        let state = await harness.state
        #expect(state.picked == nil)
        #expect(Set(state.pending.keys) == [t("a"), c("fence")])
    }

    @Test func turningACollectionOnWritesItsSettingsRecordAndAsksForWhatChangesIt() async throws {
        let harness = try await loaded([], settings: [], scopes: TasksManifest.scopes)
        #expect(await harness.presentation.collections.rows.map(\.title) == ["Tasks", "Chores"])
        #expect(await harness.presentation.collections.rows.map(\.meta) == ["0 open · grouped by list", "View only"])
        await harness.send(.setCollection(chore, on: true))
        let entries = enqueued(harness)
        // A record no device has: created under the derived id, then patched,
        // so a device that lost the race patches the winner.
        #expect(entries.map(\.method) == [.put, .patch])
        #expect(entries.map(\.recordID) == [DerivedIDs.collection(chore), DerivedIDs.collection(chore)])
        #expect(entries.first?.baseVersion == 0)
        #expect(entries.first?.properties == ["kind": .string(chore)])
        #expect(entries.last?.properties == ["enabled": true])
        let approval = try #require(await harness.presentation.approval)
        #expect(approval.message == "Substrate Tasks needs your approval to change your Chores.")
        #expect(approval.approve == "Approve")
        #expect(approval.scopes == ["write:\(chore)"])
        // The owner went through the page and approved nothing more.
        await harness.send(.approvalAnswered(completed: true))
        var answered = signedIn
        answered.scopes = TasksManifest.scopes + ["read:\(area)"]
        await harness.deliver(.records(.linked(answered)))
        #expect(await harness.presentation.approval?.refused == "Not approved. Chores stays view only.")
        // Approved, the ask is gone.
        await harness.send(.dismissApproval)
        await harness.send(.setCollection(chore, on: false))
        #expect(await harness.presentation.approval == nil)
    }

    @Test func groupingByAKindOutOfReachAsksToReadIt() async throws {
        let harness = try await loaded([], scopes: TasksManifest.scopes + ["write:\(chore)"])
        #expect(await harness.state.declarations.count == 1)
        await harness.send(.setGrouping(chore, property: nil))
        await harness.send(.setGrouping(chore, property: "area"))
        let approval = try #require(await harness.presentation.approval)
        #expect(approval.message == "Substrate Tasks needs your approval to read your areas.")
        #expect(approval.scopes == ["read:\(area)"])
        await harness.send(.approvalAnswered(completed: false))
        #expect(await harness.presentation.approval?.refused == "Not approved. Chores stays ungrouped.")
        // A collection grouping by nothing is a list of its own, under Collections.
        let sidebar = await harness.presentation.sidebar
        #expect(sidebar.ungroupedTitle == "Collections")
        #expect(sidebar.ungrouped.map(\.title) == ["Chores"])
    }

    @Test func eachTabKeepsItsOwnStack() async throws {
        let harness = try await loaded([taskRecord("a", "A", status: "proposed"), listRecord("home", "Home")])
        await harness.send(.show(.suggestions))
        await harness.send(.select(section: TasksMenu.lists))
        // Lists opens on its own stack, the queue left with Needs you.
        #expect(await harness.presentation.queue == nil)
        #expect(await harness.state.selection == nil)
        await harness.send(.show(.list("home")))
        await harness.send(.select(section: TasksMenu.needsYou))
        #expect(await harness.state.selection == .suggestions)
        #expect(await harness.presentation.queue != nil)
        await harness.send(.select(section: TasksMenu.lists))
        #expect(await harness.state.selection == .list("home"))
    }

    @Test func aViewOnlyCollectionSaysSoAndAsksToChangeIt() async throws {
        let harness = try await loaded(
            [choreRecord("fence", "Fix the fence", state: "suggested", area: "a1"), choreRecord("gutters", "Clean the gutters", state: "todo")],
            scopes: TasksManifest.scopes, selection: .suggestions)
        let row = try #require(await harness.presentation.queue?.rows.rows.first)
        #expect(row.decisions.isEmpty)
        #expect(row.viewOnly == "View only")
        // Said once, in the page's words.
        #expect(row.spoken == "Fix the fence, Suggestion in Chores · written yesterday, View only")
        await harness.send(.open(task: c("fence")))
        let page = try #require(await harness.presentation.suggestion)
        let line = try #require(page.approval)
        #expect(line.message == "Substrate Tasks needs your approval to change your Chores.")
        #expect(line.approve == "Approve")
        #expect(line.scopes == ["write:\(chore)"])
        // Areas are out of reach: never the reference's label.
        #expect(page.details.first { $0.label == "Area" }?.value == "Out of reach")
        await harness.send(.open(task: c("gutters")))
        #expect(await harness.presentation.detail?.approval == line)
        await harness.send(.showSettings(.collection(chore)))
        #expect(await harness.presentation.collection?.approval == line)
        // Approve asks whether or not the collection is on, and the owner is
        // on the approval page at once.
        await harness.send(.requestApproval(chore, group: nil))
        let asked = try #require(await harness.state.approval)
        #expect(asked.scopes == ["write:\(chore)"])
        #expect(asked.answered)
        #expect(await harness.presentation.approval?.waiting == true)
    }

    @Test func aGroupingOutOfReachStaysChosenAndAsksToReadIt() async throws {
        let harness = try await loaded(
            [choreRecord("gutters", "Clean the gutters", state: "todo", area: "a1")],
            settings: [
                Record(
                    id: DerivedIDs.collection(chore), kind: TasksKinds.collection,
                    properties: ["kind": .string(chore), "enabled": true, "group": "area"], version: 1, createdAt: "2026-11-01T10:00:00Z")
            ], scopes: TasksManifest.scopes + ["write:\(chore)"], selection: .collection(chore))
        #expect(await harness.presentation.list?.open.rows.first?.listName == "Out of reach")
        await harness.send(.showSettings(.collection(chore)))
        let page = try #require(await harness.presentation.collection)
        #expect(page.groups.map(\.selected) == [true, false])
        #expect(page.approval?.message == "Substrate Tasks needs your approval to read your areas.")
        #expect(page.approval?.scopes == ["read:\(area)"])
        #expect(page.approval?.group == area)
    }

    @Test func newTasksGoToTheChosenCollectionThroughItsReadingsOrAreRefused() async throws {
        let harness = try await loaded(
            [],
            settings: [
                collectionRecord(chore, enabled: true),
                Record(
                    id: DerivedIDs.preference, kind: TasksKinds.preference, properties: ["newTasks": .string(chore)], version: 1,
                    createdAt: "2026-11-01T10:00:00Z"),
            ])
        await harness.send(.quickAdd("Mow the lawn"))
        let entry = try #require(enqueued(harness).first)
        #expect(entry.kind == chore)
        #expect(entry.method == .put)
        // Its title through its own property, and no day alone: it has none.
        #expect(entry.properties == ["summary": "Mow the lawn"])
        await harness.send(.newTask)
        let editor = try #require(await harness.presentation.editors.first?.value)
        #expect(editor.list?.label == "Area")
        #expect(editor.due?.kinds.map(\.id) == ["none", "time"])
        #expect(editor.priority?.options.map(\.title) == ["None", "Low", "Normal", "High"])
        #expect(editor.notes != nil)
        #expect(editor.link == nil)
        // A collection it cannot write: refused, and why.
        let viewOnly = try await loaded(
            [],
            settings: [
                collectionRecord(chore, enabled: true),
                Record(
                    id: DerivedIDs.preference, kind: TasksKinds.preference, properties: ["newTasks": .string(chore)], version: 1,
                    createdAt: "2026-11-01T10:00:00Z"),
            ], scopes: TasksManifest.scopes + ["read:\(area)"])
        await viewOnly.send(.quickAdd("Mow the lawn"))
        #expect(enqueued(viewOnly).isEmpty)
        #expect(
            await viewOnly.presentation.problem == ProblemPresentation(title: "Can't add a task", message: "New tasks go to Chores, which is view only here."))
    }

    @Test func aHeldSettingsRecordIsPatchedOnTheVersionRead() async throws {
        let harness = try await loaded([])
        await harness.send(.setGrouping(chore, property: nil))
        let entry = try #require(enqueued(harness).last)
        #expect(entry.method == .patch)
        #expect(entry.recordID == DerivedIDs.collection(chore))
        #expect(entry.properties == ["group": "-"])
    }

    @Test func aGroupIsPinnedInThePreferenceAndShowsOnNeedsYou() async throws {
        let garden = Record(id: "a1", kind: area, properties: ["name": "Garden", "title": "Garden"], version: 1, createdAt: "2026-10-01T00:00:00Z")
        let harness = try await loaded([garden, choreRecord("gutters", "Clean the gutters", state: "todo", area: "a1")], selection: .group("\(area)/a1"))
        let group = try #require(await harness.presentation.list)
        #expect(group.title == "Garden")
        #expect(group.summary.detail == "Area")
        #expect(group.summary.headline == "1 open")
        #expect(group.pin == ActionButton("Pin", .pin("\(area)/a1")))
        await harness.send(.pin("\(area)/a1"))
        let entries = enqueued(harness)
        #expect(entries.map(\.method) == [.put, .patch])
        #expect(entries.first?.recordID == DerivedIDs.preference)
        #expect(entries.last?.properties == ["pins": [.string("\(area)/a1")]])
        await harness.send(.show(.needsYou))
        #expect(await harness.presentation.list?.chips.map(\.title) == ["All", "Garden"])
        #expect(await harness.presentation.sidebar.pinned.map(\.detail) == ["Area"])
    }

    @Test func listsAreASectionPerGroupKindInTheContractsWords() async throws {
        let harness = try await loaded(
            [
                listRecord("home", "Home"), taskRecord("a", "A", list: "home"), taskRecord("b", "B"), taskRecord("c", "C", list: "gone"),
                choreRecord("gutters", "Clean the gutters", state: "todo"),
            ], selection: nil)
        let sidebar = await harness.presentation.sidebar
        #expect(sidebar.search == "Search task lists and areas")
        #expect(sidebar.pinnedTitle == "Pinned")
        #expect(sidebar.sections.map(\.title) == ["Task lists", "Areas"])
        #expect(sidebar.sections.map(\.reference) == [TasksKinds.list, area])
        let lists = sidebar.sections[0].rows
        #expect(lists.map(\.title) == ["Home", "No list", "Missing"])
        #expect(lists.map(\.detail) == [nil, "Tasks with no list", "Tasks whose list is gone or out of reach"])
        #expect(sidebar.sections[1].rows.map(\.title) == ["No area"])
        #expect(sidebar.fixed.map(\.title) == ["Needs you", "Suggestions", "No date"])
    }

    @Test func needsYouCountsTheUndatedOnOneRow() async throws {
        let harness = try await loaded([taskRecord("a", "A"), taskRecord("b", "B", dueDay: "2026-11-04")])
        let list = try #require(await harness.presentation.list)
        #expect(list.noDate == CountedRow(title: "No date", detail: "Open tasks with no due date", count: 1, selection: .noDate))
        #expect(list.open.rows.map(\.id) == [t("b")])
        await harness.send(.show(.noDate))
        #expect(await harness.presentation.list?.title == "No date")
        #expect(titles(await harness.presentation) == ["A"])
    }

    @Test func aCollectionsPageSaysHowItReadsTheDeclaration() async throws {
        let harness = try await loaded([])
        await harness.send(.showSettings(.collection(chore)))
        let page = try #require(await harness.presentation.collection)
        #expect(page.title == "Chores")
        #expect(page.reference == chore)
        #expect(page.groupTitle == "Group by")
        #expect(page.groups.map(\.title) == ["Area", "Nothing"])
        #expect(page.groups.map(\.detail) == [area, "One list of every task"])
        #expect(page.groups.map(\.selected) == [true, false])
        #expect(page.statesTitle == "How its states read")
        #expect(page.states.map(\.label) == ["Suggestion", "Open", "Done", "Dropped"])
        #expect(page.states.map(\.value) == ["suggested", "todo, doing", "closed", "skipped"])
        #expect(page.fieldsTitle == "Fields the app shows")
        #expect(page.fields.map(\.label) == ["Title", "Due", "Due day", "Priority", "Order"])
        #expect(page.fields.map(\.value) == ["summary", "dueAt", "None", "priority", "None"])
        let collections = await harness.presentation.collections
        #expect(collections.showTitle == "Show tasks from")
        #expect(collections.newTasksTitle == "Save new tasks to")
        #expect(collections.newTasks.label == "New tasks")
        #expect(collections.newTasks.options.map(\.title) == ["Tasks", "Chores"])
        #expect(collections.newTasks.selected == TasksKinds.task)
    }
}

/// The app the rigs make, for its links' kinds.
enum Fixtures {
    static let app = TasksApp(cache: nil, outbox: nil, words: TasksApp.words(device: "this iPhone"), device: "this iPhone", build: BuildFacts(version: "0"))
}
