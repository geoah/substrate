import Foundation
import SubstrateRuntime

/// A command as the events it means: the engine event with its ids (UUIDv7)
/// and its instant, floored to the second and never before the previous
/// event's (§4.4 invariant 9), and the history lookups a pure engine may not
/// make. Nothing here suspends: a step whose answer waits on the device (a
/// Start's body mass and permission, a Finish's history file) is an event
/// whose effect brings the rest.
extension SessionController {
    public func events(for command: SessionCommand, in context: Enrichment<SessionController>) -> [Event] {
        let state = context.state
        let now = context.now
        let ids = MintedIDs(source: context.environment.ids)
        let calendar = Self.calendar(context.environment)
        let presented = context.presentation?.session

        switch command {
        case let .start(request):
            guard state.session == nil, !context.transient.starting else { return [] }
            return [.starting(request)]
        case let .finishThenStart(request):
            return [.noted(.finishFlow(request))]
        case .finishFlowShown:
            return [.noted(.finishFlowShown)]
        case .cancelStartAfterFinish:
            return [.noted(.startAfterFinish(nil))]
        case let .discardThenStart(request):
            return discard(state, now: now, ids: ids, then: request)
        case .discard:
            return discard(state, now: now, ids: ids, then: nil)
        case let .finish(request):
            return finish(request, state: state, presentation: presented, now: now, calendar: calendar, ids: ids,
                          then: context.transient.startAfterFinish)
        case .foreground:
            return [.foregrounded]
        case let .scroll(target):
            return [.noted(.scroll(target))]
        case .dismissEntryCheck:
            return context.transient.entryCheck == nil ? [] : [.noted(.answered(edit: false))]
        case let .coverVisible(visible):
            return context.transient.coverVisible == visible ? [] : [.noted(.coverVisible(visible))]
        case .reveal:
            // With none open, the app stays where it is.
            guard state.session != nil else { return [] }
            return [.noted(.reveal(presented?.upNext?.row ?? presented?.timedRow))]
        default:
            break
        }

        // Every other command changes the open session, and none while its
        // Finish is being saved.
        guard let session = state.session, state.finishing == nil else { return [] }
        func engine(_ event: SessionEvent) -> Event { .engine(envelope(event, now: now, after: state.lastAt, ids: ids)) }
        func rows(_ count: Int, at date: Date) -> [RowID] { (0 ..< max(0, count)).map { _ in RowID(ids.make(at: date)) } }

        switch command {
        case let .touchRow(row):
            // A done row, or a second touch that would change nothing, sends no event.
            let clockOpen = session.restClock.map { $0.endedAt == nil } ?? false
            guard session.rows[row]?.isPending == true, session.focus != row || clockOpen else { return [] }
            return [engine(.touchRow(row: row))]
        case let .edit(row, patch):
            return [engine(.editSet(SessionEvent.EditSet(row: row, values: patch, segmentRowIDs: rows(1, at: now))))]
        case let .setRowNote(row, text):
            let note: FieldPatch<String> = text.map { $0.isEmpty ? .cleared : .set($0) } ?? .cleared
            return [engine(.editSet(SessionEvent.EditSet(row: row, note: note)))]
        case let .copyPrevious(row):
            guard let previous = presented?.row(row)?.previous, session.rows[row]?.isPending == true else { return [] }
            return [engine(.editSet(SessionEvent.EditSet(row: row, values: previous.copy)))]
        case let .tick(row):
            return tick(row, stop: false, session: session, presentation: presented, now: now, calendar: calendar, state: state, ids: ids)
        case let .stopTimedSet(row):
            return tick(row, stop: true, session: session, presentation: presented, now: now, calendar: calendar, state: state, ids: ids)
        case let .untick(row):
            return [engine(.uncompleteSet(row: row))]
        case let .addSet(slot, role):
            return [engine(.addSet(SessionEvent.AddSet(slot: slot, row: RowID(ids.make(at: now)), role: role,
                                                       segmentRowIDs: rows(1, at: now))))]
        case let .removeSet(row), let .removeUndoably(row):
            return [engine(.removeSet(row: row))]
        case let .restoreSet(restore):
            return [engine(.restoreSet(restore))]
        case let .changeSetKind(row, kind):
            return [engine(.changeSetKind(SessionEvent.ChangeSetKind(row: row, kind: kind, segmentRowIDs: rows(3, at: now))))]
        case let .setRowRest(row, seconds):
            return [engine(.setRowRest(row: row, seconds: seconds))]
        case let .addExercise(exercise, after):
            let shape = Previous.lastShape(of: exercise.id, history: history, excluding: session.session.id)
            let unilateral = exercise.resolvedLaterality == .unilateral
            let count = shape.roles.count * (unilateral ? 2 : 1)
            let firstSide = unilateral ? Previous.weakerSide(of: exercise.id, history: history, excluding: session.session.id) : nil
            let added = SessionEvent.AddExercise(slot: Self.slotKey(for: exercise, in: session), exercise: exercise, roles: shape.roles,
                                                 rowIDs: rows(count, at: now), rests: shape.rests, after: after, firstSide: firstSide)
            return [engine(.addExercise(added))]
        case let .removeExercise(slot):
            return [engine(.removeExercise(slot: slot))]
        case let .reorderExercises(slots):
            return [engine(.reorderExercises(slots: slots))]
        case let .replaceExercise(slot, exercise):
            return [engine(.replaceExercise(SessionEvent.ReplaceExercise(slot: slot, newSlot: Self.slotKey(for: exercise, in: session),
                                                                         exercise: exercise, segmentRowIDs: rows(8, at: now))))]
        case let .groupExercises(slot, partner):
            return [engine(.groupExercises(slot: slot, partner: partner, group: nil))]
        case let .ungroupExercise(slot):
            return [engine(.ungroupExercise(slot: slot))]
        case let .ungroupAll(group):
            // One member at a time; the group goes when one member is left.
            var after = state.lastAt
            return session.members(ofGroup: group).dropLast().map { member in
                let next = envelope(.ungroupExercise(slot: member.key), now: now, after: after, ids: ids)
                after = next.at
                return .engine(next)
            }
        case let .setGroup(change):
            return [engine(.setGroup(change))]
        case let .setSlotRest(slot, kind, seconds):
            return [engine(.setSlotRest(slot: slot, rest: kind, seconds: seconds))]
        case let .setNote(target, text):
            return [engine(.setNote(target: target, text: text))]
        case .pauseClock:
            return [engine(.pauseClock)]
        case .resumeClock:
            return [engine(.resumeClock)]
        case let .startTimer(seconds):
            return [engine(.startRest(seconds: seconds))]
        case let .adjustRest(seconds):
            return [engine(.adjustRest(seconds: seconds))]
        case .skipRest:
            return [engine(.skipRest)]
        case let .startTimedSet(row):
            guard let presentedRow = presented?.row(row), case let .ready(target)? = presentedRow.timer else {
                return [.noted(.refuse(.noTimer))]
            }
            return [engine(.startTimedSet(row: row, target: target))]
        case let .cancelTimedSet(row):
            return [engine(.cancelTimedSet(row: row))]
        case let .completeUpNext(row):
            // Only while that row is still Up next and pending: a tap on a
            // Lock Screen that fell behind must not tick another set.
            guard presented?.upNext?.row == row, let values = presented?.row(row)?.commitValues else { return [] }
            // No dialog can ask from the Lock Screen: a set the entry check
            // would question waits for the owner in the app. The widget
            // hides Done then.
            guard EntryCheck.warning(for: row, values: values, in: session, history: history, now: now, calendar: calendar) == nil
            else { return [.noted(.leftForTheApp)] }
            return commit(row, values: values, stop: false, session: session, now: now, calendar: calendar, state: state, ids: ids)
        case let .stopFromLockScreen(row):
            guard let values = presented?.row(row)?.stopValues else { return [] }
            guard EntryCheck.warning(for: row, values: values, in: session, history: history, now: now, calendar: calendar) == nil
            else { return [.noted(.leftForTheApp)] }
            return commit(row, values: values, stop: true, session: session, now: now, calendar: calendar, state: state, ids: ids)
        case let .answerEntryCheck(answer):
            guard let request = context.transient.entryCheck else { return [] }
            switch answer {
            case .edit:
                return [.noted(.answered(edit: true))]
            case .keep:
                return commit(request.row, values: request.values, stop: request.isStop, session: session, now: now, calendar: calendar,
                              state: state, ids: ids)
            case .useCorrected:
                let values = request.warning.corrected(request.values, measurement: request.measurement)
                return commit(request.row, values: values, stop: request.isStop, session: session, now: now, calendar: calendar,
                              state: state, ids: ids)
            }
        case .start, .finishThenStart, .finishFlowShown, .cancelStartAfterFinish, .discardThenStart, .discard, .finish, .foreground,
             .scroll, .dismissEntryCheck, .coverVisible, .reveal:
            return []
        }
    }

