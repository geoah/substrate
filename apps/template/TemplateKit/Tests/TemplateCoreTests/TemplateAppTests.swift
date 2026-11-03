import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing
@testable import TemplateCore

@Suite("Substrate Template")
struct TemplateAppTests {
    func harness() throws -> ControllerHarness<TemplateApp> {
        let folder = try RepositoryFolder(root: temporaryFolder("template"))
        let app = TemplateApp(cache: RecordCache(folder: folder), outbox: Outbox(folder: folder), device: "this iPhone",
                              build: BuildFacts(version: "0.1.0 (1)"))
        return ControllerHarness(app, performsEffects: false)
    }

    @Test func anItemAddedShowsAtOnceAndGoesToTheOutbox() async throws {
        let harness = try harness()
        await harness.deliver(.records(.loaded([], cursor: nil, queue: [])))
        #expect(await harness.presentation.empty?.title == "No items")
        await harness.send(.add("  Milk "))
        let write = try #require(harness.effects.compactMap { effect -> LocalWrite? in
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
            struct Ask: Decodable, Equatable { var package: String; var atLeast: Int }
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
