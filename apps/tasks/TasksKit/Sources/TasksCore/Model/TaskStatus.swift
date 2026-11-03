import SubstrateCore

/// Where a task stands: open until marked done or abandoned, either undone
/// back to open. The package's transitions allow nothing else.
public enum TaskStatus: String, Bridged, Hashable, Sendable, CaseIterable {
    case open
    case done
    case abandoned

    /// Whether the package moves a task from this state to `next`.
    public func moves(to next: TaskStatus) -> Bool {
        switch (self, next) {
        case (.open, .done), (.done, .open), (.open, .abandoned), (.abandoned, .open): true
        default: false
        }
    }
}
