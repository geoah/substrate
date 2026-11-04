import SwiftUI
import WorkoutsCore

extension HealthSaveStatus {
    /// History's trailing glyph (§7.8); nil shows nothing.
    var symbol: String? {
        switch self {
        case .saved: "heart.fill"
        case .pending: "clock"
        case .notSaved: "exclamationmark.triangle"
        case .notAvailable: nil
        }
    }

    var accessibilityText: String {
        switch self {
        case .saved: "Saved to Apple Health"
        case .pending: "Saving to Apple Health"
        case .notSaved: "Not saved to Apple Health"
        case .notAvailable: ""
        }
    }

    var tint: Color {
        switch self {
        case .saved: .pink
        case .pending: .secondary
        case .notSaved: .orange
        case .notAvailable: .secondary
        }
    }
}
