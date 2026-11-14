import Foundation
import SubstrateCore

/// The engine: each event's step, as `Conversation` (the console's) takes
/// it, with what it asks for written into the state's `asks`.
extension Conversation {
    public func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        next.asks = []
        next.failure = nil
        // Told to the list by the event that met it, and by no later one:
        // the list believes it only until the server's document is read
        // again.
        next.continueUnserved = false
        switch event.step {
        case let .drafted(text):
            next.draft = text
            next.rewritten = false
        case let .rested(text):
            rewrite(&next, text)
        case let .filled(text):
            rewrite(&next, text)
        case let .heard(words):
            rewrite(&next, joined(next.draft, words))
        case let .chose(agent):
            next.agent = agent
        case .listened:
            next.listenOnOpen = false
        case .opened:
            // A field starts from the tree: it takes up what was typed
            // into the chat before, which the tree did not carry.
            if next.carried != next.draft { rewrite(&next, next.draft) }
            guard next.thread != nil, !next.active else { break }
            if next.reloadFailed != nil {
                retryReload(&next)
            } else if facts?.running == true {
                attach(&next, attempt: 0)
            }
        case let .sent(message, agent, at, baseline, fromDraft):
            // The person's own word: a rate limit after it may be tried
            // again once on its own.
            if fromDraft { next.autoRetried = false }
            send(&next, message: message, agent: agent, at: at, baseline: baseline, fromDraft: fromDraft)
        case let .continued(at):
            next.autoRetried = false
            resume(&next, at: at)
        case let .countdownDue(date):
            // At zero, once, and only where somebody is looking at it.
            // A server without the continue route would need a message of
            // the person's to go on, which only the person sends.
            guard visible, continues, next.phase == .idle, !next.autoRetried, next.thread != nil, retryDeadline(next) == date,
                blockedReason(next) == nil
            else { break }
            next.autoRetried = true
            resume(&next, at: date, automatic: true)
        case .reopened:
            guard next.thread != nil, !next.active else { break }
            if next.chatFailure?.attempt == .attach { next.chatFailure = nil }
            if facts?.running == true { attach(&next, attempt: 0) } else { settle(&next) }
        case .failureDismissed:
            next.chatFailure = nil
        case let .named(thread):
            guard case var .send(run)? = next.run else { break }
            run.named = true
            run.started = true
            next.run = .send(run)
            if run.resumes { next.turnFailure = nil }
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
            next.reloading = nil
            next.phase = .idle
            next.run = nil
            next.settles += 1
        case let .handoverFailed(failure):
            next.failure = failure
            next.reloadFailed = failure
            next.reloading = nil
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
            } else if next.phase == .streaming {
                // The stop failed, not the reply: the turn is still being
                // written, and the strip above the composer says so.
                next.chatFailure = failure
            }
        case .stopWaited:
            if next.phase == .streaming { attach(&next, attempt: 0) }
        case let .checked(outcome, found, failure):
            next.failure = failure
            checked(&next, outcome: outcome, found: found)
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
        // The wait for a rate limit's end follows the failure that names it.
        let deadline = retryDeadline(next)
        if deadline != next.armed {
            next.armed = deadline
            next.asks.append(deadline.map { .countdown($0) } ?? .cancel("countdown"))
        }
        return next
    }

    /// The engine writes the draft itself: the tree carries the text and
    /// the revision moves, so the field takes it, with the draft it stands
    /// in place of: this rewrite's, and the first's of the rewrites made
    /// since the field last spoke.
    private func rewrite(_ state: inout State, _ text: String) {
        state.draftBefore = state.draft
        if !state.rewritten { state.draftReplaced = state.draft }
        state.rewritten = true
        state.draft = text
        state.carried = text
        state.draftRevision += 1
    }

    /// When the rate limit the turn ended on lifts: from the failure the
    /// stream carried, else from the thread's row.
    func retryDeadline(_ state: State) -> Date? {
        guard state.phase == .idle else { return nil }
        return state.turnFailure?.retryAt ?? facts?.retryAt
    }

    // MARK: Runs

    /// Every key a run's work runs under but its own: a new run cancels
    /// them, and a check it leaves can no longer say what the run found.
    private func startRun(_ state: inout State) {
        state.asks += ["retry", "pollTimer", "poll", "stop", "stopWait", "check"].map { .cancel($0) }
        state.polling = false
        state.pollAgain = false
        // A check the run leaves can no longer say what it found.
        if let lost = state.lost {
            state.lost = nil
            unsent(&state, lost.message, typed: lost.typed, failure: lost.failure, unconfirmed: true)
        }
    }

    /// A send that started no turn: its text goes back into the composer,
    /// ahead of anything typed since, so nothing typed is lost, and the
    /// strip above the composer says what happened.
    private func unsent(_ state: inout State, _ message: String, typed: Bool, failure: Failure, unconfirmed: Bool) {
        var failure = failure.attempting(typed ? .send : .resume)
        failure.unconfirmed = unconfirmed
        guard typed else {
            // Continue's own words are nobody's to give back: the turn's
            // card takes the failure, and its button asks again.
            state.turnFailure = failure
            return
        }
        // The send emptied the composer, so whatever it holds now was typed
        // since, the same words again included.
        let typedSince = state.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "" : "\n\n" + state.draft
        state.draft = message + typedSince
        state.draftRevision += 1
        state.chatFailure = failure
    }

    private func send(_ state: inout State, message: String, agent: String, at: Date, baseline: Int?, fromDraft: Bool) {
        startRun(&state)
        // Continue sends its own words and leaves what is being typed alone.
        if fromDraft { rewrite(&state, "") }
        state.turnFailure = nil
        state.lastFailure = nil
        // The strip's failure was about a send or this chat's last run: a
        // new send answers it.
        state.chatFailure = nil
        // The last run's overlay is the stored rows' job now, unless their
        // read failed: then it is still the only view of that run.
        let kept = state.reloadFailed != nil ? state.overlay.turns : []
        let run = State.Send(
            message: message, agent: agent, sentTo: state.thread, kept: kept, failedBefore: state.reloadFailed,
            baseline: baseline, sentAt: at, typed: fromDraft)
        state.reloadFailed = nil
        state.seq += 1
        state.overlay = LiveOverlay(turns: kept + [Turn(id: "live-u\(state.seq)", role: .user, content: message, pending: true)])
        state.phase = .streaming
        state.stopping = false
        state.run = .send(run)
        state.asks.append(.chat(agent: agent, thread: state.thread, message: message))
    }

    /// Runs the thread again with no new message: Try Again and Continue.
    /// The failure whose button was pressed stays drawn, working, until the
    /// server answers, so a second failure replaces it in place.
    private func resume(_ state: inout State, at: Date, automatic: Bool = false) {
        guard let thread = state.thread, let agent = agent(state) else { return }
        startRun(&state)
        state.lastFailure = nil
        let kept = state.reloadFailed != nil ? state.overlay.turns : []
        var run = State.Send(
            message: "", agent: agent, sentTo: thread, kept: kept, failedBefore: state.reloadFailed, baseline: nil, sentAt: at,
            typed: false, resumes: true)
        run.automatic = automatic
        run.named = true
        state.reloadFailed = nil
        state.overlay = LiveOverlay(turns: kept)
        state.phase = .streaming
        state.stopping = false
        state.run = .send(run)
        state.asks.append(.resume(thread: thread))
    }

    /// Attaches to the turn running on the thread: the rows are read first
    /// and their ordinals frozen, so the replayed events they hold are
    /// dropped.
    private func attach(_ state: inout State, attempt: Int) {
        guard let thread = state.thread else { return }
        startRun(&state)
        state.turnFailure = nil
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
            if !run.started {
                // The server answered a continue: the failure it was
                // pressed on is this run's to replace.
                run.started = true
                state.run = .send(run)
                if run.resumes { state.turnFailure = nil }
            }
            if event.kind == .done {
                run.done = true
                state.run = .send(run)
                state.overlay.mention(event.result?.mentions)
                state.turnFailure = ended(event.result, at: at)
                return
            }
        case var .attach(run)?:
            switch event.kind {
            case .idle:
                run.idle = event.status ?? "unknown"
                // The turn ended. A failure the run itself reported is the
                // turn's; a continue that never got an answer is said in
                // its place; a stream that merely dropped says nothing of
                // how the turn ended, which the thread's row does.
                if run.idle != "running", let last = state.lastFailure {
                    if last.attempt == .resume {
                        state.turnFailure = last
                    } else if case .run = last.why {
                        state.turnFailure = last.attempting(.reply)
                    }
                }
                state.run = .attach(run)
                return
            case .done:
                run.done = true
                state.lastFailure = nil
                state.run = .attach(run)
                state.overlay.mention(event.result?.mentions)
                state.turnFailure = ended(event.result, at: at)
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

    /// How a `done` result says its run ended, where it did not finish: a
    /// server that names causes says why here, before the thread's row is
    /// read.
    private func ended(_ result: AgentResult?, at: Date) -> Failure? {
        guard let result, ["error", "overbudget", "stopped"].contains(result.status) else { return nil }
        return Failure(.reply, .run(status: result.status, cause: result.cause), said: result.reason, retryAfter: result.retryAfter, at: at)
    }

    private func ended(_ state: inout State, failure: Failure?, outcome: UUID) {
        switch state.run {
        case let .send(run)? where run.resumes && !run.started:
            resumeEnded(&state, run, failure: failure, outcome: outcome)
        case let .send(run)?:
            guard let failure else {
                if run.named, run.done { return settle(&state) }
                if run.named { return reattach(&state, attempt: 0) }
                return end(&state, run, unknown: Failure(.send, .streamEnded), outcome: outcome)
            }
            state.failure = failure
            if failure.report == .accessEnded { return end(&state, run, unknown: nil, outcome: outcome) }
            if run.named {
                // The turn runs on; the attach that follows reads how it ended.
                state.lastFailure = failure
                return reattach(&state, attempt: 0)
            }
            // Only the server's own 4xx says nothing ran, and a request that
            // never left ran nothing either. A 5xx, a 503 at shutdown or a
            // gateway's answer can come after the turn was admitted, and so
            // can a dropped connection.
            if failure.refusal() != nil || failure.neverSent {
                end(&state, run, unknown: nil, outcome: outcome)
                unsent(&state, run.message, typed: run.typed, failure: failure, unconfirmed: false)
                return
            }
            end(&state, run, unknown: failure, outcome: outcome)
        case let .attach(run)?:
            guard let failure else {
                if run.done { return settle(&state) }
                guard let idle = run.idle else { return reattach(&state, attempt: run.attempt + 1) }
                if idle == "running" { return background(&state) }
                return settle(&state)
            }
            state.failure = failure
            if failure.report == .accessEnded { return stopRunning(&state) }
            if let status = failure.refusal(permanent: true) {
                if status == 404 { return gone(&state) }
                // The stream will not open: the strip says so, and the
                // rows are read for what the thread holds.
                state.chatFailure = failure
                return settle(&state)
            }
            state.lastFailure = failure
            reattach(&state, attempt: run.attempt + 1)
        case nil:
            return
        }
    }

    /// A continue that ended before the server said anything. The route is
    /// called only where the server's document says it is served, so a 404
    /// or a 405 for it is a server that went back since: what Continue did
    /// before the route is done instead, and the list is told, so the next
    /// one does not ask until the document is read again. A thread that is
    /// busy, or that has nothing to continue, is read for what it holds.
    /// Anything else is the turn's failure now, in the place of the one
    /// whose button was pressed; where the request may have reached the
    /// server, an attach reads whether a turn runs first.
    private func resumeEnded(_ state: inout State, _ run: State.Send, failure: Failure?, outcome: UUID) {
        guard let failure else { return reattach(&state, attempt: 0) }
        state.failure = failure
        if failure.status == 404 || failure.status == 405 {
            // The router's own answer for a path it does not serve; a 404
            // in other words is a thread that is gone, which the send's own
            // refusal then says.
            if failure.status == 405 || failure.said?.hasPrefix("no such API path") == true { state.continueUnserved = true }
            end(&state, run, unknown: nil, outcome: outcome)
            // What it tried on its own ends here: the button is the
            // person's to press.
            if run.automatic { return }
            return send(&state, message: Self.continueMessage, agent: run.agent, at: run.sentAt, baseline: nil, fromDraft: false)
        }
        if failure.status == 409, reconciles(failure) { return reattach(&state, attempt: 0) }
        let resumed = failure.attempting(.resume)
        if failure.report == .accessEnded || failure.refusal() != nil || failure.neverSent {
            end(&state, run, unknown: nil, outcome: outcome)
            if failure.report == nil { state.turnFailure = resumed }
            return
        }
        state.lastFailure = resumed
        reattach(&state, attempt: 0)
    }

    /// Whether a 409 for a continue is one the thread's own state answers:
    /// a turn already runs on it, or its reply is complete, or the server
    /// gave no reason. Any other reason (the repository's turns are all
    /// taken, one this build does not know) is a refusal to say.
    private func reconciles(_ failure: Failure) -> Bool {
        guard case let .http(_, _, reason) = failure.why, let reason else { return true }
        switch RefusalReason(rawValue: reason) {
        case .threadBusy?, .nothingToContinue?: return true
        case .turnCap?, .spendCap?, .providerKeyMissing?, .quota?, .keyRefused?, nil: return false
        }
    }

    /// A run that never named its thread wrote nothing: the optimistic turn
    /// goes, and the chat is where it was. One whose delivery is unknown is
    /// read for, once, a second later.
    private func end(_ state: inout State, _ run: State.Send, unknown: Failure?, outcome: UUID) {
        state.overlay = LiveOverlay(turns: run.kept)
        state.reloadFailed = run.failedBefore
        state.phase = .idle
        state.stopping = false
        state.run = nil
        guard let unknown else { return }
        state.lost = State.Lost(id: outcome, message: run.message, typed: run.typed, failure: unknown)
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
        state.lastFailure = nil
        // The turn ended: a stop that failed is no longer a thing to try again.
        if state.chatFailure?.attempt == .stop { state.chatFailure = nil }
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
        guard let failed = state.reloadFailed else { return }
        startRun(&state)
        state.reloading = failed
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
        guard let lost = state.lost, lost.id == outcome else { return }
        state.lost = nil
        guard let found else {
            // It may or may not have arrived: the text is given back, and
            // the strip says to look before sending it again.
            return unsent(&state, lost.message, typed: lost.typed, failure: lost.failure, unconfirmed: true)
        }
        if state.thread == nil { state.thread = found.thread }
        if found.running { attach(&state, attempt: 0) }
    }
}
