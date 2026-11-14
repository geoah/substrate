import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime

/// The records of the kinds an app shows, kept linked to the repository: read
/// once, then followed through the watch, kept in the `RecordCache`, with the
/// outbox's waiting writes laid over them (`RecordFold`), so a change made
/// offline shows at once and stays shown until its record is read again.
/// Every write goes to the outbox, in the order made (`WriteLine`); the link
/// sends them.
///
/// The read opens the watch first and lists after its bookmark, and the
/// watch is followed from that bookmark: an app token's lists carry no head
/// (decision 0195), so a list read first leaves nothing to follow from but
/// the live head, past whatever was written meanwhile. Each change written
/// while the list was read comes again from the watch, and its record is
/// read again after the list, so the newer copy is the one kept.
///
/// It is not a controller of its own: an app's root keeps its state
/// (`LinkedRecordsState`) beside its own, wraps its events and effects in its
/// own (`case records(LinkedRecords.Event)`), and asks it at each stage, so
/// its engine reads the records it enriches commands against and its
/// presenter folds nothing. What the read and the watch do next follows from
/// how an event moved the phase, never from a flag the engine keeps.
///
/// The kinds are the ones it was made with until the app names others
/// (`rekinded`): the read and the watch are closed and opened again over the
/// new set, the records of the kinds kept stay shown meanwhile, and the
/// outbox, which outlives every read, keeps its waiting writes whatever
/// kinds they are of.
///
/// The first read takes at most `limit` records (`LinkedRecords.readLimit`
/// unless the app names another); past it, the read says there are `more`,
/// and `notice` puts it in the app's words, so a repository larger than the
/// read is never shown as complete.
public struct LinkedRecords: Equatable, Sendable {
    public enum Event: Codable, Equatable, Sendable {
        /// The app opened: the device's copy is read, and the outbox followed.
        case opened
        case linked(LinkSnapshot)
        /// The device's copy: the records, where the watch resumes, the
        /// records whose read again failed, and the outbox's queue.
        case loaded([Record], cursor: WatchCursor?, stale: Set<String> = [], queue: [OutboxEntry])
        /// The kinds read whole: `cursor` is where the watch follows the read
        /// from, nil while the watch could not open; `more` when the read
        /// stopped at its limit with records left unread.
        case listed([Record], cursor: WatchCursor?, more: Bool = false)
        case readFailed(String, LinkReport?)
        /// A record read again, or removed by the watch, by `kind/id`.
        case changed(String, Record?)
        case rereadFailed([String])
        case watchOpened(WatchCursor)
        case watchInterrupted(String)
        case watchEnded(WatchStop, String)
        /// The watch's position was refused as `compacted`, at this instant
        /// on the app's clock: the kinds are read again, once a minute
        /// (`WatchFollower.recovers`).
        case relistNeeded(at: Date)
        /// The outbox's queue, and the writes it took since it opened.
        case queueReported([OutboxEntry], landed: Set<UUID>)
        case enqueued(Set<UUID>, queue: [OutboxEntry], landed: Set<UUID>)
        case notQueued(Set<UUID>, String)
        /// The owner's change, laid over at once.
        case wrote(LocalWrite)
        case refreshAsked
        /// The app came to the front: a watch that stopped is opened again.
        case foregrounded
        case clearAsked
        case cleared
        /// The app's kinds are now these: the read and the watch start over
        /// over them.
        case rekinded([String])
    }

    /// Why the records are not being updated when the server refuses, as
    /// `compacted`, a watch within a minute of the read again it answered.
    static let notResumed = "the server could not resume their history even after reading them again"

    public enum Effect: Equatable, Sendable {
        case load
        case followOutbox
        /// The outbox's queue as it stands, once.
        case reportQueue
        case read(over: [String])
        case follow(WatchCursor?, over: [String])
        case stopFollowing
        case reread([String])
        case enqueue(LocalWrite)
        case clear
    }

    public typealias State = LinkedRecordsState

