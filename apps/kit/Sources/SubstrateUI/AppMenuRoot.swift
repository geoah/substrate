#if canImport(SwiftUI)
import SubstrateScreens
import SwiftUI

/// The App menu (§5.6) as the window's width draws it, for an app whose wide
/// layout is a split view of its own: the tabs (`AppMenuView`) on a phone,
/// the split view on an iPad and a Mac. Both are drawn from the same
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
#endif
