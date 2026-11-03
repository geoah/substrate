import Foundation
import SubstrateCore
import SubstrateRuntime

/// An app's root controller, kept by the link as its one child
/// (`LinkController<App>`): the link's snapshot reaches it as
/// `event(forLink:)` inside the step that changed it, and what its events
/// say about the connection goes back up as a `LinkReport`.
public protocol LinkedApp: Controller, Equatable {
    /// What an event the app applied tells the link: a request answered
    /// 401 or 403 `insufficient_scope`, from any effect, however deep (a
    /// child's event reaches the app through `event(fromChild:_:)` first).
    /// Nil for every other event.
    func report(of event: Event) -> LinkReport?
}

public extension LinkedApp {
    func report(of event: Event) -> LinkReport? { nil }
}

/// What an app tells the link about the connection.
public enum LinkReport: Codable, Equatable, Sendable {
    /// A request was answered 401: the token's access ended, and only
    /// signing in again gets it back.
    case accessEnded
    /// A request was answered 403 `insufficient_scope`: the token lacks a
    /// scope, which the owner's approval adds.
    case needsApproval

    /// The report an error means, nil for any other error.
    public init?(_ error: any Error) {
        switch error {
        case is SessionExpired: self = .accessEnded
        case is InsufficientScope: self = .needsApproval
        default: return nil
        }
    }
}

/// The app of a link that keeps none: a test, a preview, the fixtures.
public struct NoApp: LinkedApp {
    public typealias Command = Never
    public typealias Event = Never

    public struct State: Codable, Equatable, Sendable {
        public init() {}
    }

    public typealias Presentation = State

    public init() {}

    public var initialState: State { State() }

    public func events(for command: Never, in context: Enrichment<NoApp>) -> [Never] {}
    public func apply(_ event: Never, to state: State) throws -> State {}
    public func present(_ input: PresenterInput<NoApp>) -> State { input.state }
}
