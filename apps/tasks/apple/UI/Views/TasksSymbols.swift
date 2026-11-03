import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// The SF Symbols and colours the presentations' marks are drawn in: the
/// app's names for its marks, the priorities and the due tones.
enum TasksSymbols {
    static func symbol(_ name: String) -> String {
        switch name {
        case "today": "calendar"
        case "inbox": "tray"
        case "list": "list.bullet"
        case "lists": "checklist"
        case "settings": "gearshape"
        default: "circle"
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
