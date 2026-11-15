import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateRuntime

/// The chat list as more than one read: a window of the server's list that
/// grows at its end, and, where the server keeps a person's `pinned` and
/// `archived` on their mark of a thread (decision 0322, `records` at
/// revision 9), the pinned chats, the ones that wait on a person and the
/// archived ones, each a list of its own merged by the thread's id.
///
/// The server holds none of the rules below; each client keeps them. An
/// archived chat is in no list, no badge and no notification, but while it
/// waits on a person: then it is listed, counted and notified for as if it
/// were not archived, and its flag stays. A pinned chat stands above the
/// rest, however old. One both pinned and archived is with the archived
/// ones and keeps its pin. Until the person's mark of a chat is read, and
/// where that read failed, which list the server answered it in says which
/// it is: the server narrowed the lists by the same marks.
extension ChatAppState {
    /// The server keeps `pinned` and `archived`, and the lists are read by
    /// them: its document says so, or, until that is read, the folder holds
    /// what it last said.
    var flags: Bool {
        readsMarks && !flagsUnserved && (recordsRevision ?? 0) >= Self.flagsRevision
    }

    /// Every row a list read holds, once each, at the newest version held.
    var held: [Record] {
        guard flags else { return chats + automated }
        var at: [String: Int] = [:]
        var out: [Record] = []
        for record in chats + automated + pinned + waiting + archive.rows {
            if let index = at[record.id] {
                if record.version > out[index].version { out[index] = record }
            } else {
                at[record.id] = out.count
                out.append(record)
            }
        }
        return out
    }

    /// What this device moved of a thread's flags and has not had answered,
    /// the later of them first.
    func moved(_ id: String) -> [MarkChange] {
        [marking[id], markSending[id]].compactMap { $0 }
    }

    /// Whether the person archived a thread: by what this device just did,
    /// else by their mark, else by the list the server answered it in.
    func archived(_ id: String) -> Bool { Standing(self).archived(id) }

    /// Whether the person pinned a thread, read as `archived(_:)` is.
    func isPinned(_ id: String) -> Bool { Standing(self).pinned(id) }

    /// Which chats are pinned and which archived, for a pass over every
    /// row: `archived(_:)` and `isPinned(_:)` with the lists' ids read once.
    struct Standing {
        let state: ChatAppState
        let inList: Set<String>
        let inPinned: Set<String>
        let inArchive: Set<String>

        init(_ state: ChatAppState) {
            self.state = state
            inList = Set((state.chats + state.automated).map(\.id))
            inPinned = Set(state.pinned.map(\.id))
            inArchive = Set(state.archive.rows.map(\.id))
        }

        /// The person's mark of a thread as the server said it. One the
        /// app made up for a thread it just started (`ThreadMark.none`,
        /// version 0) was never read: the lists say which the chat is.
        private func read(_ id: String) -> ThreadMark? {
            state.marks?[id].flatMap { $0.version > 0 ? $0 : nil }
        }

        func archived(_ id: String) -> Bool {
            guard state.flags else { return false }
            return state.moved(id).lazy.compactMap(\.archived).first ?? read(id)?.archived ?? (inArchive.contains(id) && !inList.contains(id))
        }

        func pinned(_ id: String) -> Bool {
            guard state.flags else { return false }
            return state.moved(id).lazy.compactMap(\.pinned).first ?? read(id)?.pinned ?? inPinned.contains(id)
        }
    }

    /// The rows the lists hold, newest activity first: the chat list
    /// (`listed`), or the archived chats.
    func rows(listed: Bool) -> [ChatThread] {
        let standing = Standing(self)
        var out: [ChatThread] = []
        for record in held {
            let thread = ChatThread(record: record)
            guard thread.automated ? preferences.showAutomated && conversationModes.contains(thread.mode) : true else { continue }
            let isArchived = standing.archived(thread.id)
            if !listed {
                if isArchived { out.append(thread) }
                continue
            }
            if isArchived, thread.waiting == 0 { continue }
            // A chat only another list holds is listed where it belongs in
            // this one: pinned, waiting, or inside the window read so far.
            guard standing.inList.contains(thread.id) || standing.pinned(thread.id) || thread.waiting > 0 || inWindow(thread) else { continue }
            out.append(thread)
        }
        return out.sorted { ($0.activeAt, $0.id) > ($1.activeAt, $1.id) }
    }

