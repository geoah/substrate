import Foundation

/// The Finish dialog and sheet (§7.7).
public struct FinishSummary: Equatable, Sendable {
    /// The routine's name or "Training session".
    public var title: String
    /// The proposed start and end, clamped as the engine will (§4.3).
    public var start: Date
    public var end: Date
    /// The start's picker goes no later than the first performed set began.
    public var latestStart: Date?
    /// The end's picker goes no earlier than the last tick.
    public var earliestEnd: Date?
    /// From start to end, pauses excluded.
    public var duration: Seconds
    /// `47:12`.
    public var durationText: String
    /// The labelled summary rows, one per line: Working sets, Volume,
    /// Exercises. They describe what Save writes under the chosen policy
    /// (`projection`), never only what is ticked so far.
    public var rows: [LabelledValue]
    /// Pending rows; when any, the dialog asks first.
    public var untickedCount: Int
    /// `3 sets are not ticked`; nil when every row is ticked.
    public var untickedTitle: String?
    /// The dialog's message when "Mark them done as shown" cannot complete
    /// some rows: `2 have a value missing and are left out either way.`; nil
    /// when it can complete them all.
    public var untickedMessage: String?
    /// What the chosen policy does with the unticked rows, a line under the
    /// rows: `11 unticked sets are saved as shown; 2 are left out, a value is
    /// missing.` or `3 unticked sets are left out.`; nil when none are unticked.
    public var pendingText: String?
    /// Where it will be saved: `Saved to this iPhone and to Apple Health.`
    public var savedLine: String
    /// What Save writes for the policy the summary was made for.
    public var projection: FinishProjection

    public init(title: String, start: Date, end: Date, latestStart: Date?, earliestEnd: Date?, duration: Seconds,
                durationText: String, rows: [LabelledValue], untickedCount: Int, untickedTitle: String?,
                untickedMessage: String?, pendingText: String?, savedLine: String, projection: FinishProjection)
    {
        self.title = title
        self.start = start
        self.end = end
        self.latestStart = latestStart
        self.earliestEnd = earliestEnd
        self.duration = duration
        self.durationText = durationText
        self.rows = rows
        self.untickedCount = untickedCount
        self.untickedTitle = untickedTitle
        self.untickedMessage = untickedMessage
        self.pendingText = pendingText
        self.savedLine = savedLine
        self.projection = projection
    }

    /// The dialog's choices, in order (no second "are you sure", 06 §2.8).
    public static let dropTitle = "Drop them"
    public static let markDoneTitle = "Mark them done as shown"
    public static let cancelTitle = "Cancel"
}

/// A label and its value, stacked or on one line, never in fixed columns.
public struct LabelledValue: Equatable, Sendable, Identifiable {
    public var label: String
    public var value: String

    public init(_ label: String, _ value: String) {
        self.label = label
        self.value = value
    }

    public var id: String { label }
}

/// What Finish does with the rows nobody ticked: the dialog's two choices (§7.7).
public enum PendingSetPolicy: String, Codable, Equatable, Sendable, CaseIterable {
    /// "Drop them": only ticked rows are saved.
    case drop
    /// "Mark them done as shown": each unticked row whose required values
    /// are all shown (typed or ghost) is saved as shown; the rest are dropped.
    case markDone
}

/// What Save writes for one pending-set policy (§7.7), worked out by running
/// the engine's own `finish` on a copy of the state. The Finish sheet shows
/// these numbers and Save sends `unticked` unchanged (through `event`), so
/// the summary and the saved record cannot disagree.
///
/// Sets here are rows, as the grid shows them and as "3 sets are not ticked"
/// counts them: a drop's later link and a second side are a set each.
/// `workingSets` alone counts as History does, a chain or a side pair once.
public struct FinishProjection: Equatable, Sendable {
    public var policy: PendingSetPolicy
    /// The Finish event's `unticked` for this policy: send it as it is.
    public var unticked: UntickedFate
    /// Rows nobody ticked, before Finish.
    public var pendingSets: Int
    /// Set records Save writes: the ticked rows and the rows it marks done.
    public var savedSets: Int
    /// Rows the policy completes at Finish: 0 for `drop`.
    public var markedSets: Int
    /// Unticked rows "Mark them done as shown" cannot complete, under either
    /// policy: a required value has neither a typed nor a shown value, or an
    /// earlier link of its chain has none.
    public var incompleteSets: Int
    /// Working sets saved, a chain or a side pair once: History's "24 sets".
    public var workingSets: Int
    /// The volume of the saved sets, as the header and History count it (§4.11).
    public var volume: Mass
    /// Exercises with at least one saved set; the others go at Finish.
    public var exercises: Int
    /// Exercises in the session now: the "21" of "9 of 21".
    public var exercisesInSession: Int

