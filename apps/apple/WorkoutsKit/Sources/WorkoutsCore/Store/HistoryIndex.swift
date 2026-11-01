import Foundation

/// Finished sessions in memory, indexed by exercise, newest first: what
/// PREVIOUS, the suggestion inputs, the exercise history and the e1RM trend
/// read (§5.2, §5.4, §7.9). Built off the main thread at launch from the
/// history store, then kept current with `insert` and `remove`.
public struct HistoryIndex: Sendable {
    /// Every session, newest first.
    public private(set) var sessions: [SessionRecords] = []
    private var byExercise: [ExerciseID: [ExerciseHistory]] = [:]

    public init(_ sessions: [SessionRecords] = []) {
        self.sessions = sessions
        rebuild()
    }

    public func session(_ id: SessionID) -> SessionRecords? {
        sessions.first { $0.session.id == id }
    }

    /// Each session that has a set of `exercise` with this `setup`, newest
    /// first, with only those sets, in display order. V1 asks for sets with
    /// no setup (nil). `excluding` leaves out the active session.
    public func history(of exercise: ExerciseID, setup: SetupID? = nil, excluding excluded: SessionID? = nil) -> [ExerciseHistory] {
        (byExercise[exercise] ?? []).compactMap { entry in
            guard entry.session.id != excluded else { return nil }
            let sets = entry.sets.filter { $0.setup == setup }
            guard !sets.isEmpty else { return nil }
            return ExerciseHistory(session: entry.session, sets: sets)
        }
    }

    /// The newest session with `exercise` (PREVIOUS's source, §5.4).
    public func last(_ exercise: ExerciseID, setup: SetupID? = nil, excluding excluded: SessionID? = nil) -> ExerciseHistory? {
        history(of: exercise, setup: setup, excluding: excluded).first
    }

    /// When `exercise` was last done, any setup: the Exercises list and the
    /// picker's "Recent".
    public func lastDone(_ exercise: ExerciseID) -> Date? {
        byExercise[exercise]?.first?.session.at
    }

    /// Every exercise with at least one performed set.
    public var exercises: Set<ExerciseID> { Set(byExercise.keys) }

    /// Adds or replaces a session (Finish, a Health link).
    public mutating func insert(_ records: SessionRecords) {
        sessions.removeAll { $0.session.id == records.session.id }
        sessions.append(records)
        rebuild()
    }

    public mutating func remove(_ id: SessionID) {
        sessions.removeAll { $0.session.id == id }
        rebuild()
    }

    private mutating func rebuild() {
        sessions.sort { a, b in
            a.session.at != b.session.at ? a.session.at > b.session.at : a.session.id > b.session.id
        }
        var index: [ExerciseID: [ExerciseHistory]] = [:]
        for records in sessions {
            let slotOrder = Dictionary(records.session.slots.enumerated().map { ($1.key, $0) }, uniquingKeysWith: { first, _ in first })
            let ordered = records.sets.sorted { a, b in
                let (slotA, slotB) = (slotOrder[a.slot] ?? Int.max, slotOrder[b.slot] ?? Int.max)
                if slotA != slotB { return slotA < slotB }
                if a.slot != b.slot { return a.slot < b.slot }
                return a.order < b.order
            }
            var perExercise: [ExerciseID: [PerformedSet]] = [:]
            var seen: [ExerciseID] = []
            for set in ordered {
                if perExercise[set.exercise] == nil { seen.append(set.exercise) }
                perExercise[set.exercise, default: []].append(set)
            }
            for exercise in seen {
                index[exercise, default: []].append(ExerciseHistory(session: records.session, sets: perExercise[exercise]!))
            }
        }
        byExercise = index
    }
}

/// One session's sets of one exercise.
public struct ExerciseHistory: Hashable, Sendable {
    public var session: TrainingSession
    /// In display order: by the session's slot order, then `order`.
    public var sets: [PerformedSet]

    public init(session: TrainingSession, sets: [PerformedSet]) {
        self.session = session
        self.sets = sets
    }

    /// The slots that hold the exercise, in display order. When today has two
    /// slots of one exercise, the n-th matches last time's n-th (§5.4).
    public var slots: [String] {
        var seen: [String] = []
        for set in sets where !seen.contains(set.slot) {
            seen.append(set.slot)
        }
        return seen
    }

    public func sets(inSlot slot: String) -> [PerformedSet] {
        sets.filter { $0.slot == slot }
    }
}