    /// Whether a chat's place in the server's list is inside the window
    /// read so far. Before the server has answered the list once, the rows
    /// held are the folder's and say nothing of where the window ends.
    private func inWindow(_ thread: ChatThread) -> Bool {
        guard window.read, let end = window.end else { return true }
        return thread.activeAt >= end
    }

    /// The archived chats, as the Archived list draws them.
    var archivedThreads: [ChatThread] { flags ? rows(listed: false) : [] }

    /// The words of a search as the app shows them in a hit: each word of
    /// the query without what the search grammar reads as a mark.
    static func searchWords(_ query: String) -> [String] {
        query.split(whereSeparator: \.isWhitespace).compactMap { word in
            let bare = word.trimmingCharacters(in: CharacterSet(charactersIn: "\"*-"))
            return bare.isEmpty || bare == "OR" ? nil : bare
        }
    }

    /// Whether a search asks the server: with no words, offline, or on a
    /// server that refused it, the rows on this device are searched alone.
    func searchesServer(_ query: String) -> Bool {
        session != nil && online && !accessEnded && !searchUnserved && !Self.searchWords(query).isEmpty
    }
}

extension ChatApp {
    /// How long Undo stands after a chat was archived.
    static let undoSeconds: Double = 6
    /// How long after the typing stops a search asks the server.
    static let searchPause: Double = 0.35
    /// A window opens to the chat it was left in less than this ago.
    static let freshFor: TimeInterval = 30 * 60
    /// How many reads of older rows a chat opened at a message may make to
    /// reach it.
    static let seekReads = 5

    // MARK: A list read

    /// A list of threads as the server answered it, merged into a window of
    /// the same list that may have grown past its first page. Where the
    /// page and the window share a row that did not move, every chat that
    /// moved since is on the page, and the rows past the page stay. Where
    /// they share none, more chats moved than a page holds: the window
    /// starts over from the page, since what lies between the two was never
    /// read.
    static func merge(
        _ page: [Record], cursor: String?, of part: ChatLists.Part, into rows: inout [Record], _ window: inout State.ListWindow,
        keeping: (Record) -> Bool
    ) -> [Record] {
        let before = Dictionary(rows.map { ($0.id, $0) }) { a, _ in a }
        let ids = Set(page.map(\.id))
        let edge = page.last?.updatedDate
        // A row both hold that has not moved since: every chat that did
        // is above it on the page.
        let joined =
            window.read && !window.restart && window.part == part && cursor != nil && page.contains { before[$0.id]?.updatedAt == $0.updatedAt }
        var dropped: [Record] = []
        var tail: [Record] = []
        for row in rows where !ids.contains(row.id) {
            // Past the page: an older chat. Inside its span and not on it:
            // gone from the list.
            let past = edge.map { row.updatedDate <= $0 } ?? false
            if past, joined || keeping(row) { tail.append(row) } else { dropped.append(row) }
        }
        rows = page + tail
        if !joined {
            window.cursor = cursor
            window.end = cursor == nil ? nil : edge
        }
        window.read = true
        window.restart = false
        window.part = part
        return dropped
    }

