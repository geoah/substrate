import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateRuntime
import SubstrateScreens

/// The presenter: the list, each window, each conversation drawn from its
/// own presentation and the rows the list holds, the cards, and Settings.
extension ChatApp {
    public typealias Presentation = ChatPresentation

    public func present(_ input: PresenterInput<ChatApp>) -> ChatPresentation {
        let state = input.state
        let calendar = input.calendar
        let rows = state.threads.map { row(state, $0, input) }
        let sections = Self.sections(rows, state.threads, now: input.now, calendar: calendar)
        var scenes: [String: ScenePresentation] = [:]
        for (id, scene) in state.scenes {
            let query = scene.query.trimmingCharacters(in: .whitespaces)
            var found: [DaySection]?
            if !query.isEmpty {
                let matches = state.threads.filter { thread in
                    state.title(thread).localizedCaseInsensitiveContains(query)
                        || (thread.preview?.localizedCaseInsensitiveContains(query) ?? false)
                        || (state.transcripts[thread.id]?.records.contains {
                            $0["content"]?.string?.localizedCaseInsensitiveContains(query) ?? false
                        } ?? false)
                }
                found = Self.sections(matches.map { row(state, $0, input) }, matches, now: input.now, calendar: calendar)
            }
            scenes[id] = ScenePresentation(
                current: scene.current, query: scene.query, found: found,
                nothingFound: found?.isEmpty == true ? "No chat on this device matches “\(scene.query)”." : nil)
        }
        let offered = state.offered
        var empty: EmptyList?
        if state.threads.isEmpty, state.loaded {
            empty = EmptyList(
                title: "No chats yet",
                message: offered.isEmpty
                    ? "There is no assistant to chat with yet. Install a package that ships one from the console's Providers page."
                    : "Start one with \(state.defaultAgent?.name ?? "an assistant").",
                action: offered.isEmpty ? nil : "New Chat")
        }
        let list = ChatListPresentation(
            title: "Chats", sections: sections, offline: state.online ? nil : "Offline. Showing saved chats.",
            problem: state.online ? state.refreshError.map { "Couldn't refresh: \($0)" } : nil, feedProblem: state.feedProblem,
            empty: empty, loading: state.threads.isEmpty && !state.loaded, showAutomated: state.preferences.showAutomated,
            searchPrompt: "Search chats")
        var conversations: [ChildKey: ConversationScreen] = [:]
        for (key, live) in input.children {
            conversations[key] = conversation(state, key, live)
        }
        var keys: [String: KeySheet] = [:]
        for (id, record) in state.providers where state.lacksKey(Provider(record: record)) {
            keys[id] = keySheet(state, Provider(record: record))
        }
        return ChatPresentation(
            signedIn: state.session != nil, canChat: state.session?.canChat ?? true,
            host: state.session.map { ServerAddress.display($0.server) } ?? "", list: list, scenes: scenes,
            routePending: state.route != nil, onScreen: state.visibleThreads.sorted(), conversations: conversations, offered: offered.map(Self.choice),
            defaultAgent: state.defaultAgent.map(Self.choice), settings: settings(state), unread: state.unreadCount,
            renameProblem: state.renameProblem, keys: keys, keySaved: state.keySaved, answered: state.answered)
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
        // The stored rows' turns, presented once per read; the live ones
        // each time the stream moves.
        let fingerprint = Self.fingerprint(held, agent: agent?.record)
        let made = memo.turns(for: key.rawValue, fingerprint: fingerprint) {
            let titles = recordTitles(stored)
            return (TurnPresenter.present(stored, agent: agent, liveKey: nil, titles: titles), titles)
        }
        var titles = made.titles
        titles.merge(recordTitles(live.turns)) { held, _ in held }
        let decided = Set(stored.compactMap { decisionNotice($0)?.requestID })
        let turns =
            made.turns
            + live.turns.map {
                TurnPresenter.present($0, agent: agent, live: $0.id == live.liveKey, decided: decided, titles: titles)
            }
        let all = stored + live.turns
        let working = live.phase == .streaming || live.phase == .background
        var hero: NewChatHero?
        if live.thread == nil, all.isEmpty {
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
            status.append(TurnFailureWords.note(failure, agent: live.name, provider: state.provider(of: live.agent)?.name))
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
        let keyless = state.keyless(live.agent)
        let callout = keyless.map {
            KeylessCallout(
                provider: $0.id, title: "Add an API key for \($0.name)",
                message: "Agents run on your own key, sealed on the provider's record.", action: "Add Key")
        }
        // A new chat says it above its openers; the composer, over the rest.
        if hero?.empty == nil { hero?.keyless = callout }
        let blocked = live.blocked
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
            cards: cards(state, all, titles: titles), titles: titles,
            composer: ComposerPresentation(
                draft: live.draft, draftRevision: live.draftRevision, placeholder: agent.map { "Message \($0.name)" } ?? "Message",
                blocked: keyless == nil ? blocked : nil, blockedWhenEmpty: blocked == "Offline",
                keyless: hero?.keyless == nil ? callout : nil,
                picksAgent: live.thread == nil, agentLabel: live.agent.map(agentName) ?? "No assistant", listenOnOpen: live.listenOnOpen,
                canSend: blocked == nil),
            streaming: live.phase == .streaming, stopping: live.stopping,
            error: live.error, expandThinking: state.preferences.expandThinking)
    }

    private static func fingerprint(_ held: State.Held?, agent: Record?) -> Int {
        var hasher = Hasher()
        hasher.combine(held?.version)
        hasher.combine(held?.records.count)
        hasher.combine(held?.records.last?.id)
        hasher.combine(agent)
        return hasher.finalize()
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

    private func cards(_ state: State, _ turns: [Turn], titles: [String: String]) -> [String: CardPresentation] {
        var out: [String: CardPresentation] = [:]
        let proposals = CardPresenter.proposals(turns)
        var decisions: [String: DecisionNotice] = [:]
        for turn in turns {
            if let decision = decisionNotice(turn) { decisions[decision.requestID] = decision }
            for call in turn.tools {
                if let interaction = interactionID(of: call) {
                    out[interaction] = CardPresentation(question: CardPresenter.question(interaction, card: state.cards[interaction]))
                }
            }
        }
        for id in Set(proposals.keys).union(decisions.keys) {
            out[id] = CardPresentation(
                suggestion: CardPresenter.suggestion(
                    id, card: state.cards[id], pending: state.requests.first { $0.id == id }, call: proposals[id], outcome: decisions[id],
                    session: state.session, titles: titles))
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
