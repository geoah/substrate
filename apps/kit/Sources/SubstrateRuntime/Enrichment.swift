import Foundation

/// What a controller may read while it turns a command into events: the
/// state as it stands, the presentation the UI drew it from, the instant
/// and new ids from the environment, and the environment itself for
/// lookups. Nothing here is passed to the engine except through an event.
public struct Enrichment<C: Controller> {
    public var state: C.State
    public var transient: C.Transient
    /// The presentation the command was sent from; nil before the first.
    public var presentation: C.Presentation?
    public var now: Date
    public var link: LinkSnapshot?
    public var environment: AppEnvironment

    public init(state: C.State, transient: C.Transient, presentation: C.Presentation?, now: Date, link: LinkSnapshot?,
                environment: AppEnvironment)
    {
        self.state = state
        self.transient = transient
        self.presentation = presentation
        self.now = now
        self.link = link
        self.environment = environment
    }

    /// A new id from the environment's source: time-ordered, unique.
    public func mint() -> UUID { environment.ids.next() }
}