    /// A new list: an older row never replaces a newer one, what settled
    /// since notifies, and on the first sign-in all of it is history.
    func apply(_ state: inout State, threads: ChatAppStep.Threads) {
        if threads.flagsUnserved {
            state.flagsUnserved = true
            dropFlags(&state)
        }
        // The marks read with the list land first, so what the list moved
        // is told read from unread by them.
        let learned = threads.marks.map { apply(&state, marks: $0) } ?? []
        let before = Dictionary((state.held + Array(state.others.values)).map { ($0.id, $0) }) { a, b in a.version >= b.version ? a : b }
        let newer = { (record: Record) -> Record in
            if let held = before[record.id], held.version > record.version { return held }
            return record
        }
        let open = Set(state.conversations.values.compactMap(\.thread))
        // The chats were read as the ones not archived where the lists
        // beside them were read too.
        let part: ChatLists.Part = threads.pinned == nil ? .every : .kept
        let dropped = Self.merge(threads.chats.map(newer), cursor: threads.cursor, of: part, into: &state.chats, &state.window) {
            open.contains($0.id)
        }
        state.window.reading = false
        state.automated = threads.automated.map(newer)
        if let pinned = threads.pinned { state.pinned = pinned.map(newer) }
        if let waiting = threads.waiting { state.waiting = waiting.map(newer) }
        if let archived = threads.archived {
            _ = Self.merge(archived.map(newer), cursor: threads.archivedCursor, of: .archived, into: &state.archive.rows, &state.archive.window) { _ in
                false
            }
            state.archive.window.reading = false
        }
        for record in threads.named.map(newer) where !replaceHeld(&state, record) {
            // No list holds it: a chat a window shows keeps its row beside them.
            if open.contains(record.id) || state.others[record.id] != nil {
                state.others[record.id] = record
            } else if threads.waiting != nil, ChatThread(record: record).waiting > 0 {
                // It came to wait after the lists were read: one of the
                // ones that wait, as the next read of them will say.
                state.waiting.append(record)
            }
        }
        for id in threads.gone { forget(&state, id) }
        // A chat that left the list while a window shows it keeps its row.
        let listed = Set(state.held.map(\.id))
        for row in dropped where open.contains(row.id) && !listed.contains(row.id) { state.others[row.id] = row }
        level(&state)
        if !state.baselined {
            for record in state.held {
                if let finished = record["finishedAt"]?.string {
                    if state.marks == nil { state.seen[record.id] = finished }
                    state.notified.insert("reply|\(record.id)|\(finished)")
                }
            }
        }
        // Each thread whose row moved, in a list or read beside them.
        let beside = threads.named.compactMap { state.others[$0.id] }
        for record in state.held + beside where before[record.id]?.version != record.version {
            threadMoved(&state, record)
        }
        announceLearned(&state, learned)
    }

    /// Every list holds the newest copy of each row some list holds.
    func level(_ state: inout State) {
        guard state.flags else { return }
        let newest = Dictionary(state.held.map { ($0.id, $0) }) { a, _ in a }
        func leveled(_ rows: [Record]) -> [Record] { rows.map { newest[$0.id] ?? $0 } }
        state.chats = leveled(state.chats)
        state.automated = leveled(state.automated)
        state.pinned = leveled(state.pinned)
        state.waiting = leveled(state.waiting)
        state.archive.rows = leveled(state.archive.rows)
    }

    /// Puts a row in every list that holds its thread; false where none does.
    @discardableResult
    func replaceHeld(_ state: inout State, _ record: Record) -> Bool {
        var held = false
        func put(_ rows: inout [Record]) {
            for index in rows.indices where rows[index].id == record.id {
                rows[index] = record
                held = true
            }
        }
        put(&state.chats)
        put(&state.automated)
        put(&state.pinned)
        put(&state.waiting)
        put(&state.archive.rows)
        return held
    }

    /// A thread the server no longer holds leaves every list.
    private func forget(_ state: inout State, _ id: String) {
        state.chats.removeAll { $0.id == id }
        state.automated.removeAll { $0.id == id }
        state.pinned.removeAll { $0.id == id }
        state.waiting.removeAll { $0.id == id }
        state.archive.rows.removeAll { $0.id == id }
        state.others[id] = nil
        if state.announced.remove(id) != nil { state.asks.append(.cancelGroup(id)) }
    }

    /// The threads read beside the lists where the lists read do not hold
    /// them: the ones a conversation is on, the ones read on their own, and
    /// the ones a notification stands for. A list read says nothing of a
    /// thread it does not hold (it may have left the server before the
    /// chat was there, or the chat may have been archived on another
    /// device, or deleted), so each read of the lists reads these threads'
    /// rows and the person's marks of them too: a chat on screen that was
    /// archived elsewhere, or by a write of this one whose answer was
    /// lost, is then known to be, and what an archived chat notified is
    /// taken back by its mark and never by its row going missing.
    func beside(_ state: State) -> [String] {
        var seen: Set<String> = []
        return (state.conversations.values.compactMap(\.thread).sorted() + state.others.keys.sorted() + state.announced.sorted())
            .filter { seen.insert($0).inserted }
    }

