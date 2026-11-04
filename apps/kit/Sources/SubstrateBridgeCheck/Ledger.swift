import Foundation
import SubstrateCore
import SubstrateRuntime

/// A controller whose presentation carries a keyed list, words that read
/// the locale and time zone, and a value a command hands it whole: what the
/// bridge's runtime roots carry for an app's long lists (§5.5).
public struct Ledger: Controller {
    public enum Command: Bridged, Equatable {
        case add(id: String, text: String)
        case change(id: String, text: String)
        case remove(id: String)
        /// Kept as it came, every digit of its numbers included.
        case note(value: JSONValue)
    }

    public enum Event: Codable, Equatable, Sendable {
        case added(id: String, text: String)
        case changed(id: String, text: String)
        case removed(id: String)
        case noted(JSONValue)
    }

    public struct State: Codable, Equatable, Sendable {
        public var rows = KeyedList<LedgerRow>()
        public var note: JSONValue?
    }

    public struct Presentation: Bridged, Equatable {
        public var rows: KeyedList<LedgerRow>
        public var count: String
        /// The instant the ledger was drawn at, in the locale and time zone
        /// the runtime was told last.
        public var drawn: String
        public var note: JSONValue?
    }

    public init() {}

    public var initialState: State { State() }

    public func events(for command: Command, in _: Enrichment<Ledger>) -> [Event] {
        switch command {
        case let .add(id, text): [.added(id: id, text: text)]
        case let .change(id, text): [.changed(id: id, text: text)]
        case let .remove(id): [.removed(id: id)]
        case let .note(value): [.noted(value)]
        }
    }

    public func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        switch event {
        case let .added(id, text): next.rows.update(state.rows.rows + [LedgerRow(id: id, text: text)])
        case let .changed(id, text): next.rows.update(state.rows.rows.map { $0.id == id ? LedgerRow(id: id, text: text) : $0 })
        case let .removed(id): next.rows.update(state.rows.rows.filter { $0.id != id })
        case let .noted(value): next.note = value
        }
        return next
    }

    public func present(_ input: PresenterInput<Ledger>) -> Presentation {
        var style = Date.FormatStyle(date: .complete, time: .shortened)
        style.locale = input.locale
        style.timeZone = input.timeZone
        let count = input.state.rows.rows.count
        return Presentation(
            rows: input.state.rows, count: count == 1 ? "1 row" : "\(count) rows", drawn: Ledger.epoch.formatted(style),
            note: input.state.note)
    }

    /// The instant `drawn` words, fixed so a test reads only the locale's
    /// and the time zone's part in it.
    public static let epoch = Date(timeIntervalSince1970: 1_793_696_400)
}

/// One row of the ledger's keyed list.
public struct LedgerRow: Bridged, Equatable, Identifiable {
    public var id: String
    public var text: String
}
