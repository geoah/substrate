import SubstrateCore

/// A question the owner answers before something is given up.
public enum Confirmation: Bridged, Hashable, Sendable {
    /// Deleting a list and, with it, the tasks it holds.
    case deleteList(id: String)
}
