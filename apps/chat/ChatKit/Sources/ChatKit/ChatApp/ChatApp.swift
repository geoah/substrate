import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateRuntime
import SubstrateScreens

/// Substrate Chat's root under the kit's link: the chat list, the agents and
/// providers, the cards waiting on the owner, the rows of every thread
/// read, the watch, what the device keeps, and each conversation as a child
/// kept under the key this list minted for it. The link's snapshot tells it
/// whose repository it holds; a sign-out starts it over, which moves its
/// controller generation (rule 6). It speaks to the device only through the
/// environment's services.
public struct ChatApp: LinkedApp {
    public enum Command: Codable, Equatable, Sendable {
        /// A window drew; `restoring` is the chat it showed when it last
        /// ran. With `lands` it opens to something: that chat where it was
        /// left at `left`, less than half an hour ago, else a chat that is
        /// running, else a new chat (`ChatApp.land`).
        case sceneAppeared(scene: String, restoring: String?, left: Date? = nil, lands: Bool = false)
        case sceneActive(scene: String, active: Bool)
        case sceneGone(scene: String)
        case show(thread: String, scene: String)
        /// A chat opened at one of its messages, from a search.
        case open(thread: String, message: String, scene: String)
        case newChat(agent: String?, listen: Bool, scene: String)
        /// Back to the list on a phone: nothing is selected.
        case back(scene: String)
        case search(scene: String, query: String)
        /// A composer's field left the screen holding this text. Its
        /// conversation was told too, where there still is one: this keeps
        /// the draft where the conversation is already gone, as when a
        /// phone went back to the list first.
        case keepDraft(key: ChildKey, thread: String?, text: String)
        case route(ChatRoute)
        /// The window that takes the route waiting.
        case claimRoute(scene: String)
        case refresh
        case markRead(String)
        case markUnread(String)
        /// Pin and Unpin, Archive and Unarchive: offered only where the
        /// server keeps the flags (`ChatListPresentation.flags`).
        case pin(thread: String, pinned: Bool)
        case archive(thread: String, archived: Bool)
        /// Undo, on the chat archived a moment ago.
        case undoArchive
        /// A window lists the archived chats, or the chats again.
        case showArchived(scene: String, shown: Bool)
        /// A list was scrolled to its end: the chats, or the archived ones.
        case more(archived: Bool)
        /// A transcript was scrolled to the first row held.
        case olderRows(thread: String)
        /// A window's view says whether somebody looking at it sees the end
        /// of the conversation it shows: the window is in front and
        /// active, and the newest message is on screen.
        case end(scene: String, key: ChildKey, shown: Bool)
        /// The answer to the app's own offer to notify: the system's
        /// prompt follows a yes.
        case notifications(allow: Bool)
        case rename(thread: String, subject: String)
        /// The rename that failed was read.
        case dismissRename
        /// A chat's rows, read again after a read that failed.
        case reloadRows(thread: String)
        /// A provider's key, for a password sign-in: sent once, never kept.
        case addKey(provider: String, key: String)
        case loadCard(CardReference)
        case answer(question: String, picks: [String: [String]])
        case dismissQuestions(String)
        case decide(suggestion: String, accept: Bool)
        /// Approve or decline a call waiting on the owner: the engine runs
        /// the stored call, never anything the app sends.
        case decideApproval(String, approve: Bool)
        /// Try Again on a card's failure: its write once more, or its read.
        case retryCard(CardReference)
        case settings(SettingsCommand)
        /// Settings opened: the permission, the storage and the owner's
        /// memories and instructions read again.
        case readSettings
        /// The memories or the instructions shown: read again.
        case readGuidance
        case changeGuidance(RecordReference, GuidanceChange)
        case clearSavedChats
        /// Whether the sign-out that follows forgets the saved chats.
        case forgetOnSignOut(Bool)
        case phase(AppPhase)
        /// Whether a reply being written is revealed at a steady rate: the
        /// device's to say, and false where it asks for less motion.
        case pace(Bool)
    }

    /// An event, opaque outside ChatKit.
    public struct Event: Codable, Equatable, Sendable {
        let step: ChatAppStep
        init(_ step: ChatAppStep) { self.step = step }
    }

    /// An effect, opaque outside ChatKit.
    public struct Effect: Equatable, Sendable {
        let ask: ChatAppAsk
    }

    public typealias State = ChatAppState
    public typealias Child = Conversation

