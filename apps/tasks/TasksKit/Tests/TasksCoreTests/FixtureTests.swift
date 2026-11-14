import Foundation
import Testing

@testable import TasksCore

@Suite("The fixtures", .timeLimit(.minutes(10)))
struct FixtureTests {
    @Test func everyFixtureHasWhatItsScreenDraws() throws {
        let fixtures = TasksFixtures.all
        #expect(Set(fixtures.map(\.name)).count == fixtures.count)
        for index in fixtures.indices {
            #expect(Self.draws(fixtures[index]), "\(fixtures[index].name)")
            // What the Android bridge carries: every presentation encodes.
            _ = try JSONEncoder().encode(fixtures[index].app)
        }
    }

    /// Whether a fixture holds what its screen draws: its own function, so
    /// the presentation is not copied into every expectation's frame.
    static func draws(_ fixture: TasksFixtures.Fixture) -> Bool {
        let app = fixture.app
        switch fixture.screen {
        case .sidebar: return !app.sidebar.sections.isEmpty
        case .list: return app.list != nil
        case .detail: return app.detail != nil
        case let .editor(key): return app.editors[key] != nil
        case .confirmation: return app.confirmation != nil
        case let .settings(settings): return settings.sections.count >= 6
        case .queue: return app.queue != nil
        case .suggestion: return app.suggestion != nil
        case .choice: return app.choice != nil
        case .collections: return !app.collections.rows.isEmpty
        case .collection: return app.collection != nil
        case .approval: return app.approval != nil
        }
    }
}
