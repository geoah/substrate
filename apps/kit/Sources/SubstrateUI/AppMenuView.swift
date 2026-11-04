#if canImport(SwiftUI)
import SwiftUI
import SubstrateScreens

/// The app's menu on a phone, as its presentation words it: a tab per
/// section, the section shown under it. Choosing one is the app's to send;
/// the menu keeps no state. A section's mark is its symbol's name in the
/// design tokens. A wide window draws the app's own split view instead
/// (`AppMenuRoot`, `AppMenuSplit`).
public struct AppMenuView<Content: View>: View {
    let menu: AppMenuPresentation
    let select: (String) -> Void
    let content: (AppMenuSection) -> Content

    public init(_ menu: AppMenuPresentation, select: @escaping (String) -> Void,
                @ViewBuilder content: @escaping (AppMenuSection) -> Content)
    {
        self.menu = menu
        self.select = select
        self.content = content
    }

    public var body: some View {
        TabView(selection: Binding(get: { menu.selected }, set: select)) {
            ForEach(menu.sections) { section in
                Tab(section.title, systemImage: DesignTokens.menuSymbol(section.symbol).sfSymbol, value: section.id) {
                    content(section)
                }
                .badge(section.badge ?? 0)
            }
        }
    }
}
#endif
