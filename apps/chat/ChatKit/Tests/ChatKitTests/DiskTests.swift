import Foundation
@testable import ChatKit
import SubstrateAPI
import SubstrateRuntime
import SubstrateTesting
import Testing

/// What the build before ChatKit kept under `SubstrateChat/<hash>/` is read
/// after the upgrade, where it is, and drawn before the server is asked.
@Suite("The cache an earlier build wrote")
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

        // Adopted where it is: marked, its transcripts in the cache area.
        let root = rig.base.appendingPathComponent(name)
        #expect(FileManager.default.fileExists(atPath: root.appendingPathComponent("folder.json").path))
        #expect(FileManager.default.fileExists(atPath: root.appendingPathComponent("state.json").path))
        #expect(FileManager.default.fileExists(atPath: root.appendingPathComponent("cache/\(ChatStorage.hash("t1")).json").path))
        #expect(!FileManager.default.fileExists(atPath: root.appendingPathComponent("messages").path))

        // The cursor it kept beside the list is the record cache's now.
        #expect(await (try rig.storage.records(identity))?.cursor == WatchCursor(seq: 7, generation: "old"))
        // A list read under another history moves the watch to its head.
        rig.server.letGo("/api/v1/records")
        try await rig.harness.until { _ in rig.server.count("/api/v1/changes", query: "from=10&generation=g") == 1 }
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
