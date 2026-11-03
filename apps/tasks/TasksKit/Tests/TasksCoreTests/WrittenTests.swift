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

@Suite("What became of a write")
struct WrittenTests {
    @Test func aWriteThatLeftTheQueueWithoutLandingIsNotLaidOver() async throws {
        let rig = try Rig()
        let harness = rig.harness(performsEffects: false)
        await harness.deliver(.records(.loaded([taskRecord("a", "Eggs", version: 1)], cursor: nil, queue: [])))
        await harness.deliver(.settingsRead(SettingsValues(), usage: []))
        await harness.send(.show(.inbox))
        await harness.send(.complete("a"))
        let write = try #require(harness.effects.compactMap { effect -> OutboxEntry? in
            if case let .records(.enqueue(write)) = effect { return write.entries.first }
            return nil
        }.first)
        await harness.deliver(.records(.enqueued([write.id], queue: [write], landed: [])))
        #expect(await harness.presentation.list?.open.rows.isEmpty == true)
        // Discarded on the Status screen: gone from the queue, landed never.
        harness.clearTrace()
        await harness.deliver(.records(.queueReported([], landed: [])))
        await harness.advance(by: 0.2)
        #expect(await harness.state.linked.server["\(TasksKinds.task)/a"]?.properties["status"] == "open")
        #expect(titles(await harness.presentation) == ["Eggs"])
        // Read again all the same, in case another write moved it.
        #expect(harness.effects.contains(.records(.reread(["\(TasksKinds.task)/a"]))))
    }

    @Test func aWriteTheServerRefusesIsTakenBackFromWhatIsShown() async throws {
        let rig = try Rig()
        let records = [taskRecord("t1", "Eggs", position: "1")]
        rig.server.answer("GET", "/api/v1/records", 200, page(records))
        rig.server.on { $0.method == "GET" && $0.path == "/api/v1/changes" ? (200, #"{"bookmark":7,"generation":"g1"}"#) : nil }
        rig.server.answer("GET", path(TasksKinds.task, "t1"), 200, json(records[0]))
        rig.server.on { $0.method == "PATCH" ? (422, #"{"error":{"code":"invalid","message":"status: done is not allowed"}}"#) : nil }
        let harness = rig.harness()
        await harness.runtime.linkChanged(signedIn)
        try await harness.until { await $0.state.linked.phase == .following }
        await harness.send(.show(.inbox))
        await harness.send(.complete("t1"))
        #expect(titles(await harness.presentation).isEmpty)
        try await harness.until { await $0.state.linked.unconfirmed.isEmpty }
        guard case .refused = await rig.outbox.flush(rig.client) else { throw Unexpected() }
        // Parked: the record shows as the server holds it, while the Status
        // screen offers Retry and Discard.
        try await harness.until { titles(await $0.presentation) == ["Eggs"] }
        #expect(await harness.state.linked.queue.first?.parked == true)
    }

    /// The link over the app with a session, a network and the rig's server.
    func linked(_ rig: Rig) throws -> ControllerHarness<LinkController<TasksApp>> {
        let storage = MemoryStorage()
        try SessionStore(storage: storage).save(Session(server: server, repository: "ada.example.com", secret: "sub_app_x",
                                                        clientID: TasksManifest.localClientID, scopes: TasksManifest.scopes))
        let link = LinkController(app: rig.app, manifest: TasksManifest.manifest(clientID: TasksManifest.localClientID),
                                  sessions: SessionStore(storage: storage), outbox: rig.outbox, packages: TasksManifest.packages,
                                  words: TasksApp.words(device: "this iPhone"))
        var environment = rig.environment
        environment.devices.connectivity = ScriptedConnectivity()
        return ControllerHarness(link, clock: rig.clock, environment: environment)
    }

    @Test func anEditOnAVersionSinceMovedReachesStatusAsAConflict() async throws {
        let rig = try Rig()
        let records = [taskRecord("t1", "Eggs", position: "1", version: 1)]
        rig.server.answer("GET", "/api/v1/records", 200, page(records))
        rig.server.on { $0.method == "GET" && $0.path == "/api/v1/changes" ? (200, #"{"bookmark":7,"generation":"g1"}"#) : nil }
        rig.server.answer("GET", path(TasksKinds.task, "t1"), 200, json(taskRecord("t1", "Brown eggs", position: "1", version: 2)))
        rig.server.on { $0.method == "PATCH" ? (409, #"{"error":{"code":"conflict","message":"version 2, not 1"}}"#) : nil }
        let harness = try linked(rig)
        try await harness.until { await $0.runtime.state(of: LinkController<TasksApp>.appKey)?.linked.phase == .following }
        try await harness.runtime.send(TasksApp.Command.edit("t1"), to: LinkController<TasksApp>.appPath)
        let key = try #require(await harness.runtime.state(of: LinkController<TasksApp>.appKey)?.editors.keys.first)
        try await harness.runtime.send(TaskEditor.Command.setName("Six eggs"), to: LinkController<TasksApp>.appPath + [key])
        try await harness.runtime.send(TaskEditor.Command.save, to: LinkController<TasksApp>.appPath + [key])
        // The link sends it, the server says the task moved on: it is the
        // owner's to answer, said outside Settings too.
        try await harness.until { await $0.presentation.status.refused?.rows.count == 1 }
        let status = await harness.presentation.status
        #expect(status.refused?.rows.first?.label == "Task")
        #expect(status.refused?.rows.first?.status == "Change refused")
        #expect(status.notice?.text == "1 change needs you")
        #expect(rig.server.sent.contains { $0.method == "PATCH" && $0.body?["ifVersion"] == 1 })
    }

    @Test func aReadAnswered403AsksTheOwnerForApproval() async throws {
        let rig = try Rig()
        rig.server.answer("GET", "/api/v1/records", 403, #"{"error":{"code":"forbidden","message":"not in scope"}}"#,
                          headers: ["WWW-Authenticate": #"Bearer error="insufficient_scope""#])
        let harness = try linked(rig)
        try await harness.until { await $0.presentation.status.actions.contains { $0.action == .review } }
        let app = try #require(await harness.runtime.state(of: LinkController<TasksApp>.appKey))
        guard case .stopped(.needsApproval, _) = app.linked.phase else { throw Unexpected() }
        // The network's first report is no new approval: the read is not
        // asked again, to be refused the same way.
        try await harness.until { await $0.presentation.snapshot.online == true }
        await harness.settle()
        #expect(rig.server.sent.count { $0.method == "GET" && $0.path == "/api/v1/records" } == 1)
        // The access stands: the token was not refused, the approval falls short.
        #expect(await harness.presentation.snapshot.phase == .signedIn)
    }

    @Test func aWatchTheServerStoppedOpensAgainWhenTheAppComesForward() async throws {
        let rig = try Rig()
        let harness = rig.harness(performsEffects: false)
        let cursor = WatchCursor(seq: 7, generation: "g1")
        await harness.deliver(.records(.loaded([], cursor: cursor, queue: [])))
        await harness.deliver(.records(.linked(signedIn)))
        #expect(await harness.state.linked.phase == .following)
        await harness.deliver(.records(.watchEnded(.refused, "The server is restarting.")))
        #expect(await harness.state.linked.phase == .stopped(.refused, "The server is restarting."))
        // A link that moves for another reason leaves it stopped.
        var waiting = signedIn
        waiting.waiting = 1
        harness.clearTrace()
        await harness.deliver(.records(.linked(waiting)))
        #expect(!harness.effects.contains(.records(.follow(cursor))))
        await harness.send(.foreground)
        #expect(await harness.state.linked.phase == .following)
        #expect(harness.effects.contains(.records(.follow(cursor))))
    }
}
