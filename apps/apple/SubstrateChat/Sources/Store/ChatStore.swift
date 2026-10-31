import Foundation
import Network
import Observation

/// Everything one signed-in repository's chats hold, for the whole process:
/// every window reads this one store. Its concurrency rules are the
/// design's (DESIGN.md, "The store and its concurrency rules"):
///
/// 1. A conversation is keyed by a client UUID; the `thread` event binds it,
///    and there is one conversation per thread in the process.
/// 2. One stream per conversation.
/// 3. No rows publish for a thread under a live overlay but its own
///    handover's.
/// 4. Every transcript read carries its thread's read epoch and is dropped
///    when the epoch moved.
/// 5. A run's callbacks carry its run number (in `Conversation`).
/// 6. Nothing of a closed session publishes.
/// 7. Cached rows are current only while their thread is settled and its
///    version is the one they were read under.
@MainActor
@Observable
final class ChatStore {
    let session: Session
    let api: API
    let prefs: Preferences
    @ObservationIgnored let cache: DiskCache
    @ObservationIgnored private(set) var closed = false
    /// Called when the server refuses the token.
    @ObservationIgnored var onSessionExpired: (() -> Void)?

    // MARK: Data

    private(set) var agents: [Agent] = []
    private(set) var providers: [String: Provider] = [:]
    private(set) var chats: [Record] = []
    private(set) var automated: [Record] = []
    private(set) var openings: [String: String] = [:]
    private(set) var interactions: [Record] = []
    private(set) var requests: [Record] = []
    private(set) var seen: [String: String] = [:]
    private(set) var lastAgent: String?
    @ObservationIgnored private var notified: Set<String> = []
    @ObservationIgnored private var baselined = false
    @ObservationIgnored private var feedSeq: Int?
    @ObservationIgnored private var feedGeneration: String?

    /// Each thread's rows as last published, in loop order, and their fold.
    private(set) var rows: [String: [Record]] = [:]
    private(set) var turns: [String: [Turn]] = [:]
    @ObservationIgnored private var meta: [String: (version: Int, full: Bool)] = [:]
    @ObservationIgnored private var readEpochs: [String: Int] = [:]
    @ObservationIgnored private var reads: [String: Task<Void, Never>] = [:]

    // MARK: State

    private(set) var conversations: [UUID: Conversation] = [:]
    @ObservationIgnored private var byThread: [String: UUID] = [:]
    @ObservationIgnored private(set) var drafts: [String: String] = [:]
    /// The store has something to draw: the cache, or a first read.
    private(set) var loaded = false
    private(set) var refreshing = false
    private(set) var refreshError: String?
    private(set) var online = true
    private(set) var feedProblem: String?
    /// Which thread each active scene shows.
    @ObservationIgnored private var visible: [UUID: String] = [:]
    @ObservationIgnored private var monitor: NWPathMonitor?
    @ObservationIgnored private var feedTask: Task<Void, Never>?
    @ObservationIgnored private var feedBatch: Set<String> = []
    @ObservationIgnored private var feedFlush: Task<Void, Never>?
    @ObservationIgnored private var saveTask: Task<Void, Never>?
    @ObservationIgnored private var draftTask: Task<Void, Never>?
    @ObservationIgnored private var prefetchTask: Task<Void, Never>?
    @ObservationIgnored private var refreshTask: Task<Void, Never>?

    init(session: Session) {
        self.session = session
        api = API(session)
        prefs = Preferences(identity: session.identity)
        cache = DiskCache(identity: session.identity)
        if let state = cache.loadState() {
            agents = state.agents.map(Agent.init(record:))
            providers = Dictionary(state.providers.map { ($0.id, Provider(record: $0)) }) { a, _ in a }
            chats = state.threads
            automated = state.automated
            openings = state.openings
            interactions = state.interactions
            requests = state.requests
            seen = state.seen
            notified = state.notified
            baselined = state.baselined
            lastAgent = state.lastAgent
            feedSeq = state.feedSeq
            feedGeneration = state.feedGeneration
            loaded = true
        }
        drafts = cache.loadDrafts()
    }

    // MARK: Derived

    var offered: [Agent] {
        let defaults = defaultOffered(agents)
        return agents.filter { prefs.offers($0, defaults: defaults) }.sorted { $0.name < $1.name }
    }

