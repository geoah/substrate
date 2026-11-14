import Foundation
import SubstrateAPI
import SubstrateLocal
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

/// What the build before ChatKit kept under `SubstrateChat/<hash>/` is read
/// after the upgrade, where it is, and drawn before the server is asked.
@Suite("The cache an earlier build wrote", .timeLimit(.minutes(10)))
struct LegacyCache {
    @Test func theListTheTranscriptsTheDraftsAndTheCursorReadAfterTheUpgrade() async throws {
        let fixture = try #require(Bundle.module.url(forResource: "Fixtures/legacy", withExtension: nil))
        let rig = ChatRig()
        let name = ChatStorage.hash(identity)
        try FileManager.default.copyItem(at: fixture.appendingPathComponent(name), to: rig.base.appendingPathComponent(name))
        // The server holds every list: what draws is the disk's.
        rig.server.hold("/api/v1/records")
        await rig.runtime.linkChanged(LinkSnapshot(phase: .signedIn, server: origin, repository: repository, online: true))
        try await rig.harness.until { await $0.state.opened }
        let state = await rig.harness.state
        #expect(state.chats.map(\.id) == ["t1"])
        #expect(state.title(try #require(state.thread("t1"))) == "Saved before the kit")
        #expect(state.drafts["t1"] == "half a thought")
        #expect(state.lastAgent == helper)
        #expect(state.baselined)

        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello from the disk"] }
        // Current by version: the rows are not read again.
        #expect(rig.server.count("/api/v1/records", query: "llm/message") == 0)
        #expect(await rig.conversation(key)?.composer.draft == "half a thought")

        // Adopted where it is, unmarked, so a build before ChatKit still reads
        // its list; its transcripts in the cache area.
        let root = rig.base.appendingPathComponent(name)
        #expect(!FileManager.default.fileExists(atPath: root.appendingPathComponent("folder.json").path))
        #expect(FileManager.default.fileExists(atPath: root.appendingPathComponent("state.json").path))
        #expect(FileManager.default.fileExists(atPath: root.appendingPathComponent("cache/\(ChatStorage.hash("t1")).json").path))
        #expect(!FileManager.default.fileExists(atPath: root.appendingPathComponent("messages").path))

        // The cursor it kept beside the list is the record cache's now.
        #expect(await rig.storage.records(try await rig.lease())?.cursor == WatchCursor(seq: 7, generation: "old"))
        // A list read under another history moves the watch to its head.
        rig.server.letGo("/api/v1/records")
        try await rig.harness.until { _ in rig.server.count("/api/v1/changes", query: "from=10&generation=g") == 1 }
    }

