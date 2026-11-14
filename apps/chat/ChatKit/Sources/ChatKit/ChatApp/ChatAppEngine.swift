import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime

/// The chat list's engine: each event's step, with what it asks for written
/// into the state's `asks`.
extension ChatApp {
    public func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        next.asks = []
        next.restarted = false
        switch event.step {
        case let .linked(link): linked(&next, link)
        case let .diskOpened(disk): opened(&next, disk)
        case let .permissionRead(permission): next.permission = permission
        case let .usageRead(usage): next.usage = usage
        case .transcriptsCleared:
            if let disk = next.disk { next.asks.append(.readUsage(disk)) }
        case let .backgroundRun(run): backgroundRun(&next, run)
        case let .backgroundRefreshed(run, threads, cards):
            // Only the run under way: one taken back, replaced, or asked
            // for by another session lands nothing.
            guard next.backgroundRun?.id == run.id else { break }
            next.backgroundRun = nil
            if let disk = next.disk, disk.scheduled(run) {
                if let threads { apply(&next, threads: threads) }
                if let cards { apply(&next, cards: cards) }
            }
            next.asks.append(.finishRun(run, success: threads != nil))
        case let .backgroundExpired(id):
            if next.waitingRun?.id == id { next.waitingRun = nil }
            guard let run = next.backgroundRun, run.id == id else { break }
            next.backgroundRun = nil
            next.asks += [.cancel("background"), .finishRun(run, success: false)]
        case let .continued(key, update):
            if update == .ended { next.conversations[key]?.continuing = nil }
        case let .phase(phase): self.phase(&next, phase)

        case .refreshAsked: refresh(&next)
        case let .refreshed(refresh): refreshed(&next, refresh)
        case let .refreshFailed(failure):
            next.refreshing = false
            next.refreshError = failure.report == nil ? failure.words : nil
        case let .threadsRead(threads):
            apply(&next, threads: threads)
            afterList(&next)
        case let .agentsRead(agents, providers, models, keys):
            next.agents = agents
            if let providers { next.providers = Dictionary(providers.map { ($0.id, $0) }) { a, _ in a } }
            if let models { next.models = Self.byID(models) }
            if let keys { next.keys = keys }
        case let .cardsRead(cards):
            apply(&next, cards: cards)
        case .readFailed:
            break
        case let .rowsFailed(thread, epoch, _):
            // The read is no longer outstanding: the next asks again.
            if next.readEpochs[thread, default: 0] == epoch { next.reading[thread] = nil }
        case let .openingsRead(openings):
            next.openings.merge(openings) { _, new in new }
        case let .rowsRead(thread, epoch, version, rows, full):
            if next.readEpochs[thread, default: 0] == epoch { next.reading[thread] = nil }
            guard next.readEpochs[thread, default: 0] == epoch, !next.owned(thread) else { break }
            publish(&next, thread, rows, version: version, full: full)
        case let .cachedLoaded(thread, epoch, cached):
            // Rules 3 and 4, as for a read: what the disk held is older than
            // a send, an attach, a settle or a binding since, and never
            // lands under a live overlay.
            guard next.readEpochs[thread, default: 0] == epoch, !next.owned(thread) else { break }
            if let cached, next.transcripts[thread] == nil {
                next.transcripts[thread] = State.Held(
                    records: cached.records, turns: transcript(of: cached.records),
                    version: cached.threadVersion, full: cached.full)
            }
            // What a conversation shows is read where the disk held none, or
            // held rows that are not current.
            if next.key(of: thread) != nil { load(&next, thread) }
        case let .prefetched(thread, epoch, version, rows):
            guard next.readEpochs[thread, default: 0] == epoch, !next.owned(thread) else { break }
            publish(&next, thread, rows, version: version, full: rows.count >= 100)
        case let .threadFetched(record):
            upsert(&next, record)

        case let .watchOpened(fresh):
            next.feedProblem = nil
            // Opened at the live head with no list read under it (an app
            // token's lists carry none): what was written before the
            // bookmark is read again now.
            if fresh {
                next.feedBatch.formUnion([
                    .init(kind: Kinds.thread, id: ""), .init(kind: Kinds.agent, id: ""),
                    .init(kind: Kinds.interaction, id: ""),
                ])
                next.asks.append(.feedWait(seconds: 0))
            }
        case let .watchRow(row):
            next.feedBatch.insert(.init(kind: row.kind, id: row.recordID))
            // An approval moves under the engine once decided (running,
            // then done or failed), and the pending list no longer holds it:
            // a card on screen reads it again.
            if row.kind == Kinds.approval, next.cards[row.recordID] != nil {
                next.asks.append(.readCard(.approval(row.recordID), target: false))
            }
            // Only the wait restarts, never a batch being read.
            next.asks.append(.feedWait(seconds: 0.3))
        case let .watchRelist(at):
            next.watching = false
            guard WatchFollower.recovers(at: at, after: state.recoveryStarted) else {
                next.feedProblem = "the server could not resume the chats' history even after reading them again"
                break
            }
            next.recoveryStarted = at
            refresh(&next)
        case let .watchEnded(words, failure):
            next.watching = false
            if failure?.report == nil { next.feedProblem = words }
        case .feedDue: drain(&next)
        case let .feedApplied(feed): fed(&next, feed)

        case let .sceneAppeared(scene, restoring, key):
            next.scenes[scene] = next.scenes[scene] ?? State.Scene()
            if let restoring, next.scenes[scene]?.current == nil, next.thread(restoring) != nil { show(&next, restoring, in: scene, key: key) }
        case let .sceneActive(scene, active):
            next.scenes[scene, default: State.Scene()].active = active
        case let .sceneGone(scene):
            next.scenes[scene] = nil
            release(&next)
        case let .show(thread, scene, key):
            show(&next, thread, in: scene, key: key)
            release(&next)
        case let .newChat(agent, listen, scene, key):
            newChat(&next, agent: agent, listen: listen, in: scene, key: key)
            release(&next)
        case let .back(scene):
            next.scenes[scene, default: State.Scene()].current = nil
            release(&next)
        case let .searched(scene, query): next.scenes[scene, default: State.Scene()].query = query
        case let .routed(route): next.route = route
        case let .routeClaimed(scene, key):
            guard let route = next.route else { break }
            next.route = nil
            switch route {
            case let .thread(thread): show(&next, thread, in: scene, key: key)
            case let .newChat(agent, listen): newChat(&next, agent: agent, listen: listen, in: scene, key: key)
            }
            release(&next)

        case let .markedRead(thread):
            markSeen(&next, thread)
        case let .markedUnread(thread):
            next.seen[thread] = nil
        case let .renameAsked(thread, subject):
            // Optimistic: the list shows it at once, and goes back if the
            // server refuses.
            let before = next.thread(thread)?.record
            setSubject(&next, thread, subject)
            next.renameProblem = nil
            next.asks.append(.rename(thread: thread, subject: subject, before: before))
        case let .renamed(record): upsert(&next, record)
        case let .renameFailed(thread, before, failure):
            if let before { replaceRow(&next, before) } else { setSubject(&next, thread, nil) }
            next.renameProblem = failure.words
        case let .keyAsked(provider, key):
            next.keyProblem = nil
            next.asks.append(.setKey(provider: provider, key: key))
        case let .keySet(record):
            next.providers[record.id] = record
            switch next.keys {
            case var .read(resolved):
                resolved[record.id] = true
                next.keys = .read(resolved)
            case .served(var resolved, let models):
                resolved[record.id] = true
                next.keys = .served(resolved, models: models)
            case .rows, nil: break
            }
            next.keySaved.raise(record.id)
        case let .keyFailed(failure): next.keyProblem = failure.words

        case let .cardAsked(reference):
            let target = (reference.isSuggestion && next.cards[reference.id]?.target == nil)
            next.asks.append(.readCard(reference, target: target))
        case let .cardRead(id, record, target):
            var card = next.cards[id] ?? State.Card()
            card.record = record ?? card.record
            card.target = target ?? card.target
            card.problem = nil
            next.cards[id] = card
        case let .cardFailed(id, failure):
            if failure.report == nil { next.cards[id, default: State.Card()].problem = failure.words }
        case let .answerAsked(id, picks):
            guard let record = next.cards[id]?.record else { break }
            next.cards[id]?.busy = "answer"
            next.cards[id]?.problem = nil
            let questions = (record["questions"]?.array ?? []).compactMap { $0["id"]?.text }
            let answers: [JSONValue] = questions.map { q in
                ["question": .string(q), "selected": .array((picks[q] ?? []).map(JSONValue.string))]
            }
            next.asks.append(
                .writeCard(
                    id, kind: Kinds.interaction, properties: ["state": "answered", "answers": .array(answers)],
                    ifVersion: record.version))
        case let .questionsDismissed(id):
            guard let record = next.cards[id]?.record else { break }
            next.cards[id]?.busy = "dismiss"
            next.cards[id]?.problem = nil
            next.asks.append(.writeCard(id, kind: Kinds.interaction, properties: ["state": "dismissed"], ifVersion: record.version))
        case let .decideAsked(id, accept, shown):
            next.cards[id, default: State.Card()].busy = accept ? "accept" : "reject"
            next.cards[id]?.problem = nil
            // The version decided on is the one whose change is on screen:
            // read now when the card holds none.
            if let held = next.cards[id]?.record ?? next.requests.first(where: { $0.id == id }) {
                next.asks.append(
                    .writeCard(
                        id, kind: Kinds.request, properties: ["decision": .string(accept ? "accepted" : "rejected")],
                        ifVersion: held.version))
            } else {
                next.asks.append(.decideUnread(id, accept: accept, shown: shown))
            }
        case let .approveAsked(id, approve):
            // Decided on the version the owner read: one moved since is
            // refused, and read again.
            guard let held = next.cards[id]?.record ?? next.approvals.first(where: { $0.id == id }) else { break }
            next.cards[id, default: State.Card()].busy = approve ? "approve" : "decline"
            next.cards[id]?.problem = nil
            next.asks.append(
                .writeCard(id, kind: Kinds.approval, properties: ["state": .string(approve ? "approved" : "denied")], ifVersion: held.version))
        case let .cardWritten(id, record):
            next.cards[id, default: State.Card()].record = record
            next.cards[id]?.busy = nil
            // The resolution wrote into the thread and resumed the agent.
            next.asks += [
                .cards(next.plan, threads: next.threadIDs),
                .applyFeed(
                    next.plan, [.init(kind: Kinds.thread, id: "")],
                    threads: next.threadIDs),
            ]
            // An approved call runs after the decision, under the engine:
            // the card follows it to its end.
            if record.kind == Kinds.approval, record["state"]?.string == "approved" {
                next.asks.append(.readCard(.approval(id), target: false))
            }
        case let .cardWriteFailed(id, failure, conflict):
            next.cards[id, default: State.Card()].busy = nil
            if failure.report == nil { next.cards[id]?.problem = failure.status == 409 ? conflict : failure.words }
            if let reference = next.reference(of: id) { next.asks.append(.readCard(reference, target: false)) }
        case let .setting(row, toggle, choice): setting(&next, row, toggle, choice)
        case .settingsOpened:
            next.asks.append(.readPermission)
            if let disk = next.disk { next.asks.append(.readUsage(disk)) }
            guidance(&next, .asked)
        case let .guidance(step): guidance(&next, step)
        case .clearAsked:
            guard let disk = next.disk else { break }
            for thread in Array(next.transcripts.keys) where !next.owned(thread) {
                next.transcripts[thread] = nil
            }
            next.asks.append(.clearTranscripts(disk))
            // What is on screen is read again rather than left empty.
            for thread in next.visibleThreads { load(&next, thread, force: true) }
        case let .forget(forget): next.forgetOnSignOut = forget
        case let .conversation(key, report): reported(&next, key, report, before: state)
        case let .notificationOpened(response):
            // A tap opens its chat, in the window that takes the route.
            if next.session != nil, response.action == .open, let thread = response.userInfo[ChatNotifications.threadKey] {
                next.route = .thread(thread)
            }
        }
        // What came on screen is read.
        for thread in next.visibleThreads.subtracting(state.visibleThreads) { markSeen(&next, thread) }
        return next
    }

    // MARK: The session

    /// Rule 6: a sign-out, or a sign-in to another repository, starts the
    /// chats over: the runtime drops every result still on its way and
    /// closes every conversation, and nothing of one session publishes into
    /// the next. The folder is closed, and deleted on a Sign Out.
    private func linked(_ state: inout State, _ link: LinkSnapshot) {
        let session = ChatSession(link)
        let online = link.online ?? true
        if session?.identity != state.session?.identity {
            let forget = state.forgetOnSignOut
            let old = state.session
            state = State()
            state.restarted = true
            if let old {
                state.asks.append(.closeDisk(old.identity, deleting: forget))
                if forget { state.asks.append(.cancelAllNotifications) }
            }
            guard let session else { return }
            state.session = session
            state.online = online
            state.accessEnded = link.phase == .accessEnded
            state.asks += [.openDisk(session.identity), .followRuns, .followResponses]
            return
        }
        state.session = session
        let ended = link.phase == .accessEnded
        if ended, !state.accessEnded {
            state.watching = false
            state.asks.append(.cancel("watch"))
        }
        state.accessEnded = ended
        let cameOnline = online && !state.online
        state.online = online
        if cameOnline, !ended {
            refresh(&state)
        } else if !ended, !state.accessEnded, state.opened, !state.watching, state.appActive || !pausesWhenAway {
            watch(&state)
        }
    }

    private func opened(_ state: inout State, _ disk: ChatAppStep.Disk) {
        guard disk.lease.identity == state.session?.identity else { return }
        state.disk = disk.lease
        state.asks.append(.prune(disk.lease))
        state.preferences = disk.preferences
        state.drafts = disk.drafts
        if let cached = disk.state {
            state.agents = cached.agents
            state.providers = Dictionary(cached.providers.map { ($0.id, $0) }) { a, _ in a }
            state.models = cached.models.map(Self.byID)
            state.keys = cached.keys
            state.chats = cached.threads
            state.automated = cached.automated
            state.openings = cached.openings
            state.interactions = cached.interactions
            state.requests = cached.requests
            state.seen = cached.seen
            state.notified = cached.notified
            state.baselined = cached.baselined
            state.lastAgent = cached.lastAgent
            if let memories = cached.memories, let instructions = cached.instructions {
                state.guidance.memories = memories
                state.guidance.instructions = instructions
                state.guidance.read = true
            }
            state.loaded = true
        }
        state.feedBatch.formUnion(disk.stale)
        refresh(&state)
        // The windows that drew before the folder was read take their chats now.
        for thread in state.visibleThreads where state.transcripts[thread] == nil {
            state.asks.append(.loadCached(disk.lease, thread: thread, epoch: state.readEpochs[thread, default: 0]))
        }
        if let run = state.waitingRun {
            state.waitingRun = nil
            backgroundRun(&state, run)
        }
    }

    // MARK: Background work

    /// A run of the refresh job: it reads the list and the cards and
    /// notifies what settled or appeared, if the job was scheduled for
    /// this repository under the generation its folder holds. A job from
    /// before a sign-out does nothing.
    private func backgroundRun(_ state: inout State, _ run: BackgroundRun) {
        guard state.session != nil else {
            state.asks.append(.finishRun(run, success: true))
            return
        }
        guard let disk = state.disk else {
            if let earlier = state.waitingRun { state.asks.append(.finishRun(earlier, success: false)) }
            state.waitingRun = run
            return
        }
        state.asks.append(.scheduleRefresh(disk))
        guard disk.scheduled(run) else {
            state.asks.append(.finishRun(run, success: true))
            return
        }
        // The refresh under way is cancelled by this one's key.
        if let earlier = state.backgroundRun { state.asks.append(.finishRun(earlier, success: false)) }
        state.backgroundRun = run
        state.asks.append(.backgroundRefresh(run, disk, state.plan, threads: state.threadIDs))
    }

    private func phase(_ state: inout State, _ phase: AppPhase) {
        switch phase {
        case .active:
            state.appActive = true
            guard state.session != nil else { break }
            refresh(&state)
            for key in state.conversations.keys { state.conversations[key]?.resumeNudge += 1 }
        case .inactive:
            // Asked for while the app is still in front, as iOS requires of
            // a continued processing task: inactive comes before background.
            for (key, entry) in state.conversations where entry.streaming && entry.continuing == nil {
                let title = entry.thread.flatMap { state.thread($0) }.map(state.title) ?? "New chat"
                state.conversations[key]?.continuing = key.rawValue
                state.asks.append(.continueRun(key, ContinuedProcessing(id: key.rawValue, title: "Waiting for \(entry.name)", subtitle: title)))
            }
        case .background:
            state.appActive = false
            if let disk = state.disk { state.asks.append(.scheduleRefresh(disk)) }
            state.asks.append(.badge(state.unreadCount))
            if pausesWhenAway {
                state.watching = false
                state.asks.append(.cancel("watch"))
            }
        }
    }

    // MARK: Reading the list

    private func refresh(_ state: inout State) {
        guard state.session != nil, state.opened, !state.accessEnded, state.online else { return }
        state.refreshing = true
        state.asks.append(.refresh(state.plan))
    }

    private func refreshed(_ state: inout State, _ refresh: ChatAppStep.Refresh) {
        state.refreshing = false
        state.refreshError = nil
        state.agents = refresh.agents
        if let providers = refresh.providers { state.providers = Dictionary(providers.map { ($0.id, $0) }) { a, _ in a } }
        if let models = refresh.models { state.models = Self.byID(models) }
        if let keys = refresh.keys { state.keys = keys }
        apply(&state, threads: refresh.threads)
        apply(&state, cards: refresh.cards)
        // Everything the first read found is history: from here on, what
        // settles or asks is news.
        state.baselined = true
        state.loaded = true
        if !state.watching { watch(&state, head: refresh.head) }
        afterList(&state)
    }

    private func watch(_ state: inout State, head: WatchCursor? = nil) {
        guard let disk = state.disk, state.online, state.appActive || !pausesWhenAway else { return }
        state.watching = true
        state.asks.append(.watch(disk, head: head, kinds: ChatApp.watched(state.plan, approvals: state.approvalsKnown)))
    }

    /// After a list: the opening lines it lacks, then the chats to read
    /// ahead.
    private func afterList(_ state: inout State) {
        let missing = state.threads.filter { $0.subject == nil && state.openings[$0.id] == nil }.map(\.id)
        if !missing.isEmpty { state.asks.append(.openings(missing)) }
        prefetch(&state)
    }

    /// The most recent settled chats whose rows are not current, read one
    /// at a time while nothing streams, so opening any of them is instant.
    private func prefetch(_ state: inout State) {
        guard let disk = state.disk, state.online, state.appActive,
            !state.conversations.values.contains(where: \.streaming)
        else { return }
        var items: [ChatAppAsk.Prefetch] = []
        for thread in state.threads where items.count < 8 && !thread.running {
            if state.isPrefetched(thread.id) || state.owned(thread.id) { continue }
            items.append(
                .init(
                    thread: thread.id, version: thread.record.version, epoch: state.readEpochs[thread.id, default: 0],
                    held: state.transcripts[thread.id] != nil))
        }
        if !items.isEmpty { state.asks.append(.prefetch(disk, items)) }
    }

    /// A new list: an older row never replaces a newer one, what settled
    /// since notifies, and on the first sign-in all of it is history.
    func apply(_ state: inout State, threads: ChatAppStep.Threads) {
        let before = Dictionary((state.chats + state.automated).map { ($0.id, $0) }) { a, _ in a }
        let newer = { (record: Record) -> Record in
            if let held = before[record.id], held.version > record.version { return held }
            return record
        }
        state.chats = threads.chats.map(newer)
        state.automated = threads.automated.map(newer)
        if !state.baselined {
            for record in state.chats + state.automated {
                if let finished = record["finishedAt"]?.string {
                    state.seen[record.id] = finished
                    state.notified.insert("reply|\(record.id)|\(finished)")
                }
            }
        }
        for record in state.chats + state.automated where before[record.id]?.version != record.version {
            threadMoved(&state, record)
        }
    }

    /// One thread row, read on its own: a handover, a poll, a rename.
    private func upsert(_ state: inout State, _ record: Record) {
        if let held = state.thread(record.id)?.record, held.version > record.version { return }
        replaceRow(&state, record)
        threadMoved(&state, record)
    }

    private func replaceRow(_ state: inout State, _ record: Record) {
        let mode = record["mode"]?.string
        if mode == "chat" {
            if let i = state.chats.firstIndex(where: { $0.id == record.id }) { state.chats[i] = record } else { state.chats.insert(record, at: 0) }
            state.others[record.id] = nil
        } else if conversationModes.contains(mode ?? ""), state.preferences.showAutomated {
            if let i = state.automated.firstIndex(where: { $0.id == record.id }) {
                state.automated[i] = record
            } else {
                state.automated.insert(record, at: 0)
            }
            state.others[record.id] = nil
        } else {
            state.others[record.id] = record
        }
    }

    private func setSubject(_ state: inout State, _ thread: String, _ subject: String?) {
        for i in state.chats.indices where state.chats[i].id == thread { state.chats[i].properties["subject"] = subject.map(JSONValue.string) }
        for i in state.automated.indices where state.automated[i].id == thread {
            state.automated[i].properties["subject"] = subject.map(JSONValue.string)
        }
    }

    /// A thread's row moved: a reply that settled where nobody sees it
    /// notifies once, and a chat with a conversation and nothing running is
    /// read again where the rules allow. Its conversation is told by the
    /// value the list hands it.
    private func threadMoved(_ state: inout State, _ record: Record) {
        let thread = ChatThread(record: record)
        if !thread.running, let finished = record["finishedAt"]?.string {
            if state.visibleThreads.contains(thread.id) { markSeen(&state, thread.id) }
            let key = "reply|\(thread.id)|\(finished)"
            if !state.notified.contains(key) {
                state.notified.insert(key)
                // A stop is the owner's own act; an error says so rather
                // than offering the turn before it as the answer.
                if state.baselined, state.preferences.notifyReplies, !thread.automated, !state.visibleThreads.contains(thread.id),
                    thread.status != "stopped", thread.status == "error" || record["lastReply"]?.text != nil
                {
                    state.asks.append(
                        .notify(
                            ChatNotifications.reply(
                                thread: thread, agent: agentName(thread.agentID ?? ""),
                                title: state.title(thread))))
                }
            }
        }
        if !thread.running, state.key(of: thread.id) != nil { load(&state, thread.id) }
    }

    func apply(_ state: inout State, cards: ChatAppStep.Cards) {
        state.interactions = cards.interactions
        state.requests = cards.requests
        state.approvals = cards.approvals ?? []
        // The first answer of the approvals read: the watch opens again,
        // from its bookmark, following the kind too.
        if cards.approvals != nil, !state.approvalsKnown {
            state.approvalsKnown = true
            if state.watching { watch(&state) }
        }
        // A card read before keeps the newer of the two: a list read that
        // started before the owner decided must not offer the buttons again.
        for record in state.approvals where (state.cards[record.id]?.record?.version ?? .max) < record.version {
            state.cards[record.id]?.record = record
        }
        for record in cards.interactions + cards.requests + state.approvals {
            let key = "card|\(record.kind)/\(record.id)"
            guard !state.notified.contains(key) else { continue }
            state.notified.insert(key)
            let thread = record["thread"]?.referenceID
            guard state.baselined, state.preferences.notifyAttention, !(thread.map(state.visibleThreads.contains) ?? false) else { continue }
            let row = thread.flatMap { state.thread($0) }
            // An approval names the agent that asked, a sub-agent included.
            let asker = record.kind == Kinds.approval ? referent(record["agent"], pin: Kinds.agent) : nil
            let who = agentName(asker ?? row?.agentID ?? record["proposer"]?.string ?? "Your agent")
            state.asks.append(
                .notify(
                    ChatNotifications.attention(
                        record: record, thread: thread, agent: who,
                        title: row.map(state.title) ?? "A chat")))
        }
    }

    // MARK: Transcripts

    /// Rule 4: a read, a cached read or a prefetch is dropped when the
    /// thread's epoch moved since it started. A binding, a send or an
    /// attach, and a settle move it (`reported`), and cancel the read in
    /// flight.
    private func bump(_ state: inout State, _ thread: String) {
        state.readEpochs[thread, default: 0] += 1
        state.reading[thread] = nil
        state.asks.append(.cancel("read:\(thread)"))
    }

    /// Reads a thread's rows unless they are current (rule 7) or a
    /// conversation owns the thread (rule 3). A read under an older thread
    /// version than the list now holds is replaced.
    private func load(_ state: inout State, _ thread: String, force: Bool = false) {
        guard state.session != nil, !state.owned(thread), state.online else { return }
        if state.thread(thread)?.running == true, state.key(of: thread) != nil { return }
        let version = state.thread(thread)?.record.version ?? 0
        if let reading = state.reading[thread] {
            if reading >= version { return }
            bump(&state, thread)
        }
        if !force, state.isCurrent(thread) { return }
        state.reading[thread] = version
        state.asks.append(.read(thread: thread, first: 500, epoch: state.readEpochs[thread, default: 0], version: version))
    }

    /// Puts rows on screen and on disk; rows read under an older thread
    /// version than those held are stale and dropped.
    private func publish(_ state: inout State, _ thread: String, _ rows: [Record], version: Int, full: Bool) {
        if let held = state.transcripts[thread], held.version > version { return }
        state.transcripts[thread] = State.Held(records: rows, turns: transcript(of: rows), version: version, full: full)
        if let disk = state.disk {
            state.asks.append(.saveTranscript(disk, thread: thread, CachedTranscript(threadVersion: version, full: full, records: rows, openedAt: nil)))
        }
    }

    // MARK: The watch

    /// A batch of rows re-reads what they touched, once, a moment after the
    /// last of them. Batches apply one at a time; what failed to re-read is
    /// kept for the next, and the record cache keeps the rows not yet read
    /// again, so a relaunch never skips one.
    private func drain(_ state: inout State) {
        guard state.feedApplying == nil, !state.feedBatch.isEmpty, state.session != nil else { return }
        let batch = state.feedBatch
        state.feedBatch = []
        state.feedApplying = batch
        if batch.contains(where: { $0.kind == Kinds.message }) {
            for key in state.conversations.keys { state.conversations[key]?.pollNudge += 1 }
        }
        state.asks.append(.applyFeed(state.plan, batch, threads: state.threadIDs))
    }

    private func fed(_ state: inout State, _ feed: ChatAppStep.Feed) {
        let batch = state.feedApplying ?? []
        state.feedApplying = nil
        if let threads = feed.threads {
            apply(&state, threads: threads)
            afterList(&state)
        }
        if let agents = feed.agents { state.agents = agents }
        if let providers = feed.providers { state.providers = Dictionary(providers.map { ($0.id, $0) }) { a, _ in a } }
        if let models = feed.models { state.models = Self.byID(models) }
        if let keys = feed.keys { state.keys = keys }
        if let cards = feed.cards { apply(&state, cards: cards) }
        let landed = batch.subtracting(feed.failed).filter { !$0.id.isEmpty }
        if !landed.isEmpty, let disk = state.disk { state.asks.append(.markRefreshed(disk, landed.sorted { "\($0)" < "\($1)" })) }
        state.feedBatch.formUnion(feed.failed)
        // Again shortly: what failed waits out a moment, and what arrived
        // meanwhile goes with it.
        if !state.feedBatch.isEmpty { state.asks.append(.feedWait(seconds: feed.failed.isEmpty ? 0.3 : 3)) }
    }

    // MARK: Scenes and conversations

    private func show(_ state: inout State, _ thread: String, in scene: String, key: ChildKey) {
        if state.conversations[key] == nil {
            let row = state.thread(thread)
            state.conversations[key] = State.Entry(
                seed: Conversation.Seed(thread: thread, agent: row?.agentID, draft: state.drafts[thread] ?? "", listen: false),
                thread: thread, agent: row?.agentID)
        }
        state.scenes[scene, default: State.Scene()].current = key
        guard state.session != nil else { return }
        // Before the folder is read, its open asks for what the windows show.
        if state.transcripts[thread] == nil, let disk = state.disk {
            state.asks.append(.loadCached(disk, thread: thread, epoch: state.readEpochs[thread, default: 0]))
        }
        // A chat the list does not hold (a notification, a shortcut, an old
        // chat): its row names the agent, so it is read.
        if state.thread(thread) == nil { state.asks.append(.fetchThread(thread)) }
        if state.transcripts[thread] != nil { load(&state, thread) }
    }

    private static let newDraft = "new:"

    /// A new chat takes over an unsent draft a new chat left behind (one
    /// whose conversation is gone), so what was typed into it is there again.
    private func newChat(_ state: inout State, agent: String?, listen: Bool, in scene: String, key: ChildKey) {
        let orphan = state.drafts.keys.sorted().first { draft in
            draft.hasPrefix(Self.newDraft) && state.conversations[ChildKey(String(draft.dropFirst(Self.newDraft.count)))] == nil
        }
        let draft = orphan.flatMap { state.drafts.removeValue(forKey: $0) } ?? ""
        if !draft.isEmpty { state.drafts[Self.newDraft + key.rawValue] = draft }
        state.conversations[key] = State.Entry(
            seed: Conversation.Seed(
                thread: nil, agent: agent ?? state.defaultAgent?.id, draft: draft,
                listen: listen),
            agent: agent ?? state.defaultAgent?.id)
        state.scenes[scene, default: State.Scene()].current = key
    }

    /// A conversation stays while a window shows it, it runs, its overlay
    /// waits for its rows, or an outcome waits to be read.
    private func release(_ state: inout State) {
        let shown = Set(state.scenes.values.compactMap(\.current))
        for (key, entry) in state.conversations where !shown.contains(key) && !entry.owns && !entry.active && entry.outcomes == 0 {
            state.conversations[key] = nil
        }
    }

    private func markSeen(_ state: inout State, _ id: String) {
        guard let thread = state.thread(id), !thread.running, let finished = thread.record["finishedAt"]?.string,
            state.seen[id] != finished
        else { return }
        state.seen[id] = finished
    }

    /// What a conversation's event tells the list: rule 1's binding, rule
    /// 3's ownership, its own reads, its draft.
    private func reported(_ state: inout State, _ key: ChildKey, _ report: ChatAppStep.Report, before: State) {
        guard var entry = state.conversations[key] else { return }
        if let named = report.named, entry.thread != named {
            // The `thread` event named a new chat's thread: from now the list
            // finds the conversation by it, any other conversation on it
            // closes, and its draft moves with it.
            for (other, held) in state.conversations where other != key && held.thread == named {
                state.conversations[other] = nil
                for scene in state.scenes.keys where state.scenes[scene]?.current == other { state.scenes[scene]?.current = key }
            }
            entry.thread = named
            if let agent = report.agent ?? entry.agent { state.lastAgent = agent }
            bump(&state, named)
            state.drafts[Self.newDraft + key.rawValue] = nil
        }
        // Rule 4's moves: a send or an attach takes the thread's rows, a
        // settle hands them back; either way what was read before is old.
        let started = report.owns && !entry.owns
        let settled = !report.owns && entry.owns
        entry.agent = report.agent ?? entry.agent
        entry.owns = report.owns
        entry.active = report.active
        entry.streaming = report.streaming
        entry.outcomes = report.outcomes
        entry.name = report.name
        if report.settles > entry.settles { state.answered.raise(ChatAnswer(thread: entry.thread, words: "\(report.name) answered")) }
        entry.settles = report.settles
        if !report.active, entry.continuing != nil {
            state.asks.append(.endContinued(key))
            entry.continuing = nil
        }
        state.conversations[key] = entry
        if let thread = entry.thread, started || settled {
            bump(&state, thread)
            if started { state.asks.append(.cancel("prefetch")) }
        }
        if let draft = report.draft {
            let name = entry.thread ?? Self.newDraft + key.rawValue
            state.drafts[name] = draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : draft
        }
        if let read = report.read {
            // A conversation's own read publishes under its own overlay: the
            // handover is how its rows replace it in one step.
            if let record = read.record {
                publish(&state, read.thread, read.rows, version: record.version, full: read.rows.count >= 500)
                upsert(&state, record)
            }
            if read.reload { load(&state, read.thread, force: true) }
        }
        if report.sent, !state.preferences.askedNotifications {
            state.preferences.askedNotifications = true
            state.asks.append(.askPermission)
        }
        if !shownOrBusy(state, key) { release(&state) }
    }

    private func shownOrBusy(_ state: State, _ key: ChildKey) -> Bool {
        guard let entry = state.conversations[key] else { return false }
        return state.scenes.values.contains { $0.current == key } || entry.owns || entry.active || entry.outcomes > 0
    }

    // MARK: Settings

    private func setting(_ state: inout State, _ row: String, _ toggle: Bool?, _ choice: String?) {
        let defaults = defaultOffered(state.agentList)
        switch (row, toggle, choice) {
        case let ("showAutomated", on?, _):
            state.preferences.showAutomated = on
            refresh(&state)
        case let ("expandThinking", on?, _): state.preferences.expandThinking = on
        case let ("technicalDetails", on?, _): state.preferences.technicalDetails = on
        case let ("notifyReplies", on?, _): state.preferences.notifyReplies = on
        case let ("notifyAttention", on?, _): state.preferences.notifyAttention = on
        case let ("defaultAgent", _, option?): state.preferences.defaultAgent = option.isEmpty ? nil : option
        case let ("voiceLocale", _, option?): state.preferences.voiceLocale = option.isEmpty ? nil : option
        case let (row, on?, _) where row.hasPrefix("agent:"):
            state.preferences.setOffered(String(row.dropFirst("agent:".count)), on, defaults: defaults)
        default:
            return
        }
    }

    /// The live records of a read, by id: a deleted one names no model.
    static func byID(_ records: [Record]) -> [String: Record] {
        Dictionary(records.filter { $0.deletedAt == nil }.map { ($0.id, $0) }) { a, _ in a }
    }
}

extension CardReference {
    var isSuggestion: Bool {
        if case .suggestion = self { return true }
        return false
    }
}

extension ChatAppState {
    /// The card an id names, by the lists that hold it.
    func reference(of id: String) -> CardReference? {
        if interactions.contains(where: { $0.id == id }) || cards[id]?.record?.kind == Kinds.interaction { return .question(id) }
        if requests.contains(where: { $0.id == id }) || cards[id]?.record?.kind == Kinds.request { return .suggestion(id) }
        if approvals.contains(where: { $0.id == id }) || cards[id]?.record?.kind == Kinds.approval { return .approval(id) }
        return nil
    }
}
