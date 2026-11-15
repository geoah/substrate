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
        /// The archived chats' rows, for the window that lists them.
        var archived: [ThreadRow] = []
    }

    /// What one row of the list is made from: its thread's row and opening
    /// line, what the person's mark and the cards say of it, and how the
    /// device writes a time.
    struct ListRowReads: Equatable, Sendable {
        var record: Record
        var opening: String?
        var unread: Bool
        var attention: Int
        var pinned: Bool
        var archived: Bool
        var locale: Locale
        var timeZone: TimeZone
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
        /// The archived chats' rows, the messages the server found, the
        /// rows of the chats they are in that no list holds, and whether
        /// the server is searched at all.
        var archived: [ThreadRow]?
        var search: State.Search?
        var others: [String: Record]?
        var server = false
    }

    struct ConversationReads: Equatable, Sendable {
        var live: ConversationLive
        var state: State
        /// Where the chat's "New" line stands, which its entry holds.
        var newFrom: State.NewFrom?
        /// The message it was opened at, and how often it was put at a row.
        var opensAt: String?
        var visits = 0
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
            let search = searching ? state.searches[id] : nil
            let server = searching && state.searchesServer(scene.query)
            scenes[id] = kept.part(
                "scene/\(id)",
                reading: SceneReads(
                    scene: scene, thread: thread, listed: searching ? state.listed : nil, sections: searching ? listed.sections : nil,
                    transcripts: searching ? state.transcripts : nil, archived: searching ? listed.archived : nil, search: search,
                    others: search == nil ? nil : state.others, server: server)
            ) { self.scene(state, scene, thread: thread, listed: listed, search: search, server: server) }
        }
        kept.keep(under: "scene/") { scenes[String($0.dropFirst("scene/".count))] != nil }
        let framed = kept.part("frame", reading: state.framed) { self.framed(state) }
        var list = ChatListPresentation(
            title: "Chats", sections: listed.sections, offline: state.online ? nil : "Offline. Showing saved chats.",
            failure: listFailure(state), reconnecting: state.feedReconnecting,
            empty: listed.rows.isEmpty && state.loaded ? framed.emptyList : nil, loading: listed.rows.isEmpty && !state.loaded,
            showAutomated: state.preferences.showAutomated, searchPrompt: "Search chats")
        // The list's own facts that no kept part reads: how far its window
        // reaches, the chat just archived, and the archived ones.
        list.flags = state.flags
        list.more = state.window.read && state.window.cursor != nil
        list.loadingMore = state.window.reading
        list.placeholders = list.loading ? Self.placeholderRows : 0
        if let undo = state.undo, let thread = state.thread(undo.thread) {
            list.undo = UndoPresentation(thread: undo.thread, words: "Archived “\(state.title(thread))”", action: "Undo")
        }
        if state.flags {
            let window = state.archive.window
            list.archived = ArchivedList(
                title: "Archived", rows: listed.archived, loading: listed.archived.isEmpty && window.reading && !window.read,
                more: window.read && window.cursor != nil, loadingMore: window.reading && window.read,
                empty: listed.archived.isEmpty && !window.reading ? "No archived chats. A chat you archive is kept here, out of your list." : nil)
        }
        var conversations: [ChildKey: ConversationScreen] = [:]
        var redrawAt: Date?
        for (key, live) in input.children {
            // Kept until what it reads changes, or until the instant a
            // reply's rows, left as they were, may be parsed again.
            let screen = kept.timed(
                "chat/\(key.rawValue)/screen",
                reading: ConversationReads(
                    live: live, state: state.conversed(live.thread), newFrom: state.conversations[key]?.newFrom,
                    opensAt: state.conversations[key]?.opensAt, visits: state.conversations[key]?.visits ?? 0),
                at: input.now
            ) {
                let pace = TurnPresenter.Pace(now: input.now, reveals: state.paces)
                let screen = conversation(state, key, live, pace: pace)
                return (screen, pace.due ?? .distantFuture)
            }
            conversations[key] = screen.value
            if screen.until != .distantFuture { redrawAt = min(redrawAt ?? screen.until, screen.until) }
        }
        kept.keep(under: "chat/") { name in conversations[ChildKey(String(name.dropFirst("chat/".count).prefix { $0 != "/" }))] != nil }
        let settings = kept.part("settings", reading: state.settled) { self.settings(state) }
        return ChatPresentation(
            signedIn: state.session != nil, canChat: state.session?.canChat ?? true,
            host: state.session.map { ServerAddress.display($0.server) } ?? "", list: list, scenes: scenes,
            routePending: state.route != nil, onScreen: framed.onScreen, streaming: framed.streaming, conversations: conversations,
            offered: framed.offered, defaultAgent: framed.defaultAgent, settings: settings, unread: listed.unread,
            notificationOffer: state.notificationOffer == .offered ? Self.notificationOffer(platform) : nil,
            keys: framed.keys, keySaved: state.keySaved, answered: state.answered,
            redrawAt: redrawAt)
    }

    /// The tree is presented again at the instant a reply's next parse may
    /// start, where one was put off.
    public func nextChange(of presentation: ChatPresentation) -> Date? { presentation.redrawAt }

    /// The parts the app's tree goes out in: its own under no key, with no
    /// conversation in it, and each conversation's screen under its key. A
    /// delta then draws its conversation again and neither the list nor
    /// another conversation.
    public func parts(of presentation: ChatPresentation) -> [PresentationPart] {
        var own = presentation
        own.conversations = [:]
        own.redrawAt = nil
        return [PresentationPart(own)]
            + presentation.conversations.keys.sorted().map { PresentationPart(presentation.conversations[$0], at: [$0]) }
    }

    /// How many rows stand in for the chats while the first read is out.
    static let placeholderRows = 8
    /// How many of a chat's messages a search draws under it.
    static let hitsShown = 3

    /// The list from the threads, and the instant it stops being true as
    /// made: a row says "now" for a minute, a time until its day ends, a
    /// weekday for six days, and the day headings move at midnight. Each
    /// row is a part of its own, kept until its thread, its mark or its
    /// instant moves, so a list that grew by a page makes that page's rows
    /// and no row above them again.
    private func listed(_ state: State, _ input: PresenterInput<ChatApp>) -> (value: Listed, until: Date) {
        let calendar = input.calendar
        let threads = state.threads
        let archived = state.archivedThreads
        let standing = State.Standing(state)
        let today = calendar.startOfDay(for: input.now)
        let midnight = calendar.date(byAdding: .day, value: 1, to: today) ?? today.addingTimeInterval(86400)
        var until = midnight
        var made: [String: ThreadRow] = [:]
        func row(_ thread: ChatThread) -> ThreadRow {
            if let row = made[thread.id] { return row }
            let waits = state.marks != nil ? thread.waiting : state.attention(thread.id)
            let reads = ListRowReads(
                record: thread.record, opening: state.openings[thread.id], unread: state.unread(thread), attention: waits,
                pinned: standing.pinned(thread.id), archived: standing.archived(thread.id), locale: input.locale, timeZone: input.timeZone)
            let part = kept.timed("row/\(thread.id)", reading: reads, at: input.now) {
                var edge = midnight
                for age: TimeInterval in [60, 6 * 86400] {
                    let at = thread.activeAt.addingTimeInterval(age)
                    if at > input.now, at < edge { edge = at }
                }
                return (Self.row(reads, thread, agent: agentName(thread.agentID ?? ""), input), edge)
            }
            if part.until < until { until = part.until }
            made[thread.id] = part.value
            return part.value
        }
        let rows = threads.map(row)
        let shelved = archived.map(row)
        kept.keep(under: "row/") { made[String($0.dropFirst("row/".count))] != nil }
        return (
            Listed(
                rows: rows, sections: Self.sections(rows, threads, now: input.now, calendar: calendar),
                unread: rows.count { $0.unread }, archived: shelved),
            until
        )
    }

    /// One window: the chat it shows, and its search. The chats are found
    /// at once by their titles, their previews and the rows held on this
    /// device; the messages the server found come under them, by chat, with
    /// the words that matched.
    private func scene(_ state: State, _ scene: State.Scene, thread: String?, listed: Listed, search: State.Search?, server: Bool)
        -> ScenePresentation
    {
        let query = scene.query.trimmingCharacters(in: .whitespaces)
        var found: [DaySection]?
        var messages: [SearchGroup]?
        if !query.isEmpty {
            let matches = Set(
                (state.threads + state.archivedThreads).filter { thread in
                    state.title(thread).localizedCaseInsensitiveContains(query)
                        || (thread.preview?.localizedCaseInsensitiveContains(query) ?? false)
                        || (state.transcripts[thread.id]?.records.contains {
                            $0["content"]?.string?.localizedCaseInsensitiveContains(query) ?? false
                        } ?? false)
                }.map(\.id))
            // The rows as the list made them, under the same days, and the
            // archived ones after them, said to be so.
            var sections = listed.sections.compactMap { section -> DaySection? in
                let rows = section.rows.filter { matches.contains($0.id) }
                return rows.isEmpty ? nil : DaySection(title: section.title, rows: rows)
            }
            let shown = Set(sections.flatMap(\.rows).map(\.id))
            let shelved = listed.archived.filter { matches.contains($0.id) && !shown.contains($0.id) }
            if !shelved.isEmpty { sections.append(DaySection(title: "Archived", rows: shelved)) }
            found = sections
            if let search { messages = groups(state, search) }
        }
        let nothing = found?.isEmpty == true && (messages ?? []).isEmpty && search?.reading != true
        return ScenePresentation(
            current: scene.current, thread: thread, query: scene.query, found: found,
            nothingFound: nothing ? (server ? "Nothing matches “\(scene.query)”." : "No chat on this device matches “\(scene.query)”.") : nil,
            messages: messages, searching: search?.reading ?? false, archived: scene.archived && state.flags)
    }

    /// The messages a search found, by the chat each is in: the chats in
    /// the order of their best message, each with its first few and the
    /// words that matched.
    private func groups(_ state: State, _ search: State.Search) -> [SearchGroup] {
        let words = State.searchWords(search.query)
        var order: [String] = []
        var hits: [String: [Record]] = [:]
        for hit in search.hits {
            guard let thread = hit["thread"]?.referenceID else { continue }
            if hits[thread] == nil { order.append(thread) }
            hits[thread, default: []].append(hit)
        }
        return order.map { id in
            let row = state.thread(id)
            let name = agentName(row?.agentID ?? "")
            let found = hits[id] ?? []
            return SearchGroup(
                id: id, title: row.map(state.title) ?? "Chat", agent: row?.agentID, agentName: name,
                archived: state.archived(id) ? "Archived" : nil,
                hits: found.prefix(Self.hitsShown).map { hit in
                    let said = hit["content"]?.string ?? ""
                    let mine = hit["role"]?.string == "user"
                    return SearchHit(id: hit.id, who: mine ? "You" : name, runs: SearchSnippet.runs(of: mine ? said : plainText(markdown: said), words: words))
                },
                more: found.count > Self.hitsShown ? "and \(found.count - Self.hitsShown) more" : nil)
        }
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

    /// What the app notifies, in a sentence, before the system's own
    /// prompt, which says only that it would like to.
    static func notificationOffer(_ platform: ChatPlatform) -> NotificationOfferPresentation {
        NotificationOfferPresentation(
            title: "Hear when a reply is ready?",
            message: platform == .ios
                ? "Substrate Chat can notify you when an assistant finishes a reply or needs an answer from you while you are in another chat or another app."
                : "Substrate Chat can notify you when an assistant finishes a reply or needs an answer from you while you are in another chat or another window.",
            allow: "Turn On Notifications", decline: "Not Now")
    }

    /// The line above the first turn the person had not read when the chat
    /// was opened, in the rows held: the first turn after the message they
    /// had read through, or the first turn held where they never opened
    /// the chat. None where no such message is among the rows, or nothing
    /// that starts a turn follows it.
    static func newLine(_ held: State.Held?, from: State.NewFrom?) -> NewLine? {
        guard let held, let from else { return nil }
        let first: String?
        switch from {
        case .start:
            first = held.turns.first?.id
        case let .after(messages):
            guard let at = held.records.lastIndex(where: { messages.contains($0.id) }) else { return nil }
            let turns = Set(held.turns.map(\.id))
            first = held.records[(at + 1)...].first { turns.contains($0.id) }?.id
        }
        return first.map { NewLine(turn: $0, label: "New", spoken: "New messages") }
    }

    static func choice(_ agent: Agent) -> AgentChoice { AgentChoice(id: agent.id, name: agent.name, description: agent.description) }

    // MARK: The list

    static func row(_ reads: ListRowReads, _ thread: ChatThread, agent: String, _ input: PresenterInput<ChatApp>) -> ThreadRow {
        let title = thread.title(opening: reads.opening)
        let mark: ThreadRow.Mark? = reads.attention > 0 ? .attention : reads.unread ? .unread : nil
        return ThreadRow(
            id: thread.id, title: title, subject: thread.subject ?? title,
            stamp: Self.stamp(thread.activeAt, input), agent: thread.agentID, agentName: agent,
            preview: thread.running ? "Working…" : thread.preview ?? "", mode: thread.automated ? thread.mode : nil,
            unread: reads.unread, mark: mark, unfinished: Self.unfinished(thread), running: thread.running, pinned: reads.pinned,
            archived: reads.archived)
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

    /// The pinned chats first, in a section of their own, then the rest
    /// under their days: a chat is in one section.
    static func sections(_ rows: [ThreadRow], _ threads: [ChatThread], now: Date, calendar: Calendar) -> [DaySection] {
        let pinned = rows.filter(\.pinned)
        let rest = zip(rows, threads).filter { !$0.0.pinned }
        return (pinned.isEmpty ? [] : [DaySection(title: pinnedTitle, rows: pinned)])
            + DayGroup.allCases.compactMap { day in
                let inDay = rest.filter { DayGroup.of($0.1.activeAt, now: now, calendar: calendar) == day }.map(\.0)
                return inDay.isEmpty ? nil : DaySection(title: day.rawValue, rows: inDay)
            }
    }

    static let pinnedTitle = "Pinned"

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

    private func conversation(_ state: State, _ key: ChildKey, _ live: ConversationLive, pace: TurnPresenter.Pace) -> ConversationScreen {
        let row = live.thread.flatMap { state.thread($0) }
        let held = live.thread.flatMap { state.transcripts[$0] }
        let agent = state.agent(live.agent)
        let scope = "chat/\(key.rawValue)"
        // The rows' turns, under the ids they are drawn by, made and
        // presented once per read; a turn the stream draws each time it
        // moves, and a tool's row once.
        let rows = kept.part("\(scope)/rows", reading: RowReads(turns: held?.turns ?? [], aliases: live.aliases)) {
            StoredTurns(held?.turns ?? [], aliases: live.aliases)
        }
        let stored = rows.turns
        let made = kept.part("\(scope)/stored", reading: StoredReads(turns: stored, agent: agent?.record)) {
            let titles = recordTitles(stored)
            let scan = CardScan(stored)
            return Stored(
                turns: TurnPresenter.present(stored, agent: agent, liveKey: nil, titles: titles), titles: titles,
                decided: Set(scan.decisions.keys), scan: scan)
        }
        // One list of the two (`TurnFold`): a turn the stream drew, until
        // the row with its key is read, which then takes its place and
        // its id. A row whose reply is still being revealed is drawn on as
        // the stream's turn was.
        let reveal = { (turn: Turn) in "\(scope)/reveal/\(turn.id)" }
        var entries: [TurnFold.Entry]?
        var claims: [String: String] = [:]
        if !live.provisional.isEmpty {
            let folded = TurnFold.fold(rows, live.provisional)
            (entries, claims) = (folded.entries, folded.claims)
        } else if stored.contains(where: { kept.state(reveal($0), as: TextReveal.self)?.waits == true }) {
            entries = stored.enumerated().map { TurnFold.Entry(turn: $1, stored: $0) }
        }
        if let folded = entries {
            entries = folded.map { entry in
                guard entry.stored != nil, kept.state(reveal(entry.turn), as: TextReveal.self)?.waits == true else { return entry }
                return TurnFold.Entry(turn: entry.turn, stored: nil)
            }
        }
        let own = entries?.filter { $0.stored == nil }.map(\.turn) ?? []
        let all = entries?.map(\.turn) ?? stored
        let titles = kept.part("\(scope)/titles", reading: TitleReads(stored: made.titles, tools: own.map(\.tools))) {
            made.titles.merging(recordTitles(own)) { held, _ in held }
        }
        let decided = made.decided
        let turns =
            entries?.map { entry -> TurnPresentation in
                if let index = entry.stored { return made.turns[index] }
                let turn = entry.turn
                // Being written: the stream's last assistant turn, until
                // its row is read.
                let isLive = turn.row == nil && turn.id == live.liveKey
                let presented = kept.timed(
                    "\(scope)/turn/\(turn.id)",
                    reading: TurnReads(turn: turn, live: isLive, agent: agent?.record, decided: decided, titles: titles, reveals: pace.reveals),
                    at: pace.now
                ) {
                    let own = TurnPresenter.Pace(now: pace.now, reveals: pace.reveals)
                    let presented = TurnPresenter.present(
                        turn, agent: agent, live: isLive, decided: decided, titles: titles, kept: kept, scope: scope, pace: own)
                    return (presented, own.due ?? .distantFuture)
                }
                // Kept or made: where its rows wait for their next parse,
                // or more of its text for its turn to be drawn, the screen
                // is made again then too.
                if presented.until != .distantFuture { pace.wait(until: presented.until) }
                return presented.value
            } ?? made.turns
        kept.keep(under: "\(scope)/turn/") { name in own.contains { name == "\(scope)/turn/\($0.id)" } }
        kept.keep(under: "\(scope)/tool/") { name in own.contains { name.hasPrefix("\(scope)/tool/\($0.id)#") } }
        kept.keep(under: "\(scope)/blocks/") { name in own.contains { name == "\(scope)/blocks/\($0.id)" } }
        kept.keep(under: "\(scope)/reveal/") { name in own.contains { name == reveal($0) } }
        var scan = made.scan
        scan.merge(CardScan(own))
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
        var shown = cards(state, scan, titles: titles, answering: live.answering)
        for id in asked where shown[id] == nil {
            shown[id] = CardPresentation(
                approval: CardPresenter.approval(id, card: state.cards[id], pending: state.approvals.first { $0.id == id }, outcome: nil))
        }
        // A card that ended is the mark on the call that asked; and what
        // the stream still holds unread is said under its message.
        var folded = Self.queued(CardPresenter.fold(turns, cards: shown), live: live)
        // A call that says how its card ended says it once: the line the
        // rows hold for the same outcome is not drawn under it again.
        let ended = CardPresenter.ended(folded)
        let said = Set(scan.said.filter { ended.contains($0.value) }.keys)
        if !said.isEmpty { folded.removeAll { said.contains($0.id) } }
        // A decided suggestion whose call the transcript holds is drawn
        // there: the decision's own line stands without a second card.
        for (id, card) in shown where card.suggestion?.decided == true && scan.proposals[id] != nil { shown[id] = nil }
        // The "New" line stands above a turn as it is drawn: a row whose
        // turn was streamed in this conversation keeps the id the stream
        // gave it.
        var newLine = Self.newLine(held, from: state.conversations[key]?.newFrom)
        if let row = newLine?.turn, let drawn = claims[row] ?? live.aliases[row] { newLine?.turn = drawn }
        // A line that is not drawn holds nothing: the "New" line, and a
        // chat opened at a message, stand at the first turn drawn after it.
        let stands = { (turn: String) -> String in
            guard said.contains(turn), let at = all.firstIndex(where: { $0.id == turn }) else { return turn }
            return all[at...].first { !said.contains($0.id) }?.id ?? turn
        }
        if let row = newLine?.turn { newLine?.turn = stands(row) }
        let drawn = { (row: String) in claims[row] ?? live.aliases[row] ?? row }
        // Opened at a message, from a search: the turn of that row, once
        // the rows held have it; or the turn the stream drew of it, which
        // a message sent into a running turn is until that turn's rows
        // are read.
        let entry = state.conversations[key]
        let at = entry?.opensAt.flatMap { message in
            held?.turns.first { $0.row == message || $0.id == message }?.id ?? own.first { $0.id == message }?.id
        }.map(drawn).map(stands)
        let above = held?.above.map { RowsAbove(turn: stands(drawn($0)), count: held?.grown ?? 0) }
        return ConversationScreen(
            key: key, thread: live.thread, threadPath: live.thread.map { "\(Kinds.thread)/\($0)" },
            title: row.map(state.title) ?? (live.thread == nil ? "New Chat" : "Chat"),
            subtitle: working ? "\(live.name) is working…" : "with \(live.name)", subject: row?.subject ?? row.map(state.title) ?? "",
            agent: live.agent, agentName: live.name, hero: hero,
            older: Self.olderRows(held, reading: live.thread.map(state.olderReading.contains) ?? false),
            turns: folded, newLine: newLine, typing: live.awaitingFirstEvent, status: status,
            failures: failures(state, live: live, row: row, all: all, turns: folded), technical: technical,
            cards: shown, approvals: asked, titles: titles,
            composer: ComposerPresentation(
                draft: live.draft, draftRevision: live.draftRevision, draftReplaced: live.draftReplaced, draftBefore: live.draftBefore,
                draftGiven: live.draftGiven,
                placeholder: agent.map { "Message \($0.name)" } ?? "Message",
                blocked: keyless == nil ? blocked : nil, blockedWhenEmpty: blocked == "Offline",
                keyless: hero?.keyless == nil ? callout : nil,
                picksAgent: live.thread == nil, agentLabel: live.agent.map(agentName) ?? "No assistant", listenOnOpen: live.listenOnOpen,
                canSend: blocked == nil, status: Self.status(live, agent: agent), queues: live.queues,
                queued: Self.queuedNote(live), answering: Self.answering(state, live), focus: live.focuses),
            streaming: live.phase == .streaming, stopping: live.stopping,
            expandThinking: state.preferences.expandThinking, sends: live.sends,
            opens: at.map { .turn($0) } ?? newLine.map { .turn($0.turn) } ?? .end, visit: entry?.visits ?? 0, above: above)
    }

    /// The row at a transcript's top while its thread holds rows before
    /// the ones drawn.
    static func olderRows(_ held: State.Held?, reading: Bool) -> OlderRows? {
        guard let held, held.full, !held.records.isEmpty else { return nil }
        return OlderRows(label: reading ? "Loading earlier messages…" : "Earlier messages", loading: reading)
    }

    /// What the composer says is happening, from the send to the end of
    /// the reply: the newest thing the stream said.
    static func status(_ live: ConversationLive, agent: Agent?) -> ComposerStatus? {
        guard live.phase == .streaming else { return nil }
        if live.stopping { return ComposerStatus(kind: .stopping, words: "Stopping…") }
        if live.failures.reconnecting { return ComposerStatus(kind: .reconnecting, words: "Reconnecting…") }
        if !live.connected { return ComposerStatus(kind: .connecting, words: "Connecting…") }
        if let call = live.provisional.turns.reversed().lazy.compactMap({ $0.tools.last(where: \.running) }).first {
            return ComposerStatus(kind: .tool, words: TurnPresenter.runningWords(call, agent: agent))
        }
        if let writing = live.provisional.writing, !writing.thinking, !writing.content.isEmpty, writing.tools.isEmpty {
            return ComposerStatus(kind: .writing, words: "Writing…")
        }
        return ComposerStatus(kind: .thinking, words: "Thinking…")
    }

    /// The turns with each message the assistant has not read yet said so
    /// under it: sent while a reply was being written, it is read at the
    /// assistant's next step.
    static func queued(_ turns: [TurnPresentation], live: ConversationLive) -> [TurnPresentation] {
        guard !live.queued.isEmpty else { return turns }
        let waiting = Set(live.queued)
        return turns.map { turn in
            guard waiting.contains(turn.id), case var .user(message) = turn.body else { return turn }
            message.queued = "Queued for \(live.name)'s next step"
            return TurnPresentation(id: turn.id, body: .user(message))
        }
    }

    /// What the composer says while messages are queued: Stop is beside
    /// it, and a stop leaves them unread.
    static func queuedNote(_ live: ConversationLive) -> String? {
        guard live.phase == .streaming, !live.queued.isEmpty else { return nil }
        let held = live.queued.count == 1 ? "1 message is queued" : "\(live.queued.count) messages are queued"
        return "\(held). If you stop \(live.name) first, \(live.queued.count == 1 ? "it stays" : "they stay") unread until you continue."
    }

    /// What the composer says of the questions being answered in the
    /// person's own words.
    static func answering(_ state: State, _ live: ConversationLive) -> AnsweringNote? {
        guard let id = live.answering else { return nil }
        let record = state.cards[id]?.record ?? state.interactions.first { $0.id == id }
        // Answered or dismissed meanwhile, here or on another device:
        // there is nothing left to answer in any words.
        if let record, !Self.waits(record) { return nil }
        let questions = record?["questions"]?.array ?? []
        return AnsweringNote(
            id: id, title: "Answering in your own words", prompt: questions.count == 1 ? questions[0]["prompt"]?.text : nil,
            back: "Show Options")
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

    struct RowReads: Equatable, Sendable {
        var turns: [Turn]
        var aliases: [String: String]
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
        var reveals: Bool
    }

    /// The cards some turns land, by what their rows say alone: each
    /// suggestion's call and decision, each approval asked and how it
    /// ended, each set of questions.
    struct CardScan: Equatable, Sendable {
        var proposals: [String: ToolCall] = [:]
        var decisions: [String: DecisionNotice] = [:]
        var approvals: [String: ApprovalNotice] = [:]
        var answers: [String: InteractionNotice] = [:]
        var asked: [String] = []
        var interactions: [String] = []
        /// The card each outcome line is about, by the line's turn.
        var said: [String: String] = [:]

        init(_ turns: [Turn]) {
            proposals = CardPresenter.proposals(turns)
            for turn in turns {
                if let decision = decisionNotice(turn) {
                    decisions[decision.requestID] = decision
                    said[turn.id] = decision.requestID
                }
                if let ended = approvalNotice(turn) {
                    approvals[ended.approvalID] = ended
                    said[turn.id] = ended.approvalID
                }
                if let answer = interactionNotice(turn) {
                    answers[answer.interactionID] = answer
                    said[turn.id] = answer.interactionID
                }
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
            answers.merge(later.answers) { _, new in new }
            said.merge(later.said) { _, new in new }
            asked += later.asked
            interactions += later.interactions
        }
    }

    /// - Parameter answering: the questions the person is answering in
    ///   their own words, whose card is aside.
    private func cards(_ state: State, _ scan: CardScan, titles: [String: String], answering: String?) -> [String: CardPresentation] {
        var out: [String: CardPresentation] = [:]
        for interaction in scan.interactions {
            let card = state.cards[interaction]
            let pending = state.interactions.first { $0.id == interaction }
            let outcome = scan.answers[interaction]
            out[interaction] = CardPresentation(
                question: CardPresenter.question(interaction, card: card, pending: pending, outcome: outcome, aside: answering == interaction),
                resolution: CardPresenter.resolved(question: interaction, card: card, pending: pending, outcome: outcome))
        }
        for id in scan.asked {
            let pending = state.approvals.first { $0.id == id }
            let made = CardPresenter.approval(id, card: state.cards[id], pending: pending, outcome: scan.approvals[id])
            out[id] = CardPresentation(
                approval: made, resolution: CardPresenter.resolved(approval: id, made: made, card: state.cards[id], pending: pending))
        }
        for id in Set(scan.proposals.keys).union(scan.decisions.keys) {
            let made = CardPresenter.suggestion(
                id, card: state.cards[id], pending: state.requests.first { $0.id == id }, call: scan.proposals[id],
                outcome: scan.decisions[id], session: state.session, titles: titles)
            out[id] = CardPresentation(
                suggestion: made,
                resolution: CardPresenter.resolved(suggestion: id, made: made, card: state.cards[id], outcome: scan.decisions[id]))
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
            problem: failureLine(state, state.keyProblem))
    }
}
