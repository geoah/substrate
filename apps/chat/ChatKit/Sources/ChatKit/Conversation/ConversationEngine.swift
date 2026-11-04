import Foundation
import SubstrateCore

/// The engine: each event's step, as `Conversation` (the console's) takes
/// it, with what it asks for written into the state's `asks`.
extension Conversation {
    public func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        next.asks = []
        next.failure = nil
        switch event.step {
        case let .drafted(text):
            next.draft = text
        case let .chose(agent):
            next.agent = agent
        case .listened:
            next.listenOnOpen = false
        case .opened:
            guard next.thread != nil, !next.active else { break }
            if next.reloadFailed != nil {
                retryReload(&next)
            } else if facts?.running == true {
                attach(&next, attempt: 0)
            }
        case let .sent(message, agent, at, baseline):
            send(&next, message: message, agent: agent, at: at, baseline: baseline)
        case let .named(thread):
            guard case var .send(run)? = next.run else { break }
            run.named = true
            next.run = .send(run)
            if next.thread == nil { next.thread = thread }
            // The server took the message: the optimistic turn is confirmed.
            if let i = next.overlay.turns.lastIndex(where: { $0.role == .user && $0.pending }) {
                next.overlay.turns[i].pending = false
            }
        case let .streamed(streamed, at):
            stream(&next, streamed, at: at)
        case let .streamEnded(failure, outcome):
            ended(&next, failure: failure, outcome: outcome)
        case let .attachRead(record, rows):
            guard case var .attach(run)? = next.run else { break }
            guard record != nil else {
                gone(&next)
                break
            }
            run.held = persistedTurns(rows)
            next.run = .attach(run)
            next.overlay = .empty
        case let .attachReadFailed(failure):
            guard case let .attach(run)? = next.run else { break }
            if failure.report == .accessEnded {
                next.failure = failure
                stopRunning(&next)
                break
            }
            reattach(&next, attempt: run.attempt + 1)
        case let .retryDue(attempt):
            guard next.phase == .streaming else { break }
            attach(&next, attempt: attempt)
        case let .handedOver(record, _):
            guard record != nil else {
                gone(&next)
                break
            }
            next.overlay = .empty
            next.reloadFailed = nil
            next.phase = .idle
            next.run = nil
            next.settles += 1
        case let .handoverFailed(words, failure):
            next.failure = failure
            next.reloadFailed = words
            next.phase = .idle
            next.run = nil
        case .pollTick, .nudgedPoll:
            poll(&next)
        case let .polled(record, _):
            next.polling = false
            guard next.phase == .background else { break }
            guard let record else {
                gone(&next)
                break
            }
            if record["status"]?.string != "running" {
                next.asks.append(.cancel("pollTimer"))
                next.pollAgain = false
                next.phase = .idle
                next.settles += 1
            } else if next.pollAgain {
                next.pollAgain = false
                poll(&next)
            }
        case let .pollFailed(failure):
            next.polling = false
            next.failure = failure
            if failure.status == 404 { gone(&next) }
        case .stopAsked:
            guard let thread = next.thread, next.phase == .streaming, !next.stopping else { break }
            next.stopping = true
            next.asks.append(.stop(thread: thread))
        case let .stopFailed(failure):
            next.failure = failure
            guard failure.report == nil else { break }
            next.stopping = false
            // Nothing this process streams runs: the turn settled first, or
            // another process owns it. An attach reads which.
            if failure.status == 409 {
                next.asks.append(.stopWait)
            } else {
                next.error = failure.words
            }
        case .stopWaited:
            if next.phase == .streaming { attach(&next, attempt: 0) }
        case let .checked(outcome, found, failure):
            next.failure = failure
            checked(&next, outcome: outcome, found: found)
        case let .dismissed(id):
            next.outcomes.removeAll { $0.id == id }
        case .retriedReload:
            retryReload(&next)
        case let .threadMoved(moved):
            switch next.phase {
            case .idle where next.reloadFailed != nil:
                // The handover's own read completes it, atomically.
                retryReload(&next)
            case .idle where moved?.running == true && visible:
                attach(&next, attempt: 0)
            case .background where moved?.running == false:
                poll(&next)
            default:
                break
            }
        case .resumed:
            if next.phase == .streaming, next.thread != nil { attach(&next, attempt: 0) }
        }
        return next
    }

    // MARK: Runs

    /// Every key a run's work runs under but its own: a new run cancels
    /// them, and a check it leaves can no longer say what the run found.
    private func startRun(_ state: inout State) {
        state.asks += ["retry", "pollTimer", "poll", "stop", "stopWait", "check"].map { .cancel($0) }
        state.polling = false
        state.pollAgain = false
        for index in state.outcomes.indices where state.outcomes[index].kind == .checking {
            state.outcomes[index].kind = .unknown
        }
    }

    private func send(_ state: inout State, message: String, agent: String, at: Date, baseline: Int?) {
        startRun(&state)
        state.draft = ""
        state.draftRevision += 1
        state.error = nil
        state.lastError = nil
        // An outcome stays until its message is sent again or dismissed.
        state.outcomes.removeAll { $0.message == message && $0.kind != .checking }
        // The last run's overlay is the stored rows' job now, unless their
        // read failed: then it is still the only view of that run.
        let kept = state.reloadFailed != nil ? state.overlay.turns : []
        let run = State.Send(
            message: message, agent: agent, sentTo: state.thread, kept: kept, failedBefore: state.reloadFailed,
            baseline: baseline, sentAt: at)
        state.reloadFailed = nil
        state.seq += 1
        state.overlay = LiveOverlay(turns: kept + [Turn(id: "live-u\(state.seq)", role: .user, content: message, pending: true)])
        state.phase = .streaming
        state.stopping = false
        state.run = .send(run)
        state.asks.append(.chat(agent: agent, thread: state.thread, message: message))
    }

    /// Attaches to the turn running on the thread: the rows are read first
    /// and their ordinals frozen, so the replayed events they hold are
    /// dropped.
    private func attach(_ state: inout State, attempt: Int) {
        guard let thread = state.thread else { return }
        startRun(&state)
        state.error = nil
        state.stopping = false
        state.phase = .streaming
        state.run = .attach(State.Attach(attempt: attempt))
        state.asks.append(.attach(thread: thread))
    }

    /// A stream that ended without `done` is not the turn's end: attach
    /// again, backing off 0, 1, 2, 4, 8 and then 15 seconds, until the
    /// server says it settled.
    private func reattach(_ state: inout State, attempt: Int) {
        state.phase = .streaming
        let delay = attempt == 0 ? 0 : min(pow(2, Double(attempt - 1)), 15)
        if delay == 0 {
            attach(&state, attempt: attempt)
        } else {
            state.asks.append(.wait(attempt: attempt, seconds: delay))
        }
    }

    private func stream(_ state: inout State, _ event: AgentEvent, at: Date) {
        if continuing != nil, event.kind == .heartbeat || state.seq % 8 == 0 { state.asks.append(.advance) }
        switch state.run {
        case var .send(run)?:
            if event.kind == .done {
                run.done = true
                state.run = .send(run)
                return
            }
        case var .attach(run)?:
            switch event.kind {
            case .idle:
                run.idle = event.status ?? "unknown"
                if run.idle != "running", let last = state.lastError { state.error = last }
                state.run = .attach(run)
                return
            case .done:
                run.done = true
                state.lastError = nil
                state.run = .attach(run)
                return
            case .thread:
                return
            default:
                if isReplayed(event, held: run.held) { return }
            }
        case nil:
            return
        }
        state.seq += 1
        state.overlay.apply(event, seq: state.seq, now: at)
    }

    private func ended(_ state: inout State, failure: Failure?, outcome: UUID) {
        switch state.run {
        case let .send(run)?:
            guard let failure else {
                if run.named, run.done { return settle(&state) }
                if run.named { return reattach(&state, attempt: 0) }
                return end(&state, run, unknown: "The connection closed before the server said anything.", outcome: outcome)
            }
            state.failure = failure
            if failure.report == .accessEnded { return end(&state, run, unknown: nil, outcome: outcome) }
            if run.named {
                // The turn runs on; the attach that follows reads how it ended.
                state.lastError = failure.words
                return reattach(&state, attempt: 0)
            }
            // Only the server's own 4xx says nothing ran. A 5xx, a 503 at
            // shutdown or a gateway's answer can come after the turn was
            // admitted, and so can a dropped connection.
            if let refusal = failure.refusal() {
                end(&state, run, unknown: nil, outcome: outcome)
                let restored = state.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                if restored {
                    state.draft = run.message
                    state.draftRevision += 1
                }
                state.outcomes.append(Outcome(id: outcome, kind: .refused, reason: refusal.text, message: run.message, restored: restored))
                return
            }
            end(&state, run, unknown: failure.words, outcome: outcome)
        case let .attach(run)?:
            guard let failure else {
                if run.done { return settle(&state) }
                guard let idle = run.idle else { return reattach(&state, attempt: run.attempt + 1) }
                if idle == "running" { return background(&state) }
                return settle(&state)
            }
            state.failure = failure
            if failure.report == .accessEnded { return stopRunning(&state) }
            if let refusal = failure.refusal(permanent: true) {
                if refusal.status == 404 { return gone(&state) }
                state.error = refusal.text
                return settle(&state)
            }
            state.lastError = failure.words
            reattach(&state, attempt: run.attempt + 1)
        case nil:
            return
        }
    }

    /// A run that never named its thread wrote nothing: the optimistic turn
    /// goes, and the chat is where it was. One whose delivery is unknown is
    /// read for, once, a second later.
    private func end(_ state: inout State, _ run: State.Send, unknown: String?, outcome: UUID) {
        state.overlay = LiveOverlay(turns: run.kept)
        state.reloadFailed = run.failedBefore
        state.phase = .idle
        state.stopping = false
        state.run = nil
        guard let unknown else { return }
        state.outcomes.append(Outcome(id: outcome, kind: .checking, reason: unknown, message: run.message))
        state.asks.append(
            .check(
                State.Check(
                    outcome: outcome, thread: run.sentTo, agent: run.agent, message: run.message,
                    baseline: run.baseline, sentAt: run.sentAt)))
    }

    private func stopRunning(_ state: inout State) {
        state.phase = .idle
        state.stopping = false
        state.run = nil
    }

    /// The run ended: its rows are read and replace the overlay in one
    /// step. A read that fails or outlasts 15 s keeps the overlay and
    /// releases the composer.
    private func settle(_ state: inout State) {
        state.stopping = false
        guard let thread = state.thread else {
            state.overlay = .empty
            state.phase = .idle
            state.run = nil
            return
        }
        state.phase = .handingOver
        state.asks.append(.handover(thread: thread))
    }

    private func retryReload(_ state: inout State) {
        guard state.reloadFailed != nil else { return }
        startRun(&state)
        state.reloadFailed = nil
        settle(&state)
    }

    private func gone(_ state: inout State) {
        state.overlay = .empty
        state.phase = .idle
        state.run = nil
        state.deleted = true
    }

    /// A turn this server does not stream holds the thread: say so, and read
    /// the thread every 5 s until it settles.
    private func background(_ state: inout State) {
        state.overlay = .empty
        state.phase = .background
        state.stopping = false
        state.run = nil
        state.asks.append(.pollEvery)
    }

    /// One read at a time, so an older answer never lands after a newer
    /// one; a poll asked for meanwhile follows it.
    private func poll(_ state: inout State) {
        guard state.phase == .background, let thread = state.thread else { return }
        if state.polling {
            state.pollAgain = true
            return
        }
        state.polling = true
        state.asks.append(.poll(thread: thread))
    }

    private func checked(_ state: inout State, outcome: UUID, found: Found?) {
        guard let index = state.outcomes.firstIndex(where: { $0.id == outcome && $0.kind == .checking }) else { return }
        guard let found else {
            state.outcomes[index].kind = .unknown
            return
        }
        state.outcomes.remove(at: index)
        if state.thread == nil { state.thread = found.thread }
        if found.running { attach(&state, attempt: 0) }
    }
}
