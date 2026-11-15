import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateRuntime

/// Read state where the server keeps it (decision 0313): the person's mark of
/// each thread, read with every list of threads and followed on the change
/// feed, so a chat read on one device is read on every other. What this
/// device moves reads so here at once and is written a moment later; an
/// answer that left the server earlier never replaces a later one, so a dot
/// that went does not come back unless the person marks the chat unread. On
/// a server older than marks `marks` stays nil, and everything here stands
/// still: the device's own `seen` map says what is unread, as before.
extension ChatAppState {
    /// Whether the person's marks are read with the lists: the server's
    /// document says it keeps them, or, until that is read, the device
    /// holds some from the last time it did.
    var readsMarks: Bool {
        guard !marksUnserved else { return false }
        if let recordsRevision { return recordsRevision >= Self.marksRevision }
        return marks != nil
    }

    /// The person's mark of a thread as it reads on this device, for the
    /// newest message `last` the thread settled with: the server's, with
    /// what this device moved and has not heard back about laid over it.
    /// Nil while nothing is known of it, when the thread is not said to be
    /// unread.
    func mark(_ thread: String, last: String?) -> ThreadMark? {
        let moved = [markSending[thread], marking[thread]].compactMap { $0 }
        guard var mark = marks?[thread] ?? (moved.isEmpty ? nil : ThreadMark.none) else { return nil }
        for change in moved { mark = change.over(mark, last: last) }
        return mark
    }

    /// Whether anything is known of the person's mark of a thread: the
    /// server said it (or that there is none), or this device moved it.
    func marked(_ thread: String) -> Bool {
        marks?[thread] != nil || marking[thread] != nil || markSending[thread] != nil
    }

    /// Whether a thread's last call failed with nobody having looked since.
    /// A call that fails before any reply moves no `lastMessage` (the
    /// server passes over a message no reply came after), so the chat is
    /// not unread by it and its mark says nothing of the failure: it is
    /// seen only where the position was taken after the thread settled.
    func failedUnseen(_ thread: ChatThread) -> Bool {
        guard thread.status == "error", let settled = thread.record["finishedAt"]?.string.flatMap(Timestamp.parse) else { return false }
        guard let at = mark(thread.id, last: thread.lastMessage)?.readAt else { return true }
        return at <= settled
    }

    /// Everything what is unread is read from, to tell an event that
    /// moved none of it without counting again.
    var readState: ReadState {
        ReadState(marks: marks, marking: marking, sending: markSending, behind: rowsBehind, chats: chats, automated: automated, others: others)
    }

    struct ReadState: Equatable {
        var marks: [String: ThreadMark]?
        var marking: [String: MarkChange]
        var sending: [String: MarkChange]
        var behind: Set<String>
        var chats: [Record]
        var automated: [Record]
        var others: [String: Record]
    }

    /// The threads some window shows, in front or not.
    var openThreads: Set<String> {
        Set(scenes.values.compactMap { $0.current.flatMap { conversations[$0]?.thread } })
    }

    /// The chats whose end somebody is looking at, each with the newest
    /// message it settled with: a window in front and active shows the
    /// conversation, its view says the end is on screen, the thread has
    /// settled, and the rows drawn hold that message. Nobody while the
    /// sign-in has ended: the screen that says so stands over every chat.
    var seeing: [String: String] {
        guard appActive, marks != nil, !accessEnded else { return [:] }
        var out: [String: String] = [:]
        for scene in scenes.values where scene.active {
            guard let key = scene.current, scene.atEnd == key, let entry = conversations[key], !entry.owns, let id = entry.thread,
                let thread = self.thread(id), !thread.running, let last = thread.lastMessage,
                transcripts[id]?.records.contains(where: { $0.id == last }) == true
            else { continue }
            out[id] = last
        }
        return out
    }

