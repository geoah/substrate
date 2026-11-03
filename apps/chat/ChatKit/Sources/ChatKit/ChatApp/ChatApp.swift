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
        /// A window drew; `restoring` is the chat it showed when it last ran.
        case sceneAppeared(scene: String, restoring: String?)
        case sceneActive(scene: String, active: Bool)
        case sceneGone(scene: String)
        case show(thread: String, scene: String)
        case newChat(agent: String?, listen: Bool, scene: String)
        /// Back to the list on a phone: nothing is selected.
        case back(scene: String)
        case search(scene: String, query: String)
        case route(ChatRoute)
        /// The window that takes the route waiting.
        case claimRoute(scene: String)
        case refresh
        case markRead(String)
        case markUnread(String)
        case rename(thread: String, subject: String)
        /// A provider's key, for a password sign-in: sent once, never kept.
        case addKey(provider: String, key: String)
        case loadCard(CardReference)
        case answer(question: String, picks: [String: [String]])
        case dismissQuestions(String)
        case decide(suggestion: String, accept: Bool)
        case settings(SettingsCommand)
        /// Settings opened: the permission and the storage read again.
        case readSettings
        case clearSavedChats
        /// Whether the sign-out that follows forgets the saved chats.
        case forgetOnSignOut(Bool)
        case phase(AppPhase)
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
    /// The transcripts as last presented, so a streamed reply presents its
    /// own turn again and not every one above it.
    let memo = TurnMemo()

    public init(storage: ChatStorage, preferences: any PreferenceStore, platform: ChatPlatform) {
        self.storage = storage
        self.preferences = preferences
        self.platform = platform
    }

    /// The watch stops while no window is in front on a phone, and runs
    /// while the app does on a Mac.
    var pausesWhenAway: Bool { platform == .ios }

    public static func == (lhs: ChatApp, rhs: ChatApp) -> Bool {
        lhs.storage === rhs.storage && (lhs.preferences as AnyObject) === (rhs.preferences as AnyObject) && lhs.platform == rhs.platform
    }

    public var initialState: State { State() }

    // MARK: Commands into events

    public func events(for command: Command, in context: Enrichment<ChatApp>) -> [Event] {
        let state = context.state
        let key = { ChildKey(context.mint().uuidString.lowercased()) }
        switch command {
        case let .sceneAppeared(scene, restoring):
            return [Event(.sceneAppeared(scene: scene, restoring: restoring, key: restoring.flatMap { state.key(of: $0) } ?? key()))]
        case let .sceneActive(scene, active): return [Event(.sceneActive(scene: scene, active: active))]
        case let .sceneGone(scene): return [Event(.sceneGone(scene: scene))]
        case let .show(thread, scene): return [Event(.show(thread: thread, scene: scene, key: state.key(of: thread) ?? key()))]
        case let .newChat(agent, listen, scene): return [Event(.newChat(agent: agent, listen: listen, scene: scene, key: key()))]
        case let .back(scene): return [Event(.back(scene: scene))]
        case let .search(scene, query): return [Event(.searched(scene: scene, query: query))]
        case let .route(route): return [Event(.routed(route))]
        case let .claimRoute(scene):
            guard let route = state.route else { return [] }
            if case let .thread(thread) = route, let held = state.key(of: thread) { return [Event(.routeClaimed(scene: scene, key: held))] }
            return [Event(.routeClaimed(scene: scene, key: key()))]
        case .refresh: return [Event(.refreshAsked)]
        case let .markRead(thread): return [Event(.markedRead(thread))]
        case let .markUnread(thread): return [Event(.markedUnread(thread))]
        case let .rename(thread, subject):
            let text = subject.trimmingCharacters(in: .whitespacesAndNewlines)
            return text.isEmpty ? [] : [Event(.renameAsked(thread: thread, subject: text))]
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
        case let .settings(command):
            switch command {
            case let .toggle(.app(row), on): return [Event(.setting(row: row, toggle: on, choice: nil))]
            case let .choose(.app(row), option): return [Event(.setting(row: row, toggle: nil, choice: option))]
            case .perform(.storage(.clear)): return [Event(.clearAsked)]
            default: return []
            }
        case .readSettings: return [Event(.settingsOpened)]
        case .clearSavedChats: return [Event(.clearAsked)]
        case let .forgetOnSignOut(forget): return [Event(.forget(forget))]
        case let .phase(phase): return [Event(.phase(phase))]
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
        case .drafted, .sent:
            draft = state.draft
            if case .sent = event.step { sent = true }
        case .streamEnded:
            // A refusal gives the text back.
            draft = state.draft
        default:
            break
        }
        return Event(.conversation(key, ChatAppStep.Report(
            thread: state.thread, agent: state.agent, owns: state.ownsTranscript, active: state.active, streaming: state.streaming,
            outcomes: state.outcomes.count, name: state.agent.map(agentName) ?? "the assistant", settles: state.settles, named: named,
            read: read, draft: draft, sent: sent, failure: state.failure)))
    }

    public func report(of event: Event) -> LinkReport? {
        switch event.step {
        case let .conversation(_, report): report.failure?.report
        case let .refreshFailed(failure), let .readFailed(failure), let .keyFailed(failure), let .cardFailed(_, failure),
             let .cardWriteFailed(_, failure, _), let .renameFailed(_, _, failure):
            failure.report
        case let .watchEnded(_, failure?): failure.report
        case let .feedApplied(feed): feed.failure?.report
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

    public func effects(of event: Event, on state: State, presented presentation: Presentation) -> [Effect] {
        state.asks.map(Effect.init)
    }

    public func key(of effect: Effect) -> String? {
        switch effect.ask {
        case .openDisk: "disk"
        case .refresh: "refresh"
        case .cards: "cards"
        case .openings: "openings"
        case let .read(thread, _, _, _): "read:\(thread)"
        case let .loadCached(_, thread): "cached:\(thread)"
        case let .saveTranscript(_, thread, _): "save:\(thread)"
        case .saveState: "state"
        case .saveDrafts: "drafts"
        case .savePreferences: "preferences"
        case .prefetch: "prefetch"
        case let .fetchThread(thread): "fetch:\(thread)"
        case .watch: "watch"
        case .feedWait: "feedWait"
        case .applyFeed: "feedApply"
        case let .rename(thread, _, _): "rename:\(thread)"
        case .setKey: "key"
        case let .readCard(reference, _): "card:\(reference.id)"
        case let .writeCard(id, _, _, _), let .decideUnread(id, _, _): "write:\(id)"
        case .followRuns: "runs"
        case .backgroundRefresh: "background"
        case let .continueRun(key, _): "continued:\(key.rawValue)"
        case .readUsage: "usage"
        case .readPermission: "permission"
        case let .cancel(key): key
        case .closeDisk, .prune, .clearTranscripts, .markRefreshed, .notify, .cancelGroup, .cancelAllNotifications, .badge,
             .askPermission, .finishRun, .scheduleRefresh, .endContinued:
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
            out[key] = Conversation(
                key: key, seed: entry.seed, facts: facts, online: state.online, keyless: state.keyless(agent)?.name,
                newestTurn: held.map { newestTurn($0.records) }, visible: entry.thread.map(visible.contains) ?? state.shown(key),
                pollNudge: entry.pollNudge, resumeNudge: entry.resumeNudge, continuing: entry.continuing)
        }
        return out
    }
}