    func agent(_ id: String?) -> Agent? {
        guard let id else { return nil }
        return agents.first { $0.id == id }
    }

    /// The agent a new chat goes to: Settings' default, else the last one
    /// chatted with, else the first offered.
    var defaultAgent: Agent? {
        let offered = offered
        for id in [prefs.defaultAgent, lastAgent] {
            if let id, let agent = offered.first(where: { $0.id == id }) { return agent }
        }
        return offered.first
    }

    /// The chat list: chats, and automated runs when shown, newest activity
    /// first.
    var threads: [ChatThread] {
        let records = prefs.showAutomated ? chats + automated : chats
        return records.map(ChatThread.init(record:)).sorted { $0.activeAt > $1.activeAt }
    }

    func thread(_ id: String?) -> ChatThread? {
        guard let id else { return nil }
        if let r = chats.first(where: { $0.id == id }) ?? automated.first(where: { $0.id == id }) {
            return ChatThread(record: r)
        }
        return nil
    }

    func title(_ thread: ChatThread) -> String { thread.title(opening: openings[thread.id]) }

    /// The provider an agent completes against, when it has no key.
    func keyless(_ agent: Agent?) -> Provider? {
        guard let id = agent?.providerID, let provider = providers[id], !provider.hasKey else { return nil }
        return provider
    }

    func unread(_ thread: ChatThread) -> Bool {
        guard !thread.running, let finished = thread.record["finishedAt"]?.string else { return false }
        return seen[thread.id] != finished
    }

    /// The cards waiting on the owner in a thread: questions and suggestions.
    func attention(_ threadID: String) -> Int {
        interactions.count { $0["thread"]?.referenceID == threadID }
            + requests.count { $0["thread"]?.referenceID == threadID }
    }

    var unreadCount: Int { threads.count { unread($0) } }

    // MARK: Lifecycle

    func start() {
        guard monitor == nil else { return }
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { [weak self] path in
            let up = path.status == .satisfied
            Task { @MainActor in self?.setOnline(up) }
        }
        monitor.start(queue: .global(qos: .utility))
        self.monitor = monitor
        refresh()
        Task.detached(priority: .background) { [cache] in await cache.prune() }
    }

    private func setOnline(_ up: Bool) {
        guard up != online else { return }
        online = up
        if up {
            refresh()
            startFeed()
        }
    }

    /// Ends the session: every stream, read and write stops, and with
    /// `forget` the saved chats and choices are deleted.
    func close(forget: Bool) {
        closed = true
        monitor?.cancel()
        feedTask?.cancel()
        feedFlush?.cancel()
        prefetchTask?.cancel()
        refreshTask?.cancel()
        saveTask?.cancel()
        draftTask?.cancel()
        for task in reads.values { task.cancel() }
        for conversation in conversations.values { conversation.close() }
        conversations = [:]
        let cache = cache
        Task { await cache.close(deleting: forget) }
        if forget { prefs.clear() }
    }

    /// A refusal of the token, from any request.
    func handle(_ error: Error) -> Bool {
        guard error is SessionExpired else { return false }
        if !closed { onSessionExpired?() }
        return true
    }

    /// A scene became active or went away; streams that died while the app
    /// was away are opened again.
    func setActive(_ active: Bool) {
        guard !closed else { return }
        if active {
            startFeed()
            refresh()
            for conversation in conversations.values { conversation.resume() }
        } else {
            #if os(iOS)
            stopFeed()
            #endif
        }
    }

    // MARK: Visibility

    func setVisible(scene: UUID, thread: String?) {
        visible[scene] = thread
        if let thread { markSeen(thread) }
    }

    func isVisible(_ thread: String) -> Bool { visible.values.contains(thread) }

    func markSeen(_ id: String) {
        guard let thread = thread(id), !thread.running, let finished = thread.record["finishedAt"]?.string,
              seen[id] != finished
        else { return }
        seen[id] = finished
        scheduleSave()
    }

    func markUnread(_ id: String) {
        seen[id] = nil
        scheduleSave()
    }

    // MARK: Conversations

    func conversation(thread id: String) -> Conversation {
        if let key = byThread[id], let existing = conversations[key] { return existing }
        let agent = thread(id)?.agentID
        let conversation = Conversation(store: self, thread: id, agent: agent, draft: drafts[id] ?? "")
        conversations[conversation.id] = conversation
        byThread[id] = conversation.id
        return conversation
    }