    /// Where the "New" line of a chat opened now goes: after the message
    /// the person read through, where more stands below it, and above
    /// everything held where they never opened the chat. Nil for a chat
    /// with nothing new, or one only marked unread.
    func newFrom(_ thread: String) -> NewFrom? {
        guard marks != nil, let row = self.thread(thread), unread(row), let last = row.lastMessage, marked(thread) else { return nil }
        // What the server holds, and what this device saw and has not had
        // answered: the line goes after the later of them in the rows.
        let read = [marks?[thread]?.readThrough, markSending[thread]?.readThrough, marking[thread]?.readThrough].compactMap { $0 }
        if read.isEmpty { return .start }
        return read.contains(last) ? nil : .after(read)
    }

    /// The threads whose cards are read: on a server that counts what waits
    /// on each thread's row, the ones that say something does; else every
    /// thread listed, as an app's token must name them.
    var cardThreads: [String] {
        guard readsMarks else { return threadIDs }
        // Every list's rows: an archived chat that waits is in none but
        // the one of the chats that wait.
        return (held + others.values.sorted { $0.id < $1.id }).filter { ChatThread(record: $0).waiting > 0 }.map(\.id)
    }

    /// The marks worth keeping on the device: those of the threads held.
    var keptMarks: [String: ThreadMark]? {
        guard let marks else { return nil }
        let held = Set(threadIDs).union(others.keys)
        return marks.filter { held.contains($0.key) }
    }

    /// What the device moved and the server has not answered, as one write
    /// a thread: kept across a launch, and sent at the next chance.
    var unsentMarks: [String: MarkChange]? {
        guard marks != nil else { return nil }
        var out = markSending
        for (thread, change) in marking { out[thread] = out[thread].map { $0.then(change) } ?? change }
        return out.isEmpty ? nil : out
    }
}

extension ChatApp {
    /// How long the end of a chat is on screen before it is read.
    static let sightSeconds: Double = 1
    /// How long a read by sight waits before it is written, so a run of
    /// replies is one write.
    static let markDebounce: Double = 2
    /// How long a reply's notification waits for a mark.
    static let notifyHold: Double = 5

    // MARK: What the server says

    /// The server's document was read. A server that keeps marks has them
    /// read now where no list read brought them; one that keeps none (an
    /// older one, or one rolled back) goes back to the device's own map.
    func featuresRead(_ state: inout State, agents: Int, records: Int) {
        state.agentsRevision = agents
        state.continueUnserved = false
        state.idsUnserved = false
        let kept = state.readsMarks
        let flagged = state.flags
        state.recordsRevision = records
        state.marksUnserved = false
        state.flagsUnserved = false
        state.searchUnserved = false
        // The lists are other reads where the server keeps the flags, and
        // where it stopped keeping them: read again by what it says now.
        if state.flags != flagged {
            if !state.flags { dropFlags(&state) }
            if !state.refreshing { refresh(&state) }
        }
        if state.readsMarks {
            // A refresh under way asks when it lands (`askMarks`).
            if state.marks == nil, !state.refreshing { askMarks(&state) }
        } else {
            dropMarks(&state)
            if kept { announceAll(&state) }
        }
    }

    /// The marks of every thread held, read on their own: a list read that
    /// started before the server's document was, or whose marks failed.
    func askMarks(_ state: inout State) {
        guard state.readsMarks else { return }
        let threads = state.threadIDs + state.others.keys.sorted()
        if !threads.isEmpty { state.asks.append(.readMarks(threads)) }
    }

