import Foundation
import Observation

/// What became of a send that started no turn, kept until the owner sends it
/// again or dismisses it: a refusal reads differently from an agent that
/// could not answer, and a send of unknown delivery differently from both.
struct Outcome: Identifiable, Hashable, Sendable {
    enum Kind: Hashable, Sendable {
        /// The server's own refusal: nothing ran.
        case refused
        /// Reading the thread to find out whether it arrived.
        case checking
        /// It may or may not have arrived.
        case unknown
    }

    let id = UUID()
    var kind: Kind
    var reason: String
    var message: String
    /// The text went back into the composer.
    var restored = false
}

/// One chat's run state: the overlay a stream draws over the stored rows, and
/// every way a send, an attach and a handover can end. Ported from the
/// console's `Conversation` (web/console/src/components/agent/conversation.tsx),
/// with the exits DESIGN.md's tables list. It lives in the store, not in a
/// view, so leaving a chat mid-answer never drops its stream.
@MainActor
@Observable
final class Conversation: Identifiable {
    enum Phase: Equatable {
        case idle
        /// A send or an attach is streaming.
        case streaming
        /// The run settled; its rows are being read.
        case handingOver
        /// A turn runs that this server process does not stream.
        case background
    }

    /// The client's key, stable for the conversation's life, never the
    /// thread's: a new chat has none until its `thread` event.
    let id = UUID()
    private(set) var threadID: String?
    var agentID: String?
    var draft: String { didSet { if draft != oldValue { store?.setDraft(draft, for: self) } } }

    private(set) var overlay = LiveOverlay.empty
    private(set) var phase = Phase.idle
    private(set) var stopping = false
    /// "<Agent> couldn't answer: …"
    private(set) var error: String?
    private(set) var outcomes: [Outcome] = []
    /// The handover read failed or outlasted its wait: the overlay stays.
    private(set) var reloadFailed: String?
    private(set) var result: AgentResult?
    private(set) var deleted = false
    /// Bumped when a run settles, for the view's haptic and announcement.
    private(set) var settles = 0
    /// Set by a shortcut: the composer starts listening when it appears.
    var listenOnOpen = false

    @ObservationIgnored private weak var store: ChatStore?
    @ObservationIgnored private var run = 0
    @ObservationIgnored private var seq = 0
    @ObservationIgnored private var streamTask: Task<Void, Never>?
    @ObservationIgnored private var retryTask: Task<Void, Never>?
    @ObservationIgnored private var handoverTask: Task<Void, Never>?
    @ObservationIgnored private var pollTask: Task<Void, Never>?
    @ObservationIgnored private var checkTask: Task<Void, Never>?
    /// The last failure an attach saw, shown if the turn ended on it.
    @ObservationIgnored private var lastError: String?
    /// The overlay as the stream left it, published every 50 ms.
    @ObservationIgnored private var buffer = LiveOverlay.empty
    @ObservationIgnored private var flushTask: Task<Void, Never>?
    /// Called on every event and heartbeat while a stream is open: keeps a
    /// background task's progress moving.
    @ObservationIgnored var onActivity: (() -> Void)?

    init(store: ChatStore, thread: String?, agent: String?, draft: String) {
        self.store = store
        threadID = thread
        agentID = agent
        self.draft = draft
    }

    // MARK: What the view reads

    var active: Bool { phase != .idle }
    var streaming: Bool { phase == .streaming }

    var agent: Agent? { store?.agent(agentID) }
    var name: String { agentID.map(agentName) ?? "the assistant" }

    /// The stored turns, then the live ones. After a handover the overlay is
    /// empty, so the two never double.
    var turns: [Turn] {
        let stored = threadID.flatMap { store?.turns[$0] } ?? []
        return overlay.turns.isEmpty ? stored : stored + overlay.turns
    }

    /// The key of the assistant turn the stream is writing.
    var liveKey: String? {
        guard streaming else { return nil }
        return overlay.turns.last(where: { $0.role == .assistant })?.id
    }

    /// Why a send cannot go now; the text stays in the composer while it says so.
    var blockedReason: String? {
        if deleted { return "This chat was deleted" }
        if agentID == nil { return "Choose an assistant" }
        if outcomes.contains(where: { $0.kind == .checking }) { return "Checking whether your last message arrived" }
        switch phase {
        case .streaming, .handingOver: return "Waiting for the answer"
        case .background: return "\(name) is working on this chat"
        case .idle: break
        }
        if store?.online == false { return "Offline" }
        if let provider = store?.keyless(agent) { return "Add an API key for \(provider.name) first" }
        return nil
    }

    // MARK: Opening

