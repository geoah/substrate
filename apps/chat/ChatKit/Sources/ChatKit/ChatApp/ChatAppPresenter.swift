import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateRuntime
import SubstrateScreens

/// The presenter: the list, each window, each conversation drawn from its
/// own presentation and the rows the list holds, the cards, and Settings.
/// It is made of parts, each kept until what it reads changes (`kept`, and
/// `ChatAppReads.swift` for what each reads): a streamed delta makes its
/// own conversation's live turn again and nothing else, and a state that
/// moved makes again only the parts that read what moved.
extension ChatApp {
    public typealias Presentation = ChatPresentation

    /// The chat list as made from the threads: its rows in order, under
    /// their days, and how many are unread.
    struct Listed: Equatable {
        var rows: [ThreadRow]
        var sections: [DaySection]
        var unread: Int
    }

    /// The rest of the app's own part: who is offered, what is on screen,
    /// which providers lack a key.
    struct Framed: Equatable {
        var offered: [AgentChoice]
        var defaultAgent: AgentChoice?
        var emptyList: EmptyList
        var onScreen: [String]
        var streaming: [ChildKey]
        var keys: [String: KeySheet]
    }

    struct ListReads: Equatable, Sendable {
        var state: State
        var locale: Locale
        var timeZone: TimeZone
    }

    struct SceneReads: Equatable, Sendable {
        var scene: State.Scene
        var thread: String?
        /// What a search reads, while the window searches: the threads, the
        /// list as made of them, and the rows held of every thread.
        var listed: State?
        var sections: [DaySection]?
        var transcripts: [String: State.Held]?
    }

    struct ConversationReads: Equatable, Sendable {
        var live: ConversationLive
        var state: State
    }

    public func present(_ input: PresenterInput<ChatApp>) -> ChatPresentation {
        let state = input.state
        let listed = kept.part(
            "list", reading: ListReads(state: state.listed, locale: input.locale, timeZone: input.timeZone), at: input.now
        ) { self.listed(state, input) }
        var scenes: [String: ScenePresentation] = [:]
        for (id, scene) in state.scenes {
            let thread = scene.current.flatMap { state.conversations[$0]?.thread }
            let searching = !scene.query.trimmingCharacters(in: .whitespaces).isEmpty
            scenes[id] = kept.part(
                "scene/\(id)",
                reading: SceneReads(
                    scene: scene, thread: thread, listed: searching ? state.listed : nil, sections: searching ? listed.sections : nil,
                    transcripts: searching ? state.transcripts : nil)
            ) { self.scene(state, scene, thread: thread, listed: listed) }
        }
        kept.keep(under: "scene/") { scenes[String($0.dropFirst("scene/".count))] != nil }
        let framed = kept.part("frame", reading: state.framed) { self.framed(state) }
        let list = ChatListPresentation(
            title: "Chats", sections: listed.sections, offline: state.online ? nil : "Offline. Showing saved chats.",
            problem: state.online ? state.refreshError.map { "Couldn't refresh: \($0)" } : nil, feedProblem: state.feedProblem,
            empty: listed.rows.isEmpty && state.loaded ? framed.emptyList : nil, loading: listed.rows.isEmpty && !state.loaded,
            showAutomated: state.preferences.showAutomated, searchPrompt: "Search chats")
        var conversations: [ChildKey: ConversationScreen] = [:]
        for (key, live) in input.children {
            conversations[key] = kept.part(
                "chat/\(key.rawValue)/screen", reading: ConversationReads(live: live, state: state.conversed(live.thread))
            ) { conversation(state, key, live) }
        }
        kept.keep(under: "chat/") { name in conversations[ChildKey(String(name.dropFirst("chat/".count).prefix { $0 != "/" }))] != nil }
        let settings = kept.part("settings", reading: state.settled) { self.settings(state) }
        return ChatPresentation(
            signedIn: state.session != nil, canChat: state.session?.canChat ?? true,
            host: state.session.map { ServerAddress.display($0.server) } ?? "", list: list, scenes: scenes,
            routePending: state.route != nil, onScreen: framed.onScreen, streaming: framed.streaming, conversations: conversations,
            offered: framed.offered, defaultAgent: framed.defaultAgent, settings: settings, unread: listed.unread,
            renameProblem: state.renameProblem, keys: framed.keys, keySaved: state.keySaved, answered: state.answered)
    }