    /// An answered read of the person's marks. Forward only: a mark held
    /// under a later version than the one a read brought is kept, so an
    /// answer that left the server before a write of this device's, or
    /// before a newer read, never brings a dot back. Answers the threads
    /// whose mark was not known before: a reply of theirs that waited for
    /// it is announced by the caller, once the rows the marks came with are
    /// installed (`announceLearned`), so a mark is never held against a
    /// row older than it.
    @discardableResult
    func apply(_ state: inout State, marks read: ChatAppStep.Marks) -> [String] {
        if read.unserved {
            let kept = state.readsMarks
            state.marksUnserved = true
            dropMarks(&state)
            if kept { announceAll(&state) }
            return []
        }
        guard state.readsMarks else { return [] }
        var held = state.marks ?? [:]
        var answered: [String: ThreadMark] = [:]
        for record in read.records where record.deletedAt == nil {
            if let thread = record["thread"]?.referenceID { answered[thread] = ThreadMark(record: record) }
        }
        var learned: [String] = []
        for thread in read.threads {
            let mark = answered[thread] ?? .none
            if let old = held[thread], old.version > mark.version { continue }
            if held[thread] == nil { learned.append(thread) }
            held[thread] = mark
        }
        state.marks = held
        // The first answer: the watch opens again, from its bookmark,
        // following the kind too.
        if !state.marksKnown {
            state.marksKnown = true
            if state.watching { watch(&state) }
        }
        return learned
    }

    /// A mark stored without its thread's row, a read of the marks alone,
    /// the answer to a write or the folder at a launch: where it is ahead
    /// of the row held, the row is read again, and until it is the chat is
    /// not called unread for a message another device read past. A dot
    /// that went must not come back for the time one read takes to follow
    /// another. The row is read by a read of its own, so the one that ends
    /// the wait is known to have started after the mark was learned: a
    /// list read under way before it may still land the older row, and a
    /// thread opened on its own is in no list.
    func noteBehind(_ state: inout State, _ thread: String) {
        guard let mark = state.marks?[thread], let row = state.thread(thread), mark.ahead(of: row) else { return }
        state.rowsBehind.insert(thread)
        state.asks.append(.rereadBehind(thread: thread, mark: mark.version))
    }

    /// A reply that settled before its chat's mark was known waited for it:
    /// notified now, where the mark says nobody read it.
    func announceLearned(_ state: inout State, _ threads: [String]) {
        for thread in threads { if let row = state.thread(thread) { announce(&state, row) } }
    }

    /// The server turned out to keep no marks: the replies that waited for
    /// one are told by the device's own map after all. Every other reply's
    /// key is recorded already, so nothing is said twice.
    func announceAll(_ state: inout State) {
        for thread in (state.chats + state.automated + Array(state.others.values)).map(ChatThread.init(record:)) {
            announce(&state, thread)
        }
    }

    /// The server keeps no marks: what read as unread by them stays unread
    /// by the device's own map, and what read as read stays read.
    private func dropMarks(_ state: inout State) {
        guard state.marks != nil else { return }
        for thread in state.threads {
            guard !thread.running, let finished = thread.record["finishedAt"]?.string else { continue }
            if !state.unread(thread) {
                state.seen[thread.id] = finished
            } else if state.seen[thread.id] == finished {
                state.seen[thread.id] = nil
            }
        }
        state.marks = nil
        state.marking = [:]
        state.markSending = [:]
        state.rowsBehind = []
        state.sights = [:]
        if state.marksKnown {
            state.marksKnown = false
            if state.watching { watch(&state) }
        }
    }

    // MARK: What the person does

    /// Mark as Read: through the thread's newest message, which clears Mark
    /// as Unread with it; where the thread names none, the flag alone.
    func markRead(_ state: inout State, _ id: String) {
        guard let thread = state.thread(id), state.unread(thread) else { return }
        var change = state.marking[id] ?? MarkChange()
        if let last = thread.lastMessage {
            change.readThrough = last
            change.markedUnread = nil
        } else {
            change.markedUnread = false
        }
        change.due = nil
        change.held = false
        state.marking[id] = change
    }

    /// Mark as Unread: a flag, which never moves the position. A chat a
    /// window shows stays unread until it is left or says more.
    func markUnread(_ state: inout State, _ id: String) {
        guard let thread = state.thread(id), state.mark(id, last: thread.lastMessage)?.markedUnread != true else { return }
        var change = state.marking[id] ?? MarkChange()
        change.markedUnread = true
        change.due = nil
        change.held = false
        state.marking[id] = change
        if state.openThreads.contains(id), let last = thread.lastMessage {
            if state.sights[id]?.read == false { state.asks.append(.cancel("sight:\(id)")) }
            state.sights[id] = State.Sight(message: last, read: true)
        }
    }