    /// The server keeps no flags after all: the lists read by them go, and
    /// every chat is listed, as before them.
    func dropFlags(_ state: inout State) {
        state.pinned = []
        state.waiting = []
        state.archive = State.Archive()
        state.undo = nil
        state.flagRefused = nil
        // A pin or an archive still to be written is never sent to a server
        // that keeps neither; the position it waited beside still goes.
        for (thread, change) in state.marking where change.flags { state.marking[thread] = change.unflagged }
        for scene in state.scenes.keys { state.scenes[scene]?.archived = false }
    }

    // MARK: Paging

    /// The list was scrolled to its end: the page after the last one read.
    func more(_ state: inout State, archived: Bool) {
        guard state.session != nil, state.online, !state.accessEnded else { return }
        if archived {
            // Not from a cursor the list is about to leave: one cut under
            // another filter (Automated Runs switched) is refused.
            let window = state.archive.window
            guard state.flags, window.read, !window.reading, !window.restart, let cursor = window.cursor else { return }
            state.archive.window.reading = true
            state.asks.append(.page(state.plan, part: .archived, after: cursor))
        } else {
            // Also while the first page is read again: the page lands where
            // the window still ends at the cursor it was asked from.
            guard state.window.read, !state.window.reading, let cursor = state.window.cursor else { return }
            // The page after the cursor, of the list the cursor was cut for.
            state.window.reading = true
            state.asks.append(.page(state.plan, part: state.window.part ?? (state.flags ? .kept : .every), after: cursor))
        }
    }

    /// A page of a list past its first, or the archived chats' first. One
    /// asked for from a place the window has since left is dropped: a chat
    /// is never listed twice, and none between two pages is passed over.
    func pageRead(_ state: inout State, part: ChatLists.Part, after: String?, rows: [Record], cursor: String?, marks: ChatAppStep.Marks?) {
        let learned = marks.map { apply(&state, marks: $0) } ?? []
        let before = Dictionary((state.held + Array(state.others.values)).map { ($0.id, $0) }) { a, b in a.version >= b.version ? a : b }
        let newer = { (record: Record) -> Record in
            if let held = before[record.id], held.version > record.version { return held }
            return record
        }
        func extend(_ held: inout [Record], _ window: inout State.ListWindow) {
            window.reading = false
            guard window.cursor == after, after == nil || window.part == part else { return }
            if after == nil {
                // Asked for before the list was told to start over (its
                // filter moved since): the read that starts it over brings
                // the page, and this one's cursor is not the list's.
                guard !window.restart else { return }
                _ = Self.merge(rows.map(newer), cursor: cursor, of: part, into: &held, &window) { _ in false }
                return
            }
            // A chat the window already holds is not listed twice: its row
            // is the newer of the two.
            var at = Dictionary(held.enumerated().map { ($1.id, $0) }) { a, _ in a }
            for row in rows.map(newer) {
                if let index = at[row.id] {
                    if row.version > held[index].version { held[index] = row }
                } else {
                    at[row.id] = held.count
                    held.append(row)
                }
            }
            window.cursor = cursor
            window.end = cursor == nil ? nil : rows.last?.updatedDate ?? window.end
        }
        if part == .archived {
            extend(&state.archive.rows, &state.archive.window)
        } else {
            extend(&state.chats, &state.window)
        }
        level(&state)
        for record in rows where before[record.id]?.version != record.version {
            if let row = state.thread(record.id) { threadMoved(&state, row.record) }
        }
        announceLearned(&state, learned)
    }

    /// The archived chats are read when a window lists them, and again
    /// with every list while one does.
    func showArchived(_ state: inout State, scene: String, shown: Bool) {
        guard state.flags || !shown else { return }
        state.scenes[scene, default: State.Scene()].archived = shown
        shelve(&state)
        guard shown, state.session != nil, state.online, !state.accessEnded, !state.archive.window.reading else { return }
        state.archive.window.reading = true
        state.archive.window.cursor = nil
        // Asked for under the filter as it stands now.
        state.archive.window.restart = false
        state.asks.append(.page(state.plan, part: .archived, after: nil))
    }

