import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing

@testable import ChatKit

/// DESIGN.md's seven rules ("The store and its concurrency rules"), one
/// test each by its number, against a scripted server on the kit's runtime.
@Suite("The seven rules", .timeLimit(.minutes(10)))
struct SevenRules {
    @Test func rule1_aConversationIsKeyedByItsClientIDAndTheThreadEventBindsIt() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream))])
        try await rig.signIn()
        // Another window opened the thread the new chat's send will name.
        await rig.harness.send(.show(thread: "t1", scene: "b"))
        let other = try #require(await rig.harness.state.scenes["b"]?.current)
        let key = try await rig.newChat("a")
        #expect(key != other)
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { await $0.state.conversations[key]?.thread == "t1" }
        // The key is the one the list minted; the server's thread is bound to
        // it, and the other conversation on that thread is gone, its window
        // showing this one.
        let state = await rig.harness.state
        #expect(state.conversations[other] == nil)
        #expect(state.scenes["b"]?.current == key)
        #expect(state.key(of: "t1") == key)
        await rig.harness.send(.show(thread: "t1", scene: "c"))
        #expect(await rig.harness.state.scenes["c"]?.current == key)
    }

    @Test func rule2_oneStreamPerConversation() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 2))])
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { await $0.state.conversations[key]?.thread == "t1" }
        // Opening the chat the app streams, in another window, opens nothing.
        let mark = await rig.harness.effectsStarted
        await rig.harness.send(.show(thread: "t1", scene: "other"))
        try await rig.send(.open, to: key)
        // Past the folder's coalesced saves, which wait on the clock.
        try await rig.harness.untilIdle(since: mark, advancing: 1)
        #expect(rig.server.count("/events") == 0)
        #expect(rig.server.count("/chat") == 1)
        rig.server.release()
        try await rig.harness.until { await $0.runtime.state(of: key)?.settles == 1 }
    }

    @Test func rule3_noRowsPublishUnderALiveOverlay() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 2))])
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.overlay.turns.contains { $0.role == .assistant } == true }
        // An ordinary read of the thread while it streams: refused outright.
        let mark = await rig.harness.effectsStarted
        await rig.harness.send(.show(thread: "t1", scene: "other"))
        try await rig.harness.untilIdle(since: mark, advancing: 1)
        #expect(rig.server.count("/api/v1/records", query: "llm/message") == 0)
        #expect(await rig.harness.state.transcripts["t1"] == nil)
        rig.server.release()
        // Its own handover read does.
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        #expect(await rig.turns(key) == ["Hi", "Hello"])
    }

    @Test func rule4_aReadIsDroppedWhenItsThreadsEpochMoved() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = ChatAppState()
        state.session = ChatSession(LinkSnapshot(phase: .signedIn, server: origin, repository: repository))
        state.readEpochs["t1"] = 1
        let rows = [Record(id: "m", kind: Kinds.message, properties: ["role": "user", "content": "x"])]
        // Started under epoch 0: a send moved it since.
        let late = try app.apply(.init(.rowsRead(thread: "t1", epoch: 0, version: 3, rows: rows, full: false)), to: state)
        #expect(late.transcripts["t1"] == nil)
        let current = try app.apply(.init(.rowsRead(thread: "t1", epoch: 1, version: 3, rows: rows, full: false)), to: state)
        #expect(current.transcripts["t1"]?.records == rows)
    }

    @Test func rule3_aCachedReadThatLandsUnderALiveOverlayIsDropped() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        // The chat was opened, its disk read asked for; then a send took it.
        state.conversations[ChildKey("k")] = .init(
            seed: .init(thread: "t1", agent: helper, draft: "", listen: false), thread: "t1",
            agent: helper, owns: true)
        let late = try app.apply(.init(.cachedLoaded(thread: "t1", epoch: 0, cachedRows)), to: state)
        #expect(late.transcripts["t1"] == nil)
        #expect(!late.asks.contains { if case .read = $0 { true } else { false } })
    }

    @Test func rule4_aCachedReadIsDroppedWhenItsThreadsEpochMoved() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        let key = ChildKey("k")
        state.conversations[key] = .init(seed: .init(thread: "t1", agent: helper, draft: "", listen: false), thread: "t1", agent: helper)
        // An attach takes the thread and a settle hands it back: each moves
        // the epoch.
        let report = ChatAppStep.Report(
            thread: "t1", agent: helper, owns: true, active: true, streaming: true, outcomes: 0, name: "Helper",
            settles: 0, named: nil, read: nil, draft: nil, sent: false, failure: nil)
        state = try app.apply(.init(.conversation(key, report)), to: state)
        #expect(state.readEpochs["t1"] == 1)
        // The move cancels the read in flight, by its key.
        #expect(state.asks.contains(.cancel("read:t1")))
        var settled = report
        (settled.owns, settled.active, settled.streaming, settled.settles) = (false, false, false, 1)
        state = try app.apply(.init(.conversation(key, settled)), to: state)
        #expect(state.readEpochs["t1"] == 2)
        // The disk's rows, asked for before the attach, arrive after the settle.
        let late = try app.apply(.init(.cachedLoaded(thread: "t1", epoch: 0, cachedRows)), to: state)
        #expect(late.transcripts["t1"] == nil)
        let current = try app.apply(.init(.cachedLoaded(thread: "t1", epoch: 2, cachedRows)), to: state)
        #expect(current.transcripts["t1"]?.records == cachedRows?.records)
    }

    @Test func rule5_aNewRunDropsWhatAnOlderRunStillDelivers() async throws {
        let rig = ChatRig(
            baseRoutes(
                threads: #"{"head":10,"generation":"g","records":[\#(threadJSON(version: 3, status: "running"))]}"#,
                status: "running")
                + [.init(method: "GET", path: "/events", answer: .stream([#"{"kind":"idle","status":"running"}"#]))])
        try await rig.signIn()
        // The attach's first read is held on the wire.
        rig.server.hold("/llm/thread/t1")
        let first = await rig.harness.effectsStarted
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in rig.server.count("/llm/thread/t1") == 1 }
        let second = await rig.harness.effectsStarted
        // Back in front: the conversation attaches again, a run of its own.
        await rig.harness.send(.phase(.active))
        try await rig.harness.until { _ in rig.server.count("/llm/thread/t1") == 2 }
        rig.server.letGo("/llm/thread/t1")
        try await rig.harness.until { _ in await rig.live(key)?.phase == .background }
        // Everything the first attach started has ended, its held read with
        // it, the saves it asked for once the clock passes them.
        try await rig.harness.untilIdle(since: first, through: second, advancing: 1)
        // The older run's read came back too, and did nothing: had it landed,
        // it would have attached a second stream.
        #expect(rig.server.count("/events") == 1)
    }

    @Test func rule6_aSignOutDropsEverythingStillOnItsWay() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        _ = try await rig.newChat()
        rig.server.hold("/api/v1/records")
        await rig.harness.send(.refresh)
        try await rig.harness.until { await $0.state.refreshing }
        await rig.signOut()
        rig.server.letGo("/api/v1/records")
        try await rig.harness.until { await $0.runtime.counters.droppedByGeneration >= 1 }
        let state = await rig.harness.state
        #expect(state.session == nil)
        #expect(state.agents.isEmpty)
        #expect(state.conversations.isEmpty)
        // Sign Out forgets the saved chats, and every notification; the
        // folder keeps only the link generation, moved.
        try await rig.harness.until { _ in (try? FileManager.default.contentsOfDirectory(atPath: rig.root.path)) == ["link.json"] }
        #expect(try rig.generation == 1)
        #expect(rig.notifications.calls.contains(.cancelAll))
    }

    /// Rule 6's last leg is the link's: the session leaves the Keychain.
    @Test func rule6_aSignOutDeletesTheKeychainItem() async throws {
        let storage = MemoryStorage()
        let sessions = SessionStore(storage: storage)
        let manifest = ChatManifest.manifest
        try await sessions.save(
            Session(
                server: origin, repository: repository, secret: "secret", clientID: manifest.clientID,
                scopes: manifest.scopes))
        let server = ChatServer(baseRoutes() + [.init(method: "POST", path: "/oauth/revoke", answer: .json(200, "{}"))])
        var environment = AppEnvironment.testing(
            transport: server,
            client: .fixed(SubstrateClient(server: origin, token: "secret", transport: server)))
        environment.devices.notifications = RecordedNotifications()
        let app = ChatApp(storage: ChatStorage(base: temporaryFolder("chat")), preferences: MemoryPreferenceStore(), platform: .macos)
        let link = LinkController(
            app: app, manifest: manifest, sessions: sessions,
            words: StatusWords(app: ChatManifest.appName, device: "this Mac", kept: "saved chats"))
        let harness = ControllerHarness(link, environment: environment)
        try await harness.until { await $0.presentation.snapshot.phase == .signedIn }
        await harness.send(.signOut)
        try await harness.until { await $0.presentation.snapshot.phase == .signedOut }
        #expect(await sessions.load() == nil)
        #expect(server.count("/oauth/revoke") == 1)
    }

    @Test func rule7_cachedRowsAreCurrentOnlyWhileTheirThreadIsSettledAndUnmoved() {
        var state = ChatAppState()
        let thread = Record(id: "t1", kind: Kinds.thread, properties: ["mode": "chat", "status": "ok"], version: 3)
        state.chats = [thread]
        state.transcripts["t1"] = .init(records: [], turns: [], version: 3, full: false)
        #expect(state.isCurrent("t1"))
        // Read under another version: not current.
        state.chats[0].version = 4
        #expect(!state.isCurrent("t1"))
        // A running thread's rows are incomplete by definition.
        state.chats[0].version = 3
        state.chats[0].properties["status"] = "running"
        #expect(!state.isCurrent("t1"))
        // A window that filled is read whole when opened.
        state.chats[0].properties["status"] = "ok"
        state.transcripts["t1"]?.full = true
        #expect(!state.isCurrent("t1"))
    }
}

@Suite("The conversation's run", .timeLimit(.minutes(10)))
struct ConversationRuns {
    @Test func aSendStreamsThenHandsOverInOneStepWithoutDoubling() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream))])
        try await rig.signIn()
        let key = try await rig.newChat()
        let presentations = await rig.runtime.presentations()
        let seen = Seen()
        let watching = Task {
            for await tree in presentations {
                if let screen = tree.conversations[key] { seen.add(screen.turns.count) }
            }
        }
        defer { watching.cancel() }
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        #expect(await rig.turns(key) == ["Hi", "Hello"])
        // Said once, by the list, for the chat it settled in.
        try await rig.harness.until { await $0.presentation.answered.serial == 1 }
        #expect(await rig.harness.presentation.answered.value == ChatAnswer(thread: "t1", words: "Helper answered"))
        let live = try #require(await rig.live(key))
        #expect(live.phase == .idle && live.overlay.turns.isEmpty)
        // The rows landed and the overlay cleared in one step: no tree ever
        // held the turn twice.
        await rig.harness.settle()
        #expect(seen.counts.allSatisfy { $0 <= 2 })
    }

    @Test func aRefusalGivesTheTextBackAndNothingRuns() async throws {
        let refusal = #"{"error":{"code":"conflict","message":"a turn is already running on this thread"}}"#
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .json(409, refusal))])
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.outcomes.isEmpty == false }
        let live = try #require(await rig.live(key))
        #expect(live.outcomes.first?.kind == .refused)
        #expect(live.outcomes.first?.reason == "a turn is already running on this thread")
        #expect(live.draft == "Hi")
        #expect(live.phase == .idle && live.overlay.turns.isEmpty && live.thread == nil)
        // The field takes the text it gave back.
        #expect(await rig.conversation(key)?.composer.draft == "Hi")
    }

    @Test func aGatewaysAnswerIsUnknownDeliveryNeverARefusal() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .json(502, "<html>bad gateway</html>"))])
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.checking == true }
        // The check reads a second later: the clock moves to each sleep as
        // it begins, its presentation's window and then the check's, and no
        // further than two seconds on.
        try await rig.harness.until(advancing: 2) { _ in await rig.live(key)?.outcomes.contains { $0.kind == .unknown } == true }
        #expect(await rig.live(key)?.outcomes.contains { $0.kind == .refused } == false)
        // Nothing is sent again on its own.
        #expect(rig.server.count("/chat") == 1)
    }

    @Test func streamedTextPresentsWithinItsWindowAndStructureAtOnce() throws {
        let conversation = Conversation(
            key: ChildKey("k"), seed: .init(thread: nil, agent: helper, draft: "", listen: false), facts: nil,
            online: true, keyless: nil, newestTurn: nil, visible: true, pollNudge: 0, resumeNudge: 0,
            continuing: nil)
        var delta = AgentEvent(kind: .delta)
        delta.text = "a"
        #expect(conversation.coalescing(of: .init(.streamed(delta, at: Date()))) == .within(.milliseconds(50)))
        #expect(conversation.coalescing(of: .init(.streamed(AgentEvent(kind: .toolStarted), at: Date()))) == .now)
    }
}

