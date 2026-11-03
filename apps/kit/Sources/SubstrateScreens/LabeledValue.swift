/// A label and its value, read the same in a read view and an edit view.
public struct LabeledValue: Codable, Hashable, Sendable {
    public var label: String
    public var value: String
    /// The value is an identifier a person may copy (a version, an id).
    public var verbatim: Bool

    public init(_ label: String, _ value: String, verbatim: Bool = false) {
        self.label = label
        self.value = value
        self.verbatim = verbatim
    }
}