    /// The archived chats are held while a window lists them, and read
    /// with every list then. With none listing them their rows would go
    /// stale unseen, so they go, but for the chat Undo still stands for.
    func shelve(_ state: inout State) {
        guard !state.scenes.values.contains(where: \.archived) else { return }
        state.archive.rows.removeAll { $0.id != state.undo?.thread }
        state.archive.window = State.ListWindow()
    }

    // MARK: Pin and archive

    /// A flag the person set: read so here at once, and written with
    /// whatever else the thread's mark has waiting, on the path every write
    /// of a mark takes (`settleMarks`).
    func flag(_ state: inout State, _ id: String, pinned: Bool? = nil, archived: Bool? = nil) {
        guard state.flags else { return }
        var change = state.marking[id] ?? MarkChange()
        if let pinned { change.pinned = pinned }
        if let archived { change.archived = archived }
        change.due = nil
        change.held = false
        state.marking[id] = change
    }

    /// Archive: the chat leaves the list at once, with no question asked,
    /// and Undo stands for a few seconds. Its row stays held, among the
    /// archived ones, so Undo puts back exactly what was there.
    func archive(_ state: inout State, _ id: String, at: Date) {
        guard state.flags, let thread = state.thread(id), !state.archived(id) else { return }
        flag(&state, id, archived: true)
        if !state.archive.rows.contains(where: { $0.id == id }) { state.archive.rows.insert(thread.record, at: 0) }
        state.undo = State.Undo(thread: id, until: at.addingTimeInterval(Self.undoSeconds))
        state.asks.append(.undoWait(thread: id, until: at.addingTimeInterval(Self.undoSeconds)))
        // What it notified is put away with it; while it waits it still
        // notifies, and what it notified goes once it no longer does
        // (`settleMarks`).
        if !state.notifies(id, row: thread) {
            state.announced.remove(id)
            state.asks.append(.cancelGroup(id))
        }
    }

    func unarchive(_ state: inout State, _ id: String) {
        guard state.flags, state.archived(id) else { return }
        flag(&state, id, archived: false)
        if state.undo?.thread == id {
            state.undo = nil
            state.asks.append(.cancel("undo"))
        }
    }

    /// The person spoke in a chat they archived (a message, Send Again, Try
    /// Again or Continue): they are following it again, so its archive ends
    /// with it. The message never waits for this write.
    func spoke(_ state: inout State, in thread: String) {
        if state.archived(thread) { unarchive(&state, thread) }
    }

    // MARK: Search

    /// A window's search changed: its titles are matched at once by the
    /// presenter, and the server is asked for the messages once the typing
    /// has paused.
    func search(_ state: inout State, scene: String, query: String) {
        state.scenes[scene, default: State.Scene()].query = query
        let words = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard state.searchesServer(words) else {
            if state.searches[scene] != nil { state.asks.append(.cancel("search:\(scene)")) }
            state.searches[scene] = nil
            if state.listFailure?.attempt == .search { state.listFailure = nil }
            return
        }
        guard state.searches[scene]?.query != words else { return }
        // What the search before it found stays until this one answers.
        state.searches[scene] = State.Search(query: words, reading: true, hits: state.searches[scene]?.hits ?? [])
        state.asks.append(.search(state.plan, scene: scene, query: words, threads: state.threadIDs))
    }

    func searchRead(_ state: inout State, scene: String, query: String, hits: [Record], threads: [Record], marks: ChatAppStep.Marks?) {
        guard state.searches[scene]?.query == query else { return }
        state.searches[scene] = State.Search(query: query, reading: false, hits: hits)
        if state.listFailure?.attempt == .search { state.listFailure = nil }
        if let marks {
            // Marks read without their threads' rows: one ahead of the row
            // held has that row read again (`noteBehind`), and a reply that
            // waited for its chat's mark is told now.
            let learned = apply(&state, marks: marks)
            for thread in marks.threads { noteBehind(&state, thread) }
            announceLearned(&state, learned)
        }
        // The chats the messages are in, where no list holds them.
        for record in threads where (state.thread(record.id)?.record.version ?? .min) < record.version {
            if !replaceHeld(&state, record) { state.others[record.id] = record }
        }
    }