@Suite("The list", .timeLimit(.minutes(10)))
struct ListRules {
    @Test func olderRowsNeverReplaceNewerOnes() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = ChatAppState()
        state.session = ChatSession(LinkSnapshot(phase: .signedIn, server: origin, repository: repository))
        let newer = [Record(id: "a", kind: Kinds.message, properties: ["role": "user", "content": "new"])]
        let older = [Record(id: "b", kind: Kinds.message, properties: ["role": "user", "content": "old"])]
        state = try app.apply(.init(.rowsRead(thread: "t1", epoch: 0, version: 5, rows: newer, full: false)), to: state)
        state = try app.apply(.init(.rowsRead(thread: "t1", epoch: 0, version: 3, rows: older, full: false)), to: state)
        #expect(state.transcripts["t1"]?.records.first?.id == "a")
    }

    @Test func anAppTokenListsTheCardsUnderTheThreadsItKnows() async throws {
        let interaction =
            #"{"head":10,"generation":"g","records":[{"id":"i1","kind":"substrate.reamde.dev/llm/interaction","version":1,"createdAt":"2026-10-31T10:00:02Z","updatedAt":"2026-10-31T10:00:02Z","properties":{"state":"pending","thread":{"ref":"substrate.reamde.dev/llm/thread/t1"}}}]}"#
        let named = #""thread":{"in":["t1"]}"#
        let forbidden = #"{"error":{"code":"forbidden","message":"an app's token lists it under filter.properties.thread"}}"#
        let threads = #"{"head":10,"generation":"g","records":[\#(threadJSON(version: 3))]}"#
        let rig = ChatRig(
            [
                .init(method: "GET", path: "/api/v1/records", query: named, answer: .json(200, interaction)),
                .init(method: "GET", path: "/api/v1/records", query: "llm/interaction", answer: .json(403, forbidden)),
            ] + baseRoutes(threads: threads), scopes: ["agents:*"])
        try await rig.signIn()
        #expect(await rig.harness.state.refreshError == nil)
        #expect(await rig.harness.state.interactions.map(\.id) == ["i1"])
        let cards = rig.server.requests.filter { $0.contains("/api/v1/records") && $0.contains("llm/interaction") }
        #expect(!cards.isEmpty && cards.allSatisfy { $0.contains(named) })
        // Without `decide:` the suggestions are not asked for at all.
        #expect(rig.server.count("/api/v1/records", query: "recordpatchrequest") == 0)
    }

    @Test func aTranscriptReadThatFailedIsAskedForAgain() async throws {
        let threads = #"{"head":10,"generation":"g","records":[\#(threadJSON(version: 3))]}"#
        let rig = ChatRig(baseRoutes(threads: threads))
        try await rig.signIn()
        // The list's read of the opening lines lists messages too.
        try await rig.harness.until { await $0.state.openings["t1"] != nil }
        let before = rig.server.count("/api/v1/records", query: "llm/message")
        // A 500: a 503 the client waits out and tries again itself.
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(500, #"{"error":{"code":"internal","message":"later"}}"#))
        await rig.harness.send(.show(thread: "t1", scene: "a"))
        try await rig.harness.until { _ in rig.server.count("/api/v1/records", query: "llm/message") == before + 1 }
        try await rig.harness.until { await $0.state.reading["t1"] == nil }
        // The server is back; opening the chat again reads it.
        rig.server.unroute()
        await rig.harness.send(.show(thread: "t1", scene: "b"))
        let key = try #require(await rig.harness.state.scenes["b"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        #expect(rig.server.count("/api/v1/records", query: "llm/message") == before + 2)
    }

    /// A watch the server can no longer resume (its position fell below
    /// what the history keeps) reads the list again and follows from the
    /// head that read answered. The history is the same one, so only
    /// dropping the saved position keeps it from being sent again.
    @Test func aWatchToldCompactedReadsTheListAgainAndFollowsItsHead() async throws {
        let earlier = #"{"head":5,"generation":"g","records":[\#(agentJSON(helper))]}"#
        let later = #"{"head":12,"generation":"g","records":[\#(agentJSON(helper)),\#(agentJSON(otherAgent))]}"#
        let rig = ChatRig(
            [
                .init(method: "GET", path: "/api/v1/changes", query: "from=5&generation=g", answer: .json(410, compactedJSON)),
                .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, earlier), once: true),
                .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, later)),
            ] + baseRoutes())
        try await rig.signIn()
        // The list read again answers 10 as its earliest head.
        try await rig.harness.until(timeout: .seconds(20)) { harness in
            let state = await harness.state
            return rig.server.count("/api/v1/changes", query: "from=10&generation=g") == 1 && state.watching && !state.refreshing
        }
        let state = await rig.harness.state
        #expect(state.agents.map(\.id) == [helper, otherAgent])
        #expect(state.refreshError == nil && state.feedProblem == nil)
        #expect(rig.server.count("/api/v1/changes", query: "from=5&generation=g") == 1)
        #expect(await rig.storage.records(try await rig.lease())?.cursor == WatchCursor(seq: 10, generation: "g"))
    }

    /// The watch the refresh opens at the head it just read, refused as
    /// `compacted` too, is a failure shown as one: no third read.
    @Test func aWatchRefusedAgainAtTheHeadItJustReadSaysSoAndReadsNoMore() async throws {
        let earlier = #"{"head":5,"generation":"g","records":[\#(agentJSON(helper))]}"#
        let later = #"{"head":12,"generation":"g","records":[\#(agentJSON(helper)),\#(agentJSON(otherAgent))]}"#
        let rig = ChatRig(
            [
                .init(method: "GET", path: "/api/v1/changes", query: "from=5&generation=g", answer: .json(410, compactedJSON)),
                .init(method: "GET", path: "/api/v1/changes", query: "from=10&generation=g", answer: .json(410, compactedJSON)),
                .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, earlier), once: true),
                .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, later)),
            ] + baseRoutes())
        try await rig.signIn()
        try await rig.harness.until(timeout: .seconds(20)) { harness in
            await harness.state.feedProblem != nil
        }
        let state = await rig.harness.state
        #expect(state.feedProblem?.contains("could not resume") == true)
        #expect(!state.watching)
        #expect(rig.server.count("/api/v1/changes", query: "from=10&generation=g") == 1)
    }

    /// One recovery a minute: the watch the read again opens reaches its
    /// bookmark and is then refused as `compacted`. The bookmark makes
    /// nothing whole again, so within the minute that refusal is the
    /// failure, said, and the list is not read a third time.
    @Test func aWatchRefusedAfterItsBookmarkWithinAMinuteOfItsReadAgainSaysSoAndReadsNoMore() async throws {
        let earlier = #"{"head":5,"generation":"g","records":[\#(agentJSON(helper))]}"#
        let later = #"{"head":12,"generation":"g","records":[\#(agentJSON(helper)),\#(agentJSON(otherAgent))]}"#
        let rig = ChatRig(
            [
                .init(method: "GET", path: "/api/v1/changes", query: "from=5&generation=g", answer: .json(410, compactedJSON)),
                .init(
                    method: "GET", path: "/api/v1/changes", query: "from=10&generation=g",
                    answer: .stream([#"{"bookmark":10,"generation":"g"}"#, compactedJSON])),
                .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, earlier), once: true),
                .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, later)),
            ] + baseRoutes())
        try await rig.signIn()
        let lists: @Sendable () -> Int = { rig.server.count("/api/v1/records", query: "core/agent") }
        try await rig.harness.until(timeout: .seconds(20)) { harness in
            await harness.state.feedProblem != nil || lists() > 2
        }
        let state = await rig.harness.state
        // The first read, and the one read again; never a third.
        #expect(lists() == 2)
        #expect(state.feedProblem?.contains("could not resume") == true)
        #expect(!state.watching)
        #expect(rig.server.count("/api/v1/changes", query: "from=10&generation=g") == 1)
    }

    /// The minute past, a refusal is a recovery again. The read again's
    /// watch is answered busy and waits on the app's clock; an hour later it
    /// is refused as `compacted`, though it never opened in between, and
    /// the list is read once more and followed.
    @Test func aWatchRefusedAnHourAfterItsReadAgainReadsTheListAgain() async throws {
        let earlier = #"{"head":5,"generation":"g","records":[\#(agentJSON(helper))]}"#
        let later = #"{"head":12,"generation":"g","records":[\#(agentJSON(helper)),\#(agentJSON(otherAgent))]}"#
        let busy = #"{"error":{"code":"unavailable","message":"the server is busy"}}"#
        let rig = ChatRig(
            [
                .init(method: "GET", path: "/api/v1/changes", query: "from=5&generation=g", answer: .json(410, compactedJSON)),
                .init(method: "GET", path: "/api/v1/changes", query: "from=10&generation=g", answer: .json(503, busy), once: true),
                .init(method: "GET", path: "/api/v1/changes", query: "from=10&generation=g", answer: .json(410, compactedJSON), once: true),
                .init(
                    method: "GET", path: "/api/v1/changes", query: "from=10&generation=g",
                    answer: .stream([#"{"bookmark":10,"generation":"g"}"#], holdAfter: 1)),
                .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, earlier), once: true),
                .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, later)),
            ] + baseRoutes())
        try await rig.signIn()
        // Busy, the watch waits a second on the app's clock to open again.
        try await rig.harness.until(timeout: .seconds(20)) { _ in
            rig.server.count("/api/v1/changes", query: "from=10&generation=g") == 1
                && rig.clock.deadlines.contains(rig.clock.now.addingTimeInterval(1))
        }
        await rig.harness.advance(by: 3600)
        try await rig.harness.until(timeout: .seconds(20)) { harness in
            let state = await harness.state
            return state.feedProblem != nil
                || (rig.server.count("/api/v1/changes", query: "from=10&generation=g") == 3 && state.watching && !state.refreshing)
        }
        let state = await rig.harness.state
        #expect(state.feedProblem == nil)
        #expect(state.agents.map(\.id) == [helper, otherAgent])
        // The first read, the read again, and the one an hour later.
        #expect(rig.server.count("/api/v1/records", query: "core/agent") == 3)
    }

    /// The minute to the millisecond: 59.999 seconds after the read again
    /// started, a refusal is the failure, said, and the list is not read a
    /// third time, though the read again's list, its watch's bookmark and a
    /// change the watch delivered came between.
    @Test func aWatchRefusedAMillisecondShortOfTheMinuteAfterItsReadAgainSaysSo() async throws {
        let (rig, started) = try await readAgainAndFollowing()
        let lists: @Sendable () -> Int = { rig.server.count("/api/v1/records", query: "core/agent") }
        rig.clock.advance(to: started.addingTimeInterval(59.999))
        rig.server.release()
        try await rig.harness.until(timeout: .seconds(20)) { harness in
            await harness.state.feedProblem != nil || lists() > 2
        }
        let state = await rig.harness.state
        #expect(state.feedProblem == "the server could not resume the chats' history even after reading them again")
        #expect(!state.watching && !state.refreshing)
        #expect(lists() == 2)
        #expect(state.recoveryStarted == started)
        #expect(rig.server.count("/api/v1/changes", query: "from=10&generation=g") == 1)
    }

    /// And on the minute exactly, a refusal reads the list again and
    /// follows from its head.
    @Test func aWatchRefusedOnTheMinuteAfterItsReadAgainReadsTheListAgain() async throws {
        let (rig, started) = try await readAgainAndFollowing()
        let lists: @Sendable () -> Int = { rig.server.count("/api/v1/records", query: "core/agent") }
        rig.clock.advance(to: started.addingTimeInterval(60))
        rig.server.release()
        try await rig.harness.until(timeout: .seconds(20)) { harness in
            let state = await harness.state
            return state.feedProblem != nil
                || (lists() == 3 && rig.server.count("/api/v1/changes", query: "from=10&generation=g") == 2 && state.watching
                    && !state.refreshing)
        }
        let state = await rig.harness.state
        #expect(state.feedProblem == nil)
        #expect(lists() == 3)
        #expect(state.recoveryStarted == started.addingTimeInterval(60))
        #expect(state.agents.map(\.id) == [helper, otherAgent])
    }

    /// A list the server can no longer continue part way is read again from
    /// its first page, and the refresh lands whole.
    @Test func aRefreshToldCompactedPartWayReadsAgainFromTheFirstPage() async throws {
        let first = #"{"head":10,"generation":"g","cursor":"c1","records":[\#(agentJSON(helper))]}"#
        let second = #"{"head":10,"generation":"g","records":[\#(agentJSON(otherAgent))]}"#
        let rig = ChatRig(
            [
                .init(method: "GET", path: "/api/v1/records", query: "after=c1", answer: .json(410, compactedJSON), once: true),
                .init(method: "GET", path: "/api/v1/records", query: "after=c1", answer: .json(200, second)),
                .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, first)),
            ] + baseRoutes())
        await rig.runtime.linkChanged(LinkSnapshot(phase: .signedIn, server: origin, repository: repository, online: true))
        try await rig.harness.until { harness in
            let state = await harness.state
            return !state.refreshing && (state.loaded || state.refreshError != nil)
        }
        let state = await rig.harness.state
        #expect(state.refreshError == nil)
        #expect(state.agents.map(\.id) == [helper, otherAgent])
        // The first page, the refused continuation, then the walk again.
        #expect(rig.server.count("/api/v1/records", query: "core/agent") == 4)
        #expect(rig.server.count("/api/v1/records", query: "after=c1") == 2)
    }

    @Test func aChatTheDeviceHoldsNothingOfIsReadWhenOpened() async throws {
        let threads = #"{"head":10,"generation":"g","records":[\#(threadJSON(version: 3))]}"#
        let rig = ChatRig(baseRoutes(threads: threads))
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
    }

    @Test func aChatWhoseRunFailedEarlierSaysWhyWhenOpened() async throws {
        let failed = threadJSON(version: 3, status: "error")
            .replacingOccurrences(of: #""lastReply":"Hello""#, with: #""reason":"401 Unauthorized""#)
        let threads = #"{"head":10,"generation":"g","records":[\#(failed)]}"#
        let rig = ChatRig(baseRoutes(threads: threads, status: "error"))
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        let notes = try #require(await rig.conversation(key)?.status)
        #expect(notes.map(\.kind) == [.failed])
        #expect(notes.first?.title == "The provider refused the API key" && notes.first?.detail == "401 Unauthorized")
    }

    @Test func aWindowLeavingKeepsAConversationAnotherShows() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "a"))
        await rig.harness.send(.show(thread: "t1", scene: "b"))
        let key = try #require(await rig.harness.state.scenes["a"]?.current)
        #expect(await rig.harness.state.scenes["b"]?.current == key)
        await rig.harness.send(.sceneGone(scene: "a"))
        #expect(await rig.harness.state.conversations[key] != nil)
        await rig.harness.send(.sceneGone(scene: "b"))
        #expect(await rig.harness.state.conversations[key] == nil)
    }

    @Test func aReplyThatSettlesOffScreenNotifiesOnce() async throws {
        let rig = ChatRig(baseRoutes(threads: emptyPage))
        try await rig.signIn()
        let settled = Record(
            id: "t1", kind: Kinds.thread,
            properties: [
                "mode": "chat", "status": "ok", "finishedAt": "2026-10-31T10:00:05Z", "lastReply": "Hello",
                "agent": ["ref": .string("\(Kinds.agent)/\(helper)")],
            ], version: 4)
        await rig.harness.deliver(.init(.threadsRead(.init(chats: [settled], automated: []))))
        await rig.harness.deliver(.init(.threadsRead(.init(chats: [settled], automated: []))))
        let scheduled: @Sendable () -> [NotificationRequest] = {
            rig.notifications.calls.compactMap { if case let .schedule(request) = $0 { request } else { nil } }
        }
        try await rig.harness.until { _ in scheduled().count == 1 }
        await rig.harness.settle()
        #expect(scheduled().count == 1)
        let note = try #require(scheduled().first)
        #expect(note.title == "Helper")
        #expect(note.body == "Hello")
        #expect(note.thread == "t1")
        #expect(note.userInfo == [ChatNotifications.threadKey: "t1"])
        // Shown at once, and in front too: a chat on screen never notifies.
        #expect(note.fireAt == nil && note.showsWhileActive)
        // A tap opens the chat in the window that claims it, and takes back
        // what the thread still had pending as well as what it delivered.
        rig.notifications.respond(NotificationResponse(id: note.id, category: nil, action: .open, userInfo: note.userInfo))
        try await rig.harness.until { await $0.presentation.routePending }
        await rig.harness.send(.claimRoute(scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        #expect(await rig.harness.state.conversations[key]?.thread == "t1")
        try await rig.harness.until { _ in rig.notifications.pending[note.id] == nil }
        #expect(rig.notifications.calls.contains(.cancelThread("t1")))
        #expect(rig.notifications.calls.contains(.cancel(note.id)))
    }

    @Test func theBadgeAndTheSavesFollowWhatChanged() async throws {
        let threads = #"{"head":10,"generation":"g","records":[\#(threadJSON(version: 3))]}"#
        let rig = ChatRig(baseRoutes(threads: threads))
        try await rig.signIn()
        try await rig.harness.until { await $0.state.openings["t1"] != nil }
        await rig.harness.settle()
        let asked = { @Sendable (match: (ChatAppAsk) -> Bool) in rig.harness.effects.count { match($0.ask) } }
        let badges = asked { if case .badge = $0 { true } else { false } }
        let saves = asked { if case .saveState = $0 { true } else { false } }
        await rig.harness.send(.markUnread("t1"))
        #expect(asked { if case .badge(1) = $0 { true } else { false } } == 1)
        #expect(asked { if case .saveState = $0 { true } else { false } } == saves + 1)
        // Unread already: nothing moved, so nothing is written or badged.
        await rig.harness.send(.markUnread("t1"))
        #expect(asked { if case .badge = $0 { true } else { false } } == badges + 1)
        #expect(asked { if case .saveState = $0 { true } else { false } } == saves + 1)
    }
}

/// Chat signed in, its watch refused from 5 at the clock's start and the
/// list read again; then, in order before anything else is refused: the
/// read again's list landed, the watch opened from the head it gave and
/// reached its bookmark, and it delivered a change, which the list read
/// again. The watch then holds until the server is released, and is
/// refused as `compacted`. Answers the rig and when the read again started.
private func readAgainAndFollowing() async throws -> (ChatRig, Date) {
    let earlier = #"{"head":5,"generation":"g","records":[\#(agentJSON(helper))]}"#
    let later = #"{"head":12,"generation":"g","records":[\#(agentJSON(helper)),\#(agentJSON(otherAgent))]}"#
    let row = #"{"seq":11,"ts":"2026-10-31T10:00:06Z","kind":"\#(Kinds.thread)","recordId":"t1","op":"patch"}"#
    let rig = ChatRig(
        [
            .init(method: "GET", path: "/api/v1/changes", query: "from=5&generation=g", answer: .json(410, compactedJSON)),
            .init(
                method: "GET", path: "/api/v1/changes", query: "from=10&generation=g",
                answer: .stream([#"{"bookmark":10,"generation":"g"}"#, row, compactedJSON], holdAfter: 2), once: true),
            .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, earlier), once: true),
            .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, later)),
        ] + baseRoutes())
    let start = rig.clock.now
    try await rig.signIn()
    let changed = ChatAppState.WatchKey(kind: Kinds.thread, id: "t1")
    try await rig.harness.until(timeout: .seconds(20)) { harness in
        let state = await harness.state
        return state.feedProblem != nil || (state.watching && !state.refreshing && state.feedBatch.contains(changed))
    }
    #expect(await rig.harness.state.feedProblem == nil)
    let threadLists = rig.server.count("/api/v1/records", query: "llm/thread")
    // The change is read again a moment after its row, on the app's clock.
    try await rig.harness.until(advancing: 0.3) { harness in
        let state = await harness.state
        return state.feedApplying == nil && state.feedBatch.isEmpty
    }
    #expect(rig.server.count("/api/v1/records", query: "llm/thread") == threadLists + 1)
    let state = await rig.harness.state
    #expect(state.recoveryStarted == start)
    #expect(state.agents.map(\.id) == [helper, otherAgent])
    #expect(await rig.storage.records(try await rig.lease())?.cursor == WatchCursor(seq: 11, generation: "g"))
    // The read again's list was read whole before its watch opened.
    let requests = rig.server.requests
    let watch = try #require(requests.firstIndex { $0.contains("/api/v1/changes") && $0.contains("from=10&generation=g") })
    #expect(requests[..<watch].count { $0.contains("/api/v1/records") && $0.contains("core/agent") } == 2)
    return (rig, start)
}

/// A list signed in with its folder read, for an engine test.
func signedInState() -> ChatAppState {
    var state = ChatAppState()
    state.session = ChatSession(LinkSnapshot(phase: .signedIn, server: origin, repository: repository))
    state.disk = ChatStorage.Lease(identity: identity, number: 1, generation: 0)
    return state
}

let cachedRows: CachedTranscript? = CachedTranscript(
    threadVersion: 3, full: false, records: [Record(id: "m", kind: Kinds.message, properties: ["role": "user", "content": "x"])], openedAt: nil)

/// What a test watched go by.
final class Seen: @unchecked Sendable {
    private let lock = NSLock()
    private var kept: [Int] = []
    func add(_ count: Int) { lock.withLock { kept.append(count) } }
    var counts: [Int] { lock.withLock { kept } }
}
