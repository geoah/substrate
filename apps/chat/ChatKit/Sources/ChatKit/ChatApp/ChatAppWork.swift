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
    /// the approvals and the marks only once a read of them was answered,
    /// since it refuses a kind the server does not declare.
    static func watched(_ plan: ChatAppAsk.Plan, approvals: Bool, marks: Bool = false) -> [String] {
        [Kinds.thread, Kinds.message, Kinds.agent, Kinds.provider, Kinds.interaction, Kinds.request]
            + (plan.canReadModels ? [Kinds.model] : []) + (approvals ? [Kinds.approval] : []) + (marks ? [Kinds.threadMark] : [])
    }

    public func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        let source = environment.client
        let clock = environment.clock
        let storage = storage
        let preferences = preferences
        let notifications = environment.devices.notifications
        let background = environment.devices.background
        @Sendable func failed(_ attempt: Failure.Attempt, _ error: any Error, _ client: SubstrateClient) -> Failure {
            Failure(attempt, error, server: client.server)
        }
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
                            lease: lease, state: state, drafts: storage.drafts(lease), unsent: storage.unsent(lease),
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
        case let .saveDrafts(lease, drafts, atOnce):
            // Its place among the drafts' writes, taken now, in the order
            // the effects were asked: one with no wait may come to run
            // after the one that follows it.
            let ticket = storage.draftsTicket()
            return .once {
                if !atOnce { guard await pause(0.6) else { return nil } }
                storage.save(drafts: drafts, lease, ticket: ticket)
                return nil
            }
        case let .saveUnsent(lease, unsent, drafts):
            // At once, and answered: a message's first copy waits for it.
            // Answered whether or not the folder took it, with the
            // device's words where it did not: a folder that cannot be
            // written (a full disk) does not hold a message back, and the
            // list says so and writes again.
            let ticket = storage.draftsTicket()
            return .once {
                switch storage.save(drafts: drafts, unsent: unsent, lease, ticket: ticket) {
                case .written: return Event(.unsentSaved(Set(unsent.values.map(\.id)), failed: nil))
                case let .failed(seen): return Event(.unsentSaved(Set(unsent.values.map(\.id)), failed: seen))
                case .passed: return nil
                }
            }
        case let .retryUnsent(seconds):
            return .once { await pause(seconds) ? Event(.unsentRetryDue) : nil }
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

        case let .refresh(plan, held, beside):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    return Event(.refreshed(try await Self.refresh(client, plan, held: held, beside: beside)))
                } catch {
                    return Task.isCancelled ? nil : Event(.refreshFailed(failed(.refresh, error, client)))
                }
            }
        case let .cards(plan, threads):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    return Event(.cardsRead(try await Self.cards(client, plan, threads: threads)))
                } catch {
                    return Task.isCancelled ? nil : Event(.readFailed(failed(.refresh, error, client)))
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
                    return Task.isCancelled ? nil : Event(.readFailed(failed(.refresh, error, client)))
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
                    return Task.isCancelled ? nil : Event(.rowsFailed(thread: thread, epoch: epoch, failed(.rows, error, client)))
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
        case .readFeatures:
            // A read that fails changes nothing: the chat goes on as it
            // does on a server it knows nothing of. One that never reached
            // the server is made again twice, a little later each time:
            // without the document the lists are read as an older server's,
            // archived chats among them, until the next refresh asks.
            return .once {
                guard let client = await source.current() else { return nil }
                for wait in [0, 2, 6] {
                    if wait > 0 { guard await pause(Double(wait)) else { return nil } }
                    do {
                        let discovery = try await client.discovery()
                        return Event(
                            .featuresRead(
                                agents: discovery.feature("agents")?.revision ?? 0, records: discovery.feature("records")?.revision ?? 0))
                    } catch is ServerError {
                        // The server's own answer: it has no such document.
                        return nil
                    } catch {
                        continue
                    }
                }
                return nil
            }
        case let .readMarks(threads):
            // A read that fails changes nothing: the marks held stand, and
            // the next list read asks again.
            return .once {
                guard let client = await source.current(), let marks = await Self.marks(client, threads: threads) else { return nil }
                return Event(.marksRead(marks))
            }
        case let .sight(thread, message):
            return .once {
                guard await pause(Self.sightSeconds) else { return nil }
                return Event(.sighted(thread: thread, message: message, at: clock.now))
            }
        case let .marksWait(date):
            return .once {
                do { try await clock.sleep(until: date) } catch { return nil }
                return Event(.marksDue(at: clock.now))
            }
        case let .writeMark(thread, change):
            // The id is left out: the server builds it from the thread and
            // the person the token acts for, and answers the mark as it
            // stands, which for a late write is where another device left it.
            return .once {
                guard let client = await source.current() else { return Event(.markWriteFailed(thread: thread, sent: change, .later)) }
                do {
                    var properties = change.properties
                    properties["thread"] = .string(thread)
                    return Event(.markWritten(thread: thread, sent: change, try await client.create(Kinds.threadMark, properties: properties)))
                } catch {
                    let refusal = MarkRefusal(error)
                    // A pin or an archive refused for good is said.
                    let said = change.flags && refusal == .dropped ? failed(.flag, error, client) : nil
                    return Event(.markWriteFailed(thread: thread, sent: change, refusal, said))
                }
            }
        case let .page(plan, part, after):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    let modes = part == .archived ? Self.modes(plan) : ["chat"]
                    let page = try await ChatLists.threads(client, part, modes: modes, first: plan.page, after: after)
                    let marks = plan.readsMarks ? await Self.marks(client, threads: page.records.map(\.id), full: !plan.namesThreads) : nil
                    return Event(.pageRead(part: part, after: after, rows: page.records, cursor: page.cursor, marks: marks))
                } catch {
                    if Task.isCancelled { return nil }
                    let compacted = (error as? ServerError)?.isCompacted ?? false
                    return Event(.pageFailed(part: part, after: after, failed(.refresh, error, client), compacted: compacted))
                }
            }
        case let .readOlder(thread, before, epoch):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    let query = ChatLists.older(thread, before: before)
                    let page = try await client.list(query)
                    return Event(
                        .olderRowsRead(
                            thread: thread, epoch: epoch, before: before, rows: page.records.reversed(), more: page.records.count >= query.first))
                } catch {
                    return Task.isCancelled ? nil : Event(.olderRowsFailed(thread: thread, epoch: epoch))
                }
            }
        case let .search(plan, scene, query, threads):
            return .once {
                // Asked once the typing has paused: a search that follows
                // cancels this one while it waits.
                guard await pause(Self.searchPause), let client = await source.current() else { return nil }
                do {
                    return Event(try await Self.search(client, plan, scene: scene, query: query, threads: threads))
                } catch {
                    if Task.isCancelled { return nil }
                    return Event(
                        .searchFailed(scene: scene, query: query, failed(.search, error, client), unserved: Self.searchUnserved(error)))
                }
            }
        case let .undoWait(thread, date):
            return .once {
                do { try await clock.sleep(until: date) } catch { return nil }
                return Event(.undoPassed(thread))
            }
        case let .fetchThread(thread):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    return try await client.record(Kinds.thread, thread).map { Event(.threadFetched($0)) }
                } catch {
                    return Task.isCancelled ? nil : Event(.readFailed(failed(.refresh, error, client)))
                }
            }
        case let .rereadBehind(thread, mark):
            // One that fails says nothing: it is asked for again at the
            // next chance (`retryMarks`).
            return .once {
                guard let client = await source.current(), let record = try? await client.record(Kinds.thread, thread) else { return nil }
                return Event(.rowReread(thread: thread, record, mark: mark))
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
                            yield(.watchInterrupted(true))
                        case let .ended(end):
                            switch end {
                            case .signedOut: yield(.watchEnded(nil, Failure(.feed, .sessionEnded)))
                            case .needsApproval: yield(.watchEnded(nil, Failure(.feed, .needsApproval)))
                            case let .refused(words): yield(.watchEnded(words, nil))
                            }
                        }
                    }
                }
            }
        case let .feedWait(seconds):
            return .once { await pause(seconds) ? Event(.feedDue) : nil }
        case let .applyFeed(plan, batch, threads, named):
            return .once {
                guard let client = await source.current() else { return nil }
                return Event(.feedApplied(await Self.feed(client, plan, batch, threads: threads, named: named)))
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
                    return Event(.renameFailed(thread: thread, before: before, failed(.rename, error, client)))
                }
            }
        case let .setKey(provider, key):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    return Event(.keySet(try await client.patch(Kinds.provider, provider, properties: ["apiKey": .string(key)])))
                } catch {
                    return Event(.keyFailed(failed(.addKey, error, client)))
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
                    return Task.isCancelled ? nil : Event(.cardFailed(reference.id, failed(.card, error, client)))
                }
            }
        case let .writeCard(id, kind, properties, ifVersion):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    return Event(.cardWritten(id, try await client.patch(kind, id, properties: properties, ifVersion: ifVersion)))
                } catch {
                    return Event(.cardWriteFailed(id, failed(kind == Kinds.interaction ? .answer : .decide, error, client)))
                }
            }
        case let .decideUnread(id, accept, shown):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    guard let held = try await client.record(Kinds.request, id) else {
                        return Event(.cardWriteFailed(id, Failure(.decide, .http(status: 404, code: "not_found", reason: nil))))
                    }
                    if let diff = held["diff"]?["properties"]?.object, diff != shown {
                        // Not the change on screen any more: as a write over a
                        // version that moved is refused.
                        return Event(.cardWriteFailed(id, Failure(.decide, .http(status: 409, code: "conflict", reason: nil))))
                    }
                    let record = try await client.patch(
                        Kinds.request, id, properties: ["decision": .string(accept ? "accepted" : "rejected")],
                        ifVersion: held.version)
                    return Event(.cardWritten(id, record))
                } catch {
                    return Event(.cardWriteFailed(id, failed(.decide, error, client)))
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
                    return Task.isCancelled ? nil : Event(.guidance(.readFailed(failed(.guidance, error, client))))
                }
            }
        case let .writeGuidance(reference, properties, before):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    let record = try await client.patch(reference.kind, reference.recordID, properties: properties, ifVersion: before.version)
                    return Event(.guidance(.written(record)))
                } catch {
                    return Event(.guidance(.writeFailed(reference, failed(.guidanceChange, error, client), before: before)))
                }
            }
        case let .deleteGuidance(reference, before):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    try await client.delete(reference.kind, reference.recordID, ifVersion: before.version)
                    return Event(.guidance(.deleted(reference)))
                } catch {
                    return Event(.guidance(.writeFailed(reference, failed(.guidanceChange, error, client), before: before)))
                }
            }

        case let .notify(request):
            return Self.device(notifications) { await $0.schedule(request) }
        case let .notifyHeld(request, seconds):
            let fireAt = clock.now.addingTimeInterval(seconds)
            return Self.device(notifications) { notifications in
                var held = request
                held.fireAt = fireAt
                await notifications.schedule(held)
            }
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
        case let .backgroundRefresh(run, lease, plan, threads, beside):
            return .once {
                // Checked before it reads and again before its result can
                // write: a sign-out between lands nothing.
                guard storage.holds(lease), let client = await source.current() else {
                    return Event(.backgroundRefreshed(run, threads: nil, cards: nil))
                }
                let reading = { @Sendable () async -> ChatAppStep in
                    let read = try? await Self.threads(client, plan, beside: beside)
                    let cards = try? await Self.cards(client, plan, threads: Self.cardThreads(plan, read, held: threads))
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

    /// - Parameter beside: threads held that the lists may not answer (one
    ///   past the first page, one a conversation is on, one that just left
    ///   its list): each such is read on its own, with the person's mark of
    ///   it, so the row held is never left as it was.
    static func threads(_ client: SubstrateClient, _ plan: ChatAppAsk.Plan, beside: [String] = []) async throws -> ChatAppStep.Threads {
        try await threadPages(client, plan, beside: beside).threads
    }

    private static func threadPages(_ client: SubstrateClient, _ plan: ChatAppAsk.Plan, beside: [String] = []) async throws
        -> (threads: ChatAppStep.Threads, page: RecordPage)
    {
        // Where the server keeps the flags, the list is the chats not
        // archived, and the pinned ones and the ones that wait on a person
        // are read whole beside it: each may be older than the list's page,
        // and one that waits may be archived. A server that turns out not
        // to know the filter arm (`400`) is read as one that keeps none.
        var chats: RecordPage
        var flagged = plan.flags
        do {
            chats = try await ChatLists.threads(client, flagged ? .kept : .every, modes: ["chat"], first: plan.page)
        } catch let error as ServerError where flagged && error.status == 400 {
            flagged = false
            chats = try await ChatLists.threads(client, .every, modes: ["chat"], first: plan.page)
        }
        var automated: [Record] = []
        if plan.showAutomated {
            let modes = conversationModes.filter { $0 != "chat" }
            automated = try await ChatLists.threads(client, flagged ? .kept : .every, modes: modes, first: 50).records
        }
        var threads = ChatAppStep.Threads(chats: chats.records, automated: automated, cursor: chats.cursor)
        threads.flagsUnserved = plan.flags && !flagged
        if flagged {
            let pinned = try await ChatLists.all(client, .pinned, modes: modes(plan), page: plan.page)
            let waiting = try await ChatLists.all(client, .waiting, modes: modes(plan), page: plan.page)
            (threads.pinned, threads.waiting) = (pinned.records, waiting.records)
            // The watch opens from the oldest head of the reads.
            chats.head = min(chats.head, pinned.head, waiting.head)
            if plan.archived {
                let archived = try await ChatLists.threads(client, .archived, modes: modes(plan), first: plan.page)
                (threads.archived, threads.archivedCursor) = (archived.records, archived.cursor)
                chats.head = min(chats.head, archived.head)
            }
        }
        var seen: Set<String> = []
        var ids = (threads.chats + threads.automated + (threads.pinned ?? []) + (threads.waiting ?? []) + (threads.archived ?? []))
            .map(\.id).filter { seen.insert($0).inserted }
        for id in beside where seen.insert(id).inserted {
            if let row = try await client.record(Kinds.thread, id) { threads.named.append(row) } else { threads.gone.append(id) }
            ids.append(id)
        }
        if plan.readsMarks {
            threads.marks = await marks(client, threads: ids, full: !plan.namesThreads)
            // The marks are read after the threads. A mark taken since a
            // thread's row last settled, of a message that is not the row's
            // newest, may read through a reply that settled between the
            // two reads: against the older row it would look unread. The
            // threads are read once more, so the rows are the newer.
            if let marks = threads.marks, outran(marks, threads) {
                var plain = plan
                plain.readsMarks = false
                if var again = try? await threadPages(client, plain, beside: beside) {
                    again.threads.marks = marks
                    return again
                }
            }
        }
        return (threads, chats)
    }

    /// Whether a mark's position was taken after its thread's row settled
    /// and names another message than the row's newest.
    static func outran(_ marks: ChatAppStep.Marks, _ threads: ChatAppStep.Threads) -> Bool {
        let listed = threads.chats + threads.automated + (threads.pinned ?? []) + (threads.waiting ?? []) + (threads.archived ?? [])
        let rows = Dictionary(listed.map { ($0.id, $0) }) { a, _ in a }
        return marks.records.contains { mark in
            guard let id = mark["thread"]?.referenceID, let row = rows[id], let read = mark["readThrough"]?.referenceID,
                let last = row["lastMessage"]?.referenceID, read != last,
                let at = mark["readAt"]?.string.flatMap(Timestamp.parse),
                let settled = row["finishedAt"]?.string.flatMap(Timestamp.parse)
            else { return false }
            return at > settled
        }
    }

    /// The person's own marks of `threads`, a hundred threads a request:
    /// `me` is the person the token acts for, so the app never learns its
    /// actor and another person's marks of the same threads never come
    /// back. Nil where the read failed. A server that does not know the
    /// kind answers 404 (it refuses `me` for no kind it knows), which reads
    /// as a server that keeps no marks; an app's token is answered 404 for
    /// a thread it does not reach as well, which is a read that failed.
    static func marks(_ client: SubstrateClient, threads: [String], full: Bool = true) async -> ChatAppStep.Marks? {
        var out = ChatAppStep.Marks(threads: threads, records: [])
        do {
            for start in stride(from: 0, to: threads.count, by: 100) {
                let ids = threads[start ..< min(start + 100, threads.count)].map(JSONValue.string)
                out.records += try await client.list(
                    ListQuery(kinds: [Kinds.threadMark], properties: ["thread": ["in": .array(ids)], "readBy": ["eq": "me"]], first: 100)
                ).records
            }
            return out
        } catch let error as ServerError where error.status == 404 && (full || error.message.contains("unknown kind")) {
            return ChatAppStep.Marks(threads: [], records: [], unserved: true)
        } catch {
            return nil
        }
    }

    /// The threads a read of the cards names. On a server that counts what
    /// waits on each thread's row: of the list just read, the threads that
    /// say something does, and of the ones held beside it (a chat opened
    /// from further back), those that said so when asked. Else the list
    /// read, or the threads held where it failed.
    static func cardThreads(_ plan: ChatAppAsk.Plan, _ read: ChatAppStep.Threads?, held: [String]) -> [String] {
        guard let read else { return held }
        var seen: Set<String> = []
        let listed = (read.chats + read.automated + (read.waiting ?? [])).filter { seen.insert($0.id).inserted }
        guard plan.readsMarks else { return listed.map(\.id) }
        let ids = Set(listed.map(\.id))
        return listed.filter { ChatThread(record: $0).waiting > 0 }.map(\.id) + held.filter { !ids.contains($0) }
    }

    /// The modes the lists read beside the chats hold.
    static func modes(_ plan: ChatAppAsk.Plan) -> [String] { plan.showAutomated ? conversationModes : ["chat"] }

    /// A window's search of the messages: the hits, somebody's words alone
    /// (a tool's output is no message), the rows of the chats they are in
    /// where no list holds them, and the person's marks of those chats,
    /// which say the archived ones. A query the grammar finds no word in
    /// (`422`) found nothing.
    static func search(_ client: SubstrateClient, _ plan: ChatAppAsk.Plan, scene: String, query: String, threads held: [String]) async throws
        -> ChatAppStep
    {
        var hits: [Record]
        do {
            hits = plan.namesThreads ? try await ChatLists.search(client, query, threads: held) : try await ChatLists.search(client, query)
        } catch let error as ServerError where error.status == 422 {
            hits = []
        }
        hits = hits.filter { ["user", "assistant"].contains($0["role"]?.string ?? "") && $0["thread"]?.referenceID != nil }
        var seen: Set<String> = []
        let ids = hits.compactMap { $0["thread"]?.referenceID }.filter { seen.insert($0).inserted }
        let known = Set(held)
        var rows: [Record] = []
        for id in ids.filter({ !known.contains($0) }).prefix(searchThreads) {
            if let row = try? await client.record(Kinds.thread, id) { rows.append(row) }
        }
        let marks = plan.readsMarks && !ids.isEmpty ? await Self.marks(client, threads: ids, full: !plan.namesThreads) : nil
        return .searchRead(scene: scene, query: query, hits: hits, threads: rows, marks: marks)
    }

    /// How many chats a search reads the rows of, of those no list holds.
    static let searchThreads = 12

    /// Whether the server answered that it takes no search from this
    /// sign-in: an app's token reads messages thread by thread (`403`), and
    /// a server older than an arm refuses it (`400`).
    static func searchUnserved(_ error: any Error) -> Bool {
        if error is InsufficientScope { return true }
        guard let error = error as? ServerError else { return false }
        return [400, 403, 404, 501].contains(error.status)
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
    /// read is no failure: a server older than the kind holds none. On a
    /// server that counts what waits on each thread's row, `threads` are
    /// the ones that say something does, and with none nothing is asked.
    static func cards(_ client: SubstrateClient, _ plan: ChatAppAsk.Plan, threads: [String]) async throws -> ChatAppStep.Cards {
        guard plan.canChat else { return ChatAppStep.Cards(interactions: [], requests: []) }
        let named = plan.namesThreads || plan.readsMarks
        @Sendable func read(_ kind: String, _ condition: [String: JSONValue]) async throws -> [Record] {
            guard named else {
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
        let asksApprovals = !(named && threads.isEmpty)
        async let approvals: [Record]? = asksApprovals ? try? read(Kinds.approval, ["state": ["eq": "pending"]]) : nil
        return try await ChatAppStep.Cards(interactions: interactions, requests: requests, approvals: approvals)
    }

    /// Everything the list draws. The watch resumes from the earliest head
    /// of the reads in one history, so nothing written between them is
    /// missed; an app token's pages carry none, and its watch opens at the
    /// live head and reads the lists again after the bookmark.
    static func refresh(_ client: SubstrateClient, _ plan: ChatAppAsk.Plan, held: [String] = [], beside: [String] = []) async throws
        -> ChatAppStep.Refresh
    {
        async let lists = agents(client, plan)
        async let threadRead = threadPages(client, plan, beside: beside)
        async let early: ChatAppStep.Cards? = plan.namesThreads || plan.readsMarks ? nil : cards(client, plan, threads: [])
        let (read, (threads, page), cardsEarly) = try await (lists, threadRead, early)
        let cards: ChatAppStep.Cards
        if let cardsEarly {
            cards = cardsEarly
        } else {
            cards = try await self.cards(client, plan, threads: cardThreads(plan, threads, held: held))
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
    static func feed(
        _ client: SubstrateClient, _ plan: ChatAppAsk.Plan, _ batch: Set<ChatAppState.WatchKey>, threads: [String], named: [String] = []
    ) async
        -> ChatAppStep.Feed
    {
        var out = ChatAppStep.Feed(failed: [])
        let kinds = Set(batch.map(\.kind))
        var known = threads
        // A mark is read with the threads it is compared with: one that
        // reads through a reply this device has not heard of yet would
        // otherwise look unread.
        let listKinds: Set<String> = [Kinds.thread, Kinds.threadMark]
        if !kinds.isDisjoint(with: listKinds) {
            do {
                let read = try await self.threads(client, plan, beside: named)
                out.threads = read
                known = cardThreads(plan, read, held: threads)
                // The marks that could not be read are asked for again.
                if plan.readsMarks, read.marks == nil { out.failed.formUnion(batch.filter { $0.kind == Kinds.threadMark }) }
            } catch {
                out.failed.formUnion(batch.filter { listKinds.contains($0.kind) })
                out.failure = Failure(.feed, error, server: client.server)
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
                out.failure = Failure(.feed, error, server: client.server)
            }
        }
        let cardKinds: Set<String> = [Kinds.interaction, Kinds.request, Kinds.approval]
        if !kinds.isDisjoint(with: cardKinds) {
            do {
                out.cards = try await cards(client, plan, threads: known)
            } catch {
                out.failed.formUnion(batch.filter { cardKinds.contains($0.kind) })
                out.failure = Failure(.feed, error, server: client.server)
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