    public let storage: ChatStorage
    public let preferences: any PreferenceStore
    public let platform: ChatPlatform
    /// How many chats one read of the list asks for.
    let page: Int
    /// The presentation's parts as last made, each kept until what it reads
    /// changes: a streamed reply presents its own turn again, and neither
    /// the list nor the turns above it.
    let kept: PresenterParts

    /// - Parameter verifiesParts: every part answered from the store is
    ///   made again and compared, for a test: a part kept past a change to
    ///   what it reads stops the process.
    /// - Parameter page: how many chats one read of the list asks for,
    ///   for a test whose server holds few.
    public init(storage: ChatStorage, preferences: any PreferenceStore, platform: ChatPlatform, verifiesParts: Bool = false, page: Int = 100) {
        self.storage = storage
        self.preferences = preferences
        self.platform = platform
        self.page = page
        kept = PresenterParts(verifies: verifiesParts)
    }

    /// The watch stops while no window is in front on a phone, and runs
    /// while the app does on a Mac.
    var pausesWhenAway: Bool { platform == .ios }

    public static func == (lhs: ChatApp, rhs: ChatApp) -> Bool {
        lhs.storage === rhs.storage && (lhs.preferences as AnyObject) === (rhs.preferences as AnyObject) && lhs.platform == rhs.platform
            && lhs.page == rhs.page
    }

    public var initialState: State {
        var state = State()
        state.page = page
        return state
    }

    // MARK: Commands into events

    public func events(for command: Command, in context: Enrichment<ChatApp>) -> [Event] {
        let state = context.state
        let key = { ChildKey(context.mint().uuidString.lowercased()) }
        switch command {
        case let .sceneAppeared(scene, restoring, left, lands):
            guard lands else {
                return [Event(.sceneAppeared(scene: scene, restoring: restoring, key: restoring.flatMap { state.key(of: $0) } ?? key()))]
            }
            // The chat it was left in is gone back to only while that was
            // a short while ago.
            let fresh = left.map { context.now.timeIntervalSince($0) < Self.freshFor } ?? false
            return [Event(.sceneAppeared(scene: scene, restoring: fresh ? restoring : nil, key: key(), lands: true))]
        case let .sceneActive(scene, active): return [Event(.sceneActive(scene: scene, active: active))]
        case let .sceneGone(scene): return [Event(.sceneGone(scene: scene))]
        case let .show(thread, scene): return [Event(.show(thread: thread, scene: scene, key: state.key(of: thread) ?? key()))]
        case let .open(thread, message, scene):
            return [Event(.show(thread: thread, scene: scene, key: state.key(of: thread) ?? key(), message: message))]
        case let .newChat(agent, listen, scene): return [Event(.newChat(agent: agent, listen: listen, scene: scene, key: key()))]
        case let .back(scene): return [Event(.back(scene: scene))]
        case let .search(scene, query): return [Event(.searched(scene: scene, query: query))]
        case let .keepDraft(key, thread, text): return [Event(.draftKept(key: key, thread: thread, text: text))]
        case let .route(route): return [Event(.routed(route))]
        case let .claimRoute(scene):
            guard let route = state.route else { return [] }
            if case let .thread(thread) = route, let held = state.key(of: thread) { return [Event(.routeClaimed(scene: scene, key: held))] }
            return [Event(.routeClaimed(scene: scene, key: key()))]
        case .refresh: return [Event(.refreshAsked)]
        case let .markRead(thread): return [Event(.markedRead(thread))]
        case let .markUnread(thread): return [Event(.markedUnread(thread))]
        case let .pin(thread, pinned): return [Event(.pinAsked(thread: thread, pinned: pinned))]
        case let .archive(thread, archived): return [Event(.archiveAsked(thread: thread, archived: archived, at: context.now))]
        case .undoArchive: return [Event(.undoAsked)]
        case let .showArchived(scene, shown): return [Event(.archivedShown(scene: scene, shown: shown))]
        case let .more(archived): return [Event(.moreAsked(archived: archived))]
        case let .olderRows(thread): return [Event(.olderRowsAsked(thread))]
        case let .end(scene, key, shown):
            // Said again with nothing changed, it is no event.
            if (state.scenes[scene]?.atEnd == key) == shown { return [] }
            return [Event(.endShown(scene: scene, key: key, shown: shown))]
        case let .notifications(allow): return [Event(.notificationsAnswered(allow: allow))]
        case let .rename(thread, subject):
            let text = subject.trimmingCharacters(in: .whitespacesAndNewlines)
            return text.isEmpty ? [] : [Event(.renameAsked(thread: thread, subject: text))]
        case .dismissRename: return [Event(.renameDismissed)]
        case let .reloadRows(thread): return [Event(.rowsAsked(thread))]
        case let .addKey(provider, key):
            let value = key.trimmingCharacters(in: .whitespacesAndNewlines)
            return value.isEmpty ? [] : [Event(.keyAsked(provider: provider, key: value))]
        case let .loadCard(reference): return [Event(.cardAsked(reference))]
        case let .answer(question, picks): return [Event(.answerAsked(question, picks: picks))]
        case let .dismissQuestions(question): return [Event(.questionsDismissed(question))]
        case let .decide(suggestion, accept):
            // The change decided on is the one on screen.
            let shown = context.presentation?.conversations.values.lazy.compactMap { $0.cards[suggestion]?.suggestion?.values }.first ?? []
            return [Event(.decideAsked(suggestion, accept: accept, shown: Dictionary(shown.map { ($0.key, $0.value) }) { a, _ in a }))]
        case let .decideApproval(id, approve): return [Event(.approveAsked(id, approve: approve))]
        case let .retryCard(reference): return [Event(.cardRetried(reference))]
        case let .settings(command):
            switch command {
            case let .toggle(.app(row), on): return [Event(.setting(row: row, toggle: on, choice: nil))]
            case let .choose(.app(row), option): return [Event(.setting(row: row, toggle: nil, choice: option))]
            case .perform(.storage(.clear)): return [Event(.clearAsked)]
            default: return []
            }
        case .readSettings: return [Event(.settingsOpened)]
        case .readGuidance: return [Event(.guidance(.asked))]
        case let .changeGuidance(reference, change):
            // The text is required: an empty one is not sent.
            if case let .text(text) = change {
                let words = text.trimmingCharacters(in: .whitespacesAndNewlines)
                return words.isEmpty ? [] : [Event(.guidance(.changeAsked(reference, .text(words))))]
            }
            return [Event(.guidance(.changeAsked(reference, change)))]
        case .clearSavedChats: return [Event(.clearAsked)]
        case let .forgetOnSignOut(forget): return [Event(.forget(forget))]
        case let .phase(phase): return [Event(.phase(phase))]
        case let .pace(paces): return paces == state.paces ? [] : [Event(.paced(paces))]
        }
    }

