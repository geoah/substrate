import Foundation

/// How the session cover draws the session (DESIGN.md §7.6, §7.6a): Rows,
/// one line per set with the set to do now open in place, or Table, the
/// grid. Both read the same presentation and send the same commands.
public enum SessionLayout: String, Codable, CaseIterable, Equatable, Sendable {
    case rows
    case table

    public var title: String {
        switch self {
        case .rows: "Rows"
        case .table: "Table"
        }
    }

    /// The session's More menu item that switches to the other layout.
    public var switchTitle: String {
        switch self {
        case .rows: "Show as table"
        case .table: "Show as rows"
        }
    }

    public var switchSymbol: String {
        switch self {
        case .rows: "tablecells"
        case .table: "list.bullet.rectangle"
        }
    }

    public var other: SessionLayout { self == .rows ? .table : .rows }
}