    /// The parts the app's tree goes out in: its own under no key, with no
    /// conversation in it, and each conversation's screen under its key. A
    /// delta then draws its conversation again and neither the list nor
    /// another conversation.
    public func parts(of presentation: ChatPresentation) -> [PresentationPart] {
        var own = presentation
        own.conversations = [:]
        return [PresentationPart(own)]
            + presentation.conversations.keys.sorted().map { PresentationPart(presentation.conversations[$0], at: [$0]) }
    }

    /// The list from the threads, and the instant it stops being true as
    /// made: a row says "now" for a minute, a time until its day ends, a
    /// weekday for six days, and the day headings move at midnight.
    private func listed(_ state: State, _ input: PresenterInput<ChatApp>) -> (value: Listed, until: Date) {
        let calendar = input.calendar
        let threads = state.threads
        let rows = threads.map { row(state, $0, input) }
        let today = calendar.startOfDay(for: input.now)
        var until = calendar.date(byAdding: .day, value: 1, to: today) ?? today.addingTimeInterval(86400)
        for thread in threads {
            for age: TimeInterval in [60, 6 * 86400] {
                let edge = thread.activeAt.addingTimeInterval(age)
                if edge > input.now, edge < until { until = edge }
            }
        }
        return (
            Listed(
                rows: rows, sections: Self.sections(rows, threads, now: input.now, calendar: calendar),
                unread: rows.count { $0.unread }),
            until
        )
    }

    /// One window: the chat it shows, and its search over the titles, the
    /// previews and the rows held on this device.
    private func scene(_ state: State, _ scene: State.Scene, thread: String?, listed: Listed) -> ScenePresentation {
        let query = scene.query.trimmingCharacters(in: .whitespaces)
        var found: [DaySection]?
        if !query.isEmpty {
            let matches = Set(
                state.threads.filter { thread in
                    state.title(thread).localizedCaseInsensitiveContains(query)
                        || (thread.preview?.localizedCaseInsensitiveContains(query) ?? false)
                        || (state.transcripts[thread.id]?.records.contains {
                            $0["content"]?.string?.localizedCaseInsensitiveContains(query) ?? false
                        } ?? false)
                }.map(\.id))
            // The rows as the list made them, under the same days.
            found = listed.sections.compactMap { section in
                let rows = section.rows.filter { matches.contains($0.id) }
                return rows.isEmpty ? nil : DaySection(title: section.title, rows: rows)
            }
        }
        return ScenePresentation(
            current: scene.current, thread: thread, query: scene.query, found: found,
            nothingFound: found?.isEmpty == true ? "No chat on this device matches “\(scene.query)”." : nil)
    }

    private func framed(_ state: State) -> Framed {
        let offered = state.offered
        var keys: [String: KeySheet] = [:]
        for (id, record) in state.providers where state.lacksKey(Provider(record: record)) {
            keys[id] = keySheet(state, Provider(record: record))
        }
        return Framed(
            offered: offered.map(Self.choice), defaultAgent: state.defaultAgent.map(Self.choice),
            emptyList: EmptyList(
                title: "No chats yet",
                message: offered.isEmpty
                    ? "There is no assistant to chat with yet. Install a package that ships one from the console's Providers page."
                    : "Start one with \(state.defaultAgent?.name ?? "an assistant").",
                action: offered.isEmpty ? nil : "New Chat"),
            onScreen: state.visibleThreads.sorted(),
            streaming: state.conversations.filter(\.value.streaming).keys.sorted(), keys: keys)
    }

    static func choice(_ agent: Agent) -> AgentChoice { AgentChoice(id: agent.id, name: agent.name, description: agent.description) }

    // MARK: The list

    private func row(_ state: State, _ thread: ChatThread, _ input: PresenterInput<ChatApp>) -> ThreadRow {
        let unread = state.unread(thread)
        let mark: ThreadRow.Mark? = state.attention(thread.id) > 0 ? .attention : unread ? .unread : nil
        return ThreadRow(
            id: thread.id, title: state.title(thread), subject: thread.subject ?? state.title(thread),
            stamp: Self.stamp(thread.activeAt, input), agent: thread.agentID, agentName: agentName(thread.agentID ?? ""),
            preview: thread.running ? "Working…" : thread.preview ?? "", mode: thread.automated ? thread.mode : nil,
            unread: unread, mark: mark, unfinished: Self.unfinished(thread))
    }

