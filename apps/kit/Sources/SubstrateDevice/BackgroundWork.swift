import Foundation

/// Work the system runs while the app is off screen (§5.4): named jobs it
/// runs when it allows, in a process that may have started for them alone,
/// each with a budget; and continued processing, which keeps work under
/// way running after the app leaves the screen, drawn by the system with a
/// Stop. A job reads the session when it runs, never from what scheduled
/// it.
public protocol BackgroundWork: Sendable {
    /// Asks the system to run the job once it allows, no earlier than the
    /// job says.
    func schedule(_ job: BackgroundJob) async
    /// Each run the system grants from now on, the ones granted before the
    /// app asked first. Each is answered once with `finish(_:success:)`.
    func runs() -> AsyncStream<BackgroundRun>
    func finish(_ run: BackgroundRun, success: Bool) async
    /// Keeps the process running for work under way: what the system did
    /// with the request, until it ends (`ended`, the person's Stop or the
    /// system taking the time back, or `endProcessing`).
    func continueProcessing(_ request: ContinuedProcessing) -> AsyncStream<ContinuedProcessing.Update>
    /// The work moved: a continued run whose progress stands still is
    /// ended by the system.
    func advance(_ id: String) async
    func endProcessing(_ id: String) async
}

/// A job by name, and what it needs.
public struct BackgroundJob: Codable, Hashable, Sendable {
    /// The identifier the app declares to the system.
    public var name: String
    /// Not before this instant.
    public var earliest: Date?
    public var needsNetwork: Bool

    public init(name: String, earliest: Date? = nil, needsNetwork: Bool = true) {
        self.name = name
        self.earliest = earliest
        self.needsNetwork = needsNetwork
    }
}

/// One run the system granted.
public struct BackgroundRun: Codable, Hashable, Sendable {
    public var id: String
    public var job: String
    /// How long the run may take, where the system says; it may end it
    /// sooner.
    public var budget: TimeInterval?

    public init(id: String, job: String, budget: TimeInterval? = nil) {
        self.id = id
        self.job = job
        self.budget = budget
    }
}

/// Work under way the app asks the system to keep running, as the system
/// shows it.
public struct ContinuedProcessing: Codable, Hashable, Sendable {
    public enum Update: String, Codable, Hashable, Sendable {
        /// The system runs it, and shows it.
        case began
        /// The system would not run it now; the app has the short time any
        /// app leaving the screen gets.
        case refused
        /// It ended: the person's Stop, the system taking the time back, or
        /// the app's own end.
        case ended
    }

    public var id: String
    public var title: String
    public var subtitle: String

    public init(id: String, title: String, subtitle: String) {
        self.id = id
        self.title = title
        self.subtitle = subtitle
    }
}
