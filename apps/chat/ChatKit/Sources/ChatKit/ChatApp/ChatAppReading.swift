import Foundation
import SubstrateCore
import SubstrateRuntime

/// What the list's state says, derived: for the engine, the children and
/// the presenter alike.
extension ChatAppState {
    var agentList: [Agent] { agents.map(Agent.init(record:)) }

    func agent(_ id: String?) -> Agent? {
        guard let id else { return nil }
        return agents.first { $0.id == id }.map(Agent.init(record:))
    }

    /// The agents offered for a new chat, by name.
    var offered: [Agent] {
        let all = agentList
        let defaults = defaultOffered(all)
        return all.filter { preferences.offers($0, defaults: defaults) }.sorted { $0.name < $1.name }
    }

    /// The agent a new chat goes to: Settings' default, else the last one
    /// chatted with, else the first offered.
    var defaultAgent: Agent? {
        let offered = offered
        for id in [preferences.defaultAgent, lastAgent] {
            if let id, let agent = offered.first(where: { $0.id == id }) { return agent }
        }
        return offered.first
    }

    /// The chat list: chats, and automated runs when shown, newest activity
    /// first.
    var threads: [ChatThread] {
        let records = preferences.showAutomated ? chats + automated : chats
        return records.map(ChatThread.init(record:)).sorted { $0.activeAt > $1.activeAt }
    }

    func thread(_ id: String) -> ChatThread? {
        if let record = chats.first(where: { $0.id == id }) ?? automated.first(where: { $0.id == id }) ?? others[id] {
            return ChatThread(record: record)
        }
        return nil
    }

    func title(_ thread: ChatThread) -> String { thread.title(opening: openings[thread.id]) }

    /// The provider whose key a conversation's next turn waits on, where
    /// none lets it run: an agent that names its provider, that row when no
    /// key resolves for it. An agent on `runsOn`: in a new chat (`thread`
    /// nil), the row a key would open where no entry can run; in a continued
    /// one, the row its thread is stamped with where that cannot run.
    func keyless(_ agent: String?, thread: String? = nil) -> Provider? {
        guard let agent = self.agent(agent) else { return nil }
        guard let ids = agent.runsOn else {
            guard let provider = provider(of: agent.id) else { return nil }
            return lacksKey(provider) ? provider : nil
        }
        let row: String?
        if let thread {
            row = self.thread(thread).flatMap { continued(ids, thread: $0).refusal?.keyable }
        } else {
            row = runsOn(ids).keyProvider
        }
        return row.flatMap { providers[$0] }.map(Provider.init(record:))
    }

    /// Whether no key resolves for `provider`. Until the keys are read, no
    /// row is said to lack one.
    func lacksKey(_ provider: Provider) -> Bool {
        keys.map { !$0.resolves(provider) } ?? false
    }

    /// The provider an agent runs on, where its row is read: the row it
    /// names, or for an agent on `runsOn` the row of the entry a new chat
    /// takes, and in a continued chat the row its thread is stamped with.
    func provider(of agent: String?, thread: String? = nil) -> Provider? {
        guard let agent = self.agent(agent) else { return nil }
        let id =
            if agent.runsOn != nil, let thread {
                self.thread(thread).flatMap { referent($0.record["provider"], pin: Kinds.provider) }
            } else {
                providerID(of: agent)
            }
        guard let id, let record = providers[id] else { return nil }
        return Provider(record: record)
    }

    /// The row a new chat with an agent runs on, by id: the one it names, or
    /// the row of the entry a new chat takes.
    func providerID(of agent: Agent) -> String? {
        guard let ids = agent.runsOn else { return agent.providerID }
        return runsOn(ids).shown?.model?.providerID
    }

    /// The model id a new chat with an agent sends: its own, or the one of
    /// the entry a new chat takes.
    func model(of agent: Agent) -> String? {
        guard let ids = agent.runsOn else { return agent.model }
        return runsOn(ids).shown?.model?.model
    }

    // MARK: What an agent on runsOn runs on

    /// Whether a call for `model` on the row `id` can be sent now (the
    /// server's `modelAvailable`). Until the keys are read, or where the
    /// session may not read the rows, it cannot be told.
    func availability(_ id: String, model: String) -> Availability {
        guard let keys, session?.canReadProviders ?? true else { return .unknown }
        guard let record = providers[id] else { return .no(.noProvider(id)) }
        return keys.availability(Provider(record: record), model: model)
    }

