import SwiftUI

#if canImport(WidgetKit)
import WidgetKit
#endif

extension View {
    /// What the system tints where it draws a widget in one accent colour:
    /// the glyphs and the flag stripes.
    @ViewBuilder func accented() -> some View {
        #if canImport(WidgetKit)
        widgetAccentable()
        #else
        self
        #endif
    }
}