    /// The view appeared: draw what is held, read what is not current, and
    /// attach when the thread runs.
    func open() {
        guard let store, let thread = threadID else { return }
        store.ensureLoaded(thread)
        if let t = store.thread(thread), t.running, !active {
            attach()
        } else if !active {
            store.load(thread)
        }
    }

    /// The thread's row moved (the feed, a list read).
    func threadMoved(_ thread: ChatThread) {
        switch phase {
        case .idle where thread.running:
            if store?.isVisible(thread.id) == true { attach() }
        case .idle:
            store?.load(thread.id)
        case .background where !thread.running:
            poll()
        default:
            break
        }
    }

    /// The app came back: a stream that died while it was away is opened again.
    func resume() {
        guard threadID != nil else { return }
        if phase == .streaming, streamTask == nil { attach() }
    }

    func close() {
        run += 1
        streamTask?.cancel()
        retryTask?.cancel()
        handoverTask?.cancel()
        pollTask?.cancel()
        checkTask?.cancel()
        flushTask?.cancel()
    }

    // MARK: Sending

    func send() {
        let message = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let store, !message.isEmpty, blockedReason == nil, let agent = agentID else { return }
        draft = ""
        if let thread = threadID { store.bumpReadEpoch(thread) }
        error = nil
        result = nil
        lastError = nil
        // An outcome stays until its message is sent again or dismissed.
        outcomes.removeAll { $0.message == message && $0.kind != .checking }
        // The last run's overlay is the stored rows' job now, unless their
        // read failed: then it is still the only view of that run.
        let kept = reloadFailed != nil ? overlay.turns : []
        let failedBefore = reloadFailed
        // Where the transcript stood, for a delivery check: unknown while its
        // rows are unread, or stale because their re-read failed.
        let baseline: Int? = threadID.flatMap { t in
            reloadFailed == nil ? store.rows[t].map(newestTurn) : nil
        }
        let sentAt = Date()
        reloadFailed = nil
        seq += 1
        set(LiveOverlay(turns: kept + [Turn(id: "live-u\(seq)", role: .user, content: message, pending: true)]), now: true)
        phase = .streaming
        stopping = false
        run += 1
        let run = run
        let sentTo = threadID

        streamTask = Task {
            var named = false
            var done = false
            // A run that never named its thread wrote nothing: the optimistic
            // turn goes, and the page is where it was.
            @MainActor func end(unknown: String? = nil) {
                set(LiveOverlay(turns: kept), now: true)
                reloadFailed = failedBefore
                phase = .idle
                stopping = false
                streamTask = nil
                if let unknown { reconcile(thread: sentTo, message: message, reason: unknown, baseline: baseline, sentAt: sentAt) }
            }
            do {
                for try await event in store.api.chat(agent: agent, thread: sentTo, message: message) {
                    guard run == self.run else { return }
                    onActivity?()
                    switch event.kind {
                    case .thread:
                        if let thread = event.thread {
                            named = true
                            if threadID == nil { bind(thread) }
                            confirmUserTurn()
                        }
                    case .done:
                        done = true
                        result = event.result
                    default:
                        apply(event)
                    }
                }
                guard run == self.run else { return }
                streamTask = nil
                if named, done { return settle(run) }
                if named { return reattach(run, attempt: 0) }
                end(unknown: "The connection closed before the server said anything.")
            } catch {
                guard run == self.run else { return }
                if store.handle(error) { return end() }
                streamTask = nil
                if named {
                    // The turn runs on; the attach that follows reads how it ended.
                    lastError = (error as? TurnFailed)?.text ?? explain(error, server: store.session.server)
                    return reattach(run, attempt: 0)
                }
                // Only the server's own 4xx says nothing ran. A 5xx, a 503 at
                // shutdown or a gateway's answer can come after the turn was
                // admitted, and so can a dropped connection.
                if let refusal = error as? ServerError, (400 ..< 500).contains(refusal.status) {
                    end()
                    let restored = draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                    if restored { draft = message }
                    outcomes.append(Outcome(kind: .refused, reason: refusal.text, message: message, restored: restored))
                    return
                }
                end(unknown: explain(error, server: store.session.server))
            }
        }
    }

    private func bind(_ thread: String) {
        threadID = thread
        store?.bind(self, to: thread)
    }

    private func confirmUserTurn() {
        if let i = buffer.turns.lastIndex(where: { $0.role == .user && $0.pending }) {
            buffer.turns[i].pending = false
            set(buffer, now: true)
        }
    }

    // MARK: The overlay

    private func apply(_ event: AgentEvent) {
        seq += 1
        guard buffer.apply(event, seq: seq) else { return }
        // Structure (a tool card, a compaction) shows at once; text waits for
        // the next flush, so a fast model does not redraw on every token.
        set(buffer, now: event.kind != .delta && event.kind != .reasoning)
    }