    func newConversation(agent: String? = nil) -> Conversation {
        let conversation = Conversation(store: self, thread: nil, agent: agent ?? defaultAgent?.id,
                                        draft: drafts["new"] ?? "")
        conversations[conversation.id] = conversation
        return conversation
    }

    /// The `thread` event named a new chat's thread: from now the store finds
    /// the conversation by it, and its draft moves with it.
    func bind(_ conversation: Conversation, to thread: String) {
        if let other = byThread[thread], other != conversation.id {
            conversations[other]?.close()
            conversations[other] = nil
        }
        byThread[thread] = conversation.id
        if let agent = conversation.agentID {
            lastAgent = agent
        }
        bumpReadEpoch(thread)
        if drafts["new"] != nil {
            drafts["new"] = nil
            scheduleDraftSave()
        }
        scheduleSave()
    }

    /// Drops conversations nobody shows and nothing runs on.
    func forget(_ conversation: Conversation) {
        guard !conversation.active, conversation.outcomes.isEmpty else { return }
        if let thread = conversation.threadID, byThread[thread] == conversation.id { byThread[thread] = nil }
        conversations[conversation.id] = nil
    }

    func setDraft(_ text: String, for conversation: Conversation) {
        let key = conversation.threadID ?? "new"
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty { drafts[key] = nil } else { drafts[key] = text }
        scheduleDraftSave()
    }

    // MARK: Transcripts

    /// The rows a thread's view draws, loaded from disk the first time.
    func ensureLoaded(_ thread: String) {
        guard rows[thread] == nil, let cached = cache.loadTranscript(thread) else { return }
        rows[thread] = cached.records
        turns[thread] = transcript(of: cached.records)
        meta[thread] = (cached.threadVersion, cached.full)
    }

    func hasRows(_ thread: String) -> Bool { rows[thread] != nil }

    /// Whether the cached rows are current (rule 7).
    func isCurrent(_ thread: String) -> Bool {
        guard let t = self.thread(thread), let m = meta[thread] else { return false }
        return !t.running && m.version == t.record.version && !m.full
    }

    func bumpReadEpoch(_ thread: String) {
        readEpochs[thread, default: 0] += 1
        reads[thread]?.cancel()
        reads[thread] = nil
    }

    /// Reads a thread's rows unless they are current or a run owns the
    /// thread (rules 3, 4, 7). `first` below 500 is a prefetch.
    func load(_ thread: String, force: Bool = false, first: Int = 500) {
        guard !closed, reads[thread] == nil else { return }
        if let c = conversation(existing: thread), c.active { return }
        if !force, isCurrent(thread) { return }
        let epoch = readEpochs[thread, default: 0]
        let version = self.thread(thread)?.record.version ?? 0
        reads[thread] = Task {
            defer { if readEpochs[thread, default: 0] == epoch { reads[thread] = nil } }
            do {
                let page = try await api.list(Self.transcriptQuery(thread, first: first))
                guard !closed, readEpochs[thread, default: 0] == epoch,
                      conversation(existing: thread)?.active != true
                else { return }
                publish(thread, records: page.records.reversed(), version: version, full: page.records.count >= first)
            } catch {
                _ = handle(error)
            }
        }
    }

    func conversation(existing thread: String) -> Conversation? {
        byThread[thread].flatMap { conversations[$0] }
    }

    static func transcriptQuery(_ thread: String, first: Int = 500) -> ListQuery {
        ListQuery(kinds: [Kinds.message], properties: ["thread": ["eq": .string(thread)]],
                  first: first, orderBy: "turn:desc,createdAt:desc")
    }

    /// A thread and its newest rows as they stand now, read straight from
    /// the server: the owner's read (handover, attach, a delivery check).
    /// The thread first: a turn claims its thread before it writes its user
    /// row, so a thread that was not running holds every row it admitted.
    func readNow(_ thread: String) async throws -> (thread: Record?, rows: [Record]) {
        let record = try await api.get(Kinds.thread, thread)
        let page = try await api.list(Self.transcriptQuery(thread))
        return (record, page.records.reversed())
    }

