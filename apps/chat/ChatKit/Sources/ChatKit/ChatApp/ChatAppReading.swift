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

    /// The provider an agent completes against, when it has no key.
    func keyless(_ agent: String?) -> Provider? {
        guard let provider = provider(of: agent) else { return nil }
        return provider.hasKey ? nil : provider
    }

    /// The provider an agent runs on, where its row is read.
    func provider(of agent: String?) -> Provider? {
        guard let id = self.agent(agent)?.providerID, let record = providers[id] else { return nil }
        return Provider(record: record)
    }

    func unread(_ thread: ChatThread) -> Bool {
        guard !thread.running, let finished = thread.record["finishedAt"]?.string else { return false }
        return seen[thread.id] != finished
    }

    /// The cards waiting on the owner in a thread: questions and suggestions.
    func attention(_ thread: String) -> Int {
        interactions.count { $0["thread"]?.referenceID == thread } + requests.count { $0["thread"]?.referenceID == thread }
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
        ChatAppAsk.Plan(showAutomated: preferences.showAutomated, canChat: session?.canChat ?? true,
                        canDecide: session?.canDecide ?? true, canReadProviders: session?.canReadProviders ?? true,
                        namesThreads: session?.grants != nil)
    }

    var threadIDs: [String] { (chats + automated).map(\.id) }

    var cached: CachedState {
        CachedState(agents: agents, providers: providers.values.sorted { $0.id < $1.id }, threads: chats, automated: automated,
                    openings: openings, interactions: interactions, requests: requests, seen: seen, notified: notified,
                    baselined: baselined, lastAgent: lastAgent)
    }
}
