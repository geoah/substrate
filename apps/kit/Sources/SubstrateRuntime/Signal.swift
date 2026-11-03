/// A fact the UI acts on once, not draws: a haptic to play, a cover to open,
/// a row to scroll to. A presentation is drawn again and again, so a value
/// alone cannot say "now"; a signal carries a serial its controller moves
/// each time it raises one, and the UI acts on a serial other than the last
/// it saw (`PresentationModel.onSignal`), so the same value raised twice
/// acts twice and a presentation drawn again acts not at all.
///
/// The serial only grows: `clear()` drops the value and keeps it, so the next
/// raise is never taken for one already acted on. It starts afresh where the
/// state holding it does (a controller's transient state, at every launch),
/// and so does the UI that reads it.
public struct Signal<Value: Codable & Equatable & Sendable>: Codable, Equatable, Sendable {
    /// The value last raised; nil before the first and after `clear()`.
    public private(set) var value: Value?
    /// Moved by every `raise`.
    public private(set) var serial: Int

    public init() {
        value = nil
        serial = 0
    }

    /// A signal already raised: a fixture, or a preview.
    public init(_ value: Value, serial: Int) {
        self.value = value
        self.serial = serial
    }

    public mutating func raise(_ value: Value) {
        serial += 1
        self.value = value
    }

    /// Nothing to act on, until the next raise.
    public mutating func clear() {
        value = nil
    }
}
