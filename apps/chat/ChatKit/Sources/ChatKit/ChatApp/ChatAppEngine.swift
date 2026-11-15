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
        case let .unsentSaved(ids, failed): unsentSaved(&next, ids, failed)
        case .unsentRetryDue:
            if next.unsaved != nil, let disk = next.disk { next.asks.append(.saveUnsent(disk, next.unsent, drafts: next.drafts)) }
        case let .permissionRead(permission):
            next.permission = permission
            // Offered in the app's words only where the system was never
            // asked: its prompt shows once.
            if next.notificationOffer == .checking {
                next.notificationOffer = permission == .notAsked ? .offered : nil
                if permission != .notAsked { next.preferences.askedNotifications = true }
            }
        case let .notificationsAnswered(allow):
            next.notificationOffer = nil
            next.preferences.askedNotifications = true
            if allow { next.asks.append(.askPermission) }
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
        case let .paced(paces): next.paces = paces

        case .refreshAsked:
            // Asked for by hand, the list is read from its start: a window
            // that grew goes back to its first page, and whatever went
            // stale past it with it.
            next.window.restart = true
            next.archive.window.restart = true
            // Try Again on the strip also does again what it said failed:
            // the search, or the pin or archive that was refused.
            if next.listFailure?.attempt == .search {
                next.listFailure = nil
                for (scene, held) in next.scenes {
                    next.searches[scene] = nil
                    search(&next, scene: scene, query: held.query)
                }
            }
            if next.listFailure?.attempt == .flag {
                next.listFailure = nil
                if let refused = next.flagRefused { flag(&next, refused.thread, pinned: refused.change.pinned, archived: refused.change.archived) }
            }
            next.flagRefused = nil
            refresh(&next)
        case let .refreshed(refresh): refreshed(&next, refresh)
        case let .refreshFailed(failure):
            next.refreshing = false
            next.listFailure = failure.report == nil ? failure : nil
        case let .pageRead(part, after, rows, cursor, marks):
            pageRead(&next, part: part, after: after, rows: rows, cursor: cursor, marks: marks)
        case let .pageFailed(part, _, failure, compacted):
            if part == .archived { next.archive.window.reading = false } else { next.window.reading = false }
            if compacted {
                // The server cannot go on from the cursor: the list is
                // read again from its start.
                if part == .archived { next.archive.window.restart = true } else { next.window.restart = true }
                refresh(&next)
            } else if failure.report == nil {
                next.listFailure = failure
            }
        case let .moreAsked(archived): more(&next, archived: archived)
        case let .archivedShown(scene, shown): showArchived(&next, scene: scene, shown: shown)
        case let .pinAsked(thread, pinned): flag(&next, thread, pinned: pinned)
        case let .archiveAsked(thread, archived, at):
            if archived { archive(&next, thread, at: at) } else { unarchive(&next, thread) }
        case .undoAsked:
            if let undo = next.undo { unarchive(&next, undo.thread) }
        case let .undoPassed(thread):
            if next.undo?.thread == thread {
                next.undo = nil
                shelve(&next)
            }
        case let .searchRead(scene, query, hits, threads, marks):
            searchRead(&next, scene: scene, query: query, hits: hits, threads: threads, marks: marks)
        case let .searchFailed(scene, query, failure, unserved):
            searchFailed(&next, scene: scene, query: query, failure, unserved: unserved)
        case let .olderRowsAsked(thread): older(&next, thread)
        case let .olderRowsRead(thread, epoch, before, rows, more):
            olderRead(&next, thread: thread, epoch: epoch, before: before, rows: rows, more: more)
        case let .olderRowsFailed(thread, epoch):
            if next.readEpochs[thread, default: 0] == epoch {
                next.olderReading.remove(thread)
                next.seeking[thread] = nil
            }
        case let .featuresRead(agents, records): featuresRead(&next, agents: agents, records: records)
        case let .marksRead(marks):
            let learned = apply(&next, marks: marks)
            for thread in marks.threads { noteBehind(&next, thread) }
            announceLearned(&next, learned)
        case let .endShown(scene, key, shown):
            if shown {
                next.scenes[scene]?.atEnd = key
            } else if next.scenes[scene]?.atEnd == key {
                next.scenes[scene]?.atEnd = nil
            }
        case let .sighted(thread, message, at): sighted(&next, thread: thread, message: message, at: at)
        case let .marksDue(at): marksDue(&next, at: at)
        case let .markWritten(thread, sent, record): markWritten(&next, thread: thread, sent: sent, record)
        case let .markWriteFailed(thread, sent, refusal, failure):
            markWriteFailed(&next, thread: thread, sent: sent, refusal, failure)
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
        case let .rowsFailed(thread, epoch, failure):
            // The read is no longer outstanding: the next asks again. A
            // chat somebody is looking at says it could not be read.
            guard next.readEpochs[thread, default: 0] == epoch else { break }
            next.reading[thread] = nil
            if failure.report == nil, next.key(of: thread) != nil { next.rowsFailed[thread] = failure }
        case let .rowsAsked(thread):
            next.rowsFailed[thread] = nil
            load(&next, thread, force: true)
        case let .openingsRead(openings):
            next.openings.merge(openings) { _, new in new }
        case let .rowsRead(thread, epoch, version, rows, full):
            // Rule 4. Rows may land while a conversation streams the
            // thread: each is the turn the stream drew of it, in one list
            // (rule 3, `TurnFold`).
            if next.readEpochs[thread, default: 0] == epoch { next.reading[thread] = nil }
            guard next.readEpochs[thread, default: 0] == epoch else { break }
            publish(&next, thread, rows, version: version, full: full)
        case let .cachedLoaded(thread, epoch, cached):
            // Rule 4, as for a read: what the disk held is older than a
            // send, an attach, a settle or a binding since.
            guard next.readEpochs[thread, default: 0] == epoch else { break }
            if let cached, next.transcripts[thread] == nil {
                next.transcripts[thread] = State.Held(
                    records: cached.records, turns: transcript(of: cached.records),
                    version: cached.threadVersion, full: cached.full)
                seek(&next, thread)
            }
            // What a conversation shows is read where the disk held none, or
            // held rows that are not current.
            if next.key(of: thread) != nil { load(&next, thread) }
        case let .prefetched(thread, epoch, version, rows):
            guard next.readEpochs[thread, default: 0] == epoch else { break }
            publish(&next, thread, rows, version: version, full: rows.count >= 100)
        case let .rowReread(thread, record, mark):
            if let record { upsert(&next, record) }
            // This read started after the mark was learned, so the row it
            // brought is the one the mark is told against, by the rule. A
            // mark learned since has asked for its own.
            if record == nil || next.marks?[thread]?.version == mark { next.rowsBehind.remove(thread) }
        case let .threadFetched(record):
            upsert(&next, record)
            if next.readsMarks, next.marks?[record.id] == nil { next.asks.append(.readMarks([record.id])) }

        case let .watchOpened(fresh):
            next.feedReconnecting = false
            if next.listFailure?.attempt == .feed { next.listFailure = nil }
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
            // a card on screen reads it again. So does a question or a
            // suggestion: one resolved on another device folds here.
            if next.cards[row.recordID] != nil, let reference = CardReference(kind: row.kind, id: row.recordID) {
                next.asks.append(.readCard(reference, target: false))
            }
            // Only the wait restarts, never a batch being read.
            next.asks.append(.feedWait(seconds: 0.3))
        case let .watchRelist(at):
            next.watching = false
            guard WatchFollower.recovers(at: at, after: state.recoveryStarted) else {
                next.listFailure = Failure(.feed, .ended, seen: "the server could not resume the chats' history even after reading them again")
                break
            }
            next.recoveryStarted = at
            refresh(&next)
        case let .watchInterrupted(dropped):
            next.feedReconnecting = dropped
        case let .watchEnded(words, failure):
            next.watching = false
            next.feedReconnecting = false
            // Ended for good, in the server's words: drawn at the top of
            // the list, where Try Again reads the list and opens it again.
            if failure?.report == nil { next.listFailure = Failure(.feed, .ended, said: words) }
        case .feedDue: drain(&next)
        case let .feedApplied(feed): fed(&next, feed)

        case let .sceneAppeared(scene, restoring, key, lands):
            next.scenes[scene] = next.scenes[scene] ?? State.Scene()
            if lands {
                // Decided once the chats are held: the folder may not be
                // read yet.
                next.scenes[scene]?.arriving = State.Arrival(restoring: restoring, key: key)
                land(&next)
                release(&next)
            } else if let restoring, next.scenes[scene]?.current == nil, next.thread(restoring) != nil {
                show(&next, restoring, in: scene, key: key)
            }
        case let .sceneActive(scene, active):
            next.scenes[scene, default: State.Scene()].active = active
        case let .sceneGone(scene):
            next.scenes[scene] = nil
            shelve(&next)
            release(&next)
        case let .show(thread, scene, key, message):
            show(&next, thread, in: scene, key: key)
            if let message {
                // Opened at a message: the chat is put at its row, and the
                // rows before the ones held are read where they lack it.
                next.conversations[key]?.opensAt = message
                next.conversations[key]?.visits += 1
                next.seeking[thread] = State.Seek(message: message)
                seek(&next, thread)
            }
            release(&next)
        case let .newChat(agent, listen, scene, key):
            newChat(&next, agent: agent, listen: listen, in: scene, key: key)
            release(&next)
        case let .back(scene):
            next.scenes[scene, default: State.Scene()].current = nil
            release(&next)
        case let .searched(scene, query): search(&next, scene: scene, query: query)
        case let .draftKept(key, thread, text):
            // By the thread the conversation is bound to now, else the one
            // the field knew it by, else as a new chat's.
            let name = next.conversations[key]?.thread ?? thread ?? Self.newDraft + key.rawValue
            next.drafts[name] = text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : text
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
            if next.marks != nil { markRead(&next, thread) } else { markSeen(&next, thread) }
        case let .markedUnread(thread):
            if next.marks != nil { markUnread(&next, thread) } else { next.seen[thread] = nil }
        case let .renameAsked(thread, subject):
            // Optimistic: the list shows it at once, and goes back if the
            // server refuses.
            let before = next.thread(thread)?.record
            setSubject(&next, thread, subject)
            next.renameFailure = nil
            next.renaming[thread] = subject
            next.asks.append(.rename(thread: thread, subject: subject, before: before))
        case let .renamed(record):
            next.renaming[record.id] = nil
            upsert(&next, record)
        case let .renameFailed(thread, before, failure):
            if let before { replaceRow(&next, before) } else { setSubject(&next, thread, nil) }
            let subject = next.renaming.removeValue(forKey: thread)
            if failure.report == nil, let subject { next.renameFailure = State.Rename(thread: thread, subject: subject, failure: failure) }
        case .renameDismissed:
            next.renameFailure = nil
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
        case let .keyFailed(failure): next.keyProblem = failure

        case let .cardAsked(reference):
            let target = (reference.isSuggestion && next.cards[reference.id]?.target == nil)
            next.asks.append(.readCard(reference, target: target))
        case let .cardRead(id, record, target):
            var card = next.cards[id] ?? State.Card()
            // A read that started before a write of this device's was
            // answered must not bring the buttons back.
            if let record, (card.record?.version ?? 0) <= record.version { card.record = record }
            card.target = target ?? card.target
            // A read that failed is answered. A write that failed stays
            // said, with its button, while the card still waits.
            if card.problem?.attempt == .card || !Self.waits(card.record) { (card.problem, card.tried) = (nil, nil) }
            next.cards[id] = card
        case let .cardFailed(id, failure):
            if failure.report == nil { next.cards[id, default: State.Card()].problem = failure }
        case let .answerAsked(id, picks):
            guard let record = next.cards[id]?.record else { break }
            let questions = (record["questions"]?.array ?? []).compactMap { $0["id"]?.text }
            let answers: [JSONValue] = questions.map { q in
                ["question": .string(q), "selected": .array((picks[q] ?? []).map(JSONValue.string))]
            }
            write(&next, id, record, ["state": "answered", "answers": .array(answers)], as: "answer")
        case let .questionsDismissed(id):
            guard let record = next.cards[id]?.record else { break }
            write(&next, id, record, ["state": "dismissed"], as: "dismiss")
        case let .decideAsked(id, accept, shown):
            next.cards[id, default: State.Card()].busy = accept ? "accept" : "reject"
            next.cards[id]?.problem = nil
            // The version decided on is the one whose change is on screen:
            // read now when the card holds none.
            if let held = next.cards[id]?.record ?? next.requests.first(where: { $0.id == id }) {
                write(&next, id, held, ["decision": .string(accept ? "accepted" : "rejected")], as: accept ? "accept" : "reject")
            } else {
                next.asks.append(.decideUnread(id, accept: accept, shown: shown))
            }
        case let .approveAsked(id, approve):
            // Decided on the version the owner read: one moved since is
            // refused, and read again.
            guard let held = next.cards[id]?.record ?? next.approvals.first(where: { $0.id == id }) else { break }
            write(&next, id, held, ["state": .string(approve ? "approved" : "denied")], as: approve ? "approve" : "decline")
        case let .cardRetried(reference):
            retry(&next, reference)
        case let .cardWritten(id, record):
            next.cards[id, default: State.Card()].record = record
            next.cards[id]?.busy = nil
            next.cards[id]?.problem = nil
            next.cards[id]?.tried = nil
            // The resolution wrote into the thread and resumed the agent.
            next.asks += [
                .cards(next.plan, threads: next.cardThreads),
                .applyFeed(
                    next.plan, [.init(kind: Kinds.thread, id: "")],
                    threads: next.cardThreads),
            ]
            // An approved call runs after the decision, under the engine:
            // the card follows it to its end.
            if record.kind == Kinds.approval, record["state"]?.string == "approved" {
                next.asks.append(.readCard(.approval(id), target: false))
            }
        case let .cardWriteFailed(id, failure):
            next.cards[id, default: State.Card()].busy = nil
            if failure.report == nil { next.cards[id]?.problem = failure }
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
        // What came on screen is read: at once where the device keeps what
        // is unread, and once its end was looked at where the server does.
        if next.marks == nil {
            for thread in next.visibleThreads.subtracting(state.visibleThreads) { markSeen(&next, thread) }
        }
        settleMarks(&next)
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
            state = initialState
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
        // A message sent before the folder was read stays; the rest is
        // what the folder kept.
        state.unsent = disk.unsent.merging(state.unsent) { _, sent in sent }
        state.keptUnsent = Set(disk.unsent.values.map(\.id))
        state.unsaved = nil
        // A kill between the two files leaves a text in both, and one
        // owner is chosen here. A send was on its way out of the drafts:
        // the draft is the message and no more, and it is the unsent one.
        // Edit Message or a refusal was on its way back into them: the
        // draft is the message with what was typed since behind it, as it
        // is put back, and it is the draft, so the message is not also left
        // standing to be sent beside it.
        for (name, message) in state.unsent {
            guard let draft = state.drafts[name]?.trimmingCharacters(in: .whitespacesAndNewlines) else { continue }
            if draft == message.message {
                state.drafts[name] = nil
            } else if draft.hasPrefix(message.message + "\n\n") {
                state.unsent[name] = nil
            }
        }
        if let cached = disk.state {
            state.agents = cached.agents
            state.providers = Dictionary(cached.providers.map { ($0.id, $0) }) { a, _ in a }
            state.models = cached.models.map(Self.byID)
            state.keys = cached.keys
            state.chats = cached.threads
            state.automated = cached.automated
            // Which lists the last launch read, until the server's document
            // is read again: its flags' lists where it kept them.
            state.recordsRevision = cached.records
            state.pinned = cached.pinned ?? []
            state.waiting = cached.waiting ?? []
            state.openings = cached.openings
            state.interactions = cached.interactions
            state.requests = cached.requests
            state.seen = cached.seen
            state.marks = cached.marks
            // The folder's rows and marks as they were left: a mark that
            // was ahead of its row then still is.
            for thread in state.threads { noteBehind(&state, thread.id) }
            // What was moved and not answered before the app stopped waits
            // for the first read the server answers.
            if cached.marks != nil {
                state.marking = (cached.marking ?? [:]).mapValues {
                    var change = $0
                    (change.due, change.held) = (nil, true)
                    return change
                }
            }
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
        land(&state)
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
        state.asks.append(.backgroundRefresh(run, disk, state.plan, threads: state.cardThreads, beside: beside(state)))
    }

    private func phase(_ state: inout State, _ phase: AppPhase) {
        switch phase {
        case .active:
            state.appActive = true
            state.leaving = false
            guard state.session != nil else { break }
            retryMarks(&state)
            refresh(&state)
            for key in state.conversations.keys { state.conversations[key]?.resumeNudge += 1 }
        case .inactive:
            state.leaving = true
            // What was read and not yet written goes out while the app
            // may still send it.
            flushMarks(&state)
            // Asked for while the app is still in front, as iOS requires of
            // a continued processing task: inactive comes before background.
            for (key, entry) in state.conversations where entry.streaming && entry.continuing == nil {
                let title = entry.thread.flatMap { state.thread($0) }.map(state.title) ?? "New chat"
                state.conversations[key]?.continuing = key.rawValue
                state.asks.append(.continueRun(key, ContinuedProcessing(id: key.rawValue, title: "Waiting for \(entry.name)", subtitle: title)))
            }
        case .background:
            state.appActive = false
            state.leaving = true
            flushMarks(&state)
            if let disk = state.disk { state.asks.append(.scheduleRefresh(disk)) }
            state.asks.append(.badge(state.unreadCount))
            if pausesWhenAway {
                state.watching = false
                state.asks.append(.cancel("watch"))
            }
        }
    }

    // MARK: Reading the list

    func refresh(_ state: inout State) {
        guard state.session != nil, state.opened, !state.accessEnded, state.online else { return }
        state.refreshing = true
        state.asks += [.refresh(state.plan, held: state.readsMarks ? state.cardThreads : [], beside: beside(state)), .readFeatures]
    }

    private func refreshed(_ state: inout State, _ refresh: ChatAppStep.Refresh) {
        state.refreshing = false
        if state.listFailure?.attempt == .refresh { state.listFailure = nil }
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
        // The server answered: the marks no list read brought are asked
        // for, and the writes that failed are tried again.
        if refresh.threads.marks == nil { askMarks(&state) }
        retryMarks(&state)
        afterList(&state)
        land(&state)
        // Read before the server's document said it keeps the flags: the
        // lists are read again by them.
        if state.flags, refresh.threads.pinned == nil { self.refresh(&state) }
    }

    func watch(_ state: inout State, head: WatchCursor? = nil) {
        guard let disk = state.disk, state.online, state.appActive || !pausesWhenAway else { return }
        state.watching = true
        state.asks.append(
            .watch(disk, head: head, kinds: ChatApp.watched(state.plan, approvals: state.approvalsKnown, marks: state.marksKnown)))
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

    /// One thread row, read on its own: a handover, a poll, a rename.
    func upsert(_ state: inout State, _ record: Record) {
        if let held = state.thread(record.id)?.record, held.version > record.version { return }
        replaceRow(&state, record)
        threadMoved(&state, record)
    }

    private func replaceRow(_ state: inout State, _ record: Record) {
        // In every list that holds the thread: one the person archived, or
        // an old one they pinned, is not thereby one of the chats.
        if state.flags, replaceHeld(&state, record) {
            state.others[record.id] = nil
            return
        }
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
    func threadMoved(_ state: inout State, _ record: Record) {
        let thread = ChatThread(record: record)
        if state.marks == nil, !thread.running, record["finishedAt"] != nil, state.visibleThreads.contains(thread.id) {
            markSeen(&state, thread.id)
        }
        announce(&state, thread)
        if !thread.running, state.key(of: thread.id) != nil { load(&state, thread.id) }
    }

    /// A reply that settled where nobody sees it notifies, once. Where the
    /// server keeps marks, one a mark says was read, on any device, is no
    /// news; one whose mark the server has not said yet waits for it, the
    /// first read of the marks included (`apply(_:marks:)` comes back
    /// here); and the rest wait five seconds for a mark, so a reply read on
    /// a device beside this one stays quiet.
    func announce(_ state: inout State, _ thread: ChatThread) {
        guard !thread.running, let finished = thread.record["finishedAt"]?.string else { return }
        let key = "reply|\(thread.id)|\(finished)"
        guard !state.notified.contains(key) else { return }
        if state.readsMarks, !state.marked(thread.id) { return }
        state.notified.insert(key)
        // A reply in a chat its person archived reaches nobody, but while
        // the chat waits on a person.
        guard state.notifies(thread) else { return }
        // A stop is the owner's own act; an error says so rather than
        // offering the turn before it as the answer.
        guard state.baselined, state.preferences.notifyReplies, !thread.automated, !state.visibleThreads.contains(thread.id),
            thread.status != "stopped", thread.status == "error" || thread.record["lastReply"]?.text != nil,
            state.marks == nil || state.unread(thread) || state.failedUnseen(thread)
        else { return }
        let request = ChatNotifications.reply(
            thread: thread, agent: agentName(thread.agentID ?? ""),
            title: state.title(thread), failed: notificationWords(state, thread: thread))
        state.asks.append(state.marks == nil ? .notify(request) : .notifyHeld(request, seconds: Self.notifyHold))
        state.announced.insert(thread.id)
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
        if state.olderReading.remove(thread) != nil { state.asks.append(.cancel("older:\(thread)")) }
    }

    /// Reads a thread's rows unless they are current (rule 7) or a
    /// conversation is reading them for its own run, whose reads are the
    /// ones in step with its stream. A read under an older thread version
    /// than the list now holds is replaced.
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
        var next = State.Held(records: rows, turns: [], version: version, full: full)
        // The newest rows again, under a transcript that grew at its start:
        // where the read begins inside the rows held, the ones before it
        // stay. Where it does not, more rows were written since than a
        // read holds, and what lies between was never read.
        if full, let held = state.transcripts[thread], let first = rows.first, let at = held.records.firstIndex(where: { $0.id == first.id }), at > 0 {
            next.records = Array(held.records[..<at]) + rows
            (next.full, next.above, next.grown) = (held.full, held.above, held.grown)
        }
        next.turns = transcript(of: next.records)
        state.transcripts[thread] = next
        state.rowsFailed[thread] = nil
        seek(&state, thread)
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
        state.asks.append(.applyFeed(state.plan, batch, threads: state.cardThreads, named: named(state, batch)))
    }

    /// The threads held that a batch of the watch's rows names, by a row
    /// of their own or of the person's mark of them (a mark's id starts
    /// with its thread's). The lists read for the batch may hold none of
    /// them: a chat past the first page, or one that just left the list it
    /// was in, as an archived one does. Each such thread is then read on
    /// its own, so the row held is never left as it was.
    private func named(_ state: State, _ batch: Set<State.WatchKey>) -> [String] {
        let threads = Set(batch.filter { $0.kind == Kinds.thread }.map(\.id))
        let marks = batch.filter { $0.kind == Kinds.threadMark }.map(\.id)
        guard !threads.isEmpty || !marks.isEmpty else { return [] }
        // And the threads a conversation is on that no list holds yet: a
        // chat just started, archived on another device since.
        var seen: Set<String> = []
        let known = (state.threadIDs + beside(state)).filter { seen.insert($0).inserted }
        return known.filter { id in threads.contains(id) || marks.contains { $0.hasPrefix(id + ".") } }
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

    func show(_ state: inout State, _ thread: String, in scene: String, key: ChildKey) {
        if state.conversations[key] == nil {
            let row = state.thread(thread)
            let unsent = state.unsent[thread]
            state.conversations[key] = State.Entry(
                seed: Conversation.Seed(thread: thread, agent: row?.agentID, draft: state.drafts[thread] ?? "", listen: false, unsent: unsent),
                thread: thread, agent: row?.agentID, unsent: unsent?.id, newFrom: state.newFrom(thread))
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
    /// whose conversation is gone), so what was typed into it is there
    /// again; and a message one sent that the server never took, with the
    /// assistant it was sent to.
    func newChat(_ state: inout State, agent: String?, listen: Bool, in scene: String, key: ChildKey) {
        // A new chat whose first message the server has not taken, and
        // that no window shows, has no row in the list to come back by:
        // New Chat is the way back to it.
        let shown = Set(state.scenes.values.compactMap(\.current))
        if let waiting = state.conversations.keys.sorted().first(where: { held in
            state.conversations[held]?.thread == nil && state.conversations[held]?.unsent != nil && !shown.contains(held)
        }) {
            state.scenes[scene, default: State.Scene()].current = waiting
            return
        }
        let orphaned = { (name: String) in
            name.hasPrefix(Self.newDraft) && state.conversations[ChildKey(String(name.dropFirst(Self.newDraft.count)))] == nil
        }
        let orphan = state.drafts.keys.sorted().first(where: orphaned)
        let draft = orphan.flatMap { state.drafts.removeValue(forKey: $0) } ?? ""
        if !draft.isEmpty { state.drafts[Self.newDraft + key.rawValue] = draft }
        let unsent = state.unsent.keys.sorted().first(where: orphaned).flatMap { state.unsent.removeValue(forKey: $0) }
        if let unsent { state.unsent[Self.newDraft + key.rawValue] = unsent }
        let agent = unsent?.agent ?? agent
        state.conversations[key] = State.Entry(
            seed: Conversation.Seed(
                thread: nil, agent: agent ?? state.defaultAgent?.id, draft: draft,
                listen: listen, unsent: unsent),
            agent: agent ?? state.defaultAgent?.id, unsent: unsent?.id)
        state.scenes[scene, default: State.Scene()].current = key
    }

    /// A conversation stays while a window shows it, it runs, what its
    /// stream drew waits for its rows, or an outcome waits to be read.
    func release(_ state: inout State) {
        let shown = Set(state.scenes.values.compactMap(\.current))
        for (key, entry) in state.conversations where !shown.contains(key) && !entry.owns && !entry.active && entry.outcomes == 0 {
            state.conversations[key] = nil
        }
    }

    /// How long after each failed write of the unsent messages the next is
    /// made, and then every half minute for as long as it fails.
    static let unsavedDelays: [Double] = [2, 5, 15, 30]

    /// The folder answered a write of the unsent messages. Where it took
    /// them, they are kept. Where it did not, the ones it lacks are named
    /// so their chats can say so, their first copies leave all the same,
    /// and the write is made again after a wait.
    private func unsentSaved(_ state: inout State, _ ids: Set<String>, _ failed: String?) {
        guard let failed else {
            state.keptUnsent = ids
            if state.unsaved != nil { state.asks.append(.cancel("unsentRetry")) }
            state.unsaved = nil
            return
        }
        let failures = (state.unsaved?.failures ?? 0) + 1
        state.unsaved = State.Unsaved(ids: ids.subtracting(state.keptUnsent), seen: failed, failures: failures)
        state.asks.append(.retryUnsent(after: Self.unsavedDelays[min(failures, Self.unsavedDelays.count) - 1]))
    }

    func markSeen(_ state: inout State, _ id: String) {
        guard let thread = state.thread(id), !thread.running, let finished = thread.record["finishedAt"]?.string,
            state.seen[id] != finished
        else { return }
        state.seen[id] = finished
    }

    /// What a conversation's event tells the list: rule 1's binding, rule
    /// 3's ownership, its own reads, its draft.
    private func reported(_ state: inout State, _ key: ChildKey, _ report: ChatAppStep.Report, before: State) {
        guard var entry = state.conversations[key] else { return }
        if let named = report.named, entry.thread != named,
            let other = state.conversations.keys.sorted().first(where: { $0 != key && state.conversations[$0]?.thread == named }),
            report.adopted || state.drafts[named] != nil
                || state.conversations[other].map({ $0.owns || $0.active || $0.outcomes > 0 || $0.unsent != nil }) ?? false
        {
            // The thread this new chat turns out to be already has a
            // conversation that is in the middle of something (a send out,
            // a message standing unsent, a failure still to be read, a
            // draft the folder keeps for it), or
            // this one learned its thread from a read and has no run to
            // lose: that one is the chat and this one gives way to it. Its
            // own message is the server's by now, and what was typed here
            // stays in the folder for the next new chat. Closing the other
            // instead would drop what it holds.
            if let before = entry.unsent, state.unsent[Self.newDraft + key.rawValue]?.id == before {
                state.unsent[Self.newDraft + key.rawValue] = nil
            }
            state.conversations[key] = nil
            for scene in state.scenes.keys where state.scenes[scene]?.current == key { state.scenes[scene]?.current = other }
            return
        }
        if let named = report.named, entry.thread != named {
            // The `thread` event named a new chat's thread: from now the list
            // finds the conversation by it, any other conversation on it
            // closes, and its draft moves with it.
            for (other, held) in state.conversations where other != key && held.thread == named {
                state.conversations[other] = nil
                for scene in state.scenes.keys where state.scenes[scene]?.current == other { state.scenes[scene]?.current = key }
            }
            entry.thread = named
            // A thread this send just made has no mark yet: said so now,
            // so its reply is unread, and notified, without waiting for a
            // read of the marks. One the server holds replaces it.
            if state.marks != nil, state.marks?[named] == nil { state.marks?[named] = ThreadMark.none }
            if let agent = report.agent ?? entry.agent { state.lastAgent = agent }
            bump(&state, named)
            // What the new chat's composer still holds (typed while its
            // message was out, or kept from a launch before) is the
            // thread's draft from here, so it outlives the window. Where
            // the folder keeps a draft for that thread already, written on
            // it before this chat turned out to be it, both stay: what was
            // typed here above it. The conversation is told, so its
            // composer holds both and its next word of its draft cannot
            // drop the older.
            let typed = state.drafts.removeValue(forKey: Self.newDraft + key.rawValue)
            if let older = state.drafts[named] {
                entry.draftFound = older
                state.drafts[named] = Conversation.above(typed ?? "", older)
            } else if let typed {
                state.drafts[named] = typed
            }
            // The server took the new chat's message, or holds it already.
            state.unsent[Self.newDraft + key.rawValue] = nil
            entry.unsent = nil
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
        if report.continueUnserved { state.continueUnserved = true }
        if report.idsUnserved { state.idsUnserved = true }
        if report.spoke, let thread = entry.thread { spoke(&state, in: thread) }
        // The message this conversation waits on the server for, kept in
        // the folder under its chat's name. Only the one this conversation
        // reported is taken away, so another's is never dropped with it.
        if report.unsent?.id != entry.unsent {
            let name = entry.thread ?? Self.newDraft + key.rawValue
            if let before = entry.unsent, state.unsent[name]?.id == before { state.unsent[name] = nil }
            if let unsent = report.unsent { state.unsent[name] = unsent }
            entry.unsent = report.unsent?.id
        }
        if report.settles > entry.settles {
            state.answered.raise(ChatAnswer(thread: entry.thread, words: "\(report.name) answered"))
            // The first reply settled: now, and not while it streamed, the
            // app asks whether it may notify, in its own words first.
            if !state.preferences.askedNotifications, state.notificationOffer == nil {
                state.notificationOffer = .checking
                state.asks.append(.readPermission)
            }
        }
        entry.settles = report.settles
        if !report.active, entry.continuing != nil {
            state.asks.append(.endContinued(key))
            entry.continuing = nil
        }
        state.conversations[key] = entry
        if let thread = entry.thread, started || settled {
            bump(&state, thread)
            if started { state.asks.append(.cancel("prefetch")) }
            // A run that ended without reading its rows (a message left
            // standing unsent) held off every read while it had the thread:
            // what moved meanwhile is read now, and may hold the message.
            if settled, report.read == nil { load(&state, thread) }
        }
        if var draft = report.draft {
            let name = entry.thread ?? Self.newDraft + key.rawValue
            // A word of the draft from before the conversation took the
            // thread's kept draft into its composer is without it.
            if let older = entry.draftFound {
                if draft.contains(older) { state.conversations[key]?.draftFound = nil } else { draft = Conversation.above(draft, older) }
            }
            state.drafts[name] = draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : draft
        }
        if let read = report.read {
            // A conversation's own read: its rows land in the step that
            // lets go of what its stream drew of them.
            if let record = read.record {
                // A delivery check that found a new chat's thread read its
                // row and none of its messages. A thread has its opening
                // message at least, so no rows is no read: published, it
                // would stand as the chat's whole transcript at this
                // version, and nothing would read it again.
                if !read.rows.isEmpty { publish(&state, read.thread, read.rows, version: record.version, full: read.rows.count >= 500) }
                upsert(&state, record)
            }
            if read.reload { load(&state, read.thread, force: true) }
        }
        // A send empties the composer, and the folder hears of it at once:
        // a draft's own save waits out a pause, and a kill inside it would
        // bring a message that was sent back as a draft to send again.
        // A message put back into the composer is written as promptly: it
        // was sent, and is in no other place.
        if report.sent || report.gaveBack, let disk = state.disk { state.asks.append(.saveUnsent(disk, state.unsent, drafts: state.drafts)) }
        // The server holds a message that answers questions in the
        // person's own words. A typed message alone leaves a question
        // waiting, on any server: dismissing it is how the server is told
        // (the interaction kind: "prose belongs in chat"), and its agent
        // then hears of the dismissal beside the message.
        if let interaction = report.answered { answeredInWords(&state, interaction) }
        if !shownOrBusy(state, key) { release(&state) }
    }

    // MARK: Cards

    /// A card's answer or decision, written over the version read: the
    /// card says it is out, and keeps what it set, so a write that fails
    /// can be asked again.
    private func write(_ state: inout State, _ id: String, _ record: Record, _ properties: [String: JSONValue], as busy: String) {
        var card = state.cards[id] ?? State.Card()
        card.busy = busy
        card.problem = nil
        card.tried = State.Card.Tried(properties: properties, busy: busy)
        state.cards[id] = card
        let kind = record.kind.isEmpty ? Kinds.interaction : record.kind
        state.asks.append(.writeCard(id, kind: kind, properties: properties, ifVersion: record.version))
    }

    /// Try Again on a card's failure. A write that failed is asked again as
    /// it was, over the version the card holds now, while its record still
    /// waits: the engine holds a decision to the version, and refuses one
    /// whose record moved. Anything else reads the card.
    private func retry(_ state: inout State, _ reference: CardReference) {
        let id = reference.id
        guard let card = state.cards[id], let record = card.record, let tried = card.tried, card.busy == nil, Self.waits(record) else {
            state.cards[id]?.problem = nil
            state.asks.append(.readCard(reference, target: false))
            return
        }
        write(&state, id, record, tried.properties, as: tried.busy)
    }

    /// The questions an answer in the person's own words stands for are
    /// dismissed, where they still wait: one resolved meanwhile, here or on
    /// another device, is left as it is.
    private func answeredInWords(_ state: inout State, _ id: String) {
        guard let record = state.cards[id]?.record ?? state.interactions.first(where: { $0.id == id }), Self.waits(record),
            state.cards[id]?.busy == nil
        else { return }
        write(&state, id, record, ["state": "dismissed"], as: State.Card.words)
    }

    /// Whether a card's record still waits on the person.
    static func waits(_ record: Record?) -> Bool {
        guard let record else { return false }
        switch record.kind {
        case Kinds.request: return record["decision"]?.string == "proposed"
        default: return record["state"]?.string == "pending"
        }
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
            // The archived chats are another list with the runs among
            // them: its cursor was cut for the one without.
            state.archive.window.restart = true
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
