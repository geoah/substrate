import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime

/// The effects' work: the reads, the watch, the device's folder and its
/// services. Each reads the client when it runs and never keeps it.
extension ChatApp {
    /// The refresh job the app declares to the system.
    public static let refreshJob = "dev.reamde.substrate.chat.refresh"
    /// The kinds the watch follows: the model records only where the plan
    /// reads them, since the watch refuses a kind the token cannot read, and
    /// the approvals only once a read of them was answered, since it refuses
    /// a kind the server does not declare.
    static func watched(_ plan: ChatAppAsk.Plan, approvals: Bool) -> [String] {
        [Kinds.thread, Kinds.message, Kinds.agent, Kinds.provider, Kinds.interaction, Kinds.request]
            + (plan.canReadModels ? [Kinds.model] : []) + (approvals ? [Kinds.approval] : [])
    }

    public func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        let source = environment.client
        let clock = environment.clock
        let storage = storage
        let preferences = preferences
        let notifications = environment.devices.notifications
        let background = environment.devices.background
        @Sendable func failed(_ error: any Error, _ client: SubstrateClient) -> Failure { Failure(error, server: client.server) }
        @Sendable func pause(_ seconds: Double) async -> Bool {
            do {
                try await clock.sleep(until: clock.now.addingTimeInterval(seconds))
                return true
            } catch {
                return false
            }
        }
        switch effect.ask {
        case let .openDisk(identity):
            // Queued now, in the order the effects were asked, behind the
            // retirement of the session before.
            let opening = storage.open(identity)
            return .once {
                let lease = await opening.value
                var state = storage.state(lease)
                var stale: [ChatAppState.WatchKey] = []
                if let cache = storage.records(lease) {
                    // The cursor an earlier build kept beside the list moves
                    // to the record cache, which drops it when it is
                    // refused, and leaves the list's file: kept there, a
                    // refused one would be sent again at every launch that
                    // finds the cache without a cursor.
                    if let seq = state?.feedSeq, let generation = state?.feedGeneration, await cache.cursor == nil {
                        await cache.bookmark(WatchCursor(seq: seq, generation: generation))
                    }
                    if var moved = state, moved.feedSeq != nil || moved.feedGeneration != nil {
                        moved.feedSeq = nil
                        moved.feedGeneration = nil
                        storage.save(moved, lease)
                        state = moved
                    }
                    stale = await cache.stale.compactMap(Self.watchKey)
                }
                return Event(
                    .diskOpened(
                        .init(
                            lease: lease, state: state, drafts: storage.drafts(lease),
                            preferences: preferences.load(identity), stale: stale)))
            }
        case let .closeDisk(identity, deleting):
            // Fenced now, before any later effect starts.
            let retiring = storage.retire(identity, deleting: deleting) { if deleting { preferences.clear(identity) } }
            return .once {
                await retiring.value
                return nil
            }
        case let .prune(lease):
            return .once {
                storage.prune(lease, at: clock.now)
                return nil
            }
        case let .readUsage(lease):
            return .once { Event(.usageRead(storage.usage(lease))) }
        case let .clearTranscripts(lease):
            return .once {
                storage.clearTranscripts(lease)
                return Event(.transcriptsCleared)
            }
        case let .saveState(lease, cached):
            // Coalesced: a later save cancels this one while it waits.
            return .once {
                guard await pause(0.4) else { return nil }
                storage.save(cached, lease)
                return nil
            }
        case let .saveDrafts(lease, drafts):
            return .once {
                guard await pause(0.6) else { return nil }
                storage.save(drafts: drafts, lease)
                return nil
            }
        case let .savePreferences(lease, chosen):
            return .once {
                storage.whileOpen(lease) { preferences.save(chosen, lease.identity) }
                return nil
            }
        case let .saveTranscript(lease, thread, transcript):
            return .once {
                var stamped = transcript
                stamped.openedAt = clock.now
                storage.save(stamped, thread: thread, lease)
                return nil
            }
        case let .loadCached(lease, thread, epoch):
            return .once { Event(.cachedLoaded(thread: thread, epoch: epoch, storage.transcript(lease, thread: thread, at: clock.now))) }

        case let .refresh(plan):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    return Event(.refreshed(try await Self.refresh(client, plan)))
                } catch {
                    return Task.isCancelled ? nil : Event(.refreshFailed(failed(error, client)))
                }
            }
        case let .cards(plan, threads):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    return Event(.cardsRead(try await Self.cards(client, plan, threads: threads)))
                } catch {
                    return Task.isCancelled ? nil : Event(.readFailed(failed(error, client)))
                }
            }
        case let .openings(threads):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    let page = try await client.list(
                        ListQuery(
                            kinds: [Kinds.message], properties: ["role": ["eq": "user"], "thread": ["in": .array(threads.map(JSONValue.string))]],
                            first: 500, orderBy: "turn:asc,createdAt:asc"))
                    return Event(.openingsRead(openingMessages(page.records)))
                } catch {
                    return Task.isCancelled ? nil : Event(.readFailed(failed(error, client)))
                }
            }
        case let .read(thread, first, epoch, version):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    let page = try await client.list(ChatAPI.transcriptQuery(thread, first: first))
                    return Event(
                        .rowsRead(
                            thread: thread, epoch: epoch, version: version, rows: page.records.reversed(),
                            full: page.records.count >= first))
                } catch {
                    return Task.isCancelled ? nil : Event(.rowsFailed(thread: thread, epoch: epoch, failed(error, client)))
                }
            }
        case let .prefetch(lease, items):
            return .sequence {
                Self.relay { yield in
                    guard await pause(1), let client = await source.current() else { return }
                    for item in items {
                        if !item.held, let cached = storage.transcript(lease, thread: item.thread, at: clock.now) {
                            yield(.cachedLoaded(thread: item.thread, epoch: item.epoch, cached))
                            if cached.threadVersion == item.version { continue }
                        }
                        guard let page = try? await client.list(ChatAPI.transcriptQuery(item.thread, first: 100)) else { return }
                        yield(.prefetched(thread: item.thread, epoch: item.epoch, version: item.version, rows: page.records.reversed()))
                    }
                }
            }
        case let .fetchThread(thread):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    return try await client.record(Kinds.thread, thread).map { Event(.threadFetched($0)) }
                } catch {
                    return Task.isCancelled ? nil : Event(.readFailed(failed(error, client)))
                }
            }
        case let .watch(lease, head, kinds):
            return .sequence {
                Self.relay { yield in
                    guard let client = await source.current(), let cache = storage.records(lease) else { return }
                    if let head { await cache.listed(RecordPage(records: [], head: head.seq, generation: head.generation), kinds: []) }
                    var fresh = await cache.cursor == nil
                    let follower = WatchFollower(client: client, kinds: kinds, app: ChatManifest.appName) { wait in
                        let parts = wait.components
                        try await clock.sleep(until: clock.now.addingTimeInterval(Double(parts.seconds) + Double(parts.attoseconds) / 1e18))
                    }
                    for await event in follower.follow(from: await cache.cursor) {
                        switch event {
                        case let .opened(cursor):
                            await cache.bookmark(cursor)
                            yield(.watchOpened(fresh: fresh))
                            fresh = false
                        case let .row(row):
                            await cache.apply(row)
                            yield(.watchRow(row))
                        case .relist:
                            await cache.relist()
                            yield(.watchRelist(at: clock.now))
                        case .interrupted:
                            break
                        case let .ended(end):
                            switch end {
                            case .signedOut: yield(.watchEnded(nil, Failure(kind: .sessionExpired, words: "")))
                            case .needsApproval: yield(.watchEnded(nil, Failure(kind: .needsApproval, words: "")))
                            case let .refused(words): yield(.watchEnded(words, nil))
                            }
                        }
                    }
                }
            }
        case let .feedWait(seconds):
            return .once { await pause(seconds) ? Event(.feedDue) : nil }
        case let .applyFeed(plan, batch, threads):
            return .once {
                guard let client = await source.current() else { return nil }
                return Event(.feedApplied(await Self.feed(client, plan, batch, threads: threads)))
            }
        case let .markRefreshed(lease, keys):
            return .once {
                guard let cache = storage.records(lease) else { return nil }
                for key in keys { await cache.refreshed(key.kind, key.id, as: nil) }
                return nil
            }

        case let .rename(thread, subject, before):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    return Event(.renamed(try await client.patch(Kinds.thread, thread, properties: ["subject": .string(subject)])))
                } catch {
                    return Event(.renameFailed(thread: thread, before: before, failed(error, client)))
                }
            }
        case let .setKey(provider, key):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    return Event(.keySet(try await client.patch(Kinds.provider, provider, properties: ["apiKey": .string(key)])))
                } catch {
                    return Event(.keyFailed(failed(error, client)))
                }
            }
        case let .readCard(reference, target):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    switch reference {
                    case let .question(id):
                        return Event(.cardRead(id, try await client.record(Kinds.interaction, id), target: nil))
                    case let .approval(id):
                        return Event(.cardRead(id, try await client.record(Kinds.approval, id), target: nil))
                    case let .suggestion(id):
                        let request = try await client.record(Kinds.request, id)
                        // The target is context, and often out of the token's reach.
                        var read: Record?
                        if target, let request, request["op"]?.string != "create",
                            let kind = request["targetKind"]?.text ?? request["target"]?.referencePath.map(Self.kindOf),
                            let targetID = request["targetId"]?.text ?? request["target"]?.referenceID
                        {
                            read = try? await client.record(kind, targetID)
                        }
                        return Event(.cardRead(id, request, target: read))
                    }
                } catch {
                    return Task.isCancelled ? nil : Event(.cardFailed(reference.id, failed(error, client)))
                }
            }
        case let .writeCard(id, kind, properties, ifVersion):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    return Event(.cardWritten(id, try await client.patch(kind, id, properties: properties, ifVersion: ifVersion)))
                } catch {
                    return Event(.cardWriteFailed(id, failed(error, client), conflict: Self.conflictWords(kind)))
                }
            }
        case let .decideUnread(id, accept, shown):
            return .once {
                guard let client = await source.current() else { return nil }
                let conflict = Self.conflictWords(Kinds.request)
                do {
                    guard let held = try await client.record(Kinds.request, id) else {
                        return Event(.cardWriteFailed(id, Failure(kind: .other, words: "This suggestion is no longer there."), conflict: conflict))
                    }
                    if let diff = held["diff"]?["properties"]?.object, diff != shown {
                        return Event(.cardWriteFailed(id, Failure(kind: .other, words: conflict), conflict: conflict))
                    }
                    let record = try await client.patch(
                        Kinds.request, id, properties: ["decision": .string(accept ? "accepted" : "rejected")],
                        ifVersion: held.version)
                    return Event(.cardWritten(id, record))
                } catch {
                    return Event(.cardWriteFailed(id, failed(error, client), conflict: conflict))
                }
            }

        case .readGuidance:
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    async let memories = client.listAll(ListQuery(kinds: [Kinds.memory], first: 200, orderBy: "createdAt:desc"))
                    async let instructions = client.listAll(ListQuery(kinds: [Kinds.instruction], first: 200, orderBy: "createdAt:desc"))
                    let read = ChatAppStep.GuidanceStep.read(memories: try await memories.records, instructions: try await instructions.records)
                    return Event(.guidance(read))
                } catch {
                    return Task.isCancelled ? nil : Event(.guidance(.readFailed(failed(error, client))))
                }
            }
        case let .writeGuidance(reference, properties, before):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    let record = try await client.patch(reference.kind, reference.recordID, properties: properties, ifVersion: before.version)
                    return Event(.guidance(.written(record)))
                } catch {
                    return Event(.guidance(.writeFailed(reference, failed(error, client), before: before)))
                }
            }
        case let .deleteGuidance(reference, before):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    try await client.delete(reference.kind, reference.recordID, ifVersion: before.version)
                    return Event(.guidance(.deleted(reference)))
                } catch {
                    return Event(.guidance(.writeFailed(reference, failed(error, client), before: before)))
                }
            }

        case let .notify(request):
            return Self.device(notifications) { await $0.schedule(request) }
        case let .cancelGroup(thread):
            // What it delivered, and what is still pending.
            return Self.device(notifications) { notifications in
                await notifications.cancel(thread: thread)
                for id in await notifications.pending(prefix: ChatNotifications.prefix(thread)) { await notifications.cancel(id) }
            }
        case .cancelAllNotifications:
            return Self.device(notifications) { await $0.cancelAll() }
        case let .badge(count):
            return Self.device(notifications) { await $0.setBadge(count) }
        case .askPermission:
            guard let notifications else { return .none }
            return .once { Event(.permissionRead(await notifications.requestPermission())) }
        case .readPermission:
            guard let notifications else { return .none }
            return .once { Event(.permissionRead(await notifications.permission())) }
        case .followResponses:
            guard let notifications else { return .none }
            return .sequence {
                Self.relay { yield in
                    for await response in notifications.responses() { yield(.notificationOpened(response)) }
                }
            }
        case .followRuns:
            guard let background else { return .none }
            return .sequence {
                Self.relay { yield in
                    await withTaskGroup(of: Void.self) { group in
                        group.addTask { for await run in background.runs() { yield(.backgroundRun(run)) } }
                        group.addTask { for await id in background.expirations() { yield(.backgroundExpired(id)) } }
                    }
                }
            }
        case let .backgroundRefresh(run, lease, plan, threads):
            return .once {
                // Checked before it reads and again before its result can
                // write: a sign-out between lands nothing.
                guard storage.holds(lease), let client = await source.current() else {
                    return Event(.backgroundRefreshed(run, threads: nil, cards: nil))
                }
                let reading = { @Sendable () async -> ChatAppStep in
                    let read = try? await Self.threads(client, plan)
                    let cards = try? await Self.cards(client, plan, threads: read.map { ($0.chats + $0.automated).map(\.id) } ?? threads)
                    return .backgroundRefreshed(run, threads: read, cards: cards)
                }
                // Short of the budget, so the run is answered before the
                // system takes the time back.
                let step = await Self.within(run.budget.map { $0 * 0.8 }, clock: clock, reading) ?? .backgroundExpired(run.id)
                if case .backgroundRefreshed = step, !storage.holds(lease) { return Event(.backgroundRefreshed(run, threads: nil, cards: nil)) }
                return Event(step)
            }
        case let .finishRun(run, success):
            return Self.device(background) { await $0.finish(run, success: success) }
        case let .scheduleRefresh(lease):
            let job = BackgroundJob(name: Self.refreshJob, earliest: clock.now.addingTimeInterval(15 * 60), payload: lease.payload)
            return Self.device(background) { await $0.schedule(job) }
        case let .continueRun(key, request):
            guard let background else { return .none }
            return .sequence {
                Self.relay { yield in
                    for await update in background.continueProcessing(request) { yield(.continued(key, update)) }
                }
            }
        case let .endContinued(key):
            return Self.device(background) { await $0.endProcessing(key.rawValue) }
        case .cancel:
            return .cancel
        }
    }

    // MARK: Reads

    static func threads(_ client: SubstrateClient, _ plan: ChatAppAsk.Plan) async throws -> ChatAppStep.Threads {
        try await threadPages(client, plan).threads
    }

    private static func threadPages(_ client: SubstrateClient, _ plan: ChatAppAsk.Plan) async throws
        -> (threads: ChatAppStep.Threads, page: RecordPage)
    {
        let chats = try await client.list(
            ListQuery(
                kinds: [Kinds.thread], properties: ["mode": ["eq": "chat"]], first: 100,
                orderBy: "updatedAt:desc"))
        var automated: [Record] = []
        if plan.showAutomated {
            let modes = conversationModes.filter { $0 != "chat" }.map(JSONValue.string)
            automated = try await client.list(
                ListQuery(
                    kinds: [Kinds.thread], properties: ["mode": ["in": .array(modes)]], first: 50,
                    orderBy: "updatedAt:desc")
            ).records
        }
        return (ChatAppStep.Threads(chats: chats.records, automated: automated), chats)
    }

    static func agents(_ client: SubstrateClient, _ plan: ChatAppAsk.Plan) async throws -> (
        agents: RecordPage, providers: RecordPage?, models: RecordPage?, keys: ProviderKeys?
    ) {
        async let agents = client.listAll(ListQuery(kinds: [Kinds.agent], first: 200, orderBy: "createdAt:desc"))
        async let providers: RecordPage? = plan.canReadProviders ? client.listAll(ListQuery(kinds: [Kinds.provider], first: 100)) : nil
        async let models: RecordPage? = plan.canReadModels ? Self.models(client) : nil
        async let keys: ProviderKeys? = plan.readsOperator ? ProviderKeys.read(client) : .rows
        return try await (agents, providers, models, keys)
    }

    /// The `llm/model` records. A server older than the kind refuses the
    /// filter as an unknown type: it holds none, and no agent there runs on
    /// any. Every other refusal stays one.
    static func models(_ client: SubstrateClient) async throws -> RecordPage {
        do {
            return try await client.listAll(ListQuery(kinds: [Kinds.model], first: 100))
        } catch let error as ServerError where modelKindUnknown(error) {
            return RecordPage(records: [])
        }
    }

    /// Whether the server answered that it does not know the `llm/model`
    /// kind: the records read refuses an undeclared filter kind with 422,
    /// code `validation`, naming it as an unknown type.
    static func modelKindUnknown(_ error: ServerError) -> Bool {
        error.status == 422 && error.code == "validation" && error.message.contains("unknown type \"\(Kinds.model)\"")
    }

    /// The questions, suggestions and approvals waiting on the owner. An
    /// app's token lists them only under the threads it names (the server
    /// pages before it could filter otherwise), a batch at a time; the
    /// suggestions are readable only with `decide:`. A refused approvals
    /// read is no failure: a server older than the kind holds none.
    static func cards(_ client: SubstrateClient, _ plan: ChatAppAsk.Plan, threads: [String]) async throws -> ChatAppStep.Cards {
        guard plan.canChat else { return ChatAppStep.Cards(interactions: [], requests: []) }
        @Sendable func read(_ kind: String, _ condition: [String: JSONValue]) async throws -> [Record] {
            guard plan.namesThreads else {
                return try await client.list(ListQuery(kinds: [kind], properties: condition, first: 100)).records
            }
            var out: [Record] = []
            for start in stride(from: 0, to: threads.count, by: 50) {
                var properties = condition
                properties["thread"] = ["in": .array(threads[start ..< min(start + 50, threads.count)].map(JSONValue.string))]
                out += try await client.list(ListQuery(kinds: [kind], properties: properties, first: 100)).records
            }
            return out
        }
        async let interactions = read(Kinds.interaction, ["state": ["eq": "pending"]])
        async let requests: [Record] = plan.canDecide ? read(Kinds.request, ["decision": ["eq": "proposed"]]) : []
        // Only a read the server answered says it knows the kind: an app's
        // token naming no thread asks nothing.
        let asksApprovals = !(plan.namesThreads && threads.isEmpty)
        async let approvals: [Record]? = asksApprovals ? try? read(Kinds.approval, ["state": ["eq": "pending"]]) : nil
        return try await ChatAppStep.Cards(interactions: interactions, requests: requests, approvals: approvals)
    }

    /// Everything the list draws. The watch resumes from the earliest head
    /// of the reads in one history, so nothing written between them is
    /// missed; an app token's pages carry none, and its watch opens at the
    /// live head and reads the lists again after the bookmark.
    static func refresh(_ client: SubstrateClient, _ plan: ChatAppAsk.Plan) async throws -> ChatAppStep.Refresh {
        async let lists = agents(client, plan)
        async let threadRead = threadPages(client, plan)
        async let early: ChatAppStep.Cards? = plan.namesThreads ? nil : cards(client, plan, threads: [])
        let (read, (threads, page), cardsEarly) = try await (lists, threadRead, early)
        let cards: ChatAppStep.Cards
        if let cardsEarly {
            cards = cardsEarly
        } else {
            cards = try await self.cards(client, plan, threads: (threads.chats + threads.automated).map(\.id))
        }
        var head: WatchCursor?
        if !read.agents.generation.isEmpty {
            let heads =
                [read.agents, read.providers, read.models].compactMap { $0 }.filter { $0.generation == read.agents.generation }
                .map(\.head)
                + [page.head]
            head = heads.min().map { WatchCursor(seq: $0, generation: read.agents.generation) }
        }
        return ChatAppStep.Refresh(
            agents: read.agents.records, providers: read.providers?.records, models: read.models?.records, keys: read.keys,
            threads: threads, cards: cards, head: head)
    }

    /// A batch of the watch's rows, read again: the list for a thread, the
    /// agents and providers, the cards.
    static func feed(_ client: SubstrateClient, _ plan: ChatAppAsk.Plan, _ batch: Set<ChatAppState.WatchKey>, threads: [String]) async
        -> ChatAppStep.Feed
    {
        var out = ChatAppStep.Feed(failed: [])
        let kinds = Set(batch.map(\.kind))
        var known = threads
        if kinds.contains(Kinds.thread) {
            do {
                let read = try await self.threads(client, plan)
                out.threads = read
                known = (read.chats + read.automated).map(\.id)
            } catch {
                out.failed.formUnion(batch.filter { $0.kind == Kinds.thread })
                out.failure = Failure(error, server: client.server)
            }
        }
        let agentKinds: Set<String> = [Kinds.agent, Kinds.provider, Kinds.model]
        if !kinds.isDisjoint(with: agentKinds) {
            do {
                let read = try await agents(client, plan)
                out.agents = read.agents.records
                out.providers = read.providers?.records
                out.models = read.models?.records
                out.keys = read.keys
            } catch {
                out.failed.formUnion(batch.filter { agentKinds.contains($0.kind) })
                out.failure = Failure(error, server: client.server)
            }
        }
        let cardKinds: Set<String> = [Kinds.interaction, Kinds.request, Kinds.approval]
        if !kinds.isDisjoint(with: cardKinds) {
            do {
                out.cards = try await cards(client, plan, threads: known)
            } catch {
                out.failed.formUnion(batch.filter { cardKinds.contains($0.kind) })
                out.failure = Failure(error, server: client.server)
            }
        }
        return out
    }

    // MARK: Helpers

    /// The body's answer, or nil once `seconds` passed first; without a
    /// limit, the body's.
    static func within<Value: Sendable>(
        _ seconds: TimeInterval?, clock: any RuntimeClock,
        _ body: @escaping @Sendable () async -> Value
    ) async -> Value? {
        guard let seconds else { return await body() }
        return await withTaskGroup(of: Value?.self) { group in
            group.addTask { await body() }
            group.addTask {
                try? await clock.sleep(until: clock.now.addingTimeInterval(seconds))
                return nil
            }
            let first = await group.next() ?? nil
            group.cancelAll()
            return first
        }
    }

    static func conflictWords(_ kind: String) -> String {
        kind == Kinds.interaction ? "The questions changed since you read them. Check them again." : "It changed since you read it. Check it again."
    }

    /// A record cache's key, `kind/id`, back as its parts: a kind is three
    /// segments, and an id may hold slashes of its own.
    static func watchKey(_ key: String) -> ChatAppState.WatchKey? {
        let parts = key.split(separator: "/", maxSplits: 3, omittingEmptySubsequences: false)
        guard parts.count == 4 else { return nil }
        return ChatAppState.WatchKey(kind: parts[0 ... 2].joined(separator: "/"), id: String(parts[3]))
    }

    static func kindOf(_ path: String) -> String {
        path.split(separator: "/").prefix(3).joined(separator: "/")
    }

    private static func device<Service: Sendable>(_ service: Service?, _ body: @escaping @Sendable (Service) async -> Void) -> Work<Event> {
        guard let service else { return .none }
        return .once {
            await body(service)
            return nil
        }
    }

    static func relay(_ body: @escaping @Sendable (_ yield: @escaping @Sendable (ChatAppStep) -> Void) async -> Void) -> AsyncStream<Event> {
        AsyncStream { continuation in
            let task = Task {
                await body { continuation.yield(Event($0)) }
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }
}
