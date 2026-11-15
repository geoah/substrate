import SubstrateCore

/// What the chat list, Settings and the cards draw from at launch, as every
/// build since the first has kept it in `state.json`. The field names are
/// the file's: a build reads what an earlier one wrote.
struct CachedState: Codable, Equatable, Sendable {
    static let format = 1

    var format = CachedState.format
    var agents: [Record] = []
    var providers: [Record] = []
    /// The `llm/model` records, as last read; absent from a file an earlier
    /// build wrote, and where the session may not read them.
    var models: [Record]?
    /// Whose key each provider row runs on, as last read; absent from a file
    /// an earlier build wrote.
    var keys: ProviderKeys?
    var threads: [Record] = []
    var automated: [Record] = []
    var openings: [String: String] = [:]
    var interactions: [Record] = []
    var requests: [Record] = []
    /// Each chat's `finishedAt` when it was last seen settled: what is
    /// unread on a server that keeps no marks, and never sent anywhere.
    var seen: [String: String] = [:]
    /// The person's marks by thread, as the server last said them, so the
    /// list draws its dots before it asks anything; absent from a file an
    /// earlier build wrote, and while the server keeps none.
    var marks: [String: ThreadMark]?
    /// The marks this device moved that the server has not answered yet, by
    /// thread: sent at the next chance, a launch later included.
    var marking: [String: MarkChange]?
    /// What has been notified: `reply|<thread>|<finishedAt>`, `card|<kind>/<id>`.
    var notified: Set<String> = []
    /// Whether the first sign-in's history was marked seen.
    var baselined = false
    /// The default agent when Settings names none: the last one chatted with.
    var lastAgent: String?
    /// The owner's memories and instructions, as Settings last read them;
    /// absent from a file an earlier build wrote, and until the first read.
    var memories: [Record]?
    var instructions: [Record]?
    /// The pinned chats and the ones that wait on a person, as last read
    /// beside the list, so a launch draws them before it asks; absent from
    /// a file an earlier build wrote, and while the server keeps no flags.
    var pinned: [Record]?
    var waiting: [Record]?
    /// The `records` feature's revision as the server's document last said
    /// it: which lists a launch reads before the document is read again.
    var records: Int?
    /// The watch's cursor before the kit's record cache kept it: moved to
    /// the cache at the first open that finds it, and removed from the file
    /// then, so it is read once.
    var feedSeq: Int?
    var feedGeneration: String?
}
