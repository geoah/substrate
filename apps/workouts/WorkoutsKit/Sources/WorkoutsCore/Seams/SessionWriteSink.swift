import Foundation

/// Told about every change the session controller applies, in order. The
/// controller calls it after the journal write and never awaits it on the UI
/// path. V1: `NoWrites`. V2: the write planner, which turns each call into
/// outbox entries (`PUT`, `PATCH`, `DELETE`).
///
/// Generic over the engine's event and state, so the seam does not depend on
/// the engine: the controller holds an
/// `any SessionWriteSink<EventEnvelope, SessionState>`.
public protocol SessionWriteSink<Event, State>: Sendable {
    associatedtype Event: Sendable
    associatedtype State: Sendable

    func applied(_ event: Event, state: State) async
    func finished(_ records: SessionRecords) async
    func discarded(_ sessionID: SessionID) async
    func healthLinked(_ sessionID: SessionID, healthKitWorkout: String, revision: Int) async
    /// V2 PUTs the exercise before anything names it.
    func exerciseAdded(_ exercise: Exercise) async
}

/// V1's sink: nothing leaves the phone.
public struct NoWrites<Event: Sendable, State: Sendable>: SessionWriteSink {
    public init() {}

    public func applied(_: Event, state _: State) async {}
    public func finished(_: SessionRecords) async {}
    public func discarded(_: SessionID) async {}
    public func healthLinked(_: SessionID, healthKitWorkout _: String, revision _: Int) async {}
    public func exerciseAdded(_: Exercise) async {}
}
