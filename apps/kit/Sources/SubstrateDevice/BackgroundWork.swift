import Foundation

/// Work the system runs while the app is off screen (§5.4): named jobs it
/// runs when it allows, in a process that may have started for them alone,
/// each with a budget; and continued processing, which keeps work under
/// way running after the app leaves the screen, drawn by the system with a
/// Stop. A job reads the session when it runs, never from what scheduled
/// it; what it carries (the repository and the link generation it was
/// scheduled under) is checked against that session before it writes.
public protocol BackgroundWork: Sendable {
    /// Asks the system to run the job once it allows, no earlier than the
    /// job says.
    func schedule(_ job: BackgroundJob) async
    /// Each run the system grants from now on, the ones granted before the
    /// app asked first. Each is answered once with `finish(_:success:)`.
    func runs() -> AsyncStream<BackgroundRun>
    /// Each run the system takes back before the app answered it, by id,
    /// from now on: its work stops, and what it would have written is
    /// dropped. The system has ended the run itself, so answering it after
    /// is a no-op.
    func expirations() -> AsyncStream<String>
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

public extension BackgroundWork {
    /// A system that never takes a run back before its budget.
    func expirations() -> AsyncStream<String> { AsyncStream { $0.finish() } }
}

/// A job by name, and what it needs.
public struct BackgroundJob: Codable, Hashable, Sendable {
    /// The identifier the app declares to the system.
    public var name: String
    /// Not before this instant.
    public var earliest: Date?
    public var needsNetwork: Bool
    /// What the run needs to know of when it was asked for, handed back on
    /// the run it becomes: never a secret, since the system keeps it. The
    /// latest scheduled for a name replaces the one before.
    public var payload: [String: String]

    public init(name: String, earliest: Date? = nil, needsNetwork: Bool = true, payload: [String: String] = [:]) {
        self.name = name
        self.earliest = earliest
        self.needsNetwork = needsNetwork
        self.payload = payload
    }
}

/// One run the system granted.
public struct BackgroundRun: Codable, Hashable, Sendable {
    public var id: String
    public var job: String
    /// How long the run may take, where the system says; it may end it
    /// sooner.
    public var budget: TimeInterval?
    /// The payload of the job it runs, as last scheduled; empty for a job
    /// a build before payloads scheduled.
    public var payload: [String: String]

    public init(id: String, job: String, budget: TimeInterval? = nil, payload: [String: String] = [:]) {
        self.id = id
        self.job = job
        self.budget = budget
        self.payload = payload
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
