import Foundation
import SubstrateRuntime

/// An app's root whose records are the kit's `LinkedRecords` (`linked`), its
/// event carrying theirs as `case records(LinkedRecords.Event)`, its effect
/// theirs as `case records(LinkedRecords.Effect)`, and its state theirs as
/// `linked`. The extension below answers every stage for those cases, so an
/// app writes the parts that are its own alone:
///
/// - `apply(_:to:from:)`: its engine, after the records' own event is
///   applied (a `.records` event arrives with `state.linked` moved already;
///   the app may still read it, as it reads `.notQueued` for its problem);
/// - `ownEffects(of:on:from:presented:)`: what any event asks of it beyond
///   what the records ask;
/// - `ownKey(of:)` and `performOwn(_:in:)`: its own effects' keys and work.
///
/// The records open with the app (`event(atOpen:)` is `.records(.opened)`),
/// hear the link (`event(forLink:)`), report to it (`report(of:)`) and are
/// drawn within their window (`coalescing(of:)`). An app that answers one of
/// those itself writes it whole.
public protocol LinkedRecordsApp: LinkedApp
    where Event: LinkedRecordsEvent, Effect: LinkedRecordsEffect, State: LinkedRecordsHolding
{
    var linked: LinkedRecords { get }

    /// The app's engine for every event, after the records': `state` is
    /// `previous` with a `.records` event applied to `linked`.
    func apply(_ event: Event, to state: State, from previous: State) throws -> State
    /// What an event asks of the app's own, after what the records ask.
    func ownEffects(of event: Event, on state: State, from previous: State, presented presentation: Presentation) -> [Effect]
    /// The key of an effect that is not the records'.
    func ownKey(of effect: Effect) -> String?
    /// The work of an effect that is not the records'.
    func performOwn(_ effect: Effect, in environment: AppEnvironment) -> Work<Event>
}

public extension LinkedRecordsApp {
    func apply(_ event: Event, to state: State, from previous: State) throws -> State { state }
    func ownEffects(of event: Event, on state: State, from previous: State, presented presentation: Presentation) -> [Effect] { [] }
    func ownKey(of effect: Effect) -> String? { nil }
    func performOwn(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> { .none }

    func event(atOpen replayed: Bool) -> Event? { .records(.opened) }

    func event(forLink link: LinkSnapshot) -> Event? { .records(.linked(link)) }

    func report(of event: Event) -> LinkReport? { event.recordsEvent.flatMap(linked.report(of:)) }

    func coalescing(of event: Event) -> Coalescing { event.recordsEvent.map(linked.coalescing(of:)) ?? .now }

    func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        if let records = event.recordsEvent { next.linked = linked.apply(records, to: state.linked) }
        return try apply(event, to: next, from: state)
    }

    func effects(of event: Event, on state: State, from previous: State, presented presentation: Presentation) -> [Effect] {
        let records = event.recordsEvent.map { linked.effects(of: $0, on: state.linked, from: previous.linked).map(Effect.records) } ?? []
        return records + ownEffects(of: event, on: state, from: previous, presented: presentation)
    }

    func key(of effect: Effect) -> String? {
        guard let records = effect.recordsEffect else { return ownKey(of: effect) }
        return linked.key(of: records)
    }

    func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        guard let records = effect.recordsEffect else { return performOwn(effect, in: environment) }
        return linked.perform(records, in: environment).map(Event.records)
    }
}

/// An app's event that carries the records' as `case records(LinkedRecords.Event)`:
/// the case is the constructor, and the case read back is `recordsEvent`,
/// which the app writes as a `switch` over its own cases, so a case renamed
/// fails to compile rather than leaving the records' events unread.
public protocol LinkedRecordsEvent: Sendable {
    static func records(_ event: LinkedRecords.Event) -> Self
    /// The records' event this one carries, nil for the app's own.
    var recordsEvent: LinkedRecords.Event? { get }
}

/// An app's effect that carries the records' as `case records(LinkedRecords.Effect)`,
/// read back as `recordsEffect`, which the app writes as `recordsEvent` is.
public protocol LinkedRecordsEffect: Sendable {
    static func records(_ effect: LinkedRecords.Effect) -> Self
    /// The records' effect this one carries, nil for the app's own.
    var recordsEffect: LinkedRecords.Effect? { get }
}

/// An app's state that keeps the records' as `linked`.
public protocol LinkedRecordsHolding {
    var linked: LinkedRecordsState { get set }
}
