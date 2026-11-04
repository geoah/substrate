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

    @Test func anItemAddedShowsAtOnceAndGoesToTheOutbox() async throws {
        let harness = try harness()
        await harness.deliver(.records(.loaded([], cursor: nil, queue: [])))
        #expect(await harness.presentation.empty?.title == "No items")
        await harness.send(.add("  Milk "))
        let write = try #require(
            harness.effects.compactMap { effect -> LocalWrite? in
                if case let .records(.enqueue(write)) = effect { return write }
                return nil
            }.first)
        #expect(write.entries.map(\.method) == [.put])
        #expect(write.entries.first?.properties == ["name": "Milk"])
        #expect(await harness.presentation.items.rows.map(\.title) == ["Milk"])
        await harness.send(.add("   "))
        #expect(harness.effects.count(where: { if case .records(.enqueue) = $0 { true } else { false } }) == 1)
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
        await harness.send(.add("Milk"))
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
        #expect(offered == AboutPresentation.present(appName: TemplateApp.appName, version: "0.1.0 (1)", channel: nil, offered: "0.2.0"))
        await harness.send(.offered(nil))
        #expect(await harness.presentation.about == before)
    }
}