    private func set(_ next: LiveOverlay, now: Bool) {
        buffer = next
        if now {
            flushTask?.cancel()
            flushTask = nil
            overlay = next
            return
        }
        guard flushTask == nil else { return }
        flushTask = Task {
            try? await Task.sleep(for: .milliseconds(50))
            guard !Task.isCancelled else { return }
            flushTask = nil
            overlay = buffer
        }
    }

    // MARK: Attaching

    /// Attaches to the turn running on the thread: reads the rows first and
    /// freezes their ordinals, so replayed events those rows hold are
    /// dropped.
    func attach(attempt: Int = 0) {
        guard let store, let thread = threadID else { return }
        retryTask?.cancel()
        store.bumpReadEpoch(thread)
        error = nil
        result = nil
        stopping = false
        if phase != .streaming { phase = .streaming }
        run += 1
        let run = run
        streamTask?.cancel()
        streamTask = Task {
            let held: Set<Int>
            do {
                let now = try await store.readNow(thread)
                guard run == self.run else { return }
                guard let record = now.thread else { return gone(run) }
                store.upsert(record)
                store.publish(thread, records: now.rows, version: record.version, full: now.rows.count >= 500)
                held = persistedTurns(now.rows)
                set(.empty, now: true)
            } catch {
                guard run == self.run else { return }
                if store.handle(error) { return }
                return reattach(run, attempt: attempt + 1)
            }
            var idle: String?
            var done = false
            do {
                for try await event in store.api.events(thread: thread) {
                    guard run == self.run else { return }
                    onActivity?()
                    switch event.kind {
                    case .idle:
                        idle = event.status ?? "unknown"
                        if idle != "running", let lastError { error = lastError }
                        continue
                    case .done:
                        done = true
                        lastError = nil
                        result = event.result
                        continue
                    case .thread:
                        continue
                    default:
                        if isReplayed(event, held: held) { continue }
                        apply(event)
                    }
                }
                guard run == self.run else { return }
                streamTask = nil
                if done { return settle(run) }
                guard let idle else { return reattach(run, attempt: attempt + 1) }
                if idle == "running" { return background(run) }
                settle(run)
            } catch {
                guard run == self.run else { return }
                streamTask = nil
                if store.handle(error) { return }
                if let refusal = error as? ServerError, refusal.isPermanent {
                    if refusal.status == 404 { return gone(run) }
                    self.error = refusal.text
                    return settle(run)
                }
                lastError = (error as? TurnFailed)?.text ?? explain(error, server: store.session.server)
                reattach(run, attempt: attempt + 1)
            }
        }
    }

    /// A stream that ended without `done` is not the turn's end: attach again,
    /// backing off, until the server says it settled.
    private func reattach(_ run: Int, attempt: Int) {
        guard run == self.run else { return }
        retryTask?.cancel()
        let delay = attempt == 0 ? 0 : min(pow(2, Double(attempt - 1)), 15)
        phase = .streaming
        retryTask = Task {
            if delay > 0 { try? await Task.sleep(for: .seconds(delay)) }
            guard !Task.isCancelled, run == self.run else { return }
            attach(attempt: attempt)
        }
    }

    private func gone(_ run: Int) {
        guard run == self.run else { return }
        set(.empty, now: true)
        phase = .idle
        deleted = true
    }

    /// A turn this server does not stream holds the thread: say so, and read
    /// the thread until it settles.
    private func background(_ run: Int) {
        guard run == self.run else { return }
        set(.empty, now: true)
        phase = .background
        stopping = false
        pollTask?.cancel()
        pollTask = Task {
            while !Task.isCancelled, run == self.run, phase == .background {
                try? await Task.sleep(for: .seconds(5))
                guard !Task.isCancelled else { return }
                poll()
            }
        }
    }

    /// One read of a background-running thread: its rows as they grow, and
    /// the handover once it settles.
    func poll() {
        guard phase == .background, let store, let thread = threadID else { return }
        let run = run
        Task {
            guard let now = try? await store.readNow(thread), run == self.run, phase == .background else { return }
            guard let record = now.thread else { return gone(run) }
            store.upsert(record)
            store.publish(thread, records: now.rows, version: record.version, full: now.rows.count >= 500)
            if record["status"]?.string != "running" {
                pollTask?.cancel()
                phase = .idle
                settles += 1
            }
        }
    }

    // MARK: Settling

