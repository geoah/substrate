import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime

/// What is posted for a card, and what the list keeps of every notification
/// that stands.
///
/// A turn that ends waiting on a card would say two things: the card's, and
/// the reply's. One is posted, the card's, which is the one that needs the
/// person, with what the reply said above it where that fits. The two are
/// read apart (the card by the pending lists, the reply by its thread's
/// row), so a card seen while its turn still runs is held for the row to
/// settle, `cardHold` seconds at most, and a reply in a chat that waits is
/// not notified on its own.
///
/// `announced` holds every thread a notification stands for, a card's and a
/// failed action's as well as a reply's: each list read reads those
/// threads by name with their marks (`beside`), so what a chat notified is
/// taken back when it is archived or read on another device. A card's
/// notification is not answered by a read: it goes when its chat is opened
/// here or nothing waits in the chat any more. What an action from a
/// notification could not do stands until its chat is opened here, since
/// the text it kept is on this device alone.
extension ChatApp {
    /// How long a card's notification waits for its turn to settle.
    static let cardHold: Double = 3
    /// How often the mark read beside a message is asked, and how long
    /// between two asks (`confirmMark`).
    static let confirmTries = 3
    static let confirmPause: Double = 1

    /// The pending cards were read: each new one notifies, once.
    ///
    /// - Parameter holds: false in a background run, which is over before
    ///   a hold would be: a card is posted as its thread's row stands.
    func attend(_ state: inout State, holds: Bool) {
        for record in state.interactions + state.requests + state.approvals {
            let key = "card|\(record.kind)/\(record.id)"
            guard !state.notified.contains(key) else { continue }
            state.notified.insert(key)
            let thread = record["thread"]?.referenceID
            // A card is itself what waits on the person in its chat.
            guard state.baselined, state.preferences.notifyAttention, !(thread.map(state.visibleThreads.contains) ?? false),
                state.notifies(thread, waits: true)
            else { continue }
            if holds, let row = thread.flatMap({ state.thread($0) }), !Self.settled(row, since: record) {
                state.heldCards[record.id] = record
                state.asks.append(.cardHold(record.id, seconds: Self.cardHold))
            } else {
                post(&state, card: record)
            }
        }
    }

    /// Whether a thread's row is of a turn that ended after a card was
    /// made: its reply is then the one that asked.
    static func settled(_ row: ChatThread, since card: Record) -> Bool {
        guard !row.running, let finished = row.record["finishedAt"]?.string.flatMap(Timestamp.parse) else { return false }
        return finished >= card.createdDate
    }

    /// A thread's row settled: the cards held for it are posted now, with
    /// what its reply said.
    func attend(_ state: inout State, settled thread: ChatThread) {
        for id in state.heldCards.keys.sorted() where state.heldCards[id]?["thread"]?.referenceID == thread.id {
            guard let record = state.heldCards.removeValue(forKey: id) else { continue }
            state.asks.append(.cancel("cardHold:\(id)"))
            if stillAsks(state, record) { post(&state, card: record) }
        }
    }

    /// A card was held as long as it may be: posted as its row stands.
    func cardHoldDue(_ state: inout State, _ id: String) {
        guard let record = state.heldCards.removeValue(forKey: id) else { return }
        if stillAsks(state, record) { post(&state, card: record) }
    }

    /// Whether a card held for its row still waits where nobody sees it.
    private func stillAsks(_ state: State, _ held: Record) -> Bool {
        let thread = held["thread"]?.referenceID
        let now = state.cards[held.id]?.record ?? (state.interactions + state.requests + state.approvals).first { $0.id == held.id }
        return Self.waits(now) && !(thread.map(state.visibleThreads.contains) ?? false) && state.notifies(thread, waits: true)
    }

    /// Posts a card's notification, in place of its reply's.
    private func post(_ state: inout State, card record: Record) {
        let thread = record["thread"]?.referenceID
        let row = thread.flatMap { state.thread($0) }
        // An approval names the agent that asked, a sub-agent included.
        let asker = record.kind == Kinds.approval ? referent(record["agent"], pin: Kinds.agent) : nil
        let who = agentName(asker ?? row?.agentID ?? record["proposer"]?.string ?? "Your agent")
        let asked = row.flatMap { Self.settled($0, since: record) ? $0 : nil }
        let said = asked.flatMap { $0.status != "error" ? $0.preview : nil }
        let request = ChatNotifications.attention(record: record, thread: thread, agent: who, title: row.map(state.title) ?? "A chat", said: said)
        // With the categories as they stand with this record among them:
        // its options are its own category's actions.
        state.asks.append(.notifyCard(request, categories: ChatNotifications.categories(for: state.interactions)))
        guard let thread else { return }
        state.announced.insert(thread)
        state.attention[thread, default: []].append(request.id)
        // Posted after its turn settled: the reply's own notification,
        // shown or waiting to be, is this one's to stand for.
        if let finished = asked?.record["finishedAt"]?.string, state.cardSaid[record.id] == nil {
            state.cardSaid[record.id] = finished
            state.replySaid.remove(thread)
            state.asks.append(.cancelNotification(ChatNotifications.replyID(thread)))
        }
    }

