import Foundation
import SubstrateCore
import SubstrateRuntime

/// The smallest controller there is, opened over the bridge as a
/// `RuntimeRoot` with no glue of its own: what an app's roots will be.
public struct Counter: Controller {
    public enum Command: Bridged, Equatable {
        case add(amount: Int)
    }

    public enum Event: Codable, Equatable, Sendable {
        case added(Int)
    }

    public struct Presentation: Bridged, Equatable {
        public var count: Int
        public var words: String
    }

    public init() {}

    public var initialState: Int { 0 }

    public func events(for command: Command, in _: Enrichment<Counter>) -> [Event] {
        switch command {
        case let .add(amount): [.added(amount)]
        }
    }

    public func apply(_ event: Event, to state: Int) throws -> Int {
        switch event {
        case let .added(amount): state + amount
        }
    }

    public func present(_ input: PresenterInput<Counter>) -> Presentation {
        Presentation(count: input.state, words: input.state == 1 ? "one" : "\(input.state) in all")
    }
}