    /// The end of a chat was on screen for a second, in a window in front:
    /// it is read through the message it ended on, here at once, and on the
    /// server once no newer read replaced it for a moment.
    func sighted(_ state: inout State, thread: String, message: String, at: Date) {
        guard state.sights[thread] == State.Sight(message: message), state.seeing[thread] == message else { return }
        state.sights[thread]?.read = true
        // A mark that already reads through it has nothing to write; one
        // not yet read is written, and the server answers what it holds.
        if let mark = state.mark(thread, last: message), !mark.unread(lastMessage: message) { return }
        var change = state.marking[thread] ?? MarkChange()
        change.readThrough = message
        change.markedUnread = nil
        change.due = at.addingTimeInterval(Self.markDebounce)
        change.held = false
        state.marking[thread] = change
    }

    // MARK: Writing

    func marksDue(_ state: inout State, at: Date) {
        state.marksDue = nil
        for (thread, change) in state.marking {
            if let due = change.due, due <= at { state.marking[thread]?.due = nil }
        }
    }

    /// Every write waiting on the clock goes out now: the app is leaving
    /// the front.
    func flushMarks(_ state: inout State) {
        for thread in state.marking.keys { state.marking[thread]?.due = nil }
    }

    /// The next chance for the writes that failed: the app is back in
    /// front, or the server just answered a read.
    func retryMarks(_ state: inout State) {
        for (thread, change) in state.marking where change.held {
            state.marking[thread]?.held = false
            state.marking[thread]?.due = nil
        }
        // And for the rows still behind their marks: a read of one that
        // failed is asked for again.
        for thread in state.rowsBehind.sorted() {
            if let mark = state.marks?[thread] { state.asks.append(.rereadBehind(thread: thread, mark: mark.version)) }
        }
    }

    func markWritten(_ state: inout State, thread: String, sent: MarkChange, _ record: Record) {
        guard state.markSending[thread] == sent else { return }
        state.markSending[thread] = nil
        guard state.marks != nil else { return }
        let mark = ThreadMark(record: record)
        if (state.marks?[thread]?.version ?? 0) <= mark.version { state.marks?[thread] = mark }
        // The server kept a later position than the one sent: another
        // device read further, in a thread this one holds an older row of.
        noteBehind(&state, thread)
    }

    /// A write that did not land is nothing the person can act on: it waits
    /// for the next chance with whatever was moved since, and the chat goes
    /// on reading as it was left here. One the server refused for good (the
    /// thread is gone, the message is no longer one of its) is dropped, and
    /// the chat reads as the server says.
    func markWriteFailed(_ state: inout State, thread: String, sent: MarkChange, _ refusal: MarkRefusal, _ failure: Failure? = nil) {
        guard state.markSending[thread] == sent else { return }
        state.markSending[thread] = nil
        if refusal == .dropped, sent.flags {
            // A pin or an archive the server will not take: the chat reads
            // as the server says again, and the list says so, since the
            // person asked for it by name.
            if state.undo?.thread == thread { state.undo = nil }
            if let failure, failure.report == nil {
                state.listFailure = failure
                var again = MarkChange()
                (again.pinned, again.archived) = (sent.pinned, sent.archived)
                state.flagRefused = State.FlagRefused(thread: thread, change: again)
            }
            // What it carried beside the flag (a position) is not what a
            // server that keeps no flags refused: it goes again alone, and
            // is dropped if it is refused alone.
            if state.marks != nil, let rest = sent.unflagged {
                state.marking[thread] = state.marking[thread].map { rest.then($0) } ?? rest
            }
        }
        guard state.marks != nil, refusal != .dropped else { return }
        if let later = state.marking[thread] {
            state.marking[thread] = sent.then(later)
        } else {
            var again = sent
            again.held = true
            again.due = nil
            state.marking[thread] = again
        }
    }

