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
    /// A presentation's name for a mark, in the kit's design tokens.
    static func symbol(_ name: String) -> String { DesignTokens.menuSymbol(name).sfSymbol }

    static func symbol(_ priority: TaskPriority) -> String {
        switch priority {
        case .none: "minus"
        case .low: "arrow.down"
        case .medium: "exclamationmark"
        case .high: "exclamationmark.2"
        case .urgent: "exclamationmark.3"
        }
    }

    static func color(_ priority: TaskPriority) -> Color {
        switch priority {
        case .none, .low: .secondary
        case .medium: .yellow
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

    static func mark(_ status: TaskStatus) -> String {
        switch status {
        case .open: "circle"
        case .done: "checkmark.circle.fill"
        case .abandoned: "xmark.circle"
        }
    }
}