    public init(policy: PendingSetPolicy, unticked: UntickedFate, pendingSets: Int, savedSets: Int, markedSets: Int,
                incompleteSets: Int, workingSets: Int, volume: Mass, exercises: Int, exercisesInSession: Int)
    {
        self.policy = policy
        self.unticked = unticked
        self.pendingSets = pendingSets
        self.savedSets = savedSets
        self.markedSets = markedSets
        self.incompleteSets = incompleteSets
        self.workingSets = workingSets
        self.volume = volume
        self.exercises = exercises
        self.exercisesInSession = exercisesInSession
    }

    /// Unticked rows Save leaves out.
    public var droppedSets: Int { pendingSets - markedSets }

    /// The Finish event Save sends: this projection's `unticked` with the
    /// sheet's start, end, session RPE and notes.
    public func event(start: Date, end: Date, sessionRPE: Decimal? = nil, notes: String? = nil,
                      utcOffset: Int) -> SessionEvent.Finish
    {
        SessionEvent.Finish(start: start, end: end, unticked: unticked, sessionRPE: sessionRPE, notes: notes,
                            utcOffset: utcOffset)
    }
}

public enum FinishPresenter {
    /// The sheet for finishing `state` from `start` (default: the session's
    /// start) to `end`, with the unticked rows treated as `policy` says.
    /// - Parameters:
    ///   - presentation: the session's current presentation: the values
    ///     "Mark them done as shown" commits are the ones its rows show.
    ///   - healthAllowed: whether Apple Health sharing is allowed; the last
    ///     line says where the session goes.
    public static func summary(_ state: SessionState, presentation: SessionPresentation, policy: PendingSetPolicy,
                               start: Date? = nil, end: Date, healthAllowed: Bool) -> FinishSummary
    {
        // The ids only stand in: counts never read them, and Save mints its own.
        let projected = projection(state, presentation: presentation, policy: policy, end: end) {
            SetID("projected-\($0.rawValue)")
        }
        return summary(state, projection: projected, start: start, end: end, healthAllowed: healthAllowed)
    }

    /// What Save writes for `policy` (§7.7). Save must build its Finish event
    /// from the result (`projection.event(...)`), so the sheet's numbers and
    /// the saved record come from one place.
    /// - Parameter recordID: the record id for each row marked done; Save
    ///   mints UUIDv7s of the end, a preview anything unique.
    public static func projection(_ state: SessionState, presentation: SessionPresentation, policy: PendingSetPolicy,
                                  end: Date, recordID: (RowID) -> SetID) -> FinishProjection
    {
        let markAll = markDone(presentation, recordID: recordID)
        return projection(state, unticked: policy == .drop ? .drop : markAll, markAll: markAll, policy: policy, end: end)
    }

    /// The Finish event's `markDone` list from a presentation: each pending
    /// row with complete shown values, and the record id the controller mints.
    public static func markDone(_ presentation: SessionPresentation, recordID: (RowID) -> SetID) -> UntickedFate {
        .markDone(presentation.unticked.compactMap { row in
            row.values.map { UntickedFate.MarkDone(row: row.row, recordID: recordID(row.row), values: $0) }
        })
    }

    // MARK: - Building

    static func projection(_ state: SessionState, unticked: UntickedFate, markAll: UntickedFate, policy: PendingSetPolicy,
                           end: Date) -> FinishProjection
    {
        let pending = state.rows.values.filter(\.isPending).map(\.id)
        let finished = finishing(state, unticked: unticked, end: end)
        // What marking could complete, whichever policy is chosen.
        let marked = unticked == markAll ? finished : finishing(state, unticked: markAll, end: end)
        let completable = pending.count { marked.rows[$0]?.isCompleted == true }
        let sets = SessionEngine.setRecords(finished)
        return FinishProjection(
            policy: policy,
            unticked: unticked,
            pendingSets: pending.count,
            savedSets: sets.count,
            markedSets: pending.count { finished.rows[$0]?.isCompleted == true },
            incompleteSets: pending.count - completable,
            workingSets: Counting.workingSets(sets),
            volume: Counting.volume(sets, exercises: finished.exercises, bodyMass: finished.session.bodyMass,
                                    unit: finished.settings.defaultUnit,
                                    includeWarmups: finished.settings.countWarmupsInVolume),
            exercises: Set(sets.map(\.slot)).count,
            exercisesInSession: state.slots.count
        )
    }

