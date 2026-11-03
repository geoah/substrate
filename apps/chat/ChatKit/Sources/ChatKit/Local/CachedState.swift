import SubstrateCore

/// What the chat list, Settings and the cards draw from at launch, as every
/// build since the first has kept it in `state.json`. The field names are
/// the file's: a build reads what an earlier one wrote.
struct CachedState: Codable, Equatable, Sendable {
    static let format = 1

    var format = CachedState.format
    var agents: [Record] = []
    var providers: [Record] = []
    var threads: [Record] = []
    var automated: [Record] = []
    var openings: [String: String] = [:]
    var interactions: [Record] = []
    var requests: [Record] = []
    /// Each chat's `finishedAt` when it was last seen settled.
    var seen: [String: String] = [:]
    /// What has been notified: `reply|<thread>|<finishedAt>`, `card|<kind>/<id>`.
    var notified: Set<String> = []
    /// Whether the first sign-in's history was marked seen.
    var baselined = false
    /// The default agent when Settings names none: the last one chatted with.
    var lastAgent: String?
    /// The watch's cursor before the kit's record cache kept it: read once,
    /// to resume from, and written no more.
    var feedSeq: Int?
    var feedGeneration: String?
}
