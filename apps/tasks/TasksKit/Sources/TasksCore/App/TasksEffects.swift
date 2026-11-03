import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// What the root's events ask the device and the server for, and the work
/// each is. Writes go to the outbox alone; the link sees the outbox move
/// and sends them.
public extension TasksApp {
    func effects(of event: Event, on state: TasksState, presented presentation: TasksPresentation) -> [Effect] {
        var out: [Effect] = []
        switch event {
        case .opening:
            out += [.load, .followOutbox, .readPermission]
        case let .wrote(write), let .editorSaved(_, write, _):
            if !write.entries.isEmpty { out.append(.enqueue(write)) }
        case let .settingsChanged(settings):
            out.append(.saveSettings(settings))
            if TasksSettings.reminds(settings), state.permission == .notAsked { out.append(.askPermission) }
        case .clearAsked:
            out.append(.clearSaved)
        case .cleared, .foregrounded:
            out.append(.readUsage)
            if case .foregrounded = event { out.append(.readPermission) }
        default:
            break
        }
        switch event {
        case .permission(.notAsked), .loaded, .listed, .wrote, .editorSaved:
            // Asked once there is something to remind of, never at a
            // launch with nothing due: the system asks only once.
            if TasksSettings.reminds(state.settings), state.permission == .notAsked, Self.hasDue(state) { out.append(.askPermission) }
        default:
            break
        }
        switch state.syncDue {
        case .read?: out.append(.read)
        case .follow?: out.append(.follow(state.cursor))
        case .stop?: out.append(.stopFollowing)
        case nil: break
        }
        if !state.rereadDue.isEmpty { out.append(.reread(state.rereadDue)) }
        // The reminders follow the plan the presenter made, once the device
        // has said what it allows and the tasks are read.
        if state.loaded, state.permission != nil, presentation.reminders != state.scheduled {
            out.append(.schedule(presentation.reminders))
        }
        return out
    }

    private static func hasDue(_ state: TasksState) -> Bool {
        state.records.values.contains { $0.kind == TasksKinds.task && $0.properties["dueDay"] != nil }
    }

    /// A write is never cancelled for another: the line keeps their order.
    func key(of effect: Effect) -> String? {
        switch effect {
        case .load: "load"
        case .followOutbox: "outbox"
        case .readPermission, .askPermission: "permission"
        case .read: "read"
        case .follow, .stopFollowing: "watch"
        case .saveSettings: "settings"
        case .schedule: "reminders"
        case .readUsage: "usage"
        case .clearSaved: "clear"
        case .reread, .enqueue: nil
        }
    }

