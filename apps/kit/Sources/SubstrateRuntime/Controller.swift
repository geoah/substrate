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
///
/// **The names.** The loop's three stages are verbs: `apply`, `present`,
/// `perform`. Every other requirement is a noun, named for what it
/// answers (`events`, `event`, `journaling`, `transient`, `coalescing`,
/// `effects`, `key`, `nextChange`, `children`, `link`, `journal`), and its
/// argument label says what it answers about: `of` an event, an effect or a
/// presentation; `for` a command or the link; `at` the open or a tick;
/// `from` a child; `replacing` the value before; `in` a state or an
/// environment.
///
/// **The defaults**, in the extension below, so a controller writes only
/// what it uses: no journal, so nothing is journaled; the transient state
/// unchanged by an applied or a refused event; every event presented at
/// once; no effects, no keys, no work; no next change and no tick event; no
/// event at open, for the link, from a child or for a new value; no event
/// resets; no children; no link.
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
    /// A child is a value the parent makes from its state, so what the
    /// parent hands it (a catalogue, a history index, the settings) is part
    /// of the value, and a different value replaces the one running.
    associatedtype Child: Controller & Equatable = NoChild

    var initialState: State { get }
    var initialTransient: Transient { get }

    // MARK: The three stages

    /// The engine.
    func apply(_ event: Event, to state: State) throws -> State
    /// The presenter.
    func present(_ input: PresenterInput<Self>) -> Presentation
    /// An effect's work; its results are events through the same loop.
    func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event>

    // MARK: Events from outside the engine

    /// The events a command means, enriched with what a pure function may not
    /// read: new ids, the instant, lookups in the environment. Applied in
    /// order; the first the engine refuses stops the rest.
    func events(for command: Command, in context: Enrichment<Self>) -> [Event]
    /// The event the controller applies once it has opened, on the live
    /// path, so its effects run (a replayed session checked against the
    /// history, a Live Activity restored). `replayed` says a journal was
    /// folded.
    func event(atOpen replayed: Bool) -> Event?
    /// The event the instant `nextChange(of:)` named delivers; nil draws
    /// the presentation again, for a presenter that reads the instant.
    func event(atTick instant: Date) -> Event?
    /// The link's snapshot as an event, each time it changes and when the
    /// controller is made; nil ignores it.
    func event(forLink link: LinkSnapshot) -> Event?
    /// A child's applied event as this controller's own, for its engine to
    /// fold; nil leaves it the child's alone.
    func event(fromChild key: ChildKey, _ event: Child.Event) -> Event?
    /// The same, with the child's state after its event, for a parent whose
    /// rules turn on what the child holds (Chat's list drops a read for a
    /// thread whose conversation owns the transcript). The default is
    /// `event(fromChild:_:)`, which sees the event alone.
    func event(fromChild key: ChildKey, _ event: Child.Event, after state: Child.State) -> Event?
    /// The event a child applies when its parent hands it a value that
    /// differs (`children(in:)`), so what changed reaches its engine and its
    /// effects (the thread a conversation shows moved: attach, or poll);
    /// nil takes the value alone.
    func event(replacing previous: Self) -> Event?

    // MARK: What an applied event asks for

    /// What the journal does with an applied event. Only an event the state
    /// cannot be rebuilt without is kept; one whose outcome waits on work
    /// outside the engine is skipped, and its outcome is a second event.
    func journaling(of event: Event) -> Journaling
    /// Whether an applied event starts the controller over: its controller
    /// generation moves, so every effect running under it is cancelled and
    /// every result still on its way dropped, and its children are closed
    /// and made again from the state the event left (a sign-out). The
    /// engine makes that state; the event's own effects start after.
    func resetting(of event: Event) -> Bool
    /// The transient state after an event the engine applied.
    func transient(after event: Event, applied state: State, from transient: Transient) -> Transient
    /// The transient state after an event the engine refused.
    func transient(afterRefusing event: Event, because error: any Error, from transient: Transient) -> Transient
    /// Whether the presentation follows the event at once or within a window
    /// shared with the events after it.
    func coalescing(of event: Event) -> Coalescing
    /// Decided after the event is journaled and presented, on the live path
    /// only: a replay runs none.
    func effects(of event: Event, on state: State, presented presentation: Presentation) -> [Effect]
    /// The key an effect runs under: starting one cancels the effect running
    /// under the same key, whose result is then dropped.
    func key(of effect: Effect) -> String?
    /// The instant the presentation stops being true as drawn (a countdown's
    /// end), when the runtime delivers `event(atTick:)`.
    func nextChange(of presentation: Presentation) -> Date?

    // MARK: What the state holds

    /// The children the state keeps, by the key the app minted for each. A
    /// key that appears is a child made; one that goes is a child closed; a
    /// value that differs replaces the controller running under its key,
    /// whose state stays.
    func children(in state: State) -> [ChildKey: Child]
    /// The link's snapshot this state makes, delivered down the tree inside
    /// the step that changed it; nil for every controller but the link.
    func link(in state: State) -> LinkSnapshot?
    /// Where the journal lives; nil keeps none.
    func journal(in environment: AppEnvironment) -> (any JournalStore<Event, State>)?
}

public extension Controller {
    func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> { .none }
    func event(atOpen replayed: Bool) -> Event? { nil }
    func event(atTick instant: Date) -> Event? { nil }
    func event(forLink link: LinkSnapshot) -> Event? { nil }
    func event(fromChild key: ChildKey, _ event: Child.Event) -> Event? { nil }
    func event(fromChild key: ChildKey, _ event: Child.Event, after state: Child.State) -> Event? { self.event(fromChild: key, event) }
    func event(replacing previous: Self) -> Event? { nil }
    func resetting(of event: Event) -> Bool { false }
    func journaling(of event: Event) -> Journaling { .skip }
    func transient(after event: Event, applied state: State, from transient: Transient) -> Transient { transient }
    func transient(afterRefusing event: Event, because error: any Error, from transient: Transient) -> Transient { transient }
    func coalescing(of event: Event) -> Coalescing { .now }
    func effects(of event: Event, on state: State, presented presentation: Presentation) -> [Effect] { [] }
    func key(of effect: Effect) -> String? { nil }
    func nextChange(of presentation: Presentation) -> Date? { nil }
    func children(in state: State) -> [ChildKey: Child] { [:] }
    func link(in state: State) -> LinkSnapshot? { nil }
    func journal(in environment: AppEnvironment) -> (any JournalStore<Event, State>)? { nil }
}

public extension Controller where Transient == NoTransient {
    var initialTransient: NoTransient { NoTransient() }
}
