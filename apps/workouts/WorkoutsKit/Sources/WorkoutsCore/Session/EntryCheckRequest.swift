/// A set far from the recent ones, waiting for the owner's answer (§5.7).
public struct EntryCheckRequest: Codable, Equatable, Sendable {
    public var row: RowID
    public var values: SetValues
    public var warning: EntryWarning
    public var measurement: MeasurementType
    /// A timed set's Stop rather than a tick.
    public var isStop: Bool

    public init(row: RowID, values: SetValues, warning: EntryWarning, measurement: MeasurementType, isStop: Bool) {
        self.row = row
        self.values = values
        self.warning = warning
        self.measurement = measurement
        self.isStop = isStop
    }
}
