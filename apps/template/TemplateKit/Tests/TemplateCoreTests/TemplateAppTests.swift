import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing

@testable import TemplateCore

@Suite("Substrate Template", .timeLimit(.minutes(10)))
struct TemplateAppTests {
    func harness() throws -> ControllerHarness<TemplateApp> {
        let folder = try RepositoryFolder(root: temporaryFolder("template"))
        let app = TemplateApp(
            cache: RecordCache(folder: folder), outbox: Outbox(folder: folder), words: TemplateApp.words(device: "this iPhone"),
            build: BuildFacts(version: "0.1.0 (1)"))
        return ControllerHarness(app, performsEffects: false)
    }

    func enqueued(_ harness: ControllerHarness<TemplateApp>) -> [LocalWrite] {
        harness.effects.compactMap { effect -> LocalWrite? in
            if case let .records(.enqueue(write)) = effect { return write }
            return nil
        }
    }

    @Test func anItemAddedShowsAtOnceAndGoesToTheOutbox() async throws {
        let harness = try harness()
        await harness.deliver(.records(.loaded([], cursor: nil, queue: [])))
        #expect(await harness.presentation.empty?.title == "No items")
        await harness.send(.setDraft("  Milk "))
        await harness.send(.add)
        let write = try #require(enqueued(harness).first)
        #expect(write.entries.map(\.method) == [.put])
        #expect(write.entries.first?.properties == ["name": "Milk"])
        #expect(await harness.presentation.items.rows.map(\.title) == ["Milk"])
        // What was typed stays until the outbox has the item.
        #expect(await harness.presentation.add.text == "  Milk ")
        await harness.deliver(.records(.enqueued(Set(write.entries.map(\.id)), queue: write.entries, landed: [])))
        #expect(await harness.presentation.add.text.isEmpty)
        await harness.send(.setDraft("   "))
        await harness.send(.add)
        #expect(enqueued(harness).count == 1)
    }

    @Test func anAddTheDeviceCouldNotKeepLeavesWhatWasTypedToRetry() async throws {
        let harness = try harness()
        await harness.deliver(.records(.loaded([], cursor: nil, queue: [])))
        await harness.send(.setDraft("Milk"))
        await harness.send(.add)
        let first = try #require(enqueued(harness).first?.entries.first)
        // On its way to the outbox, a second add sends nothing.
        await harness.send(.add)
        #expect(enqueued(harness).count == 1)

        let words = "This iPhone could not keep the change: the disk is full."
        await harness.deliver(.records(.notQueued([first.id], words)))
        let presented = await harness.presentation
        #expect(presented.items.rows.isEmpty)
        #expect(presented.add.text == "Milk")
        #expect(
            presented.add.notKept
                == ProblemPresentation(title: "A change was not kept", message: words, fix: ProblemFix(title: "Retry", action: .tryAgain)))
        #expect(presented.problem == nil)

        // Retry adds what the field holds, and the field clears once it is kept.
        await harness.send(.add)
        let second = try #require(enqueued(harness).last?.entries.first)
        #expect(second.id != first.id)
        await harness.deliver(.records(.enqueued([second.id], queue: [second], landed: [])))
        let kept = await harness.presentation
        #expect(kept.items.rows.map(\.title) == ["Milk"])
        #expect(kept.add == AddField(prompt: "Add an item", text: "", notKept: nil))
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
        let manifest = TemplateManifest.manifest(clientID: TemplateManifest.localClientID)
        #expect(recorded.name == manifest.appName)
        #expect(recorded.scopes == manifest.scopes)
        #expect(recorded.asks == [Recorded.Ask(package: TemplateKinds.packageID, atLeast: TemplateManifest.version)])
        #expect(recorded.publishes == [TemplateKinds.packageID])
        #expect(recorded.apple["clientIDKey"] == .string(TemplateManifest.clientIDKey))
        #expect(recorded.apple["redirectKey"] == .string(TemplateManifest.redirectKey))
    }

    @Test func everyFixtureEncodesForTheBridge() throws {
        for fixture in TemplateFixtures.all { _ = try JSONEncoder().encode(fixture.app) }
    }
}

@Suite("Substrate Template's root", .timeLimit(.minutes(10)))
struct TemplateRootTests {
    @Test func storageIsReadAtOpenAndShownInSettings() async throws {
        let folder = try RepositoryFolder(root: temporaryFolder("template"))
        let app = TemplateApp(
            cache: RecordCache(folder: folder), outbox: Outbox(folder: folder), words: TemplateApp.words(device: "this iPhone"),
            build: BuildFacts(version: "0.1.0 (1)"))
        var environment = AppEnvironment.testing()
        environment.folder = folder
        let harness = ControllerHarness(app, environment: environment, performsEffects: false)
        _ = await harness.state
        #expect(harness.effects.contains(.readUsage))
        await harness.deliver(.usage([AreaUsage(area: .cache, bytes: 512, files: 1)]))
        let storage = await harness.presentation.storage
        #expect(storage.rows.map(\.id) == [.storage(.area(.cache)), .storage(.total), .storage(.clear)])
        #expect(storage.rows.last?.control == .action(destructive: true, enabled: true))
    }

    @Test func theItemsRevisionMovesWithTheItemsAlone() async throws {
        let harness = ControllerHarness(
            TemplateApp(
                cache: nil, outbox: nil, words: TemplateApp.words(device: "this iPhone"),
                build: BuildFacts(version: "0.1.0 (1)")), performsEffects: false)
        await harness.deliver(.records(.loaded([], cursor: nil, queue: [])))
        let before = await harness.presentation.items.revision
        await harness.send(.setDraft("Milk"))
        await harness.send(.add)
        let added = await harness.presentation.items.revision
        #expect(added > before)
        await harness.deliver(.usage([]))
        #expect(await harness.presentation.items.revision == added)
        // A delete is a write too: one row fewer, and the revision moves on.
        let id = try #require(await harness.presentation.items.rows.first?.id)
        await harness.send(.delete(id))
        #expect(await harness.presentation.items.rows.isEmpty)
        #expect(await harness.presentation.items.revision > added)
    }

    @Test func theUpdatersOfferIsShownInAbout() async throws {
        let harness = ControllerHarness(
            TemplateApp(
                cache: nil, outbox: nil, words: TemplateApp.words(device: "this iPhone"),
                build: BuildFacts(version: "0.1.0 (1)")), performsEffects: false)
        let before = await harness.presentation.about
        await harness.send(.offered("0.2.0"))
        let offered = await harness.presentation.about
        #expect(offered != before)
        #expect(offered == AboutPresentation.present(appName: TemplateApp.appName, version: "0.1.0 (1)", channel: nil, offered: "0.2.0", privacyPolicy: nil))
        await harness.send(.offered(nil))
        #expect(await harness.presentation.about == before)
    }
}
