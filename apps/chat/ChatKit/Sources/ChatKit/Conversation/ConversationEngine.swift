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
        next.idsUnserved = false
        next.adopted = nil
        next.gaveBack = false
        next.answered = nil
        next.replyOutcome = nil
        switch event.step {
        case let .drafted(text):
            next.draft = text
            next.rewritten = false
        case let .rested(text):
            rewrite(&next, text)
        case let .filled(text):
            rewrite(&next, text)
            // The person put this text in the place of what was there.
            next.given = []
        case let .heard(words):
            rewrite(&next, joined(next.draft, words))
        case let .chose(agent):
            next.agent = agent
        case .listened:
            next.listenOnOpen = false
        case let .ownWords(interaction):
            next.answering = interaction
            next.focuses += 1
        case .optionsShown:
            next.answering = nil
        case .opened:
            // A field starts from the tree: it takes up what was typed
            // into the chat before, which the tree did not carry.
            if next.carried != next.draft { rewrite(&next, next.draft) }
            // A message the device kept from a run of the app that ended
            // before its send was answered: read for by its id, where the
            // server can be asked, and never sent again without a press.
            if next.restored, let pending = next.pending {
                next.restored = false
                if online {
                    next.finding = true
                    next.asks.append(.find(id: pending.id))
                } else {
                    next.unsentFailure = Failure(.message, .closed)
                }
            }
            guard next.thread != nil, !next.active else { break }
            if next.reloadFailed != nil {
                retryReload(&next)
            } else if facts?.running == true {
                attach(&next, attempt: 0)
            }
        case let .sent(message, agent, at, baseline, fromDraft, id):
            // The person's own word: a rate limit after it may be tried
            // again once on its own.
            if fromDraft { next.autoRetried = false }
            send(&next, message: message, agent: agent, at: at, baseline: baseline, fromDraft: fromDraft, id: id)
        case let .sentAgain(at):
            next.autoRetried = false
            sendAgain(&next, at: at)
        case .tookBack:
            guard next.standing, let pending = next.pending else { break }
            forget(&next)
            giveBack(&next, pending.message)
        case let .repeatDue(id, at):
            guard case let .send(run)? = next.run, run.id == id, !run.named, !run.unkept, next.phase == .streaming,
                let pending = next.pending
            else { break }
            // Past the window nothing goes on its own: the message stands,
            // and Send Again is the person's to press.
            if at.timeIntervalSince(pending.sentAt) > Self.repeatWindow {
                stand(&next, run, next.lastFailure ?? Failure(.message, .timeout, at: at))
            } else {
                go(&next)
            }
        case let .found(id, thread, at):
            guard next.finding, let pending = next.pending, pending.id == id else { break }
            next.finding = false
            guard let thread else {
                next.unsentFailure = Failure(.message, .closed, at: at)
                break
            }
            // The server holds it: it is the stored row, and its chat is
            // read for the rest.
            adopt(&next, thread)
        case let .findFailed(id, failure, at):
            guard next.finding, next.pending?.id == id else { break }
            next.finding = false
            next.failure = failure
            next.unsentFailure = Failure(.message, .closed, at: at)
        case .kept, .landed, .draftFound, .replyAsked:
            // Read from the value below, as after every event.
            break
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
            next.connected = true
            run.named = true
            run.started = true
            next.run = .send(run)
            if run.resumes { next.turnFailure = nil }
            if next.thread == nil { next.thread = thread }
            // The server took the message: the optimistic turn is confirmed.
            next.provisional.confirm()
            // Its stream is the running turn's, said from its start as an
            // attach's is: written over what is held of it. The first copy
            // of a send that started its own turn is answered with that
            // turn as it happens; a repeat's answer says it again.
            if run.joins { next.provisional.begin(replaying: true) } else if run.id == nil || next.copies <= 1 { catchUp(&next) }
            if run.id != nil {
                forget(&next, taken: true)
                next.lastFailure = nil
                next.asks.append(.cancel("repeat"))
            } else if !run.resumes {
                answerDelivered(&next, nil)
                replySettled(&next, nil)
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
            taken(&next, by: rows)
            run.held = persistedTurns(rows)
            run.replied = Self.replied(rows)
            next.run = .attach(run)
            // The rows take the place of what the stream had said of them,
            // and the stream starts again from the turn's start over the
            // rest: nothing held is emptied for it.
            TurnFold.settle(rows, &next.provisional, aliases: &next.aliases, ended: false)
            next.provisional.begin(replaying: true)
        case let .attachReadFailed(failure):
            guard case let .attach(run)? = next.run else { break }
            if failure.report == .accessEnded {
                next.failure = failure
                stopRunning(&next)
                break
            }
            if run.probe {
                // The wait asks again.
                next.run = nil
                break
            }
            // Said as reconnecting while it asks again, as a stream that
            // dropped is.
            next.lastFailure = failure
            reattach(&next, attempt: run.attempt + 1)
        case let .retryDue(attempt):
            guard next.phase == .streaming else { break }
            attach(&next, attempt: attempt)
        case let .handedOver(record, rows):
            guard record != nil else {
                gone(&next)
                break
            }
            taken(&next, by: rows)
            // The run is over and its rows are read: each takes the place,
            // and the id, of the turn the stream drew.
            TurnFold.settle(rows, &next.provisional, aliases: &next.aliases, ended: true)
            next.reloadFailed = nil
            next.reloading = nil
            next.phase = .idle
            next.run = nil
            next.settles += 1
            // The run left messages unread and the thread runs again: that
            // is the turn they start, and its stream says each one taken.
            let follow = follows(next, thread: record)
            next.follows = false
            if follow { attach(&next, attempt: 0) }
        case let .handoverFailed(failure):
            next.follows = false
            next.failure = failure
            next.reloadFailed = failure
            next.reloading = nil
            next.phase = .idle
            next.run = nil
        case .pollTick:
            // A chat's run streams whatever started it (`agents` 5): one
            // that had no stream when it was asked for may have one now, so
            // the wait asks again instead of reading the rows.
            if streamsEveryRun { probe(&next) } else { poll(&next) }
        case .nudgedPoll:
            poll(&next)
        case let .polled(record, rows):
            next.polling = false
            guard next.phase == .background else { break }
            guard let record else {
                gone(&next)
                break
            }
            taken(&next, by: rows)
            let ended = record["status"]?.string != "running"
            TurnFold.settle(rows, &next.provisional, aliases: &next.aliases, ended: ended)
            if ended {
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
            if case let .send(run)? = next.run, !run.named {
                // No turn is known to run yet: the server has not answered
                // the send. Under its id the message stops being sent and
                // stands, to send again or to edit, and a turn a copy may
                // have started is stopped where its thread is known.
                // Without an id there is nothing safe to do but wait: an
                // attach here would end the send and drop what was typed.
                // A message sent into a running turn is another matter:
                // the turn is known, and the stop is for it. The message
                // stands, and the turn's own stream is read again for how
                // the stop ended it.
                guard run.id != nil, !run.unkept || run.joins, next.phase == .streaming else { break }
                stand(&next, run, Failure(.message, .interrupted))
                if let thread = next.thread, next.phase == .idle || run.joins {
                    next.stopping = run.joins
                    next.asks.append(.stop(thread: thread))
                }
                break
            }
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
            if case let .send(run)? = next.run, !run.named {
                // A send the server has not answered is not a turn to
                // attach to: an attach would end it, and what was typed
                // with it. Under its id it goes again now, where the app
                // may still repeat it; without one it ends as it ends.
                if let id = run.id, !run.unkept, next.phase == .streaming, next.tries <= Self.repeatDelays.count {
                    next.asks.append(.repeatWait(id: id, seconds: 0))
                }
                break
            }
            if next.phase == .streaming, next.thread != nil { attach(&next, attempt: 0) }
        }
        // What the list says of the pending message, read from the value
        // whatever the event was: its rows hold it, so it is the stored row
        // and no longer pending; or the device keeps it, so its first copy
        // may go.
        if landed, next.pending != nil, next.run.map({ if case .attach = $0 { true } else { false } }) ?? true {
            forget(&next, taken: true)
        }
        // A new chat's message whose first copy opened a thread the list
        // has since read: the chat is that thread, whatever became of the
        // copies still asking.
        if let thread = landedOn, next.thread == nil, next.pending != nil { adopt(&next, thread) }
        // The thread this chat turned out to be had a draft the folder
        // kept: it goes under what the composer holds, once, and the list
        // hears of the composer's text at once, as of one given back.
        if let older = draftFound, next.draftTaken != older {
            next.draftTaken = older
            if !next.draft.contains(older) {
                rewrite(&next, Self.above(next.draft, older))
                give(&next, older, below: true)
            }
            next.gaveBack = true
        }
        if case var .send(run)? = next.run, run.unkept, let pending = next.pending, kept == pending.id {
            run.unkept = false
            next.run = .send(run)
            go(&next)
        }
        // A pending message is always somebody's: a send carries it, it is
        // read for, or it stands with its card. One whose run was replaced
        // (an attach took the thread) stands, so it is never on screen with
        // nothing to press.
        if let pending = next.pending, next.unsentFailure == nil, !next.finding, !next.restored {
            var carried = false
            if case let .send(run)? = next.run, run.id == pending.id, !run.named { carried = true }
            if !carried { next.unsentFailure = Failure(.message, .interrupted) }
        }
        // A reply typed into a notification: taken up once, and stood
        // where its time ran out with no answer.
        if let reply {
            if next.repliedTo != reply.id {
                next.repliedTo = reply.id
                send(&next, reply)
            } else if reply.expired, next.replying == reply.id {
                expire(&next)
            }
        }
        // A message is queued only while a run may read it.
        quieten(&next)
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
        state.asks += ["retry", "pollTimer", "poll", "stop", "stopWait", "check", "repeat"].map { .cancel($0) }
        state.polling = false
        state.pollAgain = false
        // The stream before is let go, and a call may read a message
        // before the next is open: the new one says its turn's queue again
        // from the start, a send into a running turn's too.
        deafen(&state)
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
        giveBack(&state, message)
        state.answer = nil
        replySettled(&state, failure)
        // A sign-in that ended is the signed-out screen's to say.
        if failure.report != .accessEnded { state.chatFailure = failure }
    }

    /// Puts a message back into the composer, ahead of anything typed
    /// since: the send emptied it, so whatever the conversation holds now
    /// was typed since, the same words again included. The field takes the
    /// text, and keeps after it what it has typed and not said.
    private func giveBack(_ state: inout State, _ message: String) {
        rewrite(&state, Self.above(message, state.draft))
        give(&state, message, below: false)
        state.gaveBack = true
    }

    /// The rewrite just made put `text` into the composer, and it is in no
    /// other place: the tree says so beside the draft, for a field that
    /// keeps its own text over the rewrite.
    private func give(_ state: inout State, _ text: String, below: Bool) {
        state.given.append(DraftGiven(text: text, below: below, revision: state.draftRevision))
        // A field looks at every revision while it is on screen, and takes
        // the tree's whole text when it comes to one: the last few are all
        // any field can have missed.
        state.given = Array(state.given.suffix(8))
    }

    // MARK: A message under its own id

    /// The pending message is no longer pending: the server took it, the
    /// rows hold it, or the person took it back.
    ///
    /// - Parameter taken: the server holds it. Where it was the answer to
    ///   questions in the person's own words, they are dismissed now; given
    ///   back or taken back, it answered nothing.
    private func forget(_ state: inout State, taken: Bool = false) {
        if taken {
            answerDelivered(&state, state.pending?.id)
            replySettled(&state, nil)
        } else {
            state.answer = nil
        }
        state.pending = nil
        state.unsentFailure = nil
        state.copies = 0
        state.tries = 0
        if state.finding {
            state.finding = false
            state.asks.append(.cancel("find"))
        }
    }

    /// The rows a read of this conversation's own brought: where they hold
    /// the pending message, it is the stored row.
    private func taken(_ state: inout State, by rows: [Record]) {
        read(&state, queuedIn: rows)
        guard let pending = state.pending, rows.contains(where: { $0.id == pending.id }) else { return }
        forget(&state, taken: true)
    }

    // MARK: A reply typed into a notification

    /// Sends a reply typed into a notification, as the composer's Send
    /// would: under the id minted for it where the server takes one, kept
    /// in the folder before its request leaves, and sent again if its
    /// answer is lost. Where a send cannot go now, the text goes into the
    /// composer, ahead of what is there, and the list is told why: it is
    /// never dropped.
    private func send(_ state: inout State, _ reply: Reply) {
        guard blockedReason(state) == nil, let agent = agent(state) else {
            let why: Failure =
                switch state.phase {
                case .streaming, .handingOver, .background:
                    Failure(.send, .http(status: 409, code: "conflict", reason: RefusalReason.threadBusy.rawValue))
                case .idle:
                    Failure(.send, online ? .interrupted : .offline)
                }
            giveBack(&state, reply.text)
            state.replyOutcome = ReplyOutcome(id: reply.id, failure: why)
            return
        }
        state.replying = reply.id
        let baseline = state.thread != nil && state.reloadFailed == nil ? newestTurn : nil
        send(
            &state, message: reply.text, agent: agent, at: reply.at, baseline: baseline, fromDraft: false, id: sendsIDs ? reply.id : nil,
            typed: true, bound: reply.generation)
    }

    /// The action's time is up with the reply unanswered: it stops being
    /// sent and stands at its place in the chat, with Send Again. A send
    /// under no id cannot be stopped safely, and ends as it ends.
    private func expire(_ state: inout State) {
        if case let .send(run)? = state.run, !run.named, run.id != nil {
            stand(&state, run, Failure(.message, .timeout))
        }
        replySettled(&state, state.unsentFailure ?? Failure(.message, .timeout))
    }

    /// What became of the reply being sent is known: the list is told, once.
    private func replySettled(_ state: inout State, _ failure: Failure?) {
        guard let id = state.replying else { return }
        state.replying = nil
        state.replyOutcome = ReplyOutcome(id: id, failure: failure)
    }

    /// The server holds the message `id` (nil for one sent under no id).
    /// Where it is the answer given to questions in the person's own
    /// words, the list is told, which dismisses them: a typed message
    /// alone leaves a question waiting.
    private func answerDelivered(_ state: inout State, _ id: String?) {
        guard let answer = state.answer, answer.message == id else { return }
        state.answer = nil
        state.answered = WordsAnswer(interaction: answer.interaction, message: answer.message, linked: answer.linked)
        if state.answering == answer.interaction { state.answering = nil }
    }

    /// The pending message is a stored row of `thread`, as a read says: it
    /// is pending no more, a copy still asking is let go, and a new chat is
    /// that thread from here, read for what it holds.
    private func adopt(_ state: inout State, _ thread: String) {
        if case let .send(run)? = state.run, !run.named, run.id == state.pending?.id {
            state.provisional.keep(run.kept)
            state.reloadFailed = run.failedBefore
            state.phase = .idle
            state.stopping = false
            state.run = nil
            state.lastFailure = nil
            state.asks += [.cancel("repeat"), .cancel("run")]
        }
        forget(&state, taken: true)
        if state.thread == nil {
            state.thread = thread
            state.adopted = thread
        }
        if !state.active {
            if facts?.running == true { attach(&state, attempt: 0) } else { settle(&state) }
        }
    }

    /// One more copy of the pending message, under its id.
    private func go(_ state: inout State) {
        guard let pending = state.pending else { return }
        state.copies += 1
        state.tries += 1
        state.asks.append(
            .chat(
                agent: pending.agent, thread: pending.thread, message: pending.message, id: pending.id, repliesTo: pending.repliesTo,
                bound: pending.bound))
    }

    /// Send Again: the same request once more. The id is the one the
    /// message was first sent under, so whatever became of the copies
    /// before, the message is written once and its turn runs once.
    private func sendAgain(_ state: inout State, at: Date) {
        guard state.standing, var pending = state.pending, state.phase == .idle || joins(state) else { return }
        // A turn runs on the thread: the message joins it, as one sent now
        // from the composer does.
        let joining = joins(state)
        let known = joining ? join(&state) : ([], -1)
        startRun(&state)
        pending.sentAt = at
        // Pressed in the chat, by the person signed in now.
        pending.bound = nil
        state.pending = pending
        state.unsentFailure = nil
        state.tries = 0
        state.turnFailure = nil
        state.lastFailure = nil
        state.chatFailure = nil
        let kept = state.provisional.turns
        var run = State.Send(
            message: pending.message, agent: pending.agent, sentTo: pending.thread, kept: kept, failedBefore: state.reloadFailed,
            baseline: nil, sentAt: at)
        run.id = pending.id
        (run.joins, run.held, run.replied) = (joining, known.0, known.1)
        state.reloadFailed = nil
        state.unread = 0
        state.seq += 1
        state.sends += 1
        state.provisional.keep(kept)
        state.provisional.begin()
        state.provisional.send(pending.message, id: pending.id, seq: state.seq, baseline: nil, at: at, joining: joining)
        state.phase = .streaming
        if !joining { state.connected = false }
        state.stopping = false
        state.run = .send(run)
        go(&state)
    }

    /// The pending message stands unsent at its place in the chat, with
    /// why: its run is over, and Send Again is the person's to press.
    private func stand(_ state: inout State, _ run: State.Send, _ failure: Failure) {
        state.provisional.keep(run.kept)
        state.reloadFailed = run.failedBefore
        state.phase = .idle
        state.stopping = false
        state.run = nil
        state.lastFailure = nil
        // A copy still out is let go: whatever it wrote, the rows will say.
        state.asks += [.cancel("repeat"), .cancel("run")]
        state.unsentFailure = failure.attempting(.message)
        replySettled(&state, state.unsentFailure)
        // A turn runs on the thread, which may be this message's own: an
        // attach reads what the thread holds. So does one the message was
        // sent into, whose stream was let go for the send's.
        var busy = run.joins
        if case let .http(409, _, reason) = failure.why, reason == RefusalReason.threadBusy.rawValue { busy = true }
        if busy, state.thread != nil { attach(&state, attempt: 0) }
    }

    /// A copy of a message sent under its id ended before the server named
    /// its thread.
    ///
    /// Certain that nothing was written: the one copy sent was refused by
    /// the server's own envelope, or never left this device. The text goes
    /// back into the composer, as a send without an id does, and the id is
    /// done with.
    ///
    /// Anything else leaves it unknown whether a copy landed, a refusal of
    /// a later copy included: a copy still on its way can be written after
    /// another was refused. So the id is kept. An answer that never came is
    /// asked for again on the app's own, three times at most and within
    /// `repeatWindow` of the press; then, and after a refusal, the message
    /// stands unsent with Send Again.
    private func unanswered(_ state: inout State, _ run: State.Send, failure: Failure?, outcome: UUID) {
        if let failure { state.failure = failure }
        let failure = failure ?? Failure(.send, .streamEnded)
        let refused = failure.refusal() != nil || failure.neverSent || failure.report == .accessEnded
        if refused, state.copies <= 1 {
            forget(&state)
            end(&state, run, unknown: nil, outcome: outcome)
            if Self.refusesMessageID(failure) {
                // The server's document said it takes an id and the server
                // does not: nothing was written, so the message goes as it
                // does to a server that takes none, and the list is told.
                state.idsUnserved = true
                // But into a running turn, where one without an id is
                // refused as busy: its text goes back, as below.
                if !run.joins {
                    return send(
                        &state, message: run.message, agent: run.agent, at: run.sentAt, baseline: run.baseline, fromDraft: false, id: nil,
                        typed: true)
                }
            }
            unsent(&state, run.message, typed: true, failure: failure, unconfirmed: false)
            // The turn it was sent into runs on, and its stream was let
            // go for the send's: read again.
            if run.joins, state.thread != nil { attach(&state, attempt: 0) }
            return
        }
        if refused { return stand(&state, run, failure) }
        guard state.tries <= Self.repeatDelays.count else { return stand(&state, run, failure) }
        // Shown as reconnecting, never as a failure, while it waits.
        state.lastFailure = failure.attempting(.message)
        state.asks.append(.repeatWait(id: run.id ?? "", seconds: Self.repeatDelays[max(0, state.tries - 1)]))
    }

    /// A server older than `messageId` refuses the field by name, as it
    /// refuses any body field it does not know (`400 bad_request`).
    static func refusesMessageID(_ failure: Failure) -> Bool {
        failure.status == 400 && failure.said?.contains("unknown field \"messageId\"") == true
    }

    // MARK: Runs, continued

    private func send(
        _ state: inout State, message: String, agent: String, at: Date, baseline: Int?, fromDraft: Bool, id: String?, typed: Bool? = nil,
        bound: String? = nil
    ) {
        // A turn runs on the thread and the server takes a message into
        // it: the send's stream is that turn's, so the one being read is
        // let go for it.
        let joining = id != nil && joins(state)
        let known = joining ? join(&state) : ([], -1)
        startRun(&state)
        // Continue sends its own words and leaves what is being typed alone.
        if fromDraft {
            rewrite(&state, "")
            // What the composer was given went with the draft that is sent.
            state.given = []
        }
        state.turnFailure = nil
        state.lastFailure = nil
        // The strip's failure was about a send or this chat's last run: a
        // new send answers it.
        state.chatFailure = nil
        // What a run before this one drew and no read of this conversation
        // has paired with its rows yet stays held: a read that failed, a
        // run a sign-in's end cut off. The fold draws it once with the
        // rows whoever reads them, and it keeps its id; let go of here, its
        // row would be drawn as another turn.
        let kept = state.provisional.turns
        var run = State.Send(
            message: message, agent: agent, sentTo: state.thread, kept: kept, failedBefore: state.reloadFailed,
            baseline: baseline, sentAt: at, typed: typed ?? fromDraft)
        run.id = id
        (run.joins, run.held, run.replied) = (joining, known.0, known.1)
        // Sent from the composer while a question's card is aside for it:
        // this is the answer, in the person's own words.
        // Where the server takes it, the message names them and the
        // server resolves them with it; else they are dismissed once the
        // server holds the message.
        let words = fromDraft ? state.answering : nil
        let linked = words != nil && id != nil && linksWords && state.thread != nil
        state.answer = words.map { State.OwnAnswer(message: id, interaction: $0, linked: linked) }
        state.reloadFailed = nil
        state.unread = 0
        state.seq += 1
        state.sends += 1
        state.provisional.keep(kept)
        state.provisional.begin()
        // Under its id the bubble is the row from the start: the stored
        // message has the same id, and the fold pairs the two by it. With
        // no id it is paired by its words, past what the thread held: a
        // new chat's thread holds nothing before this message, so its row
        // is the first there is.
        state.provisional.send(message, id: id, seq: state.seq, baseline: state.thread == nil ? -1 : baseline, at: at, joining: joining)
        state.phase = .streaming
        // A turn that was being read is still being written.
        if !joining { state.connected = false }
        state.stopping = false
        guard let id else {
            state.run = .send(run)
            state.asks.append(.chat(agent: agent, thread: state.thread, message: message, id: nil, bound: bound))
            return
        }
        // The request leaves once the device keeps the message (`kept`),
        // so a kill at any moment after leaves the id to send it under.
        run.unkept = true
        state.run = .send(run)
        state.pending = UnsentMessage(
            id: id, message: message, agent: agent, thread: state.thread, sentAt: at, repliesTo: linked ? words : nil, bound: bound)
        state.copies = 0
        state.tries = 0
        state.unsentFailure = nil
    }

    /// Runs the thread again with no new message: Try Again and Continue.
    /// The failure whose button was pressed stays drawn, working, until the
    /// server answers, so a second failure replaces it in place.
    private func resume(_ state: inout State, at: Date, automatic: Bool = false) {
        guard let thread = state.thread, let agent = agent(state) else { return }
        startRun(&state)
        state.lastFailure = nil
        let kept = state.provisional.turns
        var run = State.Send(
            message: "", agent: agent, sentTo: thread, kept: kept, failedBefore: state.reloadFailed, baseline: nil, sentAt: at,
            typed: false, resumes: true)
        run.automatic = automatic
        run.named = true
        state.reloadFailed = nil
        state.unread = 0
        state.provisional.keep(kept)
        state.provisional.begin()
        state.phase = .streaming
        state.connected = false
        state.stopping = false
        state.run = .send(run)
        state.asks.append(.resume(thread: thread))
    }

    /// Attaches to the turn running on the thread: the rows are read first
    /// and their ordinals frozen, so the replayed events they hold are
    /// dropped.
    func attach(_ state: inout State, attempt: Int) {
        guard let thread = state.thread else { return }
        startRun(&state)
        state.turnFailure = nil
        state.stopping = false
        state.phase = .streaming
        state.connected = false
        state.run = .attach(State.Attach(attempt: attempt))
        state.asks.append(.attach(thread: thread))
    }

    /// A turn nothing streamed may have a stream by now: asks for it, and
    /// leaves the conversation as it is unless it has. The ask reads the
    /// rows first, as the poll it stands in for does.
    private func probe(_ state: inout State) {
        guard state.phase == .background, state.run == nil, let thread = state.thread else { return }
        state.run = .attach(State.Attach(attempt: 0, probe: true))
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
            // No stream until the wait is over and one opens.
            state.connected = false
            deafen(&state)
            state.asks.append(.wait(attempt: attempt, seconds: delay))
        }
    }

    private func stream(_ state: inout State, _ event: AgentEvent, at: Date) {
        switch state.run {
        case var .send(run)?:
            state.connected = true
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
                state.provisional.mention(event.result?.mentions)
                state.turnFailure = ended(event.result, at: at)
                ended(&state, queueAfter: event.result?.status)
                return
            }
            if event.kind == .idle {
                // A repeat of a message the server already holds, whose
                // turn this server process does not stream: the thread's
                // status is the whole answer.
                run.idle = event.status ?? "unknown"
                state.run = .send(run)
                if run.idle != "running" { ended(&state, queueAfter: run.idle) }
                return
            }
            hear(&state, event, replied: max(run.replied, rowReplied))
            // What the rows held when this send took a stream's place is
            // dropped from its replay, as that stream dropped it; and so
            // is what a turn already running said before this send joined
            // it, where its rows are drawn.
            if isReplayed(event, held: run.held) || stale(event, state) { return }
        case var .attach(run)?:
            if event.kind != .idle {
                // The stream is open. One that was only asked after has a
                // turn to draw: the conversation streams from here.
                state.connected = true
                if run.probe {
                    run.probe = false
                    state.run = .attach(run)
                    state.phase = .streaming
                    state.stopping = false
                    state.asks.append(.cancel("pollTimer"))
                }
            }
            switch event.kind {
            case .idle:
                run.idle = event.status ?? "unknown"
                // The turn ended. A failure the run itself reported is the
                // turn's. A continue whose answer was lost may or may not
                // have run, and the thread's row says how it stands either
                // way: its reply, or the failure it settled on (the one the
                // button was pressed on, or a new one) with the server's
                // cause; a refused continue never comes here. A stream that
                // merely dropped says nothing of how the turn ended, which
                // the thread's row does.
                if run.idle != "running", let last = state.lastFailure {
                    if case .run = last.why {
                        state.turnFailure = last.attempting(.reply)
                    }
                }
                state.run = .attach(run)
                // No turn runs: the one a message joined is over.
                if run.idle != "running" { ended(&state, queueAfter: run.idle) }
                return
            case .done:
                run.done = true
                state.lastFailure = nil
                state.run = .attach(run)
                state.provisional.mention(event.result?.mentions)
                state.turnFailure = ended(event.result, at: at)
                ended(&state, queueAfter: event.result?.status)
                return
            case .thread:
                // Open again after a stream that dropped: no longer
                // reconnecting. How the turn ended, where it did while the
                // stream was down, is the `idle` line's to say, which comes
                // alone.
                state.lastFailure = nil
                return
            case .lagged:
                run.lagged = true
                state.run = .attach(run)
                return
            default:
                hear(&state, event, replied: max(run.replied, rowReplied))
                if isReplayed(event, held: run.held) { return }
            }
        case nil:
            return
        }
        state.seq += 1
        let moved = state.provisional.apply(event, seq: state.seq, now: at)
        if moved, case var .attach(run)? = state.run, !run.moved {
            run.moved = true
            state.run = .attach(run)
        }
    }

    /// How a `done` result says its run ended, where it did not finish: a
    /// server that names causes says why here, before the thread's row is
    /// read.
    private func ended(_ result: AgentResult?, at: Date) -> Failure? {
        guard let result, ["error", "overbudget", "stopped"].contains(result.status) else { return nil }
        return Failure(.reply, .run(status: result.status, cause: result.cause), said: result.reason, retryAfter: result.retryAfter, at: at)
    }

    private func ended(_ state: inout State, failure: Failure?, outcome: UUID) {
        // A run that said it failed reads nothing more.
        if let failure, case .run = failure.why { ended(&state, queueAfter: nil) }
        switch state.run {
        case let .send(run)? where run.resumes && !run.started:
            resumeEnded(&state, run, failure: failure, outcome: outcome)
        case let .send(run)? where run.id != nil && !run.named:
            unanswered(&state, run, failure: failure, outcome: outcome)
        case let .send(run)?:
            guard let failure else {
                if run.named, run.done { return settle(&state) }
                if run.named, let idle = run.idle { return idle == "running" ? background(&state) : settle(&state) }
                if run.named {
                    // The stream closed with the turn still running: a
                    // proxy or the network ended it without an error.
                    state.lastFailure = Failure(.attach, .streamEnded)
                    return reattach(&state, attempt: 0)
                }
                return end(&state, run, unknown: Failure(.send, .streamEnded), outcome: outcome)
            }
            state.failure = failure
            if failure.report == .accessEnded {
                end(&state, run, unknown: nil, outcome: outcome)
                // The sign-in ended before the server took the message:
                // what was typed goes back into the composer, and the
                // signed-out screen says the rest.
                if !run.named, run.typed { giveBack(&state, run.message) }
                return
            }
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
                guard let idle = run.idle else {
                    // Cut off for reading too slowly (`lagged`): nothing
                    // failed, so it attaches again at once. A stream that
                    // brought nothing new waits first, as any that ended
                    // early does, so a reader that cannot keep up does not
                    // ask in a loop.
                    // A probe that closed before its first event drew
                    // nothing: the wait asks again, as for a failed one.
                    if run.probe {
                        state.run = nil
                        return
                    }
                    if !run.lagged { state.lastFailure = Failure(.attach, .streamEnded) }
                    return reattach(&state, attempt: run.lagged && run.moved ? 0 : run.attempt + 1)
                }
                if idle == "running" { return background(&state) }
                return settle(&state)
            }
            state.failure = failure
            if failure.report == .accessEnded { return stopRunning(&state) }
            if run.probe {
                // Nothing was drawn for it: the wait asks again.
                state.run = nil
                return
            }
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
            return send(&state, message: Self.continueMessage, agent: run.agent, at: run.sentAt, baseline: nil, fromDraft: false, id: nil)
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
        case .turnCap?, .spendCap?, .providerKeyMissing?, .quota?, .keyRefused?, .messageIDTaken?, nil: return false
        }
    }

    /// A run that never named its thread wrote nothing: the optimistic turn
    /// goes, and the chat is where it was. One whose delivery is unknown is
    /// read for, once, a second later.
    private func end(_ state: inout State, _ run: State.Send, unknown: Failure?, outcome: UUID) {
        state.provisional.keep(run.kept)
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

    /// The run ended: its rows are read, and each takes the place of the
    /// turn the stream drew, in the step they land in. A read that fails
    /// or outlasts 15 s keeps what the stream drew and releases the
    /// composer.
    private func settle(_ state: inout State) {
        state.stopping = false
        state.lastFailure = nil
        // The turn ended: a stop that failed is no longer a thing to try again.
        if state.chatFailure?.attempt == .stop { state.chatFailure = nil }
        guard let thread = state.thread else {
            state.provisional = .empty
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
        // What was typed stays to copy: a message the server never took
        // goes back into the composer.
        if let pending = state.pending {
            forget(&state)
            giveBack(&state, pending.message)
        }
        state.provisional = .empty
        state.phase = .idle
        state.run = nil
        state.deleted = true
    }

    /// A turn this server has no stream for holds the thread: say so, and
    /// every 5 s read the thread, or ask for its stream again where every
    /// run of a chat has one (`streamsEveryRun`), until it settles. What a
    /// stream drew of it before stays until its rows are read.
    private func background(_ state: inout State) {
        state.phase = .background
        state.stopping = false
        state.run = nil
        deafen(&state)
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
        // The check found it: the server holds it.
        answerDelivered(&state, nil)
        replySettled(&state, nil)
        if found.running { attach(&state, attempt: 0) }
    }
}