    /// The kinds it opens with; `kinds(in:)` is the set it reads now.
    public let kinds: [String]
    /// Nil while no repository is signed in: nothing is read or written.
    public let cache: RecordCache?
    public let outbox: Outbox?
    /// The app's words: its name, the device, and what it calls the records
    /// (`kept`: "Reading your tasks…").
    public let words: StatusWords
    public let line: WriteLine
    /// The most records a read takes.
    public let limit: Int

    /// The records a read takes unless an app names another limit.
    public static let readLimit = 10000

    public init(
        kinds: [String], cache: RecordCache?, outbox: Outbox?, words: StatusWords, line: WriteLine = WriteLine(),
        limit: Int = LinkedRecords.readLimit
    ) {
        self.kinds = kinds
        self.cache = cache
        self.outbox = outbox
        self.words = words
        self.line = line
        self.limit = limit
    }

    public static func == (lhs: LinkedRecords, rhs: LinkedRecords) -> Bool {
        lhs.kinds == rhs.kinds && lhs.cache === rhs.cache && lhs.outbox === rhs.outbox && lhs.words == rhs.words && lhs.line === rhs.line
            && lhs.limit == rhs.limit
    }

    /// The kinds the read and the watch are over in `state`: the app's last
    /// `rekinded`, else the ones it was made with.
    public func kinds(in state: State) -> [String] { state.kinds ?? kinds }

    /// What the app's event at open asks for: the device's copy read, and
    /// the outbox followed.
    public var openEffects: [Effect] { [.load, .followOutbox] }

    // MARK: The engine

    public func apply(_ event: Event, to state: State) -> State {
        var next = state
        switch event {
        case .opened:
            break
        case let .linked(link):
            let before = state.link
            next.link = link
            // A stop lifts where its reason may be gone: any, with a new
            // token (a sign-in, an approval, which changes what it carries);
            // one the server refused for its own reasons, with the network
            // back. The network's first report, at every launch, is no new
            // access.
            let newAccess = Self.newAccess(from: before, to: link)
            let networkBack = before?.online != true && link.online == true
            let running = state.phase.running
            startOrStop(&next, restarting: { newAccess || (networkBack && $0 == .refused) })
            // A read or a watch holds the token it opened with, which a new
            // one replaces and may revoke: it starts again with the new one.
            if newAccess, running, next.phase.running, next.loaded { next.phase = next.cursor == nil ? .reading : .following }
        case let .loaded(records, cursor, stale, queue):
            next.server = keyed(records, in: state)
            next.cursor = cursor
            // The watch resumes past the rows that marked these, so only a
            // read again brings them up to date.
            let kinds = kinds(in: state)
            next.stale = stale.filter { RecordReference(path: $0).map { kinds.contains($0.kind) } ?? false }
            next.queue = queue.filter { kinds.contains($0.kind) }
            next.loaded = true
            fold(&next)
            startOrStop(&next, restarting: { _ in false })
        case let .listed(records, cursor, more):
            next.server = keyed(records, in: state)
            next.stale = []
            next.limitReached = more ? records.count : nil
            fold(&next)
            // Read without the watch's bookmark, it is shown and not followed
            // from; a read that ends after a sign-out starts no watch.
            if let cursor {
                next.cursor = cursor
                if next.link?.signedIn == true, next.phase.running { next.phase = .following }
            }
        case let .readFailed(words, report):
            next.phase = .stopped(Self.stop(report), words)
        case let .changed(key, record):
            next.server[key] = record.flatMap { $0.deletedAt == nil && kinds(in: state).contains($0.kind) ? $0 : nil }
            next.stale.remove(key)
            fold(&next)
        case let .rereadFailed(keys):
            next.stale.formUnion(keys)
        case let .watchOpened(cursor):
            next.cursor = cursor
            next.phase = .following
        case let .watchInterrupted(words):
            next.phase = .interrupted(words)
        case let .watchEnded(stop, words):
            next.phase = .stopped(stop, words)
        case let .relistNeeded(at):
            // Refused, the position is never sent again, read again or not.
            next.cursor = nil
            guard WatchFollower.recovers(at: at, after: state.recoveryStarted) else {
                next.phase = .stopped(.refused, Self.notResumed)
                break
            }
            next.recoveryStarted = at
            next.phase = .reading
        case let .queueReported(entries, landed):
            report(entries, confirming: [], landed: landed, into: &next)
        case let .enqueued(ids, queue, landed):
            report(queue, confirming: ids, landed: landed, into: &next)
        case let .notQueued(ids, _):
            next.queue.removeAll { ids.contains($0.id) }
            next.unconfirmed.subtract(ids)
            fold(&next)
        case let .wrote(write):
            guard !write.entries.isEmpty else { break }
            if let batch = write.replacing { next.queue.removeAll { $0.batch == batch } }
            next.queue += write.entries
            next.unconfirmed.formUnion(write.entries.map(\.id))
            fold(&next)
        case .refreshAsked:
            if next.link?.signedIn == true, next.loaded, next.phase != .reading { next.phase = .reading }
        case .foregrounded:
            startOrStop(&next, restarting: { $0 == .refused })
        case .clearAsked:
            break
        case .cleared:
            next.server = [:]
            next.cursor = nil
            next.stale = []
            fold(&next)
            next.phase = .idle
            startOrStop(&next, restarting: { _ in true })
        case let .rekinded(kinds):
            guard kinds != self.kinds(in: state) else { break }
            next.kinds = kinds
            // The cursor followed the old set: a kind new to the set has
            // records written before it, which only a read brings.
            next.server = state.server.filter { kinds.contains($0.value.kind) }
            next.stale = state.stale.filter { RecordReference(path: $0).map { kinds.contains($0.kind) } ?? false }
            next.queue = state.queue.filter { kinds.contains($0.kind) }
            next.cursor = nil
            next.limitReached = nil
            fold(&next)
            if next.link?.signedIn == true, next.loaded { next.phase = .reading }
        }
        return next
    }