    /// Puts rows on screen and on disk.
    func publish(_ thread: String, records: [Record], version: Int, full: Bool) {
        guard !closed else { return }
        rows[thread] = records
        turns[thread] = transcript(of: records)
        meta[thread] = (version, full)
        let cached = CachedTranscript(threadVersion: version, full: full, records: records, openedAt: Date())
        let cache = cache
        Task.detached(priority: .utility) { await cache.save(cached, thread: thread) }
    }

    // MARK: Refresh

    func refresh() {
        guard !closed, refreshTask == nil else { return }
        refreshTask = Task {
            defer { refreshTask = nil }
            await refreshAll()
        }
    }

    func refreshAll() async {
        guard !closed else { return }
        refreshing = true
        defer { refreshing = false }
        do {
            async let agentPage = api.listAll(ListQuery(kinds: [Kinds.agent], first: 200, orderBy: "createdAt:desc"))
            async let providerPage = api.listAll(ListQuery(kinds: [Kinds.provider], first: 100))
            async let threadPages = readThreads()
            async let cards: Void = refreshCards()
            let (a, p, t) = try await (agentPage, providerPage, threadPages)
            try await cards
            guard !closed else { return }
            agents = a.records.map(Agent.init(record:))
            providers = Dictionary(p.records.map { ($0.id, Provider(record: $0)) }) { x, _ in x }
            // The feed resumes from the earliest read's head, so nothing
            // written between the reads is missed.
            if feedSeq == nil || feedGeneration != a.generation {
                let heads = [a, p].filter { $0.generation == a.generation }.map(\.head) + [t.head]
                feedSeq = heads.min()
                feedGeneration = a.generation
            }
            apply(chats: t.chats, automated: t.automated)
            // Everything the first read found is history: from here on, what
            // settles or asks is news.
            baselined = true
            refreshError = nil
            loaded = true
            scheduleSave()
            startFeed()
            await readOpenings()
            prefetch()
        } catch {
            if handle(error) { return }
            refreshError = explain(error, server: session.server)
        }
    }

    private struct ThreadPages {
        var chats: [Record]
        var automated: [Record]
        var head: Int
    }

    private func readThreads() async throws -> ThreadPages {
        let chatPage = try await api.list(ListQuery(
            kinds: [Kinds.thread], properties: ["mode": ["eq": "chat"]], first: 100, orderBy: "updatedAt:desc"))
        var automated: [Record] = []
        if prefs.showAutomated {
            let modes = conversationModes.filter { $0 != "chat" }.map(JSONValue.string)
            automated = try await api.list(ListQuery(
                kinds: [Kinds.thread], properties: ["mode": ["in": .array(modes)]], first: 50,
                orderBy: "updatedAt:desc")).records
        }
        return ThreadPages(chats: chatPage.records, automated: automated, head: chatPage.head)
    }

    func refreshThreads() async {
        guard !closed else { return }
        do {
            let pages = try await readThreads()
            guard !closed else { return }
            apply(chats: pages.chats, automated: pages.automated)
            scheduleSave()
            await readOpenings()
            prefetch()
        } catch {
            _ = handle(error)
        }
    }

    /// A new list: what settled since notifies, open chats learn their
    /// thread moved, and on the first sign-in all of it is history.
    private func apply(chats new: [Record], automated: [Record]) {
        let before = Dictionary((chats + self.automated).map { ($0.id, $0) }) { a, _ in a }
        chats = new
        self.automated = automated
        if !baselined {
            for r in new + automated {
                if let finished = r["finishedAt"]?.string {
                    seen[r.id] = finished
                    notified.insert("reply|\(r.id)|\(finished)")
                }
            }
        }
        for r in new + automated {
            if before[r.id]?.version != r.version { threadMoved(r) }
        }
    }

    /// One thread row, read on its own (a handover, a background poll).
    func upsert(_ record: Record) {
        guard !closed else { return }
        let mode = record["mode"]?.string
        if mode == "chat" {
            if let i = chats.firstIndex(where: { $0.id == record.id }) { chats[i] = record } else { chats.insert(record, at: 0) }
        } else if conversationModes.contains(mode ?? ""), prefs.showAutomated {
            if let i = automated.firstIndex(where: { $0.id == record.id }) { automated[i] = record } else { automated.insert(record, at: 0) }
        }
        threadMoved(record)
        scheduleSave()
    }