    /// The run ended: its rows are read and replace the overlay in one
    /// update. A read that fails or outlasts 15 s keeps the overlay and
    /// releases the composer.
    private func settle(_ run: Int) {
        guard run == self.run, let store else { return }
        guard let thread = threadID else {
            set(.empty, now: true)
            phase = .idle
            return
        }
        store.bumpReadEpoch(thread)
        phase = .handingOver
        stopping = false
        flushTask?.cancel()
        flushTask = nil
        overlay = buffer
        handoverTask?.cancel()
        handoverTask = Task {
            let read = Task { try await store.readNow(thread) }
            let timeout = Task {
                try? await Task.sleep(for: .seconds(15))
                read.cancel()
            }
            defer { timeout.cancel() }
            do {
                let now = try await read.value
                guard run == self.run else { return }
                guard let record = now.thread else { return gone(run) }
                store.publish(thread, records: now.rows, version: record.version, full: now.rows.count >= 500)
                set(.empty, now: true)
                reloadFailed = nil
                phase = .idle
                settles += 1
                store.upsert(record)
            } catch {
                guard run == self.run else { return }
                if store.handle(error) { return }
                reloadFailed = read.isCancelled
                    ? "The conversation didn't reload: the read has not come back after 15 seconds."
                    : "The conversation didn't reload: \(explain(error, server: store.session.server))"
                phase = .idle
            }
        }
    }

    /// Try Again after a reload failure: a read that lands completes the
    /// handover.
    func retryReload() {
        guard reloadFailed != nil else { return }
        reloadFailed = nil
        run += 1
        settle(run)
    }

    // MARK: Stopping

    func stop() {
        guard let store, let thread = threadID, phase == .streaming, !stopping else { return }
        stopping = true
        Task {
            do {
                _ = try await store.api.stop(thread: thread)
                // The stream's own `done` settles the overlay.
            } catch {
                stopping = false
                if store.handle(error) { return }
                // Nothing streamable runs: the turn settled first, or another
                // process owns it. The thread says which.
                if let refusal = error as? ServerError, refusal.status == 409 { return }
                self.error = explain(error, server: store.session.server)
            }
        }
    }

    // MARK: Delivery checks

    /// A send whose delivery is unknown: after a second, read whether its
    /// message arrived. Same text past the turn held before the send is
    /// evidence, not proof. Nothing is ever sent again on its own.
    private func reconcile(thread: String?, message: String, reason: String, baseline: Int?, sentAt: Date) {
        guard let store else { return }
        var outcome = Outcome(kind: .checking, reason: reason, message: message)
        outcomes.append(outcome)
        let id = outcome.id
        let run = run
        checkTask = Task {
            try? await Task.sleep(for: .seconds(1))
            var found: String?
            var running = false
            if let thread, let baseline {
                if let now = try? await store.readNow(thread),
                   now.rows.contains(where: { $0["role"]?.string == "user" && ($0["turn"]?.int ?? -1) > baseline
                       && $0["content"]?.string == message })
                {
                    found = thread
                    running = now.thread?["status"]?.string == "running"
                    if let record = now.thread {
                        store.publish(thread, records: now.rows, version: record.version, full: now.rows.count >= 500)
                        store.upsert(record)
                    }
                }
            } else if thread == nil, let agent = agentID {
                // A new chat: the agent's newest chats, one started after the
                // send whose first message is this one.
                found = await adopt(agent: agent, message: message, after: sentAt.addingTimeInterval(-5))
                if let found, let record = try? await store.api.get(Kinds.thread, found) {
                    running = record["status"]?.string == "running"
                    store.upsert(record)
                }
            }
            outcomes.removeAll { $0.id == id }
            guard let found else {
                outcome.kind = .unknown
                outcomes.append(outcome)
                return
            }
            guard run == self.run else { return }
            if threadID == nil { bind(found) }
            if running { attach() } else { store.load(found, force: true) }
        }
    }

    private func adopt(agent: String, message: String, after: Date) async -> String? {
        guard let store,
              let page = try? await store.api.list(ListQuery(
                  kinds: [Kinds.thread],
                  properties: ["mode": ["eq": "chat"], "agent": ["eq": .string("\(Kinds.agent)/\(agent)")]],
                  first: 5, orderBy: "startedAt:desc"))
        else { return nil }
        for thread in page.records where ChatThread(record: thread).startedAt >= after {
            guard let first = try? await store.api.list(ListQuery(
                kinds: [Kinds.message], properties: ["thread": ["eq": .string(thread.id)], "role": ["eq": "user"]],
                first: 1, orderBy: "turn:asc,createdAt:asc")).records.first
            else { continue }
            if first["content"]?.string == message { return thread.id }
        }
        return nil
    }

    func dismiss(_ outcome: Outcome) {
        outcomes.removeAll { $0.id == outcome.id }
    }
}
