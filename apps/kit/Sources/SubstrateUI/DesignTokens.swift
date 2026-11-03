#if canImport(SwiftUI)
import SwiftUI
import SubstrateScreens

// The presentations' marks and tones as SwiftUI draws them (§7.4). A test
// writes the same table for Android's renderers later; nothing else in the
// renderers names a colour or a symbol.

public extension Tone {
    var color: Color {
        switch self {
        case .quiet: .secondary
        case .positive: .green
        case .informative: .blue
        case .supporting: .teal
        case .caution: .orange
        case .critical: .red
        }
    }
}

public extension ScreenSymbol {
    var systemName: String {
        switch self {
        case .serverUpdate: "arrow.up.circle.fill"
        case .warning: "exclamationmark.triangle.fill"
        case .approval: "checkmark.shield.fill"
        case .clock: "clock.fill"
        case .ended: "xmark.circle.fill"
        case .offline: "wifi.exclamationmark"
        case .key: "key.fill"
        case .server: "server.rack"
        case .found: "checkmark.circle.fill"
        case .accessEnded: "person.crop.circle.badge.exclamationmark"
        case .allowed: "checkmark.circle.fill"
        case .leftOut: "circle.dashed"
        case .install: "arrow.down.circle.fill"
        case .upgrade: "arrow.up.circle.fill"
        case .create: "plus.circle.fill"
        case .webhook: "bolt.circle.fill"
        case .cannot: "xmark.circle"
        case .upToDate: "checkmark.circle.fill"
        case .sending: "arrow.triangle.2.circlepath"
        case .device: "iphone"
        case .problem: "exclamationmark.circle.fill"
        }
    }
}
#endif
