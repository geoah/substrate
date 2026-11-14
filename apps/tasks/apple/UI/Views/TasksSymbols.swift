import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// The SF Symbols and colours of Tasks' own marks: the priorities, the due
/// tones and a task's state; the menu's and the tones' are the kit's
/// design tokens.
enum TasksSymbols {
    /// A presentation's name for a mark: Tasks' own, else the kit's design
    /// tokens'.
    static func symbol(_ name: String) -> String {
        switch name {
        case "suggestion": "sparkle"
        case "nodate": "calendar.badge.exclamationmark"
        case "group": "folder"
        case "missing": "questionmark.folder"
        default: DesignTokens.menuSymbol(name).sfSymbol
        }
    }

    static func symbol(_ priority: TaskPriority) -> String {
        switch priority {
        case .none: "minus"
        case .low: "arrow.down"
        case .medium: "exclamationmark"
        case .high: "exclamationmark.2"
        case .urgent: "exclamationmark.3"
        }
    }

    /// A priority's colour, the cards' and the detail's alike: red is the
    /// most urgent (and lateness), orange high, the rest quiet.
    static func color(_ priority: TaskPriority) -> Color {
        switch priority {
        case .none, .low, .medium: .secondary
        case .high: .orange
        case .urgent: .red
        }
    }

    static func color(_ tone: DueWords.Tone) -> Color {
        switch tone {
        case .quiet: .secondary
        case .soon: .orange
        case .overdue: .red
        }
    }

    static var screen: some View {
        grouped.ignoresSafeArea()
    }

    static var grouped: Color {
        #if os(iOS)
        Color(
            uiColor: UIColor { traits in
                traits.userInterfaceStyle == .dark
                    ? UIColor(red: 0.065, green: 0.070, blue: 0.085, alpha: 1)
                    : .systemGroupedBackground
            })
        #else
        Color(
            nsColor: NSColor(name: nil) { appearance in
                appearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
                    ? NSColor(srgbRed: 0.065, green: 0.070, blue: 0.085, alpha: 1)
                    : .windowBackgroundColor
            })
        #endif
    }

    /// A card on the screen: white in light mode, raised grey in dark.
    static var card: Color {
        #if os(iOS)
        Color(
            uiColor: UIColor { traits in
                traits.userInterfaceStyle == .dark
                    ? UIColor(red: 0.115, green: 0.125, blue: 0.145, alpha: 1)
                    : .secondarySystemGroupedBackground
            })
        #else
        Color(
            nsColor: NSColor(name: nil) { appearance in
                appearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
                    ? NSColor(srgbRed: 0.115, green: 0.125, blue: 0.145, alpha: 1)
                    : .controlBackgroundColor
            })
        #endif
    }

    static func mark(_ standing: KindStatus.Standing) -> String {
        switch standing {
        case .open, .proposed: "circle"
        case .done: "checkmark.circle.fill"
        case .dropped: "xmark.circle"
        }
    }
}

/// A symbol and its words set close, as a chip or a short line: the
/// system's label leaves a gap meant for a list row.
struct TightLabelStyle: LabelStyle {
    var spacing: CGFloat = 4

    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: spacing) {
            configuration.icon
            configuration.title
        }
    }
}