    /// The console's pill for a chat whose last turn didn't finish.
    static func unfinished(_ thread: ChatThread) -> ThreadRow.Unfinished? {
        unfinishedTone(status: thread.status, reason: thread.reason).map { tone in
            let words =
                switch tone {
                case .bad: "Failed"
                case .warn: "Didn’t finish"
                case .quiet: "Stopped"
                }
            return ThreadRow.Unfinished(tone: tone, words: words)
        }
    }

    static func sections(_ rows: [ThreadRow], _ threads: [ChatThread], now: Date, calendar: Calendar) -> [DaySection] {
        DayGroup.allCases.compactMap { day in
            let inDay = zip(rows, threads).filter { DayGroup.of($0.1.activeAt, now: now, calendar: calendar) == day }.map(\.0)
            return inDay.isEmpty ? nil : DaySection(title: day.rawValue, rows: inDay)
        }
    }

    /// "now", "09:41", "Yesterday", "Mon", "12 Oct".
    static func stamp(_ date: Date, _ input: PresenterInput<ChatApp>) -> String {
        let calendar = input.calendar
        let age = input.now.timeIntervalSince(date)
        if age < 60 { return "now" }
        let style = Date.FormatStyle(locale: input.locale, calendar: calendar, timeZone: input.timeZone)
        if calendar.isDate(date, inSameDayAs: input.now) { return date.formatted(style.hour().minute()) }
        if let yesterday = calendar.date(byAdding: .day, value: -1, to: input.now), calendar.isDate(date, inSameDayAs: yesterday) {
            return "Yesterday"
        }
        if age < 6 * 86400 { return date.formatted(style.weekday(.abbreviated)) }
        return date.formatted(style.day().month(.abbreviated))
    }

    // MARK: A conversation