    /// After every event: who is looking at the end of what, and what is
    /// due. A second on screen is counted from here, and stops being
    /// counted the moment the chat scrolls away, runs, or leaves the front;
    /// a read by sight still waiting on the clock goes out at once when no
    /// window in front shows its chat any more.
    func settleMarks(_ state: inout State) {
        guard state.marks != nil else { return }
        let seeing = state.seeing
        let open = state.openThreads
        for (thread, sight) in state.sights {
            if sight.read {
                if !open.contains(thread) { state.sights[thread] = nil }
            } else if seeing[thread] != sight.message {
                state.sights[thread] = nil
                if seeing[thread] == nil { state.asks.append(.cancel("sight:\(thread)")) }
            }
        }
        for (thread, message) in seeing.sorted(by: { $0.key < $1.key }) where state.sights[thread]?.message != message {
            state.sights[thread] = State.Sight(message: message)
            state.asks.append(.sight(thread: thread, message: message))
        }
        // A row that caught up with its mark, by whatever read, is behind
        // it no longer.
        for thread in state.rowsBehind where !(state.thread(thread).map { state.marks?[thread]?.ahead(of: $0) == true } ?? false) {
            state.rowsBehind.remove(thread)
        }
        let visible = state.visibleThreads
        // A reply notified and since read: taken back. Held by thread and
        // not by what changed in one event, since a list read may drop a
        // row and a later one bring it back beside the mark that reads it.
        for thread in state.announced.sorted() {
            if visible.contains(thread) {
                state.announced.remove(thread)
            } else if !state.notifies(thread) {
                // Archived since, here or on another device: what it
                // notified is put away with it. Also where no list holds
                // its row any more and its mark, read by name, says so: a
                // chat that waits is always in a list.
                state.announced.remove(thread)
                state.asks.append(.cancelGroup(thread))
            } else if let row = state.thread(thread), !row.running, state.marked(thread), !state.unread(row), !state.failedUnseen(row) {
                // A read answers a reply's notification. A card's stands
                // while its chat waits, and what an action could not do
                // until the chat is opened here: where either does, the
                // reply's own goes alone (`readElsewhere`).
                guard !state.mayWait(thread), !state.failedActions.contains(thread) else {
                    readElsewhere(&state, thread)
                    continue
                }
                state.announced.remove(thread)
                state.asks.append(.cancelGroup(thread))
            }
        }
        for (thread, change) in state.marking where change.due != nil && !visible.contains(thread) {
            state.marking[thread]?.due = nil
        }
        for (thread, change) in state.marking.sorted(by: { $0.key < $1.key })
        where change.due == nil && !change.held && state.markSending[thread] == nil {
            state.marking[thread] = nil
            // A flag goes only to a server that keeps the flags.
            guard let change = state.flags ? change : change.unflagged else { continue }
            state.markSending[thread] = change
            state.asks.append(.writeMark(thread: thread, change))
        }
        let due = state.marking.values.compactMap { $0.held ? nil : $0.due }.min()
        if due != state.marksDue {
            state.marksDue = due
            state.asks.append(due.map { .marksWait(until: $0) } ?? .cancel("marksWait"))
        }
    }
}

/// Why a mark's write did not land.
enum MarkRefusal: String, Codable, Sendable {
    /// No answer, or one that says nothing of the write: tried again.
    case later
    /// The server's own refusal, which trying again cannot change.
    case dropped
    /// The sign-in ended.
    case signedOut

    init(_ error: any Error) {
        switch error {
        case is SessionExpired: self = .signedOut
        case is InsufficientScope: self = .dropped
        case let error as ServerError where (400 ..< 500).contains(error.status) && error.status != 408 && error.status != 429:
            self = .dropped
        default: self = .later
        }
    }
}