    private func threadMoved(_ record: Record) {
        let thread = ChatThread(record: record)
        if !thread.running, let finished = record["finishedAt"]?.string {
            if isVisible(thread.id) { markSeen(thread.id) }
            let key = "reply|\(thread.id)|\(finished)"
            if !notified.contains(key) {
                notified.insert(key)
                if baselined, prefs.notifyReplies, !thread.automated, !isVisible(thread.id), record["lastReply"]?.text != nil {
                    Notifier.reply(thread: thread, agent: agentName(thread.agentID ?? ""), title: title(thread))
                }
            }
        }
        if let conversation = conversation(existing: thread.id) { conversation.threadMoved(thread) }
        Notifier.badge(unreadCount)
    }

    /// The opening line of every listed thread without a subject: one read.
    private func readOpenings() async {
        let missing = threads.filter { $0.subject == nil && openings[$0.id] == nil }.map(\.id)
        guard !missing.isEmpty else { return }
        do {
            let page = try await api.list(ListQuery(
                kinds: [Kinds.message],
                properties: ["role": ["eq": "user"], "thread": ["in": .array(missing.map(JSONValue.string))]],
                first: 500, orderBy: "turn:asc,createdAt:asc"))
            guard !closed else { return }
            openings.merge(openingMessages(page.records)) { _, new in new }
            scheduleSave()
        } catch {
            _ = handle(error)
        }
    }

    /// The questions and suggestions waiting on the owner. A new one
    /// notifies once.
    func refreshCards() async throws {
        async let i = api.list(ListQuery(kinds: [Kinds.interaction], properties: ["state": ["eq": "pending"]], first: 100))
        async let r = api.list(ListQuery(kinds: [Kinds.request], properties: ["decision": ["eq": "proposed"]], first: 100))
        let (ip, rp) = try await (i, r)
        guard !closed else { return }
        interactions = ip.records
        requests = rp.records
        for record in ip.records + rp.records {
            let key = "card|\(record.kind)/\(record.id)"
            guard !notified.contains(key) else { continue }
            notified.insert(key)
            let threadID = record["thread"]?.referenceID
            guard baselined, prefs.notifyAttention, !(threadID.map(isVisible) ?? false) else { continue }
            let thread = self.thread(threadID)
            let who = agentName(thread?.agentID ?? record["proposer"]?.string ?? "Your agent")
            Notifier.attention(record: record, thread: threadID, agent: who,
                               title: thread.map(title) ?? "A chat")
        }
    }

    // MARK: Prefetch

    /// The most recent settled chats whose rows are not current, read one at
    /// a time in the background, so opening any of them is instant.
    private func prefetch() {
        prefetchTask?.cancel()
        prefetchTask = Task {
            try? await Task.sleep(for: .seconds(1))
            var budget = 8
            for thread in threads where budget > 0 && !thread.running {
                guard !Task.isCancelled, !closed, online else { return }
                if conversations.values.contains(where: \.streaming) { return }
                ensureLoaded(thread.id)
                if isCurrent(thread.id) || conversation(existing: thread.id)?.active == true { continue }
                budget -= 1
                load(thread.id, first: 100)
                while reads[thread.id] != nil, !Task.isCancelled { try? await Task.sleep(for: .milliseconds(100)) }
            }
        }
    }

    // MARK: The feed

    private static let feedKinds = [Kinds.thread, Kinds.message, Kinds.agent, Kinds.provider, Kinds.interaction, Kinds.request]

    private func startFeed() {
        guard !closed, feedTask == nil, online, feedGeneration != nil else { return }
        feedTask = Task { await runFeed() }
    }

    private func stopFeed() {
        feedTask?.cancel()
        feedTask = nil
    }

    private func runFeed() async {
        defer { feedTask = nil }
        while !Task.isCancelled, !closed {
            do {
                for try await line in api.watch(kinds: Self.feedKinds, from: feedSeq, generation: feedGeneration) {
                    switch line {
                    case let .bookmark(seq, generation):
                        if feedSeq == nil { feedSeq = seq }
                        if !generation.isEmpty { feedGeneration = generation }
                        feedProblem = nil
                    case let .row(row):
                        feedSeq = row.seq
                        feed(row)
                    case let .failed(code, message):
                        if code == "compacted" { throw FeedCompacted(text: message) }
                        feedProblem = message
                        return
                    }
                }
            } catch is FeedCompacted {
                // The cursor addresses no history this server holds: read
                // everything again and follow from the new head.
                feedSeq = nil
                feedGeneration = nil
                feedTask = nil
                refresh()
                return
            } catch {
                if Task.isCancelled || handle(error) { return }
            }
            try? await Task.sleep(for: .seconds(3))
        }
    }