    /// Whether the reply that settled a thread's row is said by a card's
    /// notification: one posted or held here for a card made in that turn.
    /// A card stands for the first settle after it was made and no later
    /// one, so a reply to a message sent while an older card still waits
    /// notifies as any reply does.
    func carried(_ state: inout State, _ thread: ChatThread, finished: String) -> Bool {
        guard let ended = Timestamp.parse(finished) else { return false }
        var carried = false
        for card in state.interactions + state.requests + state.approvals
        where card["thread"]?.referenceID == thread.id && Self.waits(card) && card.createdDate <= ended {
            let posted = state.attention[thread.id]?.contains(ChatNotifications.cardID(card.id, thread: thread.id)) == true
            guard posted || state.heldCards[card.id] != nil else { continue }
            if state.cardSaid[card.id] == nil { state.cardSaid[card.id] = finished }
            if state.cardSaid[card.id] == finished { carried = true }
        }
        return carried
    }

    /// A chat was read, on this device or another, while a card's
    /// notification or a failed action's still stands for it: the reply's
    /// own is taken back, once, and the others stay.
    func readElsewhere(_ state: inout State, _ thread: String) {
        if state.replySaid.remove(thread) != nil { state.asks.append(.cancelNotification(ChatNotifications.replyID(thread))) }
    }

    /// What an action from a notification could not do was said for a
    /// thread: it stands until the chat is opened here.
    func noteFailed(_ state: inout State, _ thread: String) {
        state.announced.insert(thread)
        state.failedActions.insert(thread)
    }

    /// After every event: a card's notification goes once nothing waits in
    /// its chat any more, and what is kept of a chat's notifications goes
    /// with the chat's, once it is opened or they were taken back.
    func settleAttention(_ state: inout State) {
        guard !state.attention.isEmpty || !state.failedActions.isEmpty || !state.replySaid.isEmpty else { return }
        let visible = state.visibleThreads
        for thread in state.attention.keys.sorted() {
            if visible.contains(thread) || !state.announced.contains(thread) {
                state.attention[thread] = nil
            } else if !state.mayWait(thread), state.heldCards.values.allSatisfy({ $0["thread"]?.referenceID != thread }) {
                for id in state.attention[thread] ?? [] { state.asks.append(.cancelNotification(id)) }
                state.attention[thread] = nil
                // Nothing else stands for it: the reply that follows an
                // answer is announced when it settles.
                if !state.failedActions.contains(thread), state.marks == nil { state.announced.remove(thread) }
            }
        }
        for thread in state.failedActions.sorted() where visible.contains(thread) || !state.announced.contains(thread) {
            state.failedActions.remove(thread)
        }
        state.replySaid.formIntersection(state.announced)
    }
}

extension ChatAppState {
    /// Whether a chat may be notified for at all: a reply that settled in
    /// it, a card that waits in it, what an action from a notification
    /// could not do in it. A chat its person archived is silent, but while
    /// something in it waits on a person (decision 0322). Every
    /// notification the app posts of a chat asks this, and so does taking
    /// one back; nothing else decides it.
    ///
    /// - Parameters:
    ///   - id: the chat. Nil for a card no chat asked, which nothing
    ///     archives.
    ///   - row: the chat's row, where the caller holds a newer one than
    ///     the lists do.
    ///   - waits: what is to be said is itself something that waits on the
    ///     person there, whatever the row counts yet.
    func notifies(_ id: String?, row: ChatThread? = nil, waits: Bool = false) -> Bool {
        guard let id, archived(id) else { return true }
        return waits || waitsOnPerson(id, row: row)
    }

    /// Whether something in a chat waits on a person. Where the server
    /// counts it on the thread's row, the row held says so: the pending
    /// lists may still hold a card that was answered on another device.
    /// Else, and for a chat no row is held of, a card of it among the
    /// pending ones.
    func waitsOnPerson(_ id: String, row: ChatThread? = nil) -> Bool {
        if readsMarks, let row = row ?? thread(id) { return row.waiting > 0 }
        return holdsCard(of: id)
    }

    /// Whether anything says a chat may still wait: its row's count, or a
    /// card of it among the pending ones. What takes a notification back
    /// asks this, since a card's notification taken back too soon is a
    /// question nobody hears of, and one kept too long goes at the next
    /// read.
    func mayWait(_ id: String) -> Bool {
        (thread(id)?.waiting ?? 0) > 0 || holdsCard(of: id)
    }

    private func holdsCard(of id: String) -> Bool {
        (interactions + requests + approvals).contains { $0["thread"]?.referenceID == id && ChatApp.waits($0) }
    }
}