    public func event(forLink link: LinkSnapshot) -> Event? { Event(.linked(link)) }

    public func event(atOpen replayed: Bool) -> Event? { Event(.phase(.active)) }

    /// What a conversation's event tells the list: everything but a streamed
    /// piece of text, which changes nothing the list keeps.
    public func event(fromChild key: ChildKey, _ event: Conversation.Event, after state: ConversationState) -> Event? {
        var named: String?
        var read: ChatAppStep.Read?
        var draft: String?
        var sent = false
        // A lost send gives its text back when the read for it ends, or
        // when a run that starts meanwhile ends that read.
        if state.chatFailure?.attempt == .send { draft = state.draft }
        switch event.step {
        case let .streamed(streamed, _) where streamed.kind != .done && streamed.kind != .idle:
            return nil
        case let .named(thread): named = thread
        case let .attachRead(record, rows): read = state.thread.map { ChatAppStep.Read(thread: $0, record: record, rows: rows, reload: false) }
        case let .handedOver(record, rows): read = state.thread.map { ChatAppStep.Read(thread: $0, record: record, rows: rows, reload: false) }
        case let .polled(record, rows): read = state.thread.map { ChatAppStep.Read(thread: $0, record: record, rows: rows, reload: false) }
        case let .checked(_, found?, _):
            named = found.thread
            read = ChatAppStep.Read(thread: found.thread, record: found.record, rows: found.rows, reload: !found.running)
        case .drafted, .rested, .filled, .heard, .sent:
            draft = state.draft
            if case .sent = event.step { sent = true }
        case .streamEnded:
            // A refusal gives the text back.
            draft = state.draft
        default:
            break
        }
        // A read found a new chat's pending message on a thread: the chat
        // is bound to it, as its `thread` event would have bound it.
        if let adopted = state.adopted { named = adopted }
        var spoke = false
        switch event.step {
        case .sent, .sentAgain, .continued: spoke = true
        // A reply typed into a notification is the person's message too,
        // where it went out.
        case .replyAsked: spoke = state.replying != nil
        default: break
        }
        // Whatever put a message back into the composer, the list keeps it.
        if state.gaveBack { draft = state.draft }
        return Event(
            .conversation(
                key,
                ChatAppStep.Report(
                    thread: state.thread, agent: state.agent, owns: state.ownsTranscript, active: state.active, streaming: state.streaming,
                    outcomes: state.held, name: state.agent.map(agentName) ?? "the assistant", settles: state.settles, named: named,
                    read: read, draft: draft, sent: sent, failure: state.failure, continueUnserved: state.continueUnserved,
                    unsent: state.pending, idsUnserved: state.idsUnserved, adopted: state.adopted != nil, gaveBack: state.gaveBack, spoke: spoke,
                    answered: state.answered, replied: state.replyOutcome)))
    }

