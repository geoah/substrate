import Foundation

@testable import TasksCore

/// A list's open rows' titles, as drawn.
func titles(_ presentation: TasksPresentation) -> [String] {
    presentation.list?.open.rows.map(\.title) ?? []
}

/// A formatter's time with its narrow spaces as plain ones, which ICU
/// versions write differently.
func spaced(_ text: String) -> String {
    text.replacingOccurrences(of: "\u{202F}", with: " ").replacingOccurrences(of: "\u{00A0}", with: " ")
}
