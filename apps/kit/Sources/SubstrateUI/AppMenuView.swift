#if canImport(SwiftUI)
import SwiftUI
import SubstrateScreens

/// The app's menu as its presentation words it: a tab per section on a
/// phone, the system's sidebar of them on a Mac, the section shown beside
/// it. Choosing one is the app's to send; the menu keeps no state. A
/// section's mark is its symbol's name in the design tokens. An app whose
/// wide layout is a split view of its own draws that instead there
/// (`AppMenuRoot`, `AppMenuSplit`). An `action` (an app's add) is the
/// system's prominent tab, drawn apart at the bar's trailing end, and runs
/// rather than being shown: the section stays the one chosen.
public struct AppMenuView<Content: View>: View {
    let menu: AppMenuPresentation
    let select: (String) -> Void
    let action: AppMenuAction?
    let content: (AppMenuSection) -> Content

    public init(
        _ menu: AppMenuPresentation, select: @escaping (String) -> Void, action: AppMenuAction? = nil,
        @ViewBuilder content: @escaping (AppMenuSection) -> Content
    ) {
        self.menu = menu
        self.select = select
        self.action = action
        self.content = content
    }

    /// The action's tab value, never a section's.
    static var actionID: String { "substrate.menu.action" }

    /// The role that sets a tab apart: `prominent` from iOS 27; before it,
    /// the search role, which iOS 26 draws apart on its own.
    static var apart: TabRole {
        if #available(iOS 27.0, macOS 27.0, *) { .prominent } else { .search }
    }

    public var body: some View {
        TabView(
            selection: Binding(
                get: { menu.selected },
                set: { chosen in
                    if chosen == Self.actionID { action?.perform() } else { select(chosen) }
                })
        ) {
            ForEach(menu.sections) { section in
                Tab(section.title, systemImage: DesignTokens.menuSymbol(section.symbol).sfSymbol, value: section.id) {
                    content(section)
                }
                .badge(section.badge ?? 0)
            }
            if let action {
                Tab(action.title, systemImage: action.symbol, value: Self.actionID, role: Self.apart) {
                    // Never shown: choosing the tab runs the action instead.
                    Color.clear
                }
            }
        }
        .tabViewStyle(.sidebarAdaptable)
    }
}

/// What a menu's trailing button does: its words and SF Symbol, and the
/// app's command to send.
public struct AppMenuAction {
    public var title: String
    public var symbol: String
    public var perform: () -> Void

    public init(_ title: String, symbol: String, perform: @escaping () -> Void) {
        self.title = title
        self.symbol = symbol
        self.perform = perform
    }
}
#endif
