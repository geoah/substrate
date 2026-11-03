#if canImport(SwiftUI)
import SwiftUI
import SubstrateScreens

/// How the menu is drawn: by the window, or as one of the two.
public enum AppMenuLayout: Sendable {
    /// Tabs in a compact width, a sidebar otherwise.
    case automatic
    case tabs
    case sidebar
}

/// The app's menu as its presentation words it: a tab per destination on a
/// phone, a sidebar of its sections on iPad and the Mac, the destination
/// shown beside it. Choosing one is a command; the menu keeps no state.
public struct AppMenuView<Content: View>: View {
    let menu: AppMenuPresentation
    let layout: AppMenuLayout
    let send: (AppMenuCommand) -> Void
    let content: (AppMenuItem) -> Content
    @Environment(\.horizontalSizeClass) private var sizeClass

    public init(_ menu: AppMenuPresentation, layout: AppMenuLayout = .automatic, send: @escaping (AppMenuCommand) -> Void,
                @ViewBuilder content: @escaping (AppMenuItem) -> Content)
    {
        self.menu = menu
        self.layout = layout
        self.send = send
        self.content = content
    }

    private var selection: Binding<String> {
        Binding(get: { menu.selected }, set: { send(.select(item: $0)) })
    }

    private var usesTabs: Bool {
        switch layout {
        case .tabs: true
        case .sidebar: false
        case .automatic: sizeClass == .compact
        }
    }

    public var body: some View {
        if usesTabs {
            TabView(selection: selection) {
                ForEach(menu.items) { item in
                    Tab(item.title, systemImage: item.symbol.systemName, value: item.id) {
                        content(item)
                    }
                    .badge(item.badge.map { Text($0) })
                }
            }
        } else {
            NavigationSplitView {
                List(selection: Binding<String?>(get: { menu.selected }, set: { $0.map { send(.select(item: $0)) } })) {
                    ForEach(menu.sections) { section in
                        Section {
                            ForEach(section.items) { item in
                                Label(item.title, systemImage: item.symbol.systemName)
                                    .badge(item.badge.map { Text($0) })
                                    .tag(item.id)
                            }
                        } header: {
                            if let title = section.title { Text(title) }
                        }
                    }
                }
                .navigationTitle(menu.title)
            } detail: {
                if let item = menu.items.first(where: { $0.id == menu.selected }) { content(item) }
            }
        }
    }
}
#endif