    /// Whether `link` carries a token `before` did not: a sign-in, or a new
    /// approval, which moves its scopes or when its access ends.
    static func newAccess(from before: LinkSnapshot?, to link: LinkSnapshot) -> Bool {
        link.signedIn && (before?.signedIn != true || before?.scopes != link.scopes || before?.accessEnds != link.accessEnds)
    }

    private func keyed(_ records: [Record], in state: State) -> [String: Record] {
        let kinds = kinds(in: state)
        var out: [String: Record] = [:]
        for record in records where kinds.contains(record.kind) && record.deletedAt == nil {
            out[RecordFold.key(record.kind, record.id)] = record
        }
        return out
    }

    private func fold(_ state: inout State) {
        state.records = RecordFold.fold(state.server, state.queue)
    }

    /// What a read failed for, as the stop it leaves: a read the network or
    /// the server failed is tried again as a refused watch is.
    private static func stop(_ report: LinkReport?) -> WatchStop {
        switch report {
        case .accessEnded?: .signedOut
        case .needsApproval?: .needsApproval
        case nil: .refused
        }
    }

    /// Signed in and loaded with nothing reading: the watch resumes where it
    /// was, or the repository is read first; a watch stopped for good, only
    /// where `restarting` says its reason may be gone. Signed out: it stops.
    private func startOrStop(_ state: inout State, restarting: (WatchStop) -> Bool) {
        if state.link?.signedIn == true {
            if case let .stopped(stop, _) = state.phase {
                guard state.loaded, restarting(stop) else { return }
            } else {
                guard state.loaded, state.phase == .idle else { return }
            }
            state.phase = state.cursor == nil ? .reading : .following
        } else if state.phase.running {
            state.phase = .idle
        }
    }

