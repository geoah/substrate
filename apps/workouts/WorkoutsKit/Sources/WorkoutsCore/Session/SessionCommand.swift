import Foundation

/// What a view, a Live Activity intent or a notification action asks of the
/// session. A command is an engine event minus what the controller adds: ids
/// (UUIDv7), the instant, and values that need history (the values a tick
/// commits, an added exercise's rows from last time).
public enum SessionCommand: Codable, Equatable, Sendable {
    /// A field focused, or a row's RPE or set-kind control opened (§4.8).
    case touchRow(RowID)
    case edit(RowID, SetValuesPatch)
    case setRowNote(RowID, String?)
    /// PREVIOUS tapped: last time's values, a rated RPE as `copied` (§5.4).
    case copyPrevious(RowID)
    /// The tick: commits what the row shows, after the entry check (§5.7).
    case tick(RowID)
    case untick(RowID)
    case addSet(slot: String, role: Role)
    case removeSet(RowID)
    /// Delete set with an Undo (§7.6a): removed and journaled at once, and
    /// what puts it back offered as `undo`.
    case removeUndoably(RowID)
    /// The Undo after Delete set: the set back with its values, at its place.
    case restoreSet(SessionEvent.RestoreSet)
    case changeSetKind(RowID, SetKind)
    case setRowRest(RowID, Seconds?)
    /// `after`: the slot whose exercise menu opened the picker; nil appends.
    case addExercise(Exercise, after: String?)
    case removeExercise(slot: String)
    case reorderExercises([String])
    case replaceExercise(slot: String, with: Exercise)
    case groupExercises(slot: String, partner: String)
    case ungroupExercise(slot: String)
    /// "Ungroup all" in a group's menu.
    case ungroupAll(group: String)
    case setGroup(SessionEvent.SetGroup)
    case setSlotRest(slot: String, kind: SlotRestKind, seconds: Seconds?)
    case setNote(NoteTarget, String?)
    case pauseClock
    case resumeClock
    /// The toolbar's standalone timer.
    case startTimer(Seconds)
    case adjustRest(Seconds)
    case skipRest
    case startTimedSet(RowID)
    case stopTimedSet(RowID)
    case cancelTimedSet(RowID)
    /// The Live Activity's Done: ticks the row only while it is still pending
    /// and still Up next, with the values it shows (§9.1).
    case completeUpNext(RowID)
    /// The Live Activity's Stop: commits the timed set as shown, since no
    /// dialog can ask from the Lock Screen.
    case stopFromLockScreen(RowID)
    /// The entry check's answer.
    case answerEntryCheck(EntryAnswer)
    /// The question was dismissed without an answer.
    case dismissEntryCheck
    /// Bring a row or a card into view; nil once the list has.
    case scroll(ScrollTarget?)

    /// Starts a session; the caller has already asked about an open one (§7.4).
    case start(StartRequest)
    /// The start conflict's "Finish it and start this one" (§7.4): the
    /// cover and its Finish flow; `request` starts after Save.
    case finishThenStart(StartRequest)
    /// The cover ran the Finish flow it was asked for.
    case finishFlowShown
    /// The Finish flow closed without Save: nothing starts after it.
    case cancelStartAfterFinish
    /// The start conflict's "Discard it and start this one", after its second question.
    case discardThenStart(StartRequest)
    /// Save in the Finish sheet (§6.1, §7.7).
    case finish(FinishRequest)
    /// Discard (§6.1): nothing is recorded.
    case discard
    /// The app came to the foreground (§6.3).
    case foreground
}

/// The entry check's answer.
public enum EntryAnswer: String, Codable, Equatable, Sendable {
    case useCorrected, keep, edit
}

/// What Save sends: the sheet's start, end, how the unticked rows go, the
/// session RPE and the notes.
public struct FinishRequest: Codable, Equatable, Sendable {
    public var start: Date
    public var end: Date
    public var policy: PendingSetPolicy
    public var sessionRPE: Int?
    public var notes: String?

    public init(start: Date, end: Date, policy: PendingSetPolicy, sessionRPE: Int?, notes: String?) {
        self.start = start
        self.end = end
        self.policy = policy
        self.sessionRPE = sessionRPE
        self.notes = notes
    }
}
