import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// The app's screen in the states it shows it in, made by its presenter
/// from fixed records: what each UI's gallery draws.
public enum TemplateFixtures {
    public struct Fixture: Sendable, Identifiable {
        public var name: String
        public var app: TemplatePresentation
        public var id: String { name }
    }

    static let app = TemplateApp(cache: nil, outbox: nil, words: TemplateApp.words(device: "this iPhone"), build: BuildFacts(version: "0.1.0 (1)"))

    static func present(_ records: [Record]) -> TemplatePresentation {
        var state = TemplateApp.State()
        state.linked.loaded = true
        for record in records { state.linked.records[RecordFold.key(record.kind, record.id)] = record }
        return TemplatePresenter.present(state, linked: app.linked, build: app.build)
    }

    public static var all: [Fixture] {
        let items = ["Milk", "Bread", "Coffee"].enumerated().map { index, name in
            Record(id: "i\(index)", kind: TemplateKinds.item, properties: ["name": .string(name)], version: 1)
        }
        return [
            Fixture(name: "template-items", app: present(items)),
            Fixture(name: "template-empty", app: present([])),
        ]
    }
}
