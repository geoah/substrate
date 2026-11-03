#if canImport(SwiftUI)
import SubstrateScreens
import SwiftUI

/// The App menu (§5.6) as the window's width draws it, for an app whose wide
/// layout is a split view of its own rather than `AppMenuView`'s sidebar of
/// sections: the tabs (`AppMenuView` with `.tabs`) on a phone, the split
/// view on an iPad and a Mac. Both are drawn from the same presentation;
/// which section is open is its `selected`, never the view's.
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
