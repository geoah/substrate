#if canImport(SwiftUI)
import SubstrateScreens
import SwiftUI

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