    /// The cursor the earlier build kept, once refused, is never sent
    /// again, even by a launch that finds the record cache without a cursor
    /// of its own: here the read again's watch was refused too, so none was
    /// kept, and the app closed before it saved the list again.
    @Test func theCursorAnEarlierBuildKeptIsNotSentAgainAtTheNextLaunch() async throws {
        let fixture = try #require(Bundle.module.url(forResource: "Fixtures/legacy", withExtension: nil))
        let base = temporaryFolder("chat")
        let name = ChatStorage.hash(identity)
        try FileManager.default.copyItem(at: fixture.appendingPathComponent(name), to: base.appendingPathComponent(name))
        // An app token's lists carry no head: the watch resumes from the
        // cursor the disk kept.
        let headless: [ChatServer.Route] = [
            .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, #"{"records":[\#(agentJSON(helper))]}"#)),
            .init(method: "GET", path: "/api/v1/records", answer: .json(200, #"{"records":[]}"#)),
        ]
        let signedIn = LinkSnapshot(phase: .signedIn, server: origin, repository: repository, online: true)

        let first = ChatRig(
            [
                .init(method: "GET", path: "/api/v1/changes", query: "from=7&generation=old", answer: .json(410, compactedJSON)),
                .init(method: "GET", path: "/api/v1/changes", answer: .stream([compactedJSON])),
            ] + headless + baseRoutes(), base: base)
        await first.runtime.linkChanged(signedIn)
        try await first.harness.until(timeout: .seconds(20)) { await $0.state.feedProblem != nil }
        #expect(first.server.count("/api/v1/changes", query: "from=7&generation=old") == 1)
        #expect(await first.storage.records(try await first.lease())?.cursor == nil)
        await first.runtime.close()

        let again = ChatRig(headless + baseRoutes(), base: base)
        await again.runtime.linkChanged(signedIn)
        try await again.harness.until(timeout: .seconds(20)) { harness in
            await harness.state.watching && again.server.count("/api/v1/changes") > 0
        }
        // Opened at the live head instead.
        #expect(again.server.count("/api/v1/changes", query: "from=") == 0)
    }

    @Test func theChoicesAreReadWhereEveryBuildKeptThem() throws {
        let defaults = try #require(UserDefaults(suiteName: "chat-test-\(UUID().uuidString)"))
        let prefix = "chat.\(ChatStorage.hash(identity))."
        defaults.set(true, forKey: prefix + "showAutomated")
        defaults.set(["x.example.com/pkg/quiet"], forKey: prefix + "hiddenAgents")
        defaults.set(false, forKey: prefix + "notifyReplies")
        let store = DefaultsPreferenceStore(defaults: defaults)
        let read = store.load(identity)
        #expect(read.showAutomated && !read.notifyReplies && read.notifyAttention)
        #expect(read.hiddenAgents == ["x.example.com/pkg/quiet"])
        store.clear(identity)
        #expect(defaults.object(forKey: prefix + "showAutomated") == nil)
    }
}

/// A session reaches its repository's folder only through its lease, and a
/// sign-out's retirement runs in order with the next sign-in's open.
@Suite("The folder's lease", .timeLimit(.minutes(10)))
struct FolderLease {
    @Test func aSaveStillOnItsWayAcrossASignOutWritesNothing() async throws {
        let base = temporaryFolder("chat")
        let storage = ChatStorage(base: base)
        let root = base.appendingPathComponent(ChatStorage.hash(identity))
        let old = await storage.open(identity).value
        // What a session's effects hold on to while their save waits.
        let cache = try #require(storage.records(old))
        let retiring = storage.retire(identity, deleting: true)
        storage.save(CachedState(), old)
        storage.save(drafts: ["t1": "half a thought"], old)
        await retiring.value
        await cache.bookmark(WatchCursor(seq: 3, generation: "g"))
        // Nothing of the session is written back: the folder keeps only the
        // generation the sign-out moved.
        #expect(try FileManager.default.contentsOfDirectory(atPath: root.path) == [LinkGeneration.fileName])
        #expect(LinkGeneration(folder: try RepositoryFolder(adopting: root)).current == 1)

        // The same repository signs in again: the old lease still reaches
        // nothing, the new one its own folder.
        let new = await storage.open(identity).value
        #expect(new != old && new.generation == 1)
        storage.save(CachedState(lastAgent: "old"), old)
        #expect(storage.state(new) == nil)
        storage.save(CachedState(lastAgent: helper), new)
        #expect(storage.state(new)?.lastAgent == helper)
        #expect(storage.state(old) == nil)
    }

    @Test func aSignOutThenASignInToTheSameRepositoryKeepsTheNewFolder() async throws {
        let base = temporaryFolder("chat")
        let storage = ChatStorage(base: base)
        let old = await storage.open(identity).value
        storage.save(CachedState(lastAgent: "old"), old)
        storage.save(cachedRows!, thread: "t1", old)
        // Asked back to back, as the two link steps ask them: the deletion
        // runs first, and never meets the folder the sign-in opens.
        let retiring = storage.retire(identity, deleting: true)
        let opening = storage.open(identity)
        let new = await opening.value
        #expect(storage.state(new) == nil)
        #expect(storage.transcript(new, thread: "t1", at: Date()) == nil)
        storage.save(CachedState(lastAgent: helper), new)
        storage.save(cachedRows!, thread: "t1", new)
        await retiring.value
        // A late clear under the old lease removes nothing of the new one.
        storage.clearTranscripts(old)
        storage.prune(old, at: Date().addingTimeInterval(365 * 86400))
        #expect(storage.state(new)?.lastAgent == helper)
        #expect(storage.transcript(new, thread: "t1", at: Date()) == cachedRows)
    }

    @Test func aSignOutAndASignInOnTheRuntimeKeepWhatTheNewSessionSaves() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        let old = try await rig.lease()
        await rig.signOut()
        try await rig.signIn()
        let new = try await rig.lease()
        #expect(new.generation == old.generation + 1)
        await rig.harness.send(.markUnread("t1"))
        try await rig.harness.until { harness in
            await harness.advance(by: 1)
            return FileManager.default.fileExists(atPath: rig.root.appendingPathComponent("state.json").path)
        }
        #expect(rig.storage.state(new) != nil)
    }
}

/// The transcripts a build before ChatKit kept in `messages/`.
@Suite("Adopting the transcripts", .timeLimit(.minutes(10)))
struct TranscriptAdoption {
    @Test func aCollisionKeepsTheNewerCopyAndAFailedMoveKeepsTheOldFolder() throws {
        let root = temporaryFolder("chat").appendingPathComponent("repository")
        let messages = root.appendingPathComponent("messages")
        let cache = root.appendingPathComponent("cache")
        try FileManager.default.createDirectory(at: messages, withIntermediateDirectories: true)
        try FileManager.default.createDirectory(at: cache, withIntermediateDirectories: true)
        func write(_ version: Int, _ url: URL) throws {
            try JSONEncoder().encode(CachedTranscript(threadVersion: version, full: false, records: [], openedAt: nil)).write(to: url)
        }
        func version(_ url: URL) throws -> Int {
            try JSONDecoder().decode(CachedTranscript.self, from: Data(contentsOf: url)).threadVersion
        }
        try write(5, messages.appendingPathComponent("a.json"))
        try write(3, cache.appendingPathComponent("a.json"))
        try write(2, messages.appendingPathComponent("b.json"))
        try write(4, cache.appendingPathComponent("b.json"))
        try write(1, messages.appendingPathComponent("c.json"))
        try write(1, messages.appendingPathComponent("d.json"))
        let folder = try RepositoryFolder(adopting: root)

        let first = ChatStorage.adoptTranscripts(in: folder) { from, to in
            if from.lastPathComponent == "c.json" { throw CocoaError(.fileWriteNoPermission) }
            try FileManager.default.moveItem(at: from, to: to)
        }
        #expect(first.kept == ["a.json": .messages, "b.json": .cache])
        #expect(first.moved == ["d.json"])
        #expect(first.failed == ["c.json"])
        #expect(try version(cache.appendingPathComponent("a.json")) == 5)
        #expect(try version(cache.appendingPathComponent("b.json")) == 4)
        // What could not move stays, with the folder it was in.
        #expect(try FileManager.default.contentsOfDirectory(atPath: messages.path) == ["c.json"])
        #expect(!FileManager.default.fileExists(atPath: cache.appendingPathComponent("c.json").path))

        // The next open finishes the move.
        let second = ChatStorage.adoptTranscripts(in: folder)
        #expect(second.moved == ["c.json"] && second.failed.isEmpty)
        #expect(!FileManager.default.fileExists(atPath: messages.path))
        #expect(try FileManager.default.contentsOfDirectory(atPath: cache.path).sorted() == ["a.json", "b.json", "c.json", "d.json"])
    }
}
