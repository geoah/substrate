import Foundation

/// One screen's loop (docs/plans/app-kit.md §5.1): what turns a command into
/// events, the engine that applies them, the presenter that draws the state,
/// and the effects an applied event asks for. A `Runtime` runs it; nothing
/// else calls these, and every one but `events(for:in:)` and
/// `perform(_:in:)` must be a pure function of its arguments, since a
/// replay folds the journal through `apply` and a test pins the rest by
/// value.
///
/// Commands, events and presentations are values that encode, because the
/// Android bridge carries commands in and presentations out as JSON and a
/// journal keeps events on the device.
public protocol Controller: Sendable {
    associatedtype Command: Codable & Equatable & Sendable
    associatedtype Event: Codable & Equatable & Sendable
    /// The engine's state: what a replay of the journal rebuilds.
    associatedtype State: Codable & Equatable & Sendable
    /// State the presenter reads that no replay needs (an entry check
    /// waiting, a scroll target, the last refusal): it starts afresh at
    /// every launch.
    associatedtype Transient: Equatable & Sendable = NoTransient
    associatedtype Presentation: Codable & Equatable & Sendable
    /// What an applied event asks the device or the server to do, as a
    /// value a test compares; `perform(_:in:)` turns it into work.
    associatedtype Effect: Equatable & Sendable = Never
    /// The controllers this one keeps keyed under it, one type per parent.
    associatedtype Child: Controller = NoChild

    var initialState: State { get }
    var initialTransient: Transient { get }

    // MARK: The controller

    /// The events a command means, enriched with what a pure function may not
    /// read: new ids, the instant, lookups in the environment. Applied in
    /// order; the first the engine refuses stops the rest.
    func events(for command: Command, in context: Enrichment<Self>) -> [Event]

    // MARK: The engine

    func apply(_ event: Event, to state: State) throws -> State
    /// What the journal does with an applied event. Only an event the state
    /// cannot be rebuilt without is kept; one whose outcome waits on work
    /// outside the engine is skipped, and its outcome is a second event.
    func journaling(_ event: Event) -> Journaling
    /// The transient state after an event the engine applied.
    func noting(_ event: Event, applied state: State, in transient: Transient) -> Transient
    /// The transient state after an event the engine refused.
    func refusing(_ event: Event, because error: any Error, in transient: Transient) -> Transient
    /// Whether the presentation follows the event at once or within a window
    /// shared with the events after it.
    func coalescing(_ event: Event) -> Coalescing

    // MARK: The presenter

    func present(_ input: PresenterInput<Self>) -> Presentation
    /// The instant the presentation stops being true as drawn (a countdown's
    /// end), when the runtime delivers `tick(at:)`.
    func nextChange(of presentation: Presentation) -> Date?
    func tick(at instant: Date) -> Event?

    // MARK: The effects

    /// Decided after the event is journaled and presented, on the live path
    /// only: a replay runs none.
    func effects(of event: Event, on state: State, presented presentation: Presentation) -> [Effect]
    /// The key an effect runs under: starting one cancels the effect running
    /// under the same key, whose result is then dropped.
    func key(of effect: Effect) -> String?
    func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event>

    // MARK: Composition

    /// The link's snapshot as an event; nil ignores it.
    func linkChanged(_ link: LinkSnapshot) -> Event?
    /// The children the state keeps, by the key the app minted for each. A
    /// key that appears is a child made; one that goes is a child closed.
    func children(in state: State) -> [ChildKey: Child]
    /// A child's applied event as this controller's own, for its engine to
    /// fold; nil leaves it the child's alone.
    func event(fromChild key: ChildKey, _ event: Child.Event) -> Event?
    /// Where the journal lives; nil keeps none.
    func journal(in environment: AppEnvironment) -> (any JournalStore<Event, State>)?
}

public extension Controller {
    func journaling(_ event: Event) -> Journaling { .skip }
    func noting(_ event: Event, applied state: State, in transient: Transient) -> Transient { transient }
    func refusing(_ event: Event, because error: any Error, in transient: Transient) -> Transient { transient }
    func coalescing(_ event: Event) -> Coalescing { .now }
    func nextChange(of presentation: Presentation) -> Date? { nil }
    func tick(at instant: Date) -> Event? { nil }
    func effects(of event: Event, on state: State, presented presentation: Presentation) -> [Effect] { [] }
    func key(of effect: Effect) -> String? { nil }
    func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> { .none }
    func linkChanged(_ link: LinkSnapshot) -> Event? { nil }
    func children(in state: State) -> [ChildKey: Child] { [:] }
    func event(fromChild key: ChildKey, _ event: Child.Event) -> Event? { nil }
    func journal(in environment: AppEnvironment) -> (any JournalStore<Event, State>)? { nil }
}

public extension Controller where Transient == NoTransient {
    var initialTransient: NoTransient { NoTransient() }
}