    /// What a new chat with an agent on `runsOn` runs on (the server's
    /// `chooseRunsOn`).
    func runsOn(_ ids: [String]) -> RunsOnChoice {
        RunsOnChoice(
            entries: ids.map { id in
                guard let models else { return RunsOnEntry(id: id, availability: .unknown) }
                guard let record = models[id] else { return RunsOnEntry(id: id, availability: .no(.noRecord)) }
                let model = ModelRecord(record: record)
                return RunsOnEntry(id: id, model: model, availability: availability(model.providerID, model: model.model))
            })
    }

    /// What a continued chat runs on (the server's `continuedRunsOn`): the
    /// entry naming the row and model its thread is stamped with. One the
    /// agent no longer names is refused, whatever the others offer, with no
    /// row a key would open.
    func continued(_ ids: [String], thread: ChatThread) -> Availability {
        guard let models else { return .unknown }
        let row = referent(thread.record["provider"], pin: Kinds.provider) ?? ""
        let model = thread.record["model"]?.string ?? ""
        for id in ids {
            guard let record = models[id] else { continue }
            let entry = ModelRecord(record: record)
            if entry.providerID == row, entry.model == model { return availability(row, model: model) }
        }
        return Availability(available: false)
    }

    func unread(_ thread: ChatThread) -> Bool {
        guard !thread.running, let finished = thread.record["finishedAt"]?.string else { return false }
        return seen[thread.id] != finished
    }

    /// The cards waiting on the owner in a thread: questions, suggestions
    /// and approvals.
    func attention(_ thread: String) -> Int {
        (interactions + requests + approvals).count { $0["thread"]?.referenceID == thread }
    }

    /// The approvals a sub-agent asked on a conversation: its own tool rows
    /// are in its own thread, so they wait after the transcript.
    func subagentApprovals(_ thread: String) -> [String] {
        approvals.filter { $0["thread"]?.referenceID == thread && $0["requestedIn"]?.referenceID != nil }.map(\.id)
    }

    var unreadCount: Int { threads.count { unread($0) } }

    /// Rule 1: there is one conversation per thread in the process.
    func key(of thread: String) -> ChildKey? {
        conversations.first { $0.value.thread == thread }?.key
    }

    /// Rule 3: a conversation owns its thread's rows while it runs, or
    /// while its overlay waits for a handover read that failed.
    func owned(_ thread: String) -> Bool {
        conversations.values.contains { $0.thread == thread && $0.owns }
    }

    /// The threads a window in front shows.
    var visibleThreads: Set<String> {
        Set(scenes.values.compactMap { scene in scene.active && appActive ? scene.current.flatMap { conversations[$0]?.thread } : nil })
    }

    /// A window in front shows the conversation (a new chat has no thread).
    func shown(_ key: ChildKey) -> Bool { appActive && scenes.values.contains { $0.active && $0.current == key } }

    /// Rule 7: cached rows are current only while their thread is settled
    /// and its version is the one they were read under. A message write
    /// does not move its thread, so `updatedAt` is never the test.
    func isCurrent(_ thread: String) -> Bool {
        guard let row = self.thread(thread), let held = transcripts[thread] else { return false }
        return !row.running && held.version == row.record.version && !held.full
    }

    /// Rows read under the thread's current version, a whole transcript or
    /// a prefetch's window: a prefetch has nothing to add.
    func isPrefetched(_ thread: String) -> Bool {
        guard let row = self.thread(thread), let held = transcripts[thread] else { return false }
        return !row.running && held.version == row.record.version
    }

    var plan: ChatAppAsk.Plan {
        ChatAppAsk.Plan(
            showAutomated: preferences.showAutomated, canChat: session?.canChat ?? true,
            canDecide: session?.canDecide ?? true, canReadProviders: session?.canReadProviders ?? true,
            canReadModels: session?.canReadModels ?? true, readsOperator: session?.full ?? true,
            namesThreads: !(session?.full ?? true))
    }

    var threadIDs: [String] { (chats + automated).map(\.id) }

    var cached: CachedState {
        CachedState(
            agents: agents, providers: providers.values.sorted { $0.id < $1.id },
            models: models.map { $0.values.sorted { $0.id < $1.id } }, keys: keys, threads: chats, automated: automated,
            openings: openings, interactions: interactions, requests: requests, seen: seen, notified: notified,
            baselined: baselined, lastAgent: lastAgent, memories: guidance.read ? guidance.memories : nil,
            instructions: guidance.read ? guidance.instructions : nil)
    }
}