    /// The outbox's queue as it reported it. A write that left it and that
    /// the outbox says landed is laid over the server's copy until that
    /// record is read again, so the owner never sees it undone in between;
    /// one that left otherwise (discarded, dropped as stale, reset away) is
    /// not. A write queued here and not in the report yet stays laid over,
    /// unless the report is the one its own queueing answered, after which
    /// it has left.
    private func report(_ entries: [OutboxEntry], confirming ids: Set<UUID>, landed: Set<UUID>, into state: inout State) {
        let reported = Set(entries.map(\.id))
        var waiting = state.unconfirmed.subtracting(reported)
        let left = state.queue.filter { !reported.contains($0.id) && (!waiting.contains($0.id) || ids.contains($0.id)) }
        waiting.subtract(ids)
        for entry in left where landed.contains(entry.id) && !entry.parked {
            state.server[entry.key] = RecordFold.apply(entry, to: state.server[entry.key])
        }
        state.queue = entries.filter { kinds(in: state).contains($0.kind) } + state.queue.filter { waiting.contains($0.id) }
        state.unconfirmed = waiting
        fold(&state)
    }

    // MARK: The effects

    /// What an applied event asks for. The read and the watch follow the
    /// phase it moved: to reading, a read; to following, a watch, unless it
    /// is the watch itself opened again; from running to idle, the watch
    /// stopped. A relist reads again whatever the phase was, unless it came
    /// within a minute of the last and stopped the read. A write goes to
    /// the outbox, and a record a write of which left the queue, or whose
    /// read failed, is read again.
    public func effects(of event: Event, on state: State, from previous: State) -> [Effect] {
        var out: [Effect] = []
        switch event {
        case .opened:
            out += openEffects
        case let .wrote(write):
            if !write.entries.isEmpty { out.append(.enqueue(write)) }
        case .queueReported, .enqueued:
            let kept = Set(state.queue.map(\.id))
            var keys: [String] = []
            for entry in previous.queue where !kept.contains(entry.id) && !keys.contains(entry.key) { keys.append(entry.key) }
            if !keys.isEmpty { out.append(.reread(keys)) }
        case .clearAsked:
            out.append(.clear)
        case .watchOpened, .foregrounded:
            if !state.stale.isEmpty { out.append(.reread(state.stale.sorted())) }
        case let .linked(link):
            if link.online == true, previous.link?.online != true, !state.stale.isEmpty { out.append(.reread(state.stale.sorted())) }
            // A new token while the phase stays: the running read or watch is
            // replaced (they share a key) by one that reads the new token.
            if Self.newAccess(from: previous.link, to: link), previous.phase.running, state.phase == previous.phase {
                switch state.phase {
                case .reading: out.append(.read(over: kinds(in: state)))
                case .following: out.append(.follow(state.cursor, over: kinds(in: state)))
                default: break
                }
            }
        case .relistNeeded where previous.phase == .reading && state.phase == .reading:
            out.append(.read(over: kinds(in: state)))
        case .rekinded where state.kinds != previous.kinds:
            // The queue was kept to the old set: the outbox says what of it
            // the new set holds. A read already running is replaced.
            if outbox != nil { out.append(.reportQueue) }
            if previous.phase == .reading, state.phase == .reading { out.append(.read(over: kinds(in: state))) }
        default:
            break
        }
        if state.phase != previous.phase {
            switch state.phase {
            case .reading:
                out.append(.read(over: kinds(in: state)))
            case .following:
                if case .watchOpened = event {} else { out.append(.follow(state.cursor, over: kinds(in: state))) }
            case .idle where previous.phase.running:
                out.append(.stopFollowing)
            default:
                break
            }
        }
        return out
    }

    /// A write is never cancelled for another: the line keeps their order.
    /// The read opens a watch of its own for the bookmark, so it is stopped
    /// and replaced as the watch is.
    public func key(of effect: Effect) -> String? {
        switch effect {
        case .load: "records.load"
        case .followOutbox: "records.outbox"
        case .reportQueue: "records.queue"
        case .read, .follow, .stopFollowing: "records.watch"
        case .clear: "records.clear"
        case .reread, .enqueue: nil
        }
    }

    /// A 401 or a 403 `insufficient_scope` from the read or the watch.
    public func report(of event: Event) -> LinkReport? {
        switch event {
        case let .readFailed(_, report): report
        case .watchEnded(.signedOut, _): .accessEnded
        case .watchEnded(.needsApproval, _): .needsApproval
        default: nil
        }
    }

    /// A burst of watch rows or outbox reports is drawn once per window.
    public func coalescing(of event: Event) -> Coalescing {
        switch event {
        case .changed, .queueReported: .within(.milliseconds(100))
        default: .now
        }
    }

