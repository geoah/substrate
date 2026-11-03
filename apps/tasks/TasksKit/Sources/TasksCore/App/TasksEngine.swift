import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime

/// The root's engine: every event applied to the state, purely. What the
/// read does next and which records to read again are decided here, by the
/// one event that decides them, and cleared by every other, so the effects
/// start each exactly once.
public extension TasksApp {
    func apply(_ event: Event, to state: TasksState) throws -> TasksState {
        var next = state
        next.syncDue = nil
        next.rereadDue = []
        switch event {
        case let .linked(link):
            next.link = link
            startOrStop(&next)
        case .opening:
            break
        case let .loaded(copy):
            next.server = Self.keyed(copy.records)
            next.cursor = copy.cursor
            next.queue = copy.queue
            next.settings = copy.settings
            next.usage = copy.usage
            next.loaded = true
            next.records = RecordFold.fold(next.server, next.queue)
            startOrStop(&next)
        case let .listed(records, cursor):
            next.server = Self.keyed(records)
            next.cursor = cursor ?? state.cursor
            next.records = RecordFold.fold(next.server, next.queue)
            // A read that ends after a sign-out starts no watch.
            if next.link?.signedIn == true, next.sync == .reading {
                next.sync = .following
                next.syncDue = .follow
            }
        case let .readFailed(words, _):
            next.sync = .stopped(words)
        case let .changed(key, record):
            next.server[key] = record
            next.records = RecordFold.fold(next.server, next.queue)
        case let .watchOpened(cursor):
            next.cursor = cursor
            next.sync = .following
        case let .watchInterrupted(words):
            next.sync = .interrupted(words)
        case let .watchEnded(_, words):
            next.sync = .stopped(words)
        case .relistNeeded:
            next.cursor = nil
            next.sync = .reading
            next.syncDue = .read
        case let .queueReported(entries):
            report(entries, confirming: [], into: &next)
        case let .enqueued(ids, queue):
            report(queue, confirming: ids, into: &next)
        case let .notQueued(ids, words):
            next.queue.removeAll { ids.contains($0.id) }
            next.unconfirmed.subtract(ids)
            next.problem = words
            next.records = RecordFold.fold(next.server, next.queue)
        case let .wrote(write):
            lay(write, over: &next)
        case let .sectionSelected(section):
            next.section = section
            if section == TasksMenu.today { next.selection = .today }
            if section == TasksMenu.lists, next.selection == .today { next.selection = .inbox }
        case let .shown(selection):
            next.selection = selection
            next.openTask = nil
        case let .opened(task):
            next.openTask = task
        case let .editorOpened(key, seed):
            next.editors[key] = seed
        case let .editorSaved(key, write, _):
            next.editors[key] = nil
            lay(write, over: &next)
        case let .editorClosed(key):
            next.editors[key] = nil
        case let .asked(confirmation):
            next.confirmation = confirmation
        case .answered:
            next.confirmation = nil
        case let .settingsChanged(settings):
            next.settings = settings
        case let .permission(permission):
            next.permission = permission
        case let .scheduled(plan):
            next.scheduled = plan
        case let .tapped(id):
            guard let record = next.records[RecordFold.key(TasksKinds.task, id)] else { break }
            let list = TasksCodec.listID(record.properties["list"])
            next.section = TasksMenu.lists
            next.selection = list.map(ListSelection.list) ?? .inbox
            next.openTask = id
        case let .usage(usage):
            next.usage = usage
        case .clearAsked:
            break
        case .cleared:
            next.server = [:]
            next.cursor = nil
            next.records = RecordFold.fold(next.server, next.queue)
            next.sync = .idle
            startOrStop(&next)
        case .refreshAsked:
            if next.link?.signedIn == true, next.loaded, next.sync != .reading {
                next.sync = .reading
                next.syncDue = .read
            }
        case .foregrounded:
            break
        case let .offered(version):
            next.offered = version
        case .problemDismissed:
            next.problem = nil
        }
        if next.records != state.records || next.settings != state.settings || next.selection != state.selection {
            next.revision += 1
        }
        return next
    }

    private static func keyed(_ records: [Record]) -> [String: Record] {
        var out: [String: Record] = [:]
        for record in records where TasksKinds.all.contains(record.kind) && record.deletedAt == nil {
            out[RecordFold.key(record.kind, record.id)] = record
        }
        return out
    }

    /// Signed in and loaded with nothing reading: the watch resumes where
    /// it was, or the repository is read first. Signed out: it stops.
    private func startOrStop(_ state: inout TasksState) {
        if state.link?.signedIn == true {
            guard state.loaded, state.sync.stopped else { return }
            if state.cursor == nil {
                state.sync = .reading
                state.syncDue = .read
            } else {
                state.sync = .following
                state.syncDue = .follow
            }
        } else if !state.sync.stopped {
            state.sync = .idle
            state.syncDue = .stop
        }
    }

    /// The owner's write, laid over at once: the outbox has it, or will
    /// within the effect that queues it.
    private func lay(_ write: LocalWrite, over state: inout TasksState) {
        guard !write.entries.isEmpty else { return }
        if let batch = write.replacing { state.queue.removeAll { $0.batch == batch } }
        state.queue += write.entries
        state.unconfirmed.formUnion(write.entries.map(\.id))
        state.records = RecordFold.fold(state.server, state.queue)
    }

    /// The outbox's queue as it reported it. A write that left it landed
    /// (or was given up): it is laid over the server's copy as landed until
    /// that record is read again, so the owner never sees it undone in
    /// between. A write queued here and not in the report yet stays laid
    /// over, unless the report is the one its own queueing answered, after
    /// which it can only have landed.
    private func report(_ entries: [OutboxEntry], confirming ids: Set<UUID>, into state: inout TasksState) {
        let reported = Set(entries.map(\.id))
        var waiting = state.unconfirmed.subtracting(reported)
        let landed = state.queue.filter { !reported.contains($0.id) && (!waiting.contains($0.id) || ids.contains($0.id)) }
        waiting.subtract(ids)
        for entry in landed where !entry.parked {
            state.server[entry.key] = RecordFold.apply(entry, to: state.server[entry.key])
        }
        state.queue = entries.filter { TasksKinds.all.contains($0.kind) } + state.queue.filter { waiting.contains($0.id) }
        state.unconfirmed = waiting
        state.records = RecordFold.fold(state.server, state.queue)
        var keys: [String] = []
        for entry in landed where !keys.contains(entry.key) { keys.append(entry.key) }
        state.rereadDue = keys
    }
}