    func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        let (cache, outbox, line) = (cache, outbox, line)
        let folder = environment.folder
        let notifications = environment.devices.notifications
        let source = environment.client
        switch effect {
        case .load:
            return .once {
                var copy = LocalCopy()
                if let cache {
                    copy.records = await TasksKinds.all.asyncFlatMap { await cache.records(of: $0) }
                    copy.cursor = await cache.cursor
                }
                if let outbox { copy.queue = await outbox.entries }
                if let folder {
                    copy.settings = (try? folder.load(SettingsValues.self, from: Self.settingsFile, in: .root).value) ?? SettingsValues()
                    copy.usage = folder.usage()
                }
                return .loaded(copy)
            }
        case .followOutbox:
            guard let outbox else { return .none }
            return .sequence {
                Self.relay { continuation in
                    for await _ in await outbox.changes() { continuation.yield(.queueReported(await outbox.entries)) }
                }
            }
        case .readPermission:
            guard let notifications else { return .none }
            return .once { .permission(await notifications.permission()) }
        case .askPermission:
            guard let notifications else { return .none }
            return .once { .permission(await notifications.requestPermission()) }
        case .read:
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    let page = try await client.listAll(ListQuery(kinds: TasksKinds.all, first: 200), limit: 10000)
                    await cache?.listed(page, kinds: TasksKinds.all)
                    try? await outbox?.saw(Dictionary(page.records.map { (RecordFold.key($0.kind, $0.id), $0.version) }) { a, _ in a })
                    return .listed(page.records, cursor: await cache?.cursor ?? (page.generation.isEmpty ? nil : WatchCursor(seq: page.head, generation: page.generation)))
                } catch {
                    return .readFailed(explain(error, app: Self.appName, server: client.server), LinkReport(error))
                }
            }
        case let .follow(cursor):
            return .sequence {
                Self.relay { continuation in
                    guard let client = await source.current() else { return }
                    let follower = WatchFollower(client: client, kinds: TasksKinds.all, app: Self.appName)
                    for await event in follower.follow(from: cursor) {
                        switch event {
                        case let .opened(cursor):
                            await cache?.bookmark(cursor)
                            continuation.yield(.watchOpened(cursor))
                        case let .row(row):
                            await cache?.apply(row)
                            let key = RecordFold.key(row.kind, row.recordID)
                            if row.removes {
                                continuation.yield(.changed(key, nil))
                            } else if let event = await Self.read(row.kind, row.recordID, client: client, cache: cache, outbox: outbox) {
                                continuation.yield(event)
                            }
                        case .relist:
                            continuation.yield(.relistNeeded)
                        case let .interrupted(words):
                            continuation.yield(.watchInterrupted(words))
                        case let .ended(end):
                            continuation.yield(Self.stopped(end, server: client.server))
                        }
                    }
                }
            }
        case .stopFollowing:
            return .cancel
        case let .reread(keys):
            return .sequence {
                Self.relay { continuation in
                    guard let client = await source.current() else { return }
                    for key in keys {
                        guard let reference = RecordReference(path: key) else { continue }
                        if let event = await Self.read(reference.kind, reference.recordID, client: client, cache: cache, outbox: outbox) {
                            continuation.yield(event)
                        }
                    }
                }
            }
        case let .enqueue(write):
            guard let outbox else { return .none }
            let ticket = line.ticket()
            return .once {
                await line.wait(ticket)
                defer { line.done(ticket) }
                let ids = Set(write.entries.map(\.id))
                do {
                    if let batch = write.replacing, let entry = write.entries.first {
                        try await outbox.purge(batch: batch, with: entry)
                    } else {
                        try await outbox.enqueue(write.entries)
                    }
                    return .enqueued(ids, queue: await outbox.entries)
                } catch {
                    return .notQueued(ids, "\(device.capitalizedFirstWord) could not keep the change: \(error.localizedDescription)")
                }
            }
        case let .saveSettings(settings):
            guard let folder else { return .none }
            return .once {
                let encoder = JSONEncoder()
                encoder.outputFormatting = [.sortedKeys]
                try? folder.write(encoder.encode(settings), to: Self.settingsFile, in: .root)
                return nil
            }
        case let .schedule(plan):
            guard let notifications else { return .none }
            return .once {
                let wanted = Set(plan.map(\.id))
                let stale = await notifications.pending(prefix: Reminders.prefix).filter { !wanted.contains($0) }
                for id in stale { await notifications.cancel(id) }
                for request in plan { await notifications.schedule(request) }
                return .scheduled(plan)
            }
        case .readUsage:
            guard let folder else { return .none }
            return .once { .usage(folder.usage()) }
        case .clearSaved:
            return .once {
                await cache?.clear()
                return .cleared
            }
        }
    }

    /// One record read again after the watch named it, or a write of it
    /// landed: kept in the cache, its version told to the outbox.
    private static func read(_ kind: String, _ id: String, client: SubstrateClient, cache: RecordCache?, outbox: Outbox?) async -> Event? {
        do {
            let record = try await client.record(kind, id)
            await cache?.refreshed(kind, id, as: record)
            if let record { try? await outbox?.saw([RecordFold.key(kind, id): record.version]) }
            return .changed(RecordFold.key(kind, id), record)
        } catch is SessionExpired {
            return .watchEnded(.signedOut, "Your access ended.")
        } catch is InsufficientScope {
            return .watchEnded(.needsApproval, "Your approval does not reach your tasks.")
        } catch {
            // The next row of it, or the next read, tries again.
            return nil
        }
    }

    private static func stopped(_ end: WatchEnd, server: URL) -> Event {
        switch end {
        case .signedOut: .watchEnded(.signedOut, "Your access ended.")
        case .needsApproval: .watchEnded(.needsApproval, "Your approval does not reach your tasks.")
        case let .refused(words): .watchEnded(.refused, words)
        }
    }

    /// A stream of events made by `body`, ended when it returns and
    /// cancelled when the reader stops.
    private static func relay(_ body: @escaping @Sendable (AsyncStream<Event>.Continuation) async -> Void) -> AsyncStream<Event> {
        AsyncStream { continuation in
            let task = Task {
                await body(continuation)
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }
}

extension Array where Element: Sendable {
    func asyncFlatMap<T>(_ transform: (Element) async -> [T]) async -> [T] {
        var out: [T] = []
        for element in self { out += await transform(element) }
        return out
    }
}