    private func conversation(_ state: State, _ key: ChildKey, _ live: ConversationLive) -> ConversationScreen {
        let row = live.thread.flatMap { state.thread($0) }
        let held = live.thread.flatMap { state.transcripts[$0] }
        let agent = state.agent(live.agent)
        let stored = held?.turns ?? []
        // The stored rows' turns, presented once per read; a live turn each
        // time it moves, and a tool's row once.
        let scope = "chat/\(key.rawValue)"
        let made = kept.part("\(scope)/stored", reading: StoredReads(turns: stored, agent: agent?.record)) {
            let titles = recordTitles(stored)
            let scan = CardScan(stored)
            return Stored(
                turns: TurnPresenter.present(stored, agent: agent, liveKey: nil, titles: titles), titles: titles,
                decided: Set(scan.decisions.keys), scan: scan)
        }
        let tools = live.turns.map(\.tools)
        let titles = kept.part("\(scope)/titles", reading: TitleReads(stored: made.titles, tools: tools)) {
            made.titles.merging(recordTitles(live.turns)) { held, _ in held }
        }
        let decided = made.decided
        let turns =
            made.turns
            + live.turns.map { turn in
                let isLive = turn.id == live.liveKey
                return kept.part(
                    "\(scope)/turn/\(turn.id)",
                    reading: TurnReads(turn: turn, live: isLive, agent: agent?.record, decided: decided, titles: titles)
                ) {
                    TurnPresenter.present(
                        turn, agent: agent, live: isLive, decided: decided, titles: titles, kept: kept, scope: scope)
                }
            }
        kept.keep(under: "\(scope)/turn/") { name in live.turns.contains { name == "\(scope)/turn/\($0.id)" } }
        kept.keep(under: "\(scope)/tool/") { name in live.turns.contains { name.hasPrefix("\(scope)/tool/\($0.id)#") } }
        kept.keep(under: "\(scope)/blocks/") { name in live.turns.contains { name == "\(scope)/blocks/\($0.id)" } }
        var scan = made.scan
        scan.merge(CardScan(live.turns))
        let working = live.phase == .streaming || live.phase == .background
        var hero: NewChatHero?
        if live.thread == nil, stored.isEmpty, live.turns.isEmpty {
            if let agent {
                hero = NewChatHero(title: agent.name, description: agent.description, prompts: agent.examplePrompts, empty: nil)
            } else {
                hero = NewChatHero(
                    title: "No assistant", description: nil, prompts: [],
                    empty: state.offered.isEmpty
                        ? "There is no assistant to chat with yet. Choose which agents are offered in Settings → Agents."
                        : "Choose who to chat with below.")
            }
        }
        var status: [ConversationNote] = []
        if live.incomplete, live.phase == .streaming {
            status.append(
                ConversationNote(
                    kind: .partial, title: nil,
                    message: "Part of this answer arrived before you opened the chat; it shows in full when it finishes."))
        }
        if live.phase == .background {
            status.append(
                ConversationNote(
                    kind: .background, title: nil,
                    message: "\(live.name) is working on this chat in the background. Its answer shows here when it finishes."))
        }
        // The thread's last turn, when it ended before it finished. Read off
        // the stored thread, so a reload says it too, and only once nothing
        // runs.
        let unfinished =
            live.phase == .idle && !live.deleted ? row.flatMap { unfinishedTurn(status: $0.status, reason: $0.reason, name: live.name) } : nil
        // A run that failed before this conversation was opened left its
        // words on the thread alone; the list marks it failed, so the chat says why.
        let failure = live.error ?? (working || row?.status != "error" ? nil : row?.reason ?? "The run ended in an error.")
        // The notice of a turn that ended in an error says what failed, from
        // the same words: the card stays beside it only where it names a
        // cause the notice cannot, a refused key or a rate limit.
        if let failure, unfinished?.tone != .bad || TurnFailureWords.cause(failure) != .other {
            status.append(
                TurnFailureWords.note(failure, agent: live.name, provider: state.provider(of: live.agent, thread: live.thread)?.name))
        }
        if let failed = live.reloadFailed { status.append(ConversationNote(kind: .reloadFailed, title: nil, message: failed, action: "Try Again")) }
        if live.deleted {
            status.append(
                ConversationNote(
                    kind: .deleted, title: "This chat was deleted",
                    message: "It is no longer on the server. What you typed stays below, to copy."))
        }
        var technical: [LabeledValue]?
        if state.preferences.technicalDetails, let row {
            technical = [LabeledValue("Thread", "\(Kinds.thread)/\(row.id)", verbatim: true)] + row.tally
        }
        let keyless = state.keyless(live.agent, thread: live.thread)
        let callout = keyless.map {
            KeylessCallout(
                provider: $0.id, title: "Add an API key for \($0.name)",
                message: "Agents run on your own key, sealed on the provider's record.", action: "Add Key")
        }
        // A new chat says it above its openers; the composer, over the rest.
        if hero?.empty == nil { hero?.keyless = callout }
        let blocked = live.blocked
        let asked = live.thread.map(state.subagentApprovals) ?? []
        var shown = cards(state, scan, titles: titles)
        for id in asked where shown[id] == nil {
            shown[id] = CardPresentation(
                approval: CardPresenter.approval(id, card: state.cards[id], pending: state.approvals.first { $0.id == id }, outcome: nil))
        }
        return ConversationScreen(
            key: key, thread: live.thread, threadPath: live.thread.map { "\(Kinds.thread)/\($0)" },
            title: row.map(state.title) ?? (live.thread == nil ? "New Chat" : "Chat"),
            subtitle: working ? "\(live.name) is working…" : "with \(live.name)", subject: row?.subject ?? row.map(state.title) ?? "",
            agent: live.agent, agentName: live.name, hero: hero,
            olderHidden: (held?.records.count ?? 0) >= 500 ? "Older messages aren't shown here." : nil,
            turns: turns, typing: live.awaitingFirstEvent, status: status,
            unfinished: unfinished.map {
                UnfinishedNotice(
                    tone: $0.tone, title: $0.title, detail: $0.detail, action: "Continue", blocked: blocked,
                    hint: "Sends “\(Conversation.continueMessage)”")
            },
            outcomes: live.outcomes.map(Self.outcome), technical: technical,
            cards: shown, approvals: asked, titles: titles,
            composer: ComposerPresentation(
                draft: live.draft, draftRevision: live.draftRevision, draftReplaced: live.draftReplaced, draftBefore: live.draftBefore,
                placeholder: agent.map { "Message \($0.name)" } ?? "Message",
                blocked: keyless == nil ? blocked : nil, blockedWhenEmpty: blocked == "Offline",
                keyless: hero?.keyless == nil ? callout : nil,
                picksAgent: live.thread == nil, agentLabel: live.agent.map(agentName) ?? "No assistant", listenOnOpen: live.listenOnOpen,
                canSend: blocked == nil),
            streaming: live.phase == .streaming, stopping: live.stopping,
            error: live.error, expandThinking: state.preferences.expandThinking)
    }

