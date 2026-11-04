import SwiftUI
import UIKit
import WorkoutsCore

/// A group's bracket, header and member labels. One grey for every group:
/// a colour per group read as a warm-up's orange or a failure's red, so
/// colour is kept for what a set is (owner, 2026-11-02). The grey reads at
/// 4.5:1 or better on the card in both appearances.
enum GroupPalette {
    static func color(_: Int?) -> Color {
        adaptive(light: 0x6C6C70, dark: 0x98989D)
    }

    /// The orange of a group and of a warm-up's `W`.
    static let warmup = adaptive(light: 0xB35C00, dark: 0xFF9F0A)

    static func adaptive(light: UInt32, dark: UInt32) -> Color {
        Color(
            UIColor { traits in
                UIColor(rgb: traits.userInterfaceStyle == .dark ? dark : light)
            })
    }
}