    public func report(of event: Event) -> LinkReport? {
        switch event.step {
        case let .conversation(_, report): report.failure?.report
        case let .refreshFailed(failure), let .readFailed(failure), let .rowsFailed(_, _, failure), let .keyFailed(failure),
            let .cardFailed(_, failure),
            let .cardWriteFailed(_, failure), let .renameFailed(_, _, failure):
            failure.report
        case let .watchEnded(_, failure?): failure.report
        case .markWriteFailed(_, _, .signedOut, _): .accessEnded
        case let .pageFailed(_, _, failure, _), let .searchFailed(_, _, failure, _): failure.report
        case let .feedApplied(feed): feed.failure?.report
        case let .guidance(.readFailed(failure)), let .guidance(.writeFailed(_, failure, _)): failure.report
        default: nil
        }
    }

    public func resetting(of event: Event, on state: State) -> Bool { state.restarted }

    public func coalescing(of event: Event) -> Coalescing {
        if case let .conversation(_, report) = event.step, report.read == nil, report.named == nil, report.draft == nil {
            return .within(.milliseconds(50))
        }
        return .now
    }

    // MARK: Effects

    /// What the engine asked for, then what follows from how the state
    /// moved: the folder's files and the choices written when what they
    /// keep changed (never at an open, which read them), the badge when the
    /// unread count moved, and a chat that came on screen takes back its
    /// notifications, as does one a mark says was read, here or on another
    /// device.
    public func effects(of event: Event, on state: State, from previous: State, presented presentation: Presentation) -> [Effect] {
        var asks = state.asks
        if let disk = state.disk, disk == previous.disk {
            if state.cached != previous.cached { asks.append(.saveState(disk, state.cached)) }
            // A field tells its conversation what it holds as the app leaves
            // the front, in either order with the app's own word of it: so
            // the drafts are written at once on that word, and at once for
            // whatever changes them until the app is in front again. The
            // unsent messages go first, and at once: a message's first
            // copy waits for this write, and it carries the drafts too.
            var left = false
            if case let .phase(phase) = event.step { left = phase != .active }
            if state.unsent != previous.unsent {
                asks.append(.saveUnsent(disk, state.unsent, drafts: state.drafts))
            } else if left || state.drafts != previous.drafts {
                asks.append(.saveDrafts(disk, state.drafts, atOnce: state.leaving))
            }
            if state.preferences != previous.preferences { asks.append(.savePreferences(disk, state.preferences)) }
        }
        if state.unreadCount != previous.unreadCount { asks.append(.badge(state.unreadCount)) }
        var taken = state.visibleThreads.subtracting(previous.visibleThreads)
        // Read, not merely running again: a chat at work is not unread,
        // and what it notified before is still to be read. The first marks
        // a launch learns count too: what the device's own map notified
        // before them is taken back where they say it was read.
        if state.marks != nil, state.readState != previous.readState {
            taken.formUnion(previous.unreadThreads.subtracting(state.unreadThreads).filter { state.thread($0)?.running == false })
        }
        for thread in taken.sorted() {
            // Opened here, everything the chat notified goes. Read on
            // another device, the reply's own goes: a card's stands while
            // its chat may wait, and what an action could not do until the
            // chat is opened here.
            if !state.visibleThreads.contains(thread), state.mayWait(thread) || state.failedActions.contains(thread) {
                asks.append(.cancelNotification(ChatNotifications.replyID(thread)))
            } else {
                asks.append(.cancelGroup(thread))
            }
        }
        // The questions a notification can answer changed, or the folder
        // was just read: each one's options are its category's actions.
        let answerable = { (state: State) in state.interactions.filter { ChatNotifications.answerable($0) != nil } }
        if state.disk != nil, previous.disk == nil || answerable(state) != answerable(previous) {
            asks.append(.categories(ChatNotifications.categories(for: state.interactions)))
        }
        return asks.map(Effect.init)
    }

