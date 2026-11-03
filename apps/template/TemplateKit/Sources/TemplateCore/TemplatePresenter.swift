import Foundation
import SubstrateCore
import SubstrateScreens
import SubstrateRuntime

/// The root's presenter: every word the app's screen shows.
public enum TemplatePresenter {
    public static func present(_ state: TemplateApp.State, linked: LinkedRecords, build: BuildFacts) -> TemplatePresentation {
        let rows = state.linked.records(of: TemplateKinds.item)
            .map { ItemRow(id: $0.id, title: name($0), delete: "Delete") }
            .sorted { ($0.title.lowercased(), $0.id) < ($1.title.lowercased(), $1.id) }
        return TemplatePresentation(
            title: "Items", items: KeyedList(rows, revision: state.linked.records.count), add: "Add an item",
            empty: rows.isEmpty ? EmptyPresentation(title: "No items", message: "Add one below.") : nil,
            notices: linked.notice(state.linked).map { [$0] } ?? [],
            problem: state.problem.map { ProblemPresentation(title: "A change was not kept", message: $0) }, settings: [],
            about: AboutPresentation.present(appName: TemplateApp.appName, version: build.version, channel: build.channel, offered: nil))
    }

    /// An item's name, as its record holds it.
    static func name(_ record: Record) -> String {
        let name = record.properties["name"]?.text ?? ""
        return name.isEmpty ? "Untitled item" : name
    }

    /// Settings as one screen: the app's sections, then the kit's.
    public static func settings(_ link: LinkPresentation<TemplatePresentation>, _ app: TemplatePresentation) -> SettingsPresentation {
        SettingsPresenter.screen(app.settings, link: link, storage: nil, about: app.about)
    }
}