    /// The state the engine's `finish` leaves, applied to a copy. A timed set
    /// still running (Finish stops it first, §7.7) is set aside on the copy,
    /// so the projection reads as if it had been cancelled.
    static func finishing(_ state: SessionState, unticked: UntickedFate, end: Date) -> SessionState {
        var copy = state
        if let timed = copy.timedRow {
            copy.rows[timed]?.timerStartedAt = nil
            copy.countdown = nil
        }
        guard copy.phase == .active else { return copy }
        var reduction = Reduction(state: copy, at: end, eventID: EventID("finish-projection"))
        let finish = SessionEvent.Finish(start: copy.session.at, end: end, unticked: unticked, utcOffset: 0)
        do {
            try reduction.finish(finish)
        } catch {
            return copy
        }
        return reduction.state
    }

    static func summary(_ state: SessionState, projection: FinishProjection, start: Date?, end: Date,
                        healthAllowed: Bool) -> FinishSummary
    {
        let performed = state.rows.values.compactMap(\.performed)
        let latestStart = performed.map { $0.startedAt ?? $0.completedAt }.min()
        let earliestEnd = performed.map(\.completedAt).max()
        let end = max(end, earliestEnd ?? end)
        var start = start ?? state.session.at
        if let latestStart { start = min(start, latestStart) }
        start = min(start, end)
        let duration = SessionPresenter.activeSeconds(from: start, to: end, pauses: state.session.pauses)
        let unticked = projection.pendingSets
        return FinishSummary(
            title: state.plan?.name ?? "Training session",
            start: start,
            end: end,
            latestStart: latestStart,
            earliestEnd: earliestEnd,
            duration: duration,
            durationText: Format.duration(duration),
            rows: [
                LabelledValue("Working sets", "\(projection.workingSets)"),
                LabelledValue("Volume", Format.volume(projection.volume)),
                LabelledValue("Exercises", "\(projection.exercises) of \(projection.exercisesInSession)"),
            ],
            untickedCount: unticked,
            untickedTitle: unticked == 0 ? nil : "\(Format.count(unticked, "set")) \(unticked == 1 ? "is" : "are") not ticked",
            untickedMessage: untickedMessage(projection),
            pendingText: pendingText(projection),
            savedLine: healthAllowed
                ? "Saved to this iPhone and to Apple Health."
                : "Apple Health access is off; saved to this iPhone.",
            projection: projection
        )
    }

    /// `2 have a value missing and are left out either way.`
    static func untickedMessage(_ projection: FinishProjection) -> String? {
        let incomplete = projection.incompleteSets
        guard incomplete > 0 else { return nil }
        if incomplete == projection.pendingSets {
            return incomplete == 1
                ? "It has a value missing and is left out either way."
                : "They have a value missing and are left out either way."
        }
        return "\(incomplete) \(incomplete == 1 ? "has" : "have") a value missing and \(incomplete == 1 ? "is" : "are") left out either way."
    }

    /// `11 unticked sets are saved as shown; 2 are left out, a value is missing.`
    static func pendingText(_ projection: FinishProjection) -> String? {
        let pending = projection.pendingSets
        guard pending > 0 else { return nil }
        let dropped = projection.droppedSets
        switch projection.policy {
        case .drop:
            return "\(Format.count(pending, "unticked set")) \(pending == 1 ? "is" : "are") left out."
        case .markDone:
            let marked = projection.markedSets
            guard marked > 0 else {
                return "\(Format.count(pending, "unticked set")) \(pending == 1 ? "is" : "are") left out, a value is missing."
            }
            let saved = "\(Format.count(marked, "unticked set")) \(marked == 1 ? "is" : "are") saved as shown"
            guard dropped > 0 else { return saved + "." }
            return saved + "; \(dropped) \(dropped == 1 ? "is" : "are") left out, a value is missing."
        }
    }
}
