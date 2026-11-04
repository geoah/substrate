#if canImport(SwiftUI)
import SwiftUI
#if canImport(UIKit)
import UIKit
#else
import AppKit
#endif

public extension Color {
    /// The app's accent as its icon has it, for its own fills and graphics:
    /// its asset catalog's `BrandAccent`, else the accent. `accentColor`,
    /// which the system draws text, links and controls in, is the accent's
    /// text tone (app.json's `accent.text`), which reads at 4.5:1 where the
    /// accent itself may not; an app whose two differ ships `BrandAccent`
    /// (`lint:apps` holds it to).
    static let brandAccent: Color = {
        #if canImport(UIKit)
        UIColor(named: "BrandAccent").map { Color(uiColor: $0) } ?? .accentColor
        #else
        NSColor(named: "BrandAccent").map { Color(nsColor: $0) } ?? .accentColor
        #endif
    }()
}
#endif