    func searchFailed(_ state: inout State, scene: String, query: String, _ failure: Failure, unserved: Bool) {
        guard state.searches[scene]?.query == query else { return }
        state.searches[scene]?.reading = false
        if unserved {
            // Not a failure of this search: the server takes none from
            // this sign-in, and the rows on the device are searched alone.
            state.searchUnserved = true
            state.searches = [:]
        } else if failure.report == nil {
            state.listFailure = failure
        }
    }

    // MARK: A transcript's older rows

    /// The transcript was scrolled to its first row held: the rows before
    /// it are read.
    func older(_ state: inout State, _ thread: String) {
        guard state.session != nil, state.online, let held = state.transcripts[thread], held.full, !state.olderReading.contains(thread),
            let turn = held.records.first?["turn"]?.int
        else { return }
        state.olderReading.insert(thread)
        state.asks.append(.readOlder(thread: thread, before: turn, epoch: state.readEpochs[thread, default: 0]))
    }

    /// Rows read in above the ones held. The turn that stood first is
    /// named, so a transcript on screen stays at it.
    func olderRead(_ state: inout State, thread: String, epoch: Int, before: Int, rows: [Record], more: Bool) {
        guard state.readEpochs[thread, default: 0] == epoch else { return }
        state.olderReading.remove(thread)
        guard var held = state.transcripts[thread] else { return }
        // Asked for from another first row than the one held now (a read
        // of the newest rows replaced the window meanwhile): these are not
        // the rows before it, and say nothing of whether any stand there.
        guard held.records.first?["turn"]?.int == before else {
            seek(&state, thread)
            return
        }
        let have = Set(held.records.map(\.id))
        let fresh = rows.filter { !have.contains($0.id) }
        let stood = held.turns
        held.records = fresh + held.records
        held.turns = transcript(of: held.records)
        held.full = more && !fresh.isEmpty
        if !fresh.isEmpty {
            let now = Set(held.turns.map(\.id))
            held.above = stood.first { now.contains($0.id) }?.id
            held.grown += 1
        }
        state.transcripts[thread] = held
        seek(&state, thread)
    }

    /// A chat opened at a message: the rows before the ones held are read
    /// until they hold it, a few pages at most. Past that it opens where
    /// the rows held begin.
    func seek(_ state: inout State, _ thread: String) {
        guard let seek = state.seeking[thread], let held = state.transcripts[thread] else { return }
        if held.records.contains(where: { $0.id == seek.message }) || !held.full || seek.reads >= Self.seekReads {
            state.seeking[thread] = nil
            return
        }
        guard !state.olderReading.contains(thread) else { return }
        state.seeking[thread]?.reads += 1
        older(&state, thread)
        if !state.olderReading.contains(thread) { state.seeking[thread] = nil }
    }

    // MARK: The first screen

    /// What a window that just appeared opens to, once the chats are held:
    /// the chat it was left in less than half an hour ago; else the newest
    /// chat that is running; else a new chat, whose composer takes the
    /// focus. A route (a notification, a shortcut) the window is about to
    /// take wins over all three, and a window that already shows a chat
    /// keeps it.
    func land(_ state: inout State) {
        guard state.session != nil, state.loaded, state.opened else { return }
        for scene in state.scenes.keys.sorted() {
            guard let arrival = state.scenes[scene]?.arriving else { continue }
            state.scenes[scene]?.arriving = nil
            guard state.scenes[scene]?.current == nil, state.route == nil else { continue }
            if let thread = arrival.restoring {
                show(&state, thread, in: scene, key: state.key(of: thread) ?? arrival.key)
            } else if let running = state.threads.first(where: { $0.running && !$0.automated }) {
                show(&state, running.id, in: scene, key: state.key(of: running.id) ?? arrival.key)
            } else {
                newChat(&state, agent: nil, listen: false, in: scene, key: arrival.key)
            }
        }
    }
}
