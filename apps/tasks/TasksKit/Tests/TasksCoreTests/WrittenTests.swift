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

/// Whether a watch asks for `kind`.
func asksFor(watch sent: ScriptedServer.Sent, _ kind: String) -> Bool {
    ((sent.query ?? "").removingPercentEncoding ?? "").contains("kinds=\(kind)")
}

/// Whether a list read asks for `kind`.
func asksFor(_ sent: ScriptedServer.Sent, _ kind: String) -> Bool {
    ((sent.query ?? "").removingPercentEncoding ?? "").contains("\"\(kind)\"")
}

@Suite("What became of a write", .timeLimit(.minutes(10)))
struct WrittenTests {
    @Test func aWriteThatLeftTheQueueWithoutLandingIsNotLaidOver() async throws {
        let rig = try Rig()
        let harness = rig.harness(performsEffects: false)
        await harness.deliver(.records(.loaded([taskRecord("a", "Eggs", version: 1)], cursor: nil, queue: [])))
        await harness.deliver(.settingsRead(SettingsValues(), usage: []))
        await harness.send(.show(.inbox))
        await harness.send(.complete(t("a")))
        // The window over: the write the completion waited with.
        await harness.deliver(.records(.wrote(try #require(await harness.state.pending[t("a")]?.write))))
        let write = try #require(
            harness.effects.compactMap { effect -> OutboxEntry? in
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
        await harness.send(.complete(t("t1")))
        try await window(harness)
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
        try storage.write(
            JSONEncoder().encode(
                Session(
                    server: server, repository: "ada.example.com", secret: "sub_app_x",
                    clientID: TasksManifest.localClientID, scopes: TasksManifest.scopes)),
            account: "session")
        let link = LinkController(
            app: rig.app, manifest: TasksManifest.manifest(clientID: TasksManifest.localClientID),
            sessions: SessionStore(storage: storage), outbox: rig.outbox, packages: TasksManifest.packages,
            words: TasksApp.words(device: "this iPhone"))
        var environment = rig.environment
        environment.devices.connectivity = ScriptedConnectivity()
        return ControllerHarness(link, clock: rig.clock, environment: environment)
    }

    @Test func anApprovalsNewTokenIsWhatBothLinksReadAndFollowWith() async throws {
        let rig = try Rig()
        let old = "Bearer sub_app_x"
        let new = "Bearer sub_app_y"
        // The old token is revoked once the new one is kept: every request
        // still carrying it is refused, as the server refuses a revoked one.
        let revoked = Network()
        revoked.online = false
        rig.server.on { sent in
            revoked.online && sent.authorization == old
                ? (401, #"{"error":{"code":"unauthorized","message":"the token was revoked"}}"#) : nil
        }
        rig.server.answer("GET", "/api/v1/records", 200, page([taskRecord("t1", "Eggs")]))
        rig.server.on { $0.method == "GET" && $0.path == "/api/v1/changes" ? (200, #"{"bookmark":7,"generation":"g1"}"#) : nil }
        let storage = MemoryStorage()
        let session = { (secret: String, scopes: [String]) in
            try JSONEncoder().encode(
                Session(server: server, repository: "ada.example.com", secret: secret, clientID: TasksManifest.localClientID, scopes: scopes))
        }
        try storage.write(session("sub_app_x", TasksManifest.scopes), account: "session")
        let sessions = SessionStore(storage: storage)
        let link = LinkController(
            app: rig.app, manifest: TasksManifest.manifest(clientID: TasksManifest.localClientID),
            sessions: sessions, outbox: rig.outbox, packages: TasksManifest.packages,
            words: TasksApp.words(device: "this iPhone"))
        var environment = rig.environment
        environment.devices.connectivity = ScriptedConnectivity()
        // The token each request carries is the session's as it is kept now.
        let transport = rig.server
        environment.client = ClientSource {
            await sessions.load().map { session in
                var client = SubstrateClient(server: session.server, token: session.secret, transport: transport)
                client.pause = { _ in }
                return client
            }
        }
        let harness = ControllerHarness(link, clock: rig.clock, environment: environment)
        let key = LinkController<TasksApp>.appKey
        try await harness.until { h in
            let app = await h.runtime.state(of: key)
            return app?.linked.phase == .following && app?.settingsLinked.phase == .following
        }

        // Approved: a new session with a scope more, kept, and the old token revoked.
        let approved = TasksManifest.scopes + ["write:ada.example.com/work/chore"]
        try storage.write(session("sub_app_y", approved), account: "session")
        revoked.online = true
        await harness.runtime.send(LinkController<TasksApp>.Command.signedIn)
        try await harness.until { h in await h.runtime.state(of: key)?.linked.link?.scopes == approved }
        // Both links follow again, with the new token, and nothing says the
        // tasks are not being updated. (The scripted watch ends after its
        // bookmark, so either may be waiting to open again: never stopped.)
        let watches: @Sendable (String) -> Bool = { kind in
            rig.server.sent.contains { $0.path == "/api/v1/changes" && $0.authorization == new && asksFor(watch: $0, kind) }
        }
        try await harness.until { _ in watches(TasksKinds.task) && watches(TasksKinds.collection) }
        let state = try #require(await harness.runtime.state(of: key))
        for phase in [state.linked.phase, state.settingsLinked.phase] {
            if case .stopped = phase { Issue.record("a link stopped: \(phase)") }
        }
        let presented = try #require(await harness.presentation.app)
        #expect(!presented.notices.contains { $0.text.hasPrefix("Your tasks are not being updated") }, "\(presented.notices)")
    }

    @Test func aDecisionOnAVersionAnotherDeviceMovedIsParkedOnItsRow() async throws {
        let rig = try Rig()
        let records = [taskRecord("t1", "Reply to Maya", status: "proposed", version: 1)]
        rig.server.answer("GET", "/api/v1/records", 200, page(records))
        rig.server.on { $0.method == "GET" && $0.path == "/api/v1/changes" ? (200, #"{"bookmark":7,"generation":"g1"}"#) : nil }
        rig.server.answer("GET", path(TasksKinds.task, "t1"), 200, json(taskRecord("t1", "Reply to Maya", status: "abandoned", version: 2)))
        rig.server.on { $0.method == "PATCH" ? (409, #"{"error":{"code":"conflict","message":"version 2, not 1"}}"#) : nil }
        let harness = rig.harness()
        await harness.runtime.linkChanged(signedIn)
        try await harness.until { await $0.state.linked.phase == .following }
        await harness.send(.show(.suggestions))
        // Accepted here while another device dismissed it.
        await harness.send(.accept(t("t1")))
        try await window(harness)
        try await harness.until { _ in await !rig.outbox.entries.isEmpty }
        let entry = try #require(await rig.outbox.entries.first)
        #expect(entry.baseVersion == 1)
        #expect(entry.properties == ["status": "open"])
        guard case .refused = await rig.outbox.flush(rig.client) else { throw Unexpected() }
        #expect(rig.server.sent.contains { $0.method == "PATCH" && $0.body?["ifVersion"] == 1 })
        // Parked as a conflict, never written over; the row says so.
        let parked = try #require(await rig.outbox.parked.first)
        #expect(parked.conflict)
        try await harness.until { await $0.presentation.queue?.rows[t("t1")]?.refused != nil }
        #expect(await harness.presentation.queue?.rows[t("t1")]?.refused == "Change refused: it changed on another device")
    }

    @Test func aDecisionInItsWindowOutlivesTheAppAndIsWrittenOnce() async throws {
        let rig = try Rig()
        let harness = rig.harness(performsEffects: false)
        await harness.deliver(.records(.loaded([taskRecord("t1", "Eggs", version: 2)], cursor: nil, queue: [])))
        await harness.send(.complete(t("t1")))
        // Kept on the device before the owner saw it made.
        let kept = try #require(DecisionFile.load(from: rig.folder)[t("t1")])
        #expect(kept.decision == .complete)
        #expect(kept.deadline == now.addingTimeInterval(TasksApp.undoWindow))
        #expect(kept.write.entries.first?.baseVersion == 2)

        // The app closed inside the window, and opened again after it: the
        // decision is written at once, offline as online, and once.
        await harness.advance(by: TasksApp.undoWindow + 5)
        let again = rig.harness()
        try await again.until { await $0.state.settingsRead }
        try await again.until { _ in await rig.outbox.entries.map(\.id) == kept.write.entries.map(\.id) }
        // Taken off the device once the outbox has it (a file write, which
        // moves no state: polled).
        for _ in 0 ..< 500 where !DecisionFile.load(from: rig.folder).isEmpty { try await Task.sleep(for: .milliseconds(10)) }
        #expect(DecisionFile.load(from: rig.folder).isEmpty)
        let third = rig.harness()
        try await third.until { await $0.state.settingsRead }
        await third.advance(by: 10)
        await third.settle()
        #expect(await rig.outbox.entries.count == 1)
        // Closed between the outbox taking it and the device forgetting it:
        // the next launch finds it queued and writes nothing more.
        DecisionFile.update(rig.folder) { $0[t("t1")] = kept }
        let fourth = rig.harness()
        try await fourth.until { await $0.state.settingsRead }
        try await fourth.until { _ in
            fourth.effects.contains(.forgetDecisions([], paths: [t("t1")]))
        }
        await fourth.settle()
        #expect(await rig.outbox.entries.count == 1)
        #expect(await fourth.state.pending.isEmpty)
    }

    @Test func aDecisionTakenBackLeavesNothingOnTheDeviceAndOneBeforeItsDeadlineWaitsForIt() async throws {
        let rig = try Rig()
        let harness = rig.harness(performsEffects: false)
        await harness.deliver(.records(.loaded([taskRecord("t1", "Eggs"), taskRecord("t2", "Milk")], cursor: nil, queue: [])))
        await harness.send(.complete(t("t1")))
        await harness.send(.undo(t("t1")))
        #expect(DecisionFile.load(from: rig.folder).isEmpty)
        await harness.send(.abandon(t("t2")))
        // Opened again inside the window: shown decided, written at its deadline.
        let again = rig.harness()
        try await again.until { await $0.state.pending[t("t2")]?.decision == .wontDo }
        #expect(await rig.outbox.entries.isEmpty)
        try await window(again)
        try await again.until { _ in await rig.outbox.entries.first?.properties == ["status": "abandoned"] }
    }

    @Test func aGroupKindsDeclarationIsReadOnItsOwnWhereTheTokenReadsIt() async throws {
        let rig = try Rig()
        let chore =
            #"{"identity":"ada.example.com/work/chore","definition":{"traits":["substrate.reamde.dev/core/lifecycle(status: state)"],"properties":{"summary":{"type":"string"},"state":{"type":"enum","values":["todo","closed"],"done":["closed"]},"area":{"type":"reference","kind":"ada.example.com/work/area"},"site":{"type":"reference","kind":"ada.example.com/work/site"}}}}"#
        rig.server.answer(
            "GET", "/api/v1/substrate.reamde.dev/core/trait/substrate.reamde.dev%2Fcore%2Flifecycle/implementors", 200, #"{"items":[\#(chore)]}"#)
        rig.server.answer(
            "GET", "/api/v1/substrate.reamde.dev/core/kind/ada.example.com%2Fwork%2Farea", 200,
            #"{"id":"ada.example.com/work/area","kind":"substrate.reamde.dev/core/kind","version":1,"properties":{"label":{"singular":"Area","plural":"Garden areas"},"properties":{"name":{"type":"string"},"position":{"type":"decimal"}}}}"#
        )
        // A kind the token does not read answers none.
        rig.server.answer("GET", "/api/v1/substrate.reamde.dev/core/kind/ada.example.com%2Fwork%2Fsite", 404, #"{"error":{"code":"not_found","message":"no"}}"#)
        rig.server.answer("GET", "/api/v1/records", 200, page([]))
        rig.server.on { $0.method == "GET" && $0.path == "/api/v1/changes" ? (200, #"{"bookmark":7,"generation":"g1"}"#) : nil }
        let harness = rig.harness()
        await harness.runtime.linkChanged(signedIn)
        try await harness.until { await !$0.state.declarations.isEmpty }
        let declarations = await harness.state.declarations
        #expect(declarations.map(\.identity) == ["ada.example.com/work/chore", "ada.example.com/work/area"])
        #expect(declarations.last?.pluralLabel == "Garden areas")
        #expect(declarations.last?.type(of: "position") == "decimal")
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
        try await harness.runtime.send(TasksApp.Command.edit(t("t1")), to: LinkController<TasksApp>.appPath)
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
        rig.server.answer(
            "GET", "/api/v1/records", 403, #"{"error":{"code":"forbidden","message":"not in scope"}}"#,
            headers: ["WWW-Authenticate": #"Bearer error="insufficient_scope""#])
        let harness = try linked(rig)
        try await harness.until { await $0.presentation.status.actions.contains { $0.action == .review } }
        let app = try #require(await harness.runtime.state(of: LinkController<TasksApp>.appKey))
        guard case .stopped(.needsApproval, _) = app.linked.phase else { throw Unexpected() }
        // The network's first report is no new approval: the read is not
        // asked again, to be refused the same way.
        let mark = await harness.effectsStarted
        try await harness.until { await $0.presentation.snapshot.online == true }
        try await harness.untilIdle(since: mark)
        // Asked for once, as every effect is traced in the step that asks.
        let reads = harness.trace.count { entry in
            if case let .effect(_, effect) = entry, (effect as? TasksApp.Effect) == .records(.read(over: TasksKinds.all)) { true } else { false }
        }
        #expect(reads == 1)
        // The data link's read; the settings link reads its own kinds.
        #expect(rig.server.sent.count { $0.method == "GET" && $0.path == "/api/v1/records" && asksFor($0, TasksKinds.task) } == 1)
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
        #expect(!harness.effects.contains(.records(.follow(cursor, over: TasksKinds.all))))
        await harness.send(.foreground)
        #expect(await harness.state.linked.phase == .following)
        #expect(harness.effects.contains(.records(.follow(cursor, over: TasksKinds.all))))
    }
}
