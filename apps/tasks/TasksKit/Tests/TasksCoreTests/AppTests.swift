import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing

@testable import TasksCore

/// Whether the scripted server answers, which a test flips.
final class Network: @unchecked Sendable {
    private let lock = NSLock()
    private var up = true
    var online: Bool {
        get { lock.withLock { up } }
        set { lock.withLock { up = newValue } }
    }
}

@Suite("Substrate Tasks, live", .timeLimit(.minutes(10)))
struct LiveAppTests {
    /// The server holds one list, Groceries, and one task in it.
    func serve(_ rig: Rig, network: Network = Network(), extra: [Record] = [], watch: Bool = true) {
        let records = [listRecord("groceries", "Groceries", position: "1"), taskRecord("t1", "Eggs", list: "groceries", position: "1")] + extra
        rig.server.on { _ in network.online ? nil : (-1, "") }
        rig.server.answer("GET", "/api/v1/records", 200, page(records))
        if watch {
            rig.server.on { $0.method == "GET" && $0.path == "/api/v1/changes" ? (200, #"{"bookmark":7,"generation":"g1"}"#) : nil }
        }
        for record in records { rig.server.answer("GET", path(record.kind, record.id), 200, json(record)) }
    }

    @Test func signedInItReadsTheRepositoryThenFollowsTheWatch() async throws {
        let rig = try Rig()
        serve(rig)
        let harness = rig.harness()
        await harness.runtime.linkChanged(signedIn)
        try await harness.until { await $0.state.linked.cursor == WatchCursor(seq: 7, generation: "g1") }
        await harness.send(.show(.list("groceries")))
        let presented = await harness.presentation
        #expect(titles(presented) == ["Eggs"])
        #expect(presented.sidebar.lists.rows.map(\.title) == ["Groceries"])
        #expect(presented.sidebar.lists.rows.first?.openWords == "1 open task")
        // What was read is kept, and where the watch resumes.
        #expect(await rig.cache.records(of: TasksKinds.task).map(\.id) == ["t1"])
        #expect(await rig.cache.cursor == WatchCursor(seq: 7, generation: "g1"))
    }

    @Test func aTaskAddedOfflineShowsAtOnceWaitsInTheOutboxAndIsSentWhenTheServerAnswers() async throws {
        let rig = try Rig()
        let network = Network()
        serve(rig, network: network)
        rig.server.on { $0.method == "PUT" ? (200, json(taskRecord("x", "Milk"))) : nil }
        let harness = rig.harness()
        await harness.runtime.linkChanged(signedIn)
        try await harness.until { await $0.state.linked.phase == .following }
        await harness.send(.show(.list("groceries")))

        network.online = false
        await harness.send(.quickAdd("Milk"))
        #expect(titles(await harness.presentation) == ["Eggs", "Milk"])
        try await harness.until { await $0.state.linked.unconfirmed.isEmpty }
        let waiting = await rig.outbox.entries
        #expect(waiting.count == 1)
        let entry = try #require(waiting.first)
        #expect(entry.method == .put)
        #expect(entry.batch == "\(TasksKinds.task)/\(entry.recordID)")
        #expect(entry.properties?["list"] == .string("substrate.reamde.dev/tasks/list/groceries"))
        #expect(entry.properties?["position"] == .string("2"))
        guard case .offline = await rig.outbox.flush(rig.client) else { throw Unexpected() }
        #expect(titles(await harness.presentation) == ["Eggs", "Milk"])

        // Back online: the write lands, leaves the queue, and the record is
        // read again; the row never goes away in between.
        network.online = true
        rig.server.answer(
            "GET", path(TasksKinds.task, entry.recordID), 200,
            json(taskRecord(entry.recordID, "Milk", list: "groceries", position: "2", version: 1)))
        #expect(await rig.outbox.flush(rig.client) == .sent)
        try await harness.until { harness in
            let state = await harness.state
            return state.linked.queue.isEmpty && state.linked.server["\(TasksKinds.task)/\(entry.recordID)"]?.version == 1
        }
        #expect(titles(await harness.presentation) == ["Eggs", "Milk"])
        #expect(rig.server.sent.contains { $0.method == "PUT" && $0.path == path(TasksKinds.task, entry.recordID) })
    }

    @Test func aWatchRowIsReadAgainAndADeleteRowRemovesTheTask() async throws {
        let rig = try Rig()
        serve(rig, watch: false)
        rig.server.answer("GET", path(TasksKinds.task, "t2"), 200, json(taskRecord("t2", "Bread", list: "groceries", position: "3")))
        // The watch opens at the head, then again from its cursor with two
        // rows on it.
        rig.server.on { sent in
            guard sent.path == "/api/v1/changes" else { return nil }
            guard sent.query?.contains("from=7") == true else { return (200, #"{"bookmark":7,"generation":"g1"}"#) }
            let row = { (seq: Int, id: String, op: String) in
                #"{"seq":\#(seq),"ts":"2026-11-03T09:00:00Z","kind":"\#(TasksKinds.task)","recordId":"\#(id)","op":"\#(op)"}"#
            }
            return (200, [#"{"bookmark":7,"generation":"g1"}"#, row(8, "t2", "put"), row(9, "t1", "delete")].joined(separator: "\n"))
        }
        let harness = rig.harness()
        await harness.runtime.linkChanged(signedIn)
        try await harness.until { await $0.state.linked.phase == .following }
        await harness.send(.show(.list("groceries")))
        try await harness.until(timeout: .seconds(10)) { titles(await $0.presentation) == ["Bread"] }
    }

    @Test func remindersFollowTheTasksAndAreCancelledWhenOneIsDone() async throws {
        let rig = try Rig()
        serve(
            rig,
            extra: [
                taskRecord("t2", "Call the vet", position: "1", dueDay: "2026-11-03", dueAt: "2026-11-03T15:00:00Z"),
                taskRecord("t3", "Pay rent", position: "2", dueDay: "2026-11-04"),
            ])
        rig.server.on { $0.method == "PATCH" ? (200, json(taskRecord("t2", "Call the vet", status: "done", version: 2))) : nil }
        let harness = rig.harness()
        await harness.runtime.linkChanged(signedIn)
        try await harness.until { _ in rig.notifications.pending.count == 2 }
        let vet = try #require(rig.notifications.pending[Reminders.id("t2")])
        #expect(vet.fireAt == Date(timeIntervalSince1970: 1_793_718_000))
        #expect(vet.title == "Call the vet")
        #expect(vet.body.map(spaced) == "Due at 3:00 PM")
        #expect(vet.userInfo == ["task": "t2"])
        #expect(vet.category == Reminders.category)
        // A day's task reminds at the chosen hour of its day.
        #expect(rig.notifications.pending[Reminders.id("t3")]?.fireAt == Date(timeIntervalSince1970: 1_793_696_400 + 86400))

        await harness.send(.complete("t2"))
        try await harness.until { _ in rig.notifications.pending.keys.sorted() == [Reminders.id("t3")] }
        #expect(rig.notifications.calls.contains(.cancel(Reminders.id("t2"))))

        // Turned off, every one goes.
        await harness.send(.settings(.toggle(row: .app(TasksSettings.remind), on: false)))
        try await harness.until { _ in rig.notifications.pending.isEmpty }
        // A tap, which the app's delegate sends, opens the task in its list.
        await harness.send(.reminderTapped("t3"))
        try await harness.until { await $0.state.openTask == "t3" }
        #expect(await harness.state.selection == .inbox)
        #expect(await harness.presentation.detail?.title == "Pay rent")
    }

    @Test func permissionIsAskedOnceSomethingIsDueAndNotBefore() async throws {
        let rig = try Rig(notifications: RecordedNotifications(.notAsked, granting: .on))
        serve(rig)
        let harness = rig.harness()
        await harness.runtime.linkChanged(signedIn)
        try await harness.until { await $0.state.linked.phase == .following }
        #expect(!rig.notifications.calls.contains(.requestPermission))
        await harness.send(.show(.today))
        await harness.send(.quickAdd("Water plants"))
        try await harness.until { _ in rig.notifications.calls.filter { $0 == .requestPermission }.count == 1 }
        try await harness.until { await $0.state.permission == .on }
        // Due today at the chosen hour, which is past: nothing to schedule.
        #expect(rig.notifications.pending.isEmpty)
    }

    @Test func settingsAreKeptInTheRepositorysFolder() async throws {
        let rig = try Rig()
        serve(rig)
        let harness = rig.harness()
        await harness.send(.settings(.choose(row: .app(TasksSettings.order), option: TaskOrder.due.rawValue)))
        try await harness.until { _ in (try? rig.folder.read(TasksApp.settingsFile.name, in: .root)) != nil }
        let again = ControllerHarness(rig.app, clock: rig.clock, environment: rig.environment)
        try await again.until { await $0.state.settingsRead }
        #expect(TasksSettings.order(await again.state.settings) == .due)
    }
}

@Suite("Substrate Tasks, by value", .timeLimit(.minutes(10)))
struct AppTests {
    func loaded(_ records: [Record], selection: ListSelection? = .inbox) async throws -> (Rig, ControllerHarness<TasksApp>) {
        let rig = try Rig()
        let harness = rig.harness(performsEffects: false)
        await harness.deliver(.records(.loaded(records, cursor: nil, queue: [])))
        await harness.deliver(.settingsRead(SettingsValues(), usage: []))
        await harness.deliver(.permission(.on))
        await harness.deliver(.shown(selection))
        harness.clearTrace()
        return (rig, harness)
    }

    func enqueued(_ harness: ControllerHarness<TasksApp>) -> [OutboxEntry] {
        harness.effects.flatMap { effect -> [OutboxEntry] in
            if case let .records(.enqueue(write)) = effect { return write.entries }
            return []
        }
    }

    @Test func aDraggedTaskWritesItsOwnPositionAlone() async throws {
        let (_, harness) = try await loaded([
            taskRecord("a", "A", position: "1"), taskRecord("b", "B", position: "2"),
            taskRecord("c", "C", position: "3"),
        ])
        #expect(titles(await harness.presentation) == ["A", "B", "C"])
        await harness.send(.reorderTask("c", to: 0))
        let writes = enqueued(harness)
        #expect(writes.count == 1)
        #expect(writes.first?.properties == ["position": "0"])
        #expect(writes.first?.guarded == true)
        #expect(titles(await harness.presentation) == ["C", "A", "B"])
        await harness.send(.reorderTask("c", to: 1))
        #expect(enqueued(harness).last?.properties == ["position": "1.5"])
        #expect(titles(await harness.presentation) == ["A", "C", "B"])
    }

    @Test func listsReorderToo() async throws {
        let (_, harness) = try await loaded([listRecord("a", "Home", position: "1"), listRecord("b", "Work", position: "2")])
        await harness.send(.reorderList("b", to: 0))
        #expect(await harness.presentation.sidebar.lists.rows.map(\.title) == ["Work", "Home"])
        #expect(enqueued(harness).first?.properties == ["position": "0"])
    }

    @Test func sortedByDueDateTheOrderIsNotTheOwnersAndCannotBeDragged() async throws {
        let (_, harness) = try await loaded([
            taskRecord("a", "A", position: "1"), taskRecord("b", "B", position: "2", dueDay: "2026-11-05"),
            taskRecord("c", "C", position: "3", dueDay: "2026-11-04"),
        ])
        await harness.send(.settings(.choose(row: .app(TasksSettings.order), option: TaskOrder.due.rawValue)))
        let list = try #require(await harness.presentation.list)
        #expect(list.open.rows.map(\.title) == ["C", "B", "A"])
        #expect(list.reorders == false)
        #expect(list.orderNote == "Sorted by due date")
        await harness.send(.reorderTask("a", to: 0))
        #expect(enqueued(harness).isEmpty)
    }

    @Test func deletingAListSaysHowManyTasksGoWithIt() async throws {
        let (_, harness) = try await loaded(
            [
                listRecord("home", "Home"), taskRecord("a", "A", list: "home"), taskRecord("b", "B", list: "home"),
                taskRecord("c", "C", list: "home", status: "done"), taskRecord("d", "D"),
            ],
            selection: .list("home"))
        await harness.send(.deleteList("home"))
        let asked = try #require(await harness.presentation.confirmation)
        #expect(asked.title == "Delete “Home”?")
        #expect(asked.message == "Its 3 tasks are deleted with it.")
        #expect(asked.confirm == "Delete List and 3 Tasks")
        #expect(asked.cancel == "Cancel")
        #expect(enqueued(harness).isEmpty)

        await harness.send(.confirm)
        let write = try #require(
            harness.effects.compactMap { effect -> LocalWrite? in
                if case let .records(.enqueue(write)) = effect { return write }
                return nil
            }.first)
        #expect(write.entries.map(\.method) == [.delete])
        #expect(write.replacing == "\(TasksKinds.list)/home")
        let presented = await harness.presentation
        #expect(presented.confirmation == nil)
        #expect(presented.sidebar.lists.rows.isEmpty)
        #expect(presented.list?.selection == .inbox)
        #expect(titles(presented) == ["D"])
    }

    @Test func completeReopenAndWontDoMoveTheStateAsThePackageAllows() async throws {
        let (_, harness) = try await loaded([taskRecord("a", "A", version: 4)])
        await harness.send(.complete("a"))
        #expect(enqueued(harness).last?.properties == ["status": "done"])
        #expect(await harness.presentation.list?.open.rows.isEmpty == true)
        // Done cannot become abandoned: nothing is written.
        await harness.send(.abandon("a"))
        #expect(enqueued(harness).count == 1)
        await harness.send(.reopen("a"))
        #expect(enqueued(harness).last?.properties == ["status": "open", "completedAt": .null])
        await harness.send(.abandon("a"))
        #expect(enqueued(harness).last?.properties == ["status": "abandoned"])
    }

    @Test func aRowSaysEachFactInItsOwnRole() async throws {
        let (_, harness) = try await loaded(
            [
                listRecord("home", "Home"),
                taskRecord("a", "Pay rent", list: "home", dueDay: "2026-11-02", priority: "high"),
                taskRecord("b", "Call", dueDay: "2026-11-03", dueAt: "2026-11-03T15:00:00Z"),
            ],
            selection: .today)
        let rows = try #require(await harness.presentation.list?.open.rows)
        #expect(rows.map(\.title) == ["Pay rent", "Call"])
        #expect(rows[0].due == DueBadge(text: "Overdue: Yesterday", tone: .overdue))
        #expect(rows[0].priority == PriorityBadge(text: "High", priority: .high))
        #expect(rows[0].listName == "Home")
        #expect(rows[0].spoken == "Pay rent, due Overdue: Yesterday, priority High, in Home")
        #expect(rows[1].due.map { spaced($0.text) } == "Today, 3:00 PM")
        #expect(rows[1].due?.tone == .soon)
        #expect(rows[1].listName == "No List")
        #expect(await harness.presentation.menu.sections.first?.badge == 2)
    }

    @Test func quickAddInTodayIsDueToday() async throws {
        let (_, harness) = try await loaded([], selection: .today)
        await harness.send(.quickAdd("  Water plants "))
        let entry = try #require(enqueued(harness).first)
        #expect(entry.properties == ["name": "Water plants", "position": "1", "dueDay": "2026-11-03"])
        #expect(titles(await harness.presentation) == ["Water plants"])
        await harness.send(.quickAdd("   "))
        #expect(enqueued(harness).count == 1)
    }

    @Test func theDetailAndTheEditorUseTheSameLabels() async throws {
        var rent = taskRecord("a", "Pay rent", list: "home", dueDay: "2026-11-06", priority: "high")
        rent.properties["description"] = "Before the 6th."
        rent.properties["url"] = "https://example.com/rent"
        let (_, harness) = try await loaded([listRecord("home", "Home"), rent], selection: .list("home"))
        await harness.send(.open(task: "a"))
        let detail = try #require(await harness.presentation.detail)
        // Each fact once: the list and the priority are the choices that
        // change them, under their own labels, and Edit is the toolbar's.
        #expect(detail.fields.map(\.label) == ["Status", "List", "Due", "Priority", "Notes", "Link"])
        #expect(detail.fields.map(\.value) == ["Open", "Home", "Friday, November 6, 2026", "High", "Before the 6th.", "https://example.com/rent"])
        #expect(detail.fields.compactMap { $0.choice?.label } == ["List", "Priority"])
        #expect(detail.fields.first { $0.field == .list }?.choice?.selected == "home")
        #expect(detail.fields.first { $0.field == .priority }?.choice?.selected == "high")
        #expect(detail.actions.map(\.title) == ["Mark as Done", "Won't Do", "Delete Task"])
        #expect(detail.edit == "Edit")
        await harness.send(.edit("a"))
        let (key, editor) = try #require(await harness.presentation.editors.first)
        // Every fact the two views share has one label in both.
        let shared = detail.fields.filter { editor.labels[$0.field] != nil }
        #expect(shared.map(\.field) == [.list, .due, .priority, .notes, .link])
        for field in shared { #expect(field.label == editor.labels[field.field], "\(field.field)") }
        for field in detail.fields { if let choice = field.choice { #expect(choice.label == editor.labels[field.field]) } }
        #expect(editor.list.selected == "home")
        #expect(editor.due.kind == .day)

        // Edited and saved: one patch on the version the owner saw.
        try await harness.runtime.send(TaskEditor.Command.setName("Pay the rent"), to: [key])
        try await harness.runtime.send(TaskEditor.Command.setPriority(.urgent), to: [key])
        try await harness.runtime.send(TaskEditor.Command.save, to: [key])
        let entry = try #require(enqueued(harness).last)
        #expect(entry.method == .patch)
        #expect(entry.properties == ["name": "Pay the rent", "priority": "urgent"])
        #expect(entry.baseVersion == 1)
        let presented = await harness.presentation
        #expect(presented.editors.isEmpty)
        #expect(presented.detail?.title == "Pay the rent")
    }

    @Test func aNewTaskFromTheEditorGoesAfterTheLastOfItsList() async throws {
        let (_, harness) = try await loaded(
            [listRecord("home", "Home"), taskRecord("a", "A", list: "home", position: "4")],
            selection: .list("home"))
        await harness.send(.newTask)
        let (key, editor) = try #require(await harness.presentation.editors.first)
        #expect(editor.title == "New Task")
        #expect(editor.canSave == false)
        try await harness.runtime.send(TaskEditor.Command.save, to: [key])
        #expect(enqueued(harness).isEmpty)
        try await harness.runtime.send(TaskEditor.Command.setName("B"), to: [key])
        try await harness.runtime.send(TaskEditor.Command.setDueKind(.time), to: [key])
        try await harness.runtime.send(TaskEditor.Command.save, to: [key])
        let entry = try #require(enqueued(harness).last)
        #expect(entry.method == .put)
        #expect(
            entry.properties == [
                "name": "B", "list": "substrate.reamde.dev/tasks/list/home", "position": "5",
                "dueDay": "2026-11-03", "dueAt": "2026-11-03T09:00:00Z",
            ])
        #expect(titles(await harness.presentation) == ["A", "B"])
    }

    @Test func aWriteTheDeviceCouldNotKeepIsUndoneAndSaid() async throws {
        let (_, harness) = try await loaded([])
        await harness.send(.quickAdd("Milk"))
        let entry = try #require(enqueued(harness).first)
        await harness.deliver(.records(.notQueued([entry.id], "This iPhone could not keep the change: the disk is full.")))
        let presented = await harness.presentation
        #expect(titles(presented).isEmpty)
        #expect(presented.problem?.message == "This iPhone could not keep the change: the disk is full.")
    }

    @Test func aLandedWriteStaysShownUntilItsRecordIsReadAgain() async throws {
        let (_, harness) = try await loaded([taskRecord("a", "Eggs", version: 1)])
        await harness.send(.complete("a"))
        let entry = try #require(enqueued(harness).first)
        await harness.deliver(.records(.enqueued([entry.id], queue: [entry], landed: [])))
        harness.clearTrace()
        // The flush landed it: the queue reports it gone.
        await harness.deliver(.records(.queueReported([], landed: [entry.id])))
        // Reports are drawn within a window, and effects follow the drawing.
        await harness.advance(by: 0.2)
        #expect(harness.effects.contains(.records(.reread(["\(TasksKinds.task)/a"]))))
        #expect(await harness.presentation.list?.open.rows.isEmpty == true)
    }

    @Test func theSettingsScreenIsTheAppsSectionsThenTheLinksAndEachCommandFindsItsController() async throws {
        let (_, harness) = try await loaded([taskRecord("a", "A", dueDay: "2026-11-04")])
        let app = await harness.presentation
        #expect(app.settings.map(\.title) == ["Reminders", "Lists"])
        #expect(app.settings[0].rows.map(\.label) == ["Remind Me When Tasks Are Due", "Tasks Due on a Day", "Reminders Set on This iPhone"])
        #expect(app.about.version == LabeledValue("Version", "0.1.0 (1)"))
        // Android's device words are lowercase; the label is title case throughout.
        for (device, label) in [
            ("this phone", "Reminders Set on This Phone"), ("this tablet", "Reminders Set on This Tablet"),
            ("this iPad", "Reminders Set on This iPad"), ("this Mac", "Reminders Set on This Mac"),
        ] {
            let sections = TasksSettings.sections(SettingsValues(), permission: .on, scheduled: 0, device: device, locale: Locale(identifier: "en_US"))
            #expect(sections[0].rows.last?.label == label)
        }
        // Routed as the shell routes them, with what TasksShell configures.
        let route = { SettingsRoute<TasksApp>.route($0, settings: { .settings($0) }, clearSaved: .clearSaved) }
        #expect(
            route(.toggle(row: .app(TasksSettings.remind), on: false))
                == .app(.settings(.toggle(row: .app(TasksSettings.remind), on: false))))
        #expect(route(.perform(row: .repository(.signOut))) == .link(.signOut))
        #expect(route(.perform(row: .storage(.clear))) == .app(.clearSaved))
        #expect(route(.perform(row: .repository(.changeAccess))) == .changeAccess)
        #expect(route(.perform(row: .about(.check))) == .checkForUpdates)
    }
}

@Suite("Substrate Tasks under the link", .timeLimit(.minutes(10)))
struct LinkedTests {
    @Test func aReadAnswered401EndsTheAccessOnTheLink() async throws {
        let rig = try Rig()
        rig.server.answer("GET", "/api/v1/records", 401, #"{"error":{"code":"unauthorized","message":"token expired"}}"#)
        let storage = MemoryStorage()
        try SessionStore(storage: storage).save(
            Session(
                server: server, repository: "ada.example.com", secret: "sub_app_x",
                clientID: TasksManifest.localClientID, scopes: TasksManifest.scopes))
        let link = LinkController(
            app: rig.app, manifest: TasksManifest.manifest(clientID: TasksManifest.localClientID),
            sessions: SessionStore(storage: storage), outbox: rig.outbox, packages: TasksManifest.packages,
            words: StatusWords(app: TasksApp.appName, device: "this iPhone", kept: "tasks"))
        var environment = rig.environment
        environment.devices.connectivity = ScriptedConnectivity()
        let harness = ControllerHarness(link, clock: rig.clock, environment: environment)
        try await harness.until { await $0.presentation.snapshot.phase == .accessEnded }
        let presented = await harness.presentation
        #expect(presented.ended?.title == "Substrate Tasks's access ended")
        let app = try #require(await harness.runtime.state(of: LinkController<TasksApp>.appKey))
        guard case .stopped = app.linked.phase else { throw Unexpected() }
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
        let manifest = TasksManifest.manifest(clientID: TasksManifest.localClientID)
        #expect(recorded.name == manifest.appName)
        #expect(recorded.scopes == manifest.scopes)
        #expect(recorded.asks == [Recorded.Ask(package: TasksKinds.packageID, atLeast: TasksManifest.version)])
        #expect(recorded.publishes == [TasksKinds.packageID])
        #expect(recorded.apple["clientIDKey"] == .string(TasksManifest.clientIDKey))
        #expect(recorded.apple["redirectKey"] == .string(TasksManifest.redirectKey))
    }
}