    public func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        let (cache, outbox, line, words) = (cache, outbox, line, words)
        let source = environment.client
        let clock = environment.clock
        switch effect {
        case .load:
            return .once {
                var records: [Record] = []
                if let cache { for kind in self.kinds { records += await cache.records(of: kind) } }
                return .loaded(
                    records, cursor: await cache?.cursor, stale: await cache?.stale ?? [], queue: await outbox?.entries ?? [])
            }
        case .followOutbox:
            guard let outbox else { return .none }
            return .relay { continuation in
                for await _ in await outbox.changes() { continuation.yield(.queueReported(await outbox.entries, landed: await outbox.landed)) }
            }
        case .reportQueue:
            guard let outbox else { return .none }
            return .once { .queueReported(await outbox.entries, landed: await outbox.landed) }
        case let .read(kinds):
            let limit = limit
            return .relay { continuation in
                guard let client = await source.current() else { return }
                let list = { (bookmark: WatchCursor?) async -> Bool in
                    await Self.list(
                        kinds, limit: limit, from: bookmark, client: client, cache: cache, outbox: outbox, words: words,
                        into: continuation)
                }
                // A watch that cannot open holds back neither the records nor
                // why the list was refused, which says more than the watch's
                // refusal: a kind out of reach is a 403 to the list and an
                // unknown kind to the watch.
                var bookmark: WatchCursor?
                var listed = false
                opening: for await event in WatchFollower(client: client, kinds: kinds, app: words.app).follow(from: nil) {
                    switch event {
                    case let .opened(cursor):
                        bookmark = cursor
                        break opening
                    case let .interrupted(reason):
                        if !listed {
                            guard await list(nil) else { return }
                            listed = true
                        }
                        continuation.yield(.watchInterrupted(reason))
                    case let .ended(end):
                        if !listed {
                            guard await list(nil) else { return }
                        }
                        continuation.yield(Self.stopped(end, words: words))
                        return
                    case .relist:
                        await cache?.relist()
                        continuation.yield(.relistNeeded(at: clock.now))
                        return
                    case .row:
                        continue
                    }
                }
                guard let bookmark else { return }
                _ = await list(bookmark)
            }
        case let .follow(cursor, kinds):
            return .relay { continuation in
                guard let client = await source.current() else { return }
                let follower = WatchFollower(client: client, kinds: kinds, app: words.app)
                for await event in follower.follow(from: cursor) {
                    switch event {
                    case let .opened(cursor):
                        await cache?.bookmark(cursor)
                        continuation.yield(.watchOpened(cursor))
                    case let .row(row):
                        await cache?.apply(row)
                        let key = RecordFold.key(row.kind, row.recordID)
                        if row.removes {
                            continuation.yield(.changed(key, nil))
                        } else {
                            switch await Self.read(row.kind, row.recordID, client: client, cache: cache, outbox: outbox, words: words) {
                            case let .read(event): continuation.yield(event)
                            case .failed: continuation.yield(.rereadFailed([key]))
                            }
                        }
                    case .relist:
                        // Dropped here, or the read would resume from the
                        // cursor the server just refused.
                        await cache?.relist()
                        continuation.yield(.relistNeeded(at: clock.now))
                    case let .interrupted(reason):
                        continuation.yield(.watchInterrupted(reason))
                    case let .ended(end):
                        continuation.yield(Self.stopped(end, words: words))
                    }
                }
            }
        case .stopFollowing:
            return .cancel
        case let .reread(keys):
            return .relay { continuation in
                guard let client = await source.current() else {
                    continuation.yield(.rereadFailed(keys))
                    return
                }
                var failed: [String] = []
                for key in keys {
                    guard let reference = RecordReference(path: key) else { continue }
                    switch await Self.read(reference.kind, reference.recordID, client: client, cache: cache, outbox: outbox, words: words) {
                    case let .read(event): continuation.yield(event)
                    case .failed: failed.append(key)
                    }
                }
                if !failed.isEmpty { continuation.yield(.rereadFailed(failed)) }
            }
        case let .enqueue(write):
            guard let outbox else { return .none }
            let ticket = line.ticket()
            return .once {
                await line.wait(ticket)
                defer { line.done(ticket) }
                let ids = Set(write.entries.map(\.id))
                do {
                    if let batch = write.replacing, let entry = write.entries.first {
                        try await outbox.purge(batch: batch, with: entry)
                    } else {
                        try await outbox.enqueue(write.entries)
                    }
                    return .enqueued(ids, queue: await outbox.entries, landed: await outbox.landed)
                } catch {
                    return .notQueued(ids, "\(words.device.capitalizedFirst) could not keep the change: \(error.localizedDescription)")
                }
            }
        case .clear:
            return .once {
                await cache?.clear()
                return .cleared
            }
        }
    }

    /// The kinds read whole and kept, with the watch's bookmark as where the
    /// watch follows them from. Answers whether they were read.
    private static func list(
        _ kinds: [String], limit: Int, from bookmark: WatchCursor?, client: SubstrateClient, cache: RecordCache?,
        outbox: Outbox?, words: StatusWords, into continuation: AsyncStream<Event>.Continuation
    ) async -> Bool {
        do {
            let page = try await client.listAll(ListQuery(kinds: kinds, first: 200), limit: limit)
            await cache?.listed(page, kinds: kinds, from: bookmark)
            try? await outbox?.saw(Dictionary(page.records.map { (RecordFold.key($0.kind, $0.id), $0.version) }) { a, _ in a })
            var cursor: WatchCursor?
            if let bookmark { cursor = await cache?.cursor ?? bookmark }
            continuation.yield(.listed(page.records, cursor: cursor, more: page.cursor != nil))
            return true
        } catch {
            continuation.yield(.readFailed(explain(error, app: words.app, server: client.server), LinkReport(error)))
            return false
        }
    }

    private enum Read {
        case read(Event)
        /// Not read now (the network, the server): the record is read again
        /// at the next chance.
        case failed
    }

    /// One record read again after the watch named it, or a write of it
    /// landed: kept in the cache, its version told to the outbox.
    private static func read(
        _ kind: String, _ id: String, client: SubstrateClient, cache: RecordCache?, outbox: Outbox?,
        words: StatusWords
    ) async -> Read {
        do {
            let record = try await client.record(kind, id)
            await cache?.refreshed(kind, id, as: record)
            if let record { try? await outbox?.saw([RecordFold.key(kind, id): record.version]) }
            return .read(.changed(RecordFold.key(kind, id), record))
        } catch is SessionExpired {
            return .read(stopped(.signedOut, words: words))
        } catch is InsufficientScope {
            return .read(stopped(.needsApproval, words: words))
        } catch {
            return .failed
        }
    }

    private static func stopped(_ end: WatchEnd, words: StatusWords) -> Event {
        switch end {
        case .signedOut: .watchEnded(.signedOut, "Your access ended.")
        case .needsApproval: .watchEnded(.needsApproval, "Your approval does not reach your \(words.kept).")
        case let .refused(reason): .watchEnded(.refused, reason)
        }
    }

    // MARK: The presenter

    /// The line about reading the records, nil when nothing is worth
    /// saying: the first read, the watch waiting to open again, or stopped;
    /// else a read that stopped at its limit.
    public func notice(_ state: State) -> NoticeLine? {
        guard state.link?.signedIn == true else { return nil }
        switch state.phase {
        case .reading where state.records.isEmpty: return NoticeLine("Reading your \(words.kept)…", symbol: .sending, tone: .quiet)
        case let .interrupted(reason): return NoticeLine(reason, symbol: .offline, tone: .caution)
        case let .stopped(_, reason): return NoticeLine("Your \(words.kept) are not being updated: \(reason)", symbol: .warning, tone: .critical)
        default: break
        }
        guard let limit = state.limitReached else { return nil }
        return NoticeLine(
            "Only the first \(limit) of your \(words.kept) are shown: \(words.app) reads no more.", symbol: .warning,
            tone: .caution)
    }
}