    /// A tick or a Stop: the shown values, through the entry check (§5.7).
    private func tick(_ row: RowID, stop: Bool, session: SessionState, presentation: SessionPresentation?, now: Date,
                      calendar: Calendar, state: State, ids: MintedIDs) -> [Event]
    {
        guard let presented = presentation?.row(row), let values = stop ? presented.stopValues : presented.commitValues,
              let measurement = session.exercise(ofRow: row)?.measurement
        else {
            return [.noted(.refuse(.incompleteValues(missing: presentation?.row(row)?.missing ?? [])))]
        }
        if let warning = EntryCheck.warning(for: row, values: values, in: session, history: history, now: now, calendar: calendar) {
            return [.noted(.asked(EntryCheckRequest(row: row, values: values, warning: warning, measurement: measurement, isStop: stop)))]
        }
        return commit(row, values: values, stop: stop, session: session, now: now, calendar: calendar, state: state, ids: ids)
    }

    private func commit(_ row: RowID, values: SetValues, stop: Bool, session: SessionState, now: Date, calendar: Calendar, state: State,
                        ids: MintedIDs) -> [Event]
    {
        let noLighterLoad = SuggestionInputs.noLighterLoad(afterTicking: row, with: values, in: session, history: history, now: now,
                                                           calendar: calendar)
        let at = now.flooredToSecond
        let complete = SessionEvent.CompleteSet(row: row, recordID: SetID(ids.make(at: at)), values: values,
                                                segmentRowIDs: (0 ..< 2).map { _ in RowID(ids.make(at: at)) }, noLighterLoad: noLighterLoad)
        var events: [Event] = [.engine(envelope(stop ? .stopTimedSet(complete) : .completeSet(complete), now: now, after: state.lastAt,
                                                ids: ids))]
        // §7.6: with superset auto-scroll on and a 0 s transition, the Table
        // brings the next member's row into view; Rows follows its open row
        // on its own (§7.6a). Worked out from the state the tick makes.
        if settings.sessionLayout == .table, settings.supersetAutoScroll, case let .engine(applied) = events[0],
           let next = try? SessionEngine.apply(applied, to: session), next.countdown == nil, next.restClock?.slot == .transition
        {
            let upNext = SessionPresenter.present(next, catalog: catalog, history: history, settings: presenterSettings(calendar: calendar),
                                                  now: now).upNext
            if let upNext { events.append(.noted(.scroll(ScrollTarget(row: upNext.row)))) }
        }
        return events
    }

