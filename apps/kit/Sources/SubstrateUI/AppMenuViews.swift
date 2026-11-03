#if canImport(SwiftUI)
import SubstrateScreens
import SwiftUI

/// The App menu (§5.6) as the window's width draws it: a tab per section on
/// a phone, a split view on an iPad and a Mac. Both are drawn from the same
/// presentation; which section is open is its `selected`, never the view's.
public struct AppMenuRoot<Tabs: View, Split: View>: View {
    @Environment(\.horizontalSizeClass) private var sizeClass
    let tabs: Tabs
    let split: Split

    public init(@ViewBuilder tabs: () -> Tabs, @ViewBuilder split: () -> Split) {
        self.tabs = tabs()
        self.split = split()
    }

    public var body: some View {
        if sizeClass == .compact { tabs } else { split }
    }
}

/// A tab per section of the App menu, each with its badge, the selected one
/// the presentation's; choosing another is a command.
public struct AppMenuTabs<Content: View>: View {
    let menu: AppMenuPresentation
    let symbol: (String) -> String
    let select: (String) -> Void
    let content: (AppMenuSection) -> Content

    /// - Parameter symbol: the SF Symbol for the app's name of a section's
    ///   mark.
    public init(_ menu: AppMenuPresentation, symbol: @escaping (String) -> String, select: @escaping (String) -> Void,
                @ViewBuilder content: @escaping (AppMenuSection) -> Content)
    {
        self.menu = menu
        self.symbol = symbol
        self.select = select
        self.content = content
    }

    public var body: some View {
        TabView(selection: Binding(get: { menu.selected }, set: select)) {
            ForEach(menu.sections) { section in
                Tab(section.title, systemImage: symbol(section.symbol), value: section.id) {
                    content(section)
                }
                .badge(section.badge ?? 0)
            }
        }
    }
}

/// The App menu on a wide window: the sidebar, the selected section's
/// content beside it, and what is open in it in the detail.
public struct AppMenuSplit<Sidebar: View, Content: View, Detail: View>: View {
    let sidebar: Sidebar
    let content: Content
    let detail: Detail

    public init(@ViewBuilder sidebar: () -> Sidebar, @ViewBuilder content: () -> Content, @ViewBuilder detail: () -> Detail) {
        self.sidebar = sidebar()
        self.content = content()
        self.detail = detail()
    }

    public var body: some View {
        NavigationSplitView {
            sidebar.navigationSplitViewColumnWidth(min: 220, ideal: 250)
        } content: {
            content.navigationSplitViewColumnWidth(min: 320, ideal: 400)
        } detail: {
            detail
        }
    }
}
#endif