    /// A batch of rows re-reads what they touched, once, 300 ms after the
    /// last of them.
    private func feed(_ row: ChangeRow) {
        feedBatch.insert(row.kind == Kinds.thread ? "thread:\(row.recordID)" : row.kind)
        feedFlush?.cancel()
        feedFlush = Task {
            try? await Task.sleep(for: .milliseconds(300))
            guard !Task.isCancelled else { return }
            let batch = feedBatch
            feedBatch = []
            if batch.contains(where: { $0.hasPrefix("thread:") }) { await refreshThreads() }
            if batch.contains(Kinds.message) {
                for c in conversations.values where c.phase == .background { c.poll() }
            }
            if batch.contains(Kinds.agent) || batch.contains(Kinds.provider) { await refreshAgents() }
            if batch.contains(Kinds.interaction) || batch.contains(Kinds.request) { try? await refreshCards() }
            scheduleSave()
        }
    }

    func refreshAgents() async {
        do {
            async let a = api.listAll(ListQuery(kinds: [Kinds.agent], first: 200, orderBy: "createdAt:desc"))
            async let p = api.listAll(ListQuery(kinds: [Kinds.provider], first: 100))
            let (ap, pp) = try await (a, p)
            guard !closed else { return }
            agents = ap.records.map(Agent.init(record:))
            providers = Dictionary(pp.records.map { ($0.id, Provider(record: $0)) }) { x, _ in x }
        } catch {
            _ = handle(error)
        }
    }

    // MARK: Writes a chat makes

    /// A rename is optimistic: the list shows it at once, and goes back if
    /// the server refuses.
    func rename(_ id: String, to subject: String) async throws {
        let text = subject.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, let i = chats.firstIndex(where: { $0.id == id }) else { return }
        let before = chats[i]
        chats[i].properties["subject"] = .string(text)
        do {
            let record = try await api.patch(Kinds.thread, id, properties: ["subject": text])
            upsert(record)
        } catch {
            if let j = chats.firstIndex(where: { $0.id == id }) { chats[j] = before }
            _ = handle(error)
            throw error
        }
    }

    func setKey(_ key: String, provider: String) async throws {
        let record = try await api.patch(Kinds.provider, provider, properties: ["apiKey": key])
        providers[record.id] = Provider(record: record)
    }

    // MARK: Saving

    func scheduleSave() {
        guard !closed else { return }
        saveTask?.cancel()
        saveTask = Task {
            try? await Task.sleep(for: .milliseconds(400))
            guard !Task.isCancelled, !closed else { return }
            let state = CachedState(
                agents: agents.map(\.record), providers: providers.values.map(\.record),
                threads: chats, automated: automated, openings: openings,
                interactions: interactions, requests: requests, seen: seen, notified: notified,
                baselined: baselined, lastAgent: lastAgent, feedSeq: feedSeq, feedGeneration: feedGeneration)
            let cache = cache
            await Task.detached(priority: .utility) { await cache.save(state) }.value
        }
    }

    private func scheduleDraftSave() {
        draftTask?.cancel()
        draftTask = Task {
            try? await Task.sleep(for: .milliseconds(600))
            guard !Task.isCancelled, !closed else { return }
            let drafts = drafts
            let cache = cache
            await cache.save(drafts: drafts)
        }
    }

    /// For Settings → Storage.
    func cacheSize() -> Int { cache.size() }

    func clearSavedChats() async {
        for thread in Array(rows.keys) where conversation(existing: thread)?.active != true {
            rows[thread] = nil
            turns[thread] = nil
            meta[thread] = nil
        }
        await cache.clearTranscripts()
    }

    // MARK: Background (iOS)

    /// What a background refresh does: the list and the cards, which notify
    /// what settled or appeared since.
    func backgroundRefresh() async {
        await refreshThreads()
        try? await refreshCards()
        scheduleSave()
        try? await Task.sleep(for: .milliseconds(500))
    }
}
