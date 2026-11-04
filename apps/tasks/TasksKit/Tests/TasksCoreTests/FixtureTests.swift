import Foundation
import Testing

@testable import TasksCore

@Suite("The fixtures", .timeLimit(.minutes(10)))
struct FixtureTests {
    @Test func everyFixtureHasWhatItsScreenDraws() throws {
        let fixtures = TasksFixtures.all
        #expect(Set(fixtures.map(\.name)).count == fixtures.count)
        for fixture in fixtures {
            switch fixture.screen {
            case .sidebar: #expect(!fixture.app.sidebar.lists.rows.isEmpty, "\(fixture.name)")
            case .list: #expect(fixture.app.list != nil, "\(fixture.name)")
            case .detail: #expect(fixture.app.detail != nil, "\(fixture.name)")
            case let .editor(key): #expect(fixture.app.editors[key] != nil, "\(fixture.name)")
            case .confirmation: #expect(fixture.app.confirmation != nil, "\(fixture.name)")
            case let .settings(settings): #expect(settings.sections.count >= 6, "\(fixture.name)")
            }
            // What the Android bridge carries: every presentation encodes.
            _ = try JSONEncoder().encode(fixture.app)
        }
    }
}
