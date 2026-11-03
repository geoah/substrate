import SubstrateCore

/// What a column shows with nothing in it: a title, and what fills it.
public struct EmptyPresentation: Bridged, Hashable, Sendable {
    public var title: String
    public var message: String

    public init(title: String, message: String) {
        self.title = title
        self.message = message
    }
}
