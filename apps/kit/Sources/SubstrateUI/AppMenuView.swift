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

/// The app's menu as its presentation words it: a tab per section on a
/// phone, a sidebar of them on iPad and the Mac, the section shown beside
/// it. Choosing one is the app's to send; the menu keeps no state. A
/// section's mark is its symbol's name in the design tokens.
public struct AppMenuView<Content: View>: View {
    let menu: AppMenuPresentation
    let title: String?
    let layout: AppMenuLayout
    let select: (String) -> Void
    let content: (AppMenuSection) -> Content
    @Environment(\.horizontalSizeClass) private var sizeClass

    /// - Parameter title: the app's name, over a sidebar.
    public init(_ menu: AppMenuPresentation, title: String? = nil, layout: AppMenuLayout = .automatic,
                select: @escaping (String) -> Void, @ViewBuilder content: @escaping (AppMenuSection) -> Content)
    {
        self.menu = menu
        self.title = title
        self.layout = layout
        self.select = select
        self.content = content
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
            TabView(selection: Binding(get: { menu.selected }, set: select)) {
                ForEach(menu.sections) { section in
                    Tab(section.title, systemImage: DesignTokens.menuSymbol(section.symbol).sfSymbol, value: section.id) {
                        content(section)
                    }
                    .badge(section.badge ?? 0)
                }
            }
        } else {
            NavigationSplitView {
                List(selection: Binding<String?>(get: { menu.selected }, set: { $0.map(select) })) {
                    ForEach(menu.sections) { section in
                        Label(section.title, systemImage: DesignTokens.menuSymbol(section.symbol).sfSymbol)
                            .badge(section.badge ?? 0)
                            .tag(section.id)
                    }
                }
                .navigationTitle(title ?? "")
            } detail: {
                if let section = menu.sections.first(where: { $0.id == menu.selected }) { content(section) }
            }
        }
    }
}
#endif