    public func key(of effect: Effect) -> String? {
        switch effect.ask {
        case .openDisk: "disk"
        case .refresh: "refresh"
        case .readFeatures: "features"
        case .readMarks: "marks"
        case let .confirmMark(thread): "confirmMark:\(thread)"
        case let .sight(thread, _): "sight:\(thread)"
        case .marksWait: "marksWait"
        case .cards: "cards"
        case .openings: "openings"
        case let .read(thread, _, _, _): "read:\(thread)"
        case let .loadCached(_, thread, _): "cached:\(thread)"
        case let .saveTranscript(_, thread, _): "save:\(thread)"
        case .saveState: "state"
        case .saveDrafts: "drafts"
        case .saveUnsent: "unsent"
        case .retryUnsent: "unsentRetry"
        case .savePreferences: "preferences"
        case .prefetch: "prefetch"
        case let .fetchThread(thread): "fetch:\(thread)"
        case let .rereadBehind(thread, _): "behind:\(thread)"
        case let .page(_, part, _): "page:\(part.rawValue)"
        case let .readOlder(thread, _, _): "older:\(thread)"
        case let .search(_, scene, _, _): "search:\(scene)"
        case .undoWait: "undo"
        case .watch: "watch"
        case .feedWait: "feedWait"
        case .applyFeed: "feedApply"
        case let .rename(thread, _, _): "rename:\(thread)"
        case .setKey: "key"
        case let .readCard(reference, _): "card:\(reference.id)"
        case let .writeCard(id, _, _, _), let .decideUnread(id, _, _): "write:\(id)"
        case .categories: "categories"
        case let .actionWait(id, _): "actWait:\(id)"
        case let .cardHold(id, _): "cardHold:\(id)"
        case let .readOwn(thread, _): "own:\(thread)"
        case .readGuidance: "guidance"
        case let .writeGuidance(reference, _, _), let .deleteGuidance(reference, _): "guidance:\(reference.path)"
        case .followRuns: "runs"
        case .followResponses: "responses"
        case .backgroundRefresh: "background"
        case let .continueRun(key, _): "continued:\(key.rawValue)"
        case .readUsage: "usage"
        case .readPermission: "permission"
        case let .cancel(key): key
        case .closeDisk, .prune, .clearTranscripts, .markRefreshed, .notify, .notifyHeld, .writeMark, .cancelGroup, .cancelAllNotifications, .badge,
            .askPermission, .finishRun, .scheduleRefresh, .endContinued, .notifyCard, .finishAction, .cancelNotification, .noteRow:
            nil
        }
    }

    // MARK: Children

    public func children(in state: State) -> [ChildKey: Conversation] {
        let visible = state.visibleThreads
        var out: [ChildKey: Conversation] = [:]
        for (key, entry) in state.conversations {
            let row = entry.thread.flatMap { state.thread($0) }
            let facts = row.map(ThreadFacts.init)
            let agent = facts?.agent ?? entry.agent ?? entry.seed.agent
            let held = entry.thread.flatMap { state.transcripts[$0] }
            // With no folder there is nothing to keep it in, and nothing
            // to wait for; nor is there where the folder refused the write.
            let unsaved = entry.unsent.flatMap { state.unsaved?.ids.contains($0) == true ? state.unsaved?.seen : nil }
            let kept = entry.unsent.flatMap { state.disk == nil || unsaved != nil || state.keptUnsent.contains($0) ? $0 : nil }
            let landed = entry.unsent.map { id in held?.records.contains { $0.id == id } ?? false } ?? false
            // A new chat's message, looked for in the rows of every thread
            // read: its first copy may have opened one.
            var landedOn: String?
            if entry.thread == nil, let id = entry.unsent {
                landedOn = state.transcripts.keys.sorted().first { thread in state.transcripts[thread]?.records.contains { $0.id == id } ?? false }
            }
            out[key] = Conversation(
                key: key, seed: entry.seed, facts: facts, online: state.online,
                keyless: state.keyless(agent, thread: entry.thread)?.name,
                newestTurn: held.flatMap { state.knows($0, of: entry.thread) ? newestTurn($0.records) : nil },
                visible: entry.thread.map(visible.contains) ?? state.shown(key),
                pollNudge: entry.pollNudge, resumeNudge: entry.resumeNudge, continuing: entry.continuing,
                continues: state.continues, agents: state.agentsRevision ?? 0, sendsIDs: state.sendsIDs, kept: kept, unsaved: unsaved,
                landed: landed, landedOn: landedOn, draftFound: entry.draftFound, rowTurns: held.map { persistedTurns($0.records) } ?? [],
                rowReplied: held.map { Self.Child.replied($0.records) } ?? -1, reply: entry.reply)
        }
        return out
    }
}