    /// Discard: the engine's event, then the journal goes.
    private func discard(_ state: State, now: Date, ids: MintedIDs, then: StartRequest?) -> [Event] {
        guard let session = state.session, state.finishing == nil else { return then.map { [.starting($0)] } ?? [] }
        return [.discard(envelope(.discard, now: now, after: state.lastAt, ids: ids), session: session.session.id, then: then)]
    }

    /// Save (§6.1, §7.7): `finish`, built from the same projection the sheet
    /// showed for the policy, with the records it makes, which the effect
    /// writes to the history file. A Save the engine would refuse is
    /// refused under its receipt, so the sheet hears of it.
    private func finish(_ request: FinishRequest, state: State, presentation: SessionPresentation?, now: Date, calendar: Calendar,
                        ids: MintedIDs, then: StartRequest?) -> [Event]
    {
        guard var session = state.session, state.finishing == nil, var presented = presentation else {
            return [.finishRefused(request.receipt, .noSession)]
        }
        var events: [Event] = []
        var after = state.lastAt
        let trimmed = request.notes?.trimmingCharacters(in: .whitespacesAndNewlines)
        // `finish` keeps the notes when given none, so clearing them is its own event.
        if trimmed?.isEmpty == true, session.session.notes != nil {
            let cleared = envelope(.setNote(target: .session, text: nil), now: now, after: after, ids: ids)
            let next: SessionState
            do {
                next = try SessionEngine.apply(cleared, to: session)
            } catch {
                return [.finishRefused(request.receipt, error)]
            }
            events.append(.engine(cleared))
            after = cleared.at
            session = next
            presented = SessionPresenter.present(next, catalog: catalog, history: history, settings: presenterSettings(calendar: calendar),
                                                 now: now)
        }
        let ended = request.end.flooredToSecond
        // The sheet's numbers came from this projection: Save sends its `unticked` as it is.
        let projection = FinishPresenter.projection(session, presentation: presented, policy: request.policy, end: ended) { _ in
            SetID(ids.make(at: ended))
        }
        let event = SessionEvent.finish(projection.event(
            start: request.start.flooredToSecond, end: ended, sessionRPE: request.sessionRPE.map { Decimal($0) },
            notes: trimmed.flatMap { $0.isEmpty ? nil : $0 }, utcOffset: calendar.timeZone.secondsFromGMT(for: ended) / 60))
        let finish = envelope(event, now: now, after: after, ids: ids)
        let finished: SessionState
        do {
            finished = try SessionEngine.apply(finish, to: session)
        } catch {
            return events + [.finishRefused(request.receipt, error)]
        }
        let records = SessionEngine.records(finished, adapter: adapter)
        return events + [.finishRequested(finish, FinishedSession(entry: HistoryEntry(records: records), then: then,
                                                                  activity: presented.liveActivity, receipt: request.receipt))]
    }

    /// The event with its id and instant: floored to the second and never
    /// before the previous event's (§4.4 invariant 9).
    func envelope(_ event: SessionEvent, now: Date, after last: Date?, ids: MintedIDs) -> EventEnvelope {
        var at = now.flooredToSecond
        if let last, at < last { at = last }
        return EventEnvelope(id: EventID(ids.make(at: at)), at: at, event: event)
    }

    /// A slot key for an exercise added in the gym: its id, then `-2`, `-3`.
    static func slotKey(for exercise: Exercise, in state: SessionState) -> String {
        let taken = Set(state.slots.map(\.key))
        var key = exercise.id.rawValue
        var suffix = 2
        while taken.contains(key) {
            key = "\(exercise.id.rawValue)-\(suffix)"
            suffix += 1
        }
        return key
    }

    /// The owner's calendar: the environment's time zone and locale, never
    /// the process's (on Android they do not follow the device).
    static func calendar(_ environment: AppEnvironment) -> Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = environment.timeZone
        calendar.locale = environment.locale
        return calendar
    }
}
