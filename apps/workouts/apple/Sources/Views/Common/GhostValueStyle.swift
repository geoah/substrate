import SwiftUI
import UIKit

/// A ghost value's grey in the set grid (§5.5, §7.6): readable at arm's
/// length, regular weight, distinct from a typed value (black) and a done one
/// (black, bold). About 5:1 on the light field and 6:1 to 7:1 on the dark
/// one, where the system's placeholder grey is under 2:1; darker or lighter
/// again with Increase Contrast.
enum GhostValueStyle {
    static let color = Color(UIColor { traits in
        switch (traits.userInterfaceStyle == .dark, traits.accessibilityContrast == .high) {
        case (false, false): UIColor(rgb: 0x6C6C70)
        case (false, true): UIColor(rgb: 0x48484A)
        case (true, false): UIColor(rgb: 0x98989D)
        case (true, true): UIColor(rgb: 0xC7C7CC)
        }
    })
}