    /// The stored rows' part of a conversation: its turns as drawn, what
    /// its tool results named each record, and the cards its turns land.
    struct Stored: Equatable {
        var turns: [TurnPresentation]
        var titles: [String: String]
        /// The suggestions a stored turn decided.
        var decided: Set<String>
        var scan: CardScan
    }

    struct StoredReads: Equatable, Sendable {
        var turns: [Turn]
        var agent: Record?
    }

    struct TitleReads: Equatable, Sendable {
        var stored: [String: String]
        var tools: [[ToolCall]]
    }

    struct TurnReads: Equatable, Sendable {
        var turn: Turn
        var live: Bool
        var agent: Record?
        var decided: Set<String>
        var titles: [String: String]
    }

    /// The cards some turns land, by what their rows say alone: each
    /// suggestion's call and decision, each approval asked and how it
    /// ended, each set of questions.
    struct CardScan: Equatable, Sendable {
        var proposals: [String: ToolCall] = [:]
        var decisions: [String: DecisionNotice] = [:]
        var approvals: [String: ApprovalNotice] = [:]
        var asked: [String] = []
        var interactions: [String] = []

        init(_ turns: [Turn]) {
            proposals = CardPresenter.proposals(turns)
            for turn in turns {
                if let decision = decisionNotice(turn) { decisions[decision.requestID] = decision }
                if let ended = approvalNotice(turn) { approvals[ended.approvalID] = ended }
                for call in turn.tools {
                    if let interaction = interactionID(of: call) { interactions.append(interaction) }
                    if let approval = approvalID(of: call) { asked.append(approval) }
                }
            }
        }

        /// The turns after these: theirs replace what these said of the
        /// same card.
        mutating func merge(_ later: CardScan) {
            proposals.merge(later.proposals) { _, new in new }
            decisions.merge(later.decisions) { _, new in new }
            approvals.merge(later.approvals) { _, new in new }
            asked += later.asked
            interactions += later.interactions
        }
    }

    static func outcome(_ outcome: Outcome) -> OutcomePresentation {
        switch outcome.kind {
        case .refused:
            OutcomePresentation(
                id: outcome.id.uuidString, kind: .refused, title: "Your message wasn't sent",
                message: "The server said: \(outcome.reason)" + (outcome.restored ? "\nIt's back in the box below." : ""),
                text: outcome.message)
        case .checking:
            OutcomePresentation(
                id: outcome.id.uuidString, kind: .checking, title: nil,
                message: "The connection dropped before the server confirmed your message. Checking whether it arrived…", text: nil)
        case .unknown:
            OutcomePresentation(
                id: outcome.id.uuidString, kind: .unknown, title: "It may not have arrived",
                message:
                    "The connection dropped before the server confirmed your message (\(outcome.reason)). Look for it in this chat, or for a new chat in the list, before sending it again.",
                text: outcome.message)
        }
    }

    private func cards(_ state: State, _ scan: CardScan, titles: [String: String]) -> [String: CardPresentation] {
        var out: [String: CardPresentation] = [:]
        for interaction in scan.interactions {
            out[interaction] = CardPresentation(question: CardPresenter.question(interaction, card: state.cards[interaction]))
        }
        for id in scan.asked {
            out[id] = CardPresentation(
                approval: CardPresenter.approval(
                    id, card: state.cards[id], pending: state.approvals.first { $0.id == id }, outcome: scan.approvals[id]))
        }
        for id in Set(scan.proposals.keys).union(scan.decisions.keys) {
            out[id] = CardPresentation(
                suggestion: CardPresenter.suggestion(
                    id, card: state.cards[id], pending: state.requests.first { $0.id == id }, call: scan.proposals[id],
                    outcome: scan.decisions[id], session: state.session, titles: titles))
        }
        return out
    }

    private func keySheet(_ state: State, _ provider: Provider) -> KeySheet {
        let console = state.session?.canWriteKeys == false ? state.session?.console("data/\(Kinds.provider)/\(segment(provider.id))") : nil
        let host = state.session?.server.host ?? "your server"
        return KeySheet(
            id: provider.id, title: "Add API Key", header: "\(provider.name) API key",
            footer: console == nil
                ? "Agents run on your own key. It is sealed on the provider's record on \(host) and sent to \(provider.endpoint)."
                : "Its completions go to \(provider.endpoint).",
            console: console, consoleLink: "Open \(provider.name) in the Console",
            consoleNote: "Agents run on your own key, sealed on the provider's record. Add it in the console, where only you can write it.",
            problem: state.keyProblem)
    }
}
