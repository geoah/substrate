import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// The app's screen in the states it shows it in, made by its presenter
/// from fixed records: what each UI's gallery draws.
public enum TodayFixtures {
    public struct Fixture: Sendable, Identifiable {
        public var name: String
        public var app: TodayPresentation
        public var id: String { name }
    }

    static let app = TodayApp(cache: nil, outbox: nil, words: TodayApp.words(device: "this iPhone"), build: BuildFacts(version: "0.1.0 (1)"))

    static func present(_ records: [Record], draft: String = "", adding: TodayApp.Adding? = nil) -> TodayPresentation {
        var state = TodayApp.State()
        state.draft = draft
        state.adding = adding
        state.linked.loaded = true
        for record in records { state.linked.records[RecordFold.key(record.kind, record.id)] = record }
        return TodayPresenter.present(state, linked: app.linked, build: app.build)
    }

    public static var all: [Fixture] {
        let items = ["Milk", "Bread", "Coffee"].enumerated().map { index, name in
            Record(id: "i\(index)", kind: TodayKinds.item, properties: ["name": .string(name)], version: 1)
        }
        return [
            Fixture(name: "today-items", app: present(items)),
            Fixture(name: "today-empty", app: present([])),
            Fixture(
                name: "today-add-not-kept",
                app: present(items, draft: "Eggs", adding: .notKept("This iPhone could not keep the change: the disk is full."))),
        ]
    }
}
