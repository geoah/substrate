import Foundation
import SubstrateTesting

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

/// A completion's undo window, waited out: once the settle effect's sleep
/// is on the clock (however late it starts on a slow host), the clock moves
/// past it. A wait on the clock alone, moved too early, would sleep until
/// a deadline the clock had already passed.
func window(_ harness: ControllerHarness<TasksApp>) async throws {
    let end = harness.clock.now.addingTimeInterval(TasksApp.undoWindow)
    try await harness.until { $0.clock.deadlines.contains(end) }
    await harness.advance(by: TasksApp.undoWindow)
}
