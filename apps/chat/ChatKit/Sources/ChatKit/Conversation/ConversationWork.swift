import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateRuntime

/// The effects' work: the streams, the reads, Stop and the waits, each
/// reading the client when it runs, never keeping it.
extension Conversation {
    public func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        let source = environment.client
        let clock = environment.clock
        let ids = environment.ids
        let timeZone = environment.timeZone.identifier
        switch effect.ask {
        case let .chat(agent, thread, message):
            return .sequence {
                Self.relay { yield in
                    guard let client = await source.current() else { return }
                    do {
                        for try await event in ChatAPI.chat(client, agent: agent, thread: thread, message: message, timezone: timeZone) {
                            if event.kind == .thread, let named = event.thread {
                                yield(.named(named))
                            } else {
                                yield(.streamed(event, at: clock.now))
                            }
                        }
                        yield(.streamEnded(nil, outcome: ids.next()))
                    } catch {
                        if Task.isCancelled { return }
                        yield(.streamEnded(Failure(error, server: client.server), outcome: ids.next()))
                    }
                }
            }
        case let .attach(thread):
            return .sequence {
                Self.relay { yield in
                    guard let client = await source.current() else { return }
                    do {
                        let now = try await ChatAPI.readNow(client, thread: thread)
                        // A run started since: this one opens no stream.
                        guard !Task.isCancelled else { return }
                        yield(.attachRead(thread: now.thread, rows: now.rows))
                        guard now.thread != nil else { return }
                    } catch {
                        if !Task.isCancelled { yield(.attachReadFailed(Failure(error, server: client.server))) }
                        return
                    }
                    do {
                        for try await event in ChatAPI.events(client, thread: thread) {
                            yield(.streamed(event, at: clock.now))
                        }
                        yield(.streamEnded(nil, outcome: ids.next()))
                    } catch {
                        if Task.isCancelled { return }
                        yield(.streamEnded(Failure(error, server: client.server), outcome: ids.next()))
                    }
                }
            }
        case let .wait(attempt, seconds):
            return .once {
                do { try await clock.sleep(until: clock.now.addingTimeInterval(seconds)) } catch { return nil }
                return Event(.retryDue(attempt: attempt))
            }
        case let .handover(thread):
            return .once {
                guard let client = await source.current() else { return nil }
                let step = await withTaskGroup(of: Step?.self) { group -> Step? in
                    group.addTask {
                        do {
                            let now = try await ChatAPI.readNow(client, thread: thread)
                            return .handedOver(thread: now.thread, rows: now.rows)
                        } catch {
                            if Task.isCancelled { return nil }
                            let failure = Failure(error, server: client.server)
                            return .handoverFailed("The conversation didn't reload: \(failure.words)", failure)
                        }
                    }
                    group.addTask {
                        do { try await clock.sleep(until: clock.now.addingTimeInterval(15)) } catch { return nil }
                        return .handoverFailed("The conversation didn't reload: the read has not come back after 15 seconds.", nil)
                    }
                    let first = await group.next() ?? nil
                    group.cancelAll()
                    return first
                }
                return step.map(Event.init)
            }
        case .pollEvery:
            return .sequence {
                Self.relay { yield in
                    while !Task.isCancelled {
                        do { try await clock.sleep(until: clock.now.addingTimeInterval(5)) } catch { return }
                        yield(.pollTick)
                    }
                }
            }
        case let .poll(thread):
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    let now = try await ChatAPI.readNow(client, thread: thread)
                    return Event(.polled(thread: now.thread, rows: now.rows))
                } catch {
                    return Task.isCancelled ? nil : Event(.pollFailed(Failure(error, server: client.server)))
                }
            }
        case let .stop(thread):
            // The stream's own `done` settles the overlay.
            return .once {
                guard let client = await source.current() else { return nil }
                do {
                    try await ChatAPI.stop(client, thread: thread)
                    return nil
                } catch {
                    return Task.isCancelled ? nil : Event(.stopFailed(Failure(error, server: client.server)))
                }
            }
        case .stopWait:
            return .once {
                do { try await clock.sleep(until: clock.now.addingTimeInterval(1)) } catch { return nil }
                return Event(.stopWaited)
            }
        case let .check(check):
            return .once {
                do { try await clock.sleep(until: clock.now.addingTimeInterval(1)) } catch { return nil }
                guard let client = await source.current() else { return nil }
                do {
                    let found = try await Self.find(check, client: client)
                    return Event(.checked(outcome: check.outcome, found: found, failure: nil))
                } catch {
                    // A read that failed proves nothing either way.
                    if Task.isCancelled { return nil }
                    return Event(.checked(outcome: check.outcome, found: nil, failure: Failure(error, server: client.server)))
                }
            }
        case .advance:
            guard let id = continuing, let background = environment.devices.background else { return .none }
            return .once {
                await background.advance(id)
                return nil
            }
        case .cancel:
            return .cancel
        }
    }

    /// A send whose delivery is unknown, looked for. On an existing chat: a
    /// user row with the same text past the newest turn held before the
    /// send (or written since it, where none was held). On a new chat: the
    /// agent's newest chats, one started since the send whose first message
    /// is this one. Same text is evidence, not proof.
    static func find(_ check: State.Check, client: SubstrateClient) async throws -> Found? {
        let since = check.sentAt.addingTimeInterval(-5)
        if let thread = check.thread {
            let now = try await ChatAPI.readNow(client, thread: thread)
            let arrived = now.rows.contains { row in
                row["role"]?.string == "user" && row["content"]?.string == check.message
                    && (check.baseline.map { (row["turn"]?.int ?? -1) > $0 } ?? (row.createdDate >= since))
            }
            guard arrived else { return nil }
            return Found(thread: thread, record: now.thread, rows: now.rows, running: now.thread?["status"]?.string == "running")
        }
        let page = try await client.list(
            ListQuery(
                kinds: [Kinds.thread], properties: ["mode": ["eq": "chat"], "agent": ["eq": .string("\(Kinds.agent)/\(check.agent)")]],
                first: 5, orderBy: "startedAt:desc"))
        for thread in page.records where ChatThread(record: thread).startedAt >= since {
            let first = try await client.list(
                ListQuery(
                    kinds: [Kinds.message], properties: ["thread": ["eq": .string(thread.id)], "role": ["eq": "user"]],
                    first: 1, orderBy: "turn:asc,createdAt:asc")
            ).records.first
            guard first?["content"]?.string == check.message else { continue }
            let record = try await client.record(Kinds.thread, thread.id)
            return Found(thread: thread.id, record: record, rows: [], running: record?["status"]?.string == "running")
        }
        return nil
    }

    /// A stream of events from a body that yields steps, ended when the
    /// body returns and cancelled with the stream.
    static func relay(_ body: @escaping @Sendable (_ yield: @escaping @Sendable (Step) -> Void) async -> Void) -> AsyncStream<Event> {
        AsyncStream { continuation in
            let task = Task {
                await body { continuation.yield(Event($0)) }
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }
}
