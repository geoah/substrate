import Foundation

// What the session cover, the in-progress accessory, the Live Activity and the
// countdown notification show, as plain values (§5.6, §7.5, §7.6, §9.1, §9.2).
// `SessionPresenter.present` builds one after every change; the views render
// it and compute no rule of their own.

/// What the presenter needs beyond the session: the owner's calendar, and for
/// screens that are not a session the settings a session would have snapshot.
public struct PresenterSettings: Equatable, Sendable {
    /// The owner's calendar: its time zone for local times, whole days to past sessions.
    public var calendar: Calendar
    /// The unit totals are shown in outside a session (a session uses its own snapshot).
    public var defaultUnit: MassUnit
    /// "Count warm-ups in volume", outside a session.
    public var countWarmupsInVolume: Bool
    /// The session screen shows the plan's RPE pre-filled (the Rows screen,
    /// §4.14): a tick, Done and "Mark them done as shown" commit a row's
    /// pre-fill as `prefilled` when it holds no RPE of its own
    /// (`SetRowPresentation.commitValues`, `UntickedRow.values`).
    public var prefillsRPE: Bool

    public init(calendar: Calendar = .current, defaultUnit: MassUnit = .kg, countWarmupsInVolume: Bool = false,
                prefillsRPE: Bool = false)
    {
        self.calendar = calendar
        self.defaultUnit = defaultUnit
        self.countWarmupsInVolume = countWarmupsInVolume
        self.prefillsRPE = prefillsRPE
    }

    public var timeZone: TimeZone { calendar.timeZone }
}

/// Everything a session's screens show.
public struct SessionPresentation: Equatable, Sendable {
    public var sessionID: SessionID
    /// The routine's name, or "Training session".
    public var title: String
    public var isFinished: Bool
    public var header: SessionHeaderStats
    /// Display order: a plain exercise is a block of one card; a group is one
    /// block with its header and member cards.
    public var blocks: [ExerciseBlock]
    /// The row the session points at (§4.6); nil when every set is done.
    public var upNext: UpNextPresentation?
    public var restBar: RestBarPresentation
    public var liveActivity: LiveActivityContent
    /// The countdown's alert, when it notifies (§4.7, §9.2); the coordinator
    /// schedules it only when `fireDate` is in the future.
    public var notification: CountdownNotification?
    public var accessory: AccessoryPresentation
    /// Pending rows in display order, with what "Mark them done as shown"
    /// commits (§7.7); nil values are dropped.
    public var unticked: [UntickedRow]
    /// Completed sets of any role, a chain or a side pair once: "11 done sets".
    public var completedSets: Int
    /// The conflict dialog's message (§7.4): "Fully Body Collection #1, started 13:42, 11 sets done."
    public var summary: String
    /// The row whose timed set runs.
    public var timedRow: RowID?
    /// The next instant the time-dependent parts change (a countdown ends,
    /// "Rest over" gives way to the Up next chip): present again then.
    public var nextChange: Date?
    /// The same session as the Rows layout shows it (§7.6 Rows): each block's
    /// items in the order they are done, the open row's editor and the
    /// floating capsule. The Table layout reads `blocks` and `restBar`; both
    /// send the same commands.
    public var rowsLayout: RowsLayout

    public init(sessionID: SessionID, title: String, isFinished: Bool, header: SessionHeaderStats, blocks: [ExerciseBlock],
                upNext: UpNextPresentation?, restBar: RestBarPresentation, liveActivity: LiveActivityContent,
                notification: CountdownNotification?, accessory: AccessoryPresentation, unticked: [UntickedRow],
                completedSets: Int, summary: String, timedRow: RowID?, nextChange: Date?, rowsLayout: RowsLayout = .empty)
    {
        self.sessionID = sessionID
        self.title = title
        self.isFinished = isFinished
        self.header = header
        self.blocks = blocks
        self.upNext = upNext
        self.restBar = restBar
        self.liveActivity = liveActivity
        self.notification = notification
        self.accessory = accessory
        self.unticked = unticked
        self.completedSets = completedSets
        self.summary = summary
        self.timedRow = timedRow
        self.nextChange = nextChange
        self.rowsLayout = rowsLayout
    }

    /// Every card in display order (the jump menu, scrolling).
    public var cards: [ExerciseCard] { blocks.flatMap(\.cards) }

    public func card(_ slot: String) -> ExerciseCard? {
        cards.first { $0.slot == slot }
    }

    public func row(_ id: RowID) -> SetRowPresentation? {
        for card in cards {
            if let row = card.rows.first(where: { $0.id == id }) { return row }
        }
        return nil
    }

    /// All sets are done: the bar offers Finish. A session with no rows is
    /// not done; it has not begun.
    public var allDone: Bool { upNext == nil && timedRow == nil && cards.contains { !$0.rows.isEmpty } }

    /// The `…` menu's Discard question (§7.6): `Discard this session? 11 done sets will be deleted.`
    public var discardQuestion: String {
        guard completedSets > 0 else { return "Discard this session?" }
        let sets = Format.count(completedSets, "done set")
        return "Discard this session? \(sets) will be deleted."
    }

    /// The Start conflict's second question (§7.4): `Discard 11 sets?`
    public var discardBeforeStartQuestion: String {
        guard completedSets > 0 else { return "Discard the open session?" }
        return "Discard \(Format.count(completedSets, "set"))?"
    }

    /// The Start conflict dialog's title (§7.4); `summary` is its message.
    public static let conflictTitle = "A training session is in progress"
}

/// The header's three labelled stats (§7.6).
public struct SessionHeaderStats: Equatable, Sendable {
    /// The stats' labels, in order. The sets stat counts working sets only,
    /// so its label says so: warm-ups ticked do not move it.
    public static let durationTitle = "Duration"
    public static let volumeTitle = "Volume"
    public static let setsTitle = "Working sets"

    /// The session start plus every closed pause: `Text(timerInterval:)` from
    /// here shows the clock with no app code running.
    public var clockStart: Date
    /// While paused, where the clock stopped.
    public var clockPausedAt: Date?
    /// Seconds on the clock at `now`, pauses excluded.
    public var elapsed: Seconds
    /// `14:02`, or `Paused` while the clock is paused.
    public var durationText: String
    public var isPaused: Bool
    public var volume: Mass
    /// `375 kg`.
    public var volumeText: String
    /// Working sets done and planned, a chain or a side pair once; warm-ups never.
    public var setsDone: Int
    public var setsPlanned: Int
    /// `3 of 40`, under `setsTitle`.
    public var setsText: String

    public init(clockStart: Date, clockPausedAt: Date?, elapsed: Seconds, durationText: String, isPaused: Bool, volume: Mass,
                volumeText: String, setsDone: Int, setsPlanned: Int, setsText: String)
    {
        self.clockStart = clockStart
        self.clockPausedAt = clockPausedAt
        self.elapsed = elapsed
        self.durationText = durationText
        self.isPaused = isPaused
        self.volume = volume
        self.volumeText = volumeText
        self.setsDone = setsDone
        self.setsPlanned = setsPlanned
        self.setsText = setsText
    }
}

/// A plain exercise, or a group with its members.
public struct ExerciseBlock: Equatable, Sendable, Identifiable {
    /// The slot's key, or `group-<key>`.
    public var id: String
    public var group: GroupHeader?
    public var cards: [ExerciseCard]

    public init(id: String, group: GroupHeader?, cards: [ExerciseCard]) {
        self.id = id
        self.group = group
        self.cards = cards
    }
}

/// A group's header row (§7.6).
public struct GroupHeader: Equatable, Sendable {
    public var key: String
    public var kind: GroupKind
    public var pairing: Pairing?
    /// The rounds the members' working sets make.
    public var rounds: Int
    public var roundRest: Seconds
    /// `Superset A: 2 rounds, 1:00 after each round`.
    public var title: String
    /// The group's colour, 0 to `GroupHeader.paletteSize − 1`, in display order.
    public var colorIndex: Int
    /// Member slot keys, in order.
    public var members: [String]

    /// The fixed palette of four accessible colours the groups take in turn.
    public static let paletteSize = 4

    public init(key: String, kind: GroupKind, pairing: Pairing?, rounds: Int, roundRest: Seconds, title: String,
                colorIndex: Int, members: [String])
    {
        self.key = key
        self.kind = kind
        self.pairing = pairing
        self.rounds = rounds
        self.roundRest = roundRest
        self.title = title
        self.colorIndex = colorIndex
        self.members = members
    }
}

/// One exercise card (§7.6).
public struct ExerciseCard: Equatable, Sendable, Identifiable {
    public var slot: String
    public var exercise: ExerciseID
    public var name: String
    public var measurement: MeasurementType
    /// The exercise's unit: the load column's header and the keyboard steps.
    public var unit: MassUnit
    /// Added on this phone ("On this iPhone").
    public var isLocal: Bool
    public var group: String?
    /// `A1`.
    public var groupLabel: String?
    public var colorIndex: Int?
    /// `Rest 1:30`; a member's `Then 0:00 to A2`; the last member's `Round rest 1:00`.
    public var restChip: String
    /// The rests the "Rest…" menu offers for this slot.
    public var rests: [SlotRestOption]
    /// The note written in the gym.
    public var notes: String?
    /// The routine slot's own notes.
    public var planNotes: String?
    public var columns: [GridColumn]
    /// Each column's header, in `columns` order.
    public var columnTitles: [String]
    public var rows: [SetRowPresentation]
    /// The keyboard's −/+ step for loads (the exercise's grid); nil with no grid.
    public var loadStep: Mass?
    /// The keyboard's −/+ step for times.
    public var timeStep: Seconds
    public var workingDone: Int
    public var workingPlanned: Int
    /// `2 of 3 done`, for the jump menu.
    public var progress: String
    /// Completed sets of any role: "Remove Chest Press (Machine) and its 3 done sets?"
    public var completedSets: Int
    public var hasWarmups: Bool

    public var id: String { slot }

    /// Remove exercise asks only when sets are done (§7.6):
    /// `Remove Chest Press (Machine) and its 3 done sets?`; nil when none are.
    public var removeQuestion: String? {
        guard completedSets > 0 else { return nil }
        return "Remove \(name) and its \(Format.count(completedSets, "done set"))?"
    }

    public init(slot: String, exercise: ExerciseID, name: String, measurement: MeasurementType, unit: MassUnit, isLocal: Bool,
                group: String?, groupLabel: String?, colorIndex: Int?, restChip: String, rests: [SlotRestOption],
                notes: String?, planNotes: String?, columns: [GridColumn], columnTitles: [String], rows: [SetRowPresentation],
                loadStep: Mass?, timeStep: Seconds, workingDone: Int, workingPlanned: Int, progress: String,
                completedSets: Int, hasWarmups: Bool)
    {
        self.slot = slot
        self.exercise = exercise
        self.name = name
        self.measurement = measurement
        self.unit = unit
        self.isLocal = isLocal
        self.group = group
        self.groupLabel = groupLabel
        self.colorIndex = colorIndex
        self.restChip = restChip
        self.rests = rests
        self.notes = notes
        self.planNotes = planNotes
        self.columns = columns
        self.columnTitles = columnTitles
        self.rows = rows
        self.loadStep = loadStep
        self.timeStep = timeStep
        self.workingDone = workingDone
        self.workingPlanned = workingPlanned
        self.progress = progress
        self.completedSets = completedSets
        self.hasWarmups = hasWarmups
    }
}

/// One of a slot's rests in the "Rest…" menu and the rest picker (§7.6).
public struct SlotRestOption: Equatable, Sendable {
    public var kind: SlotRestKind
    public var seconds: Seconds
    /// What the rest goes back to when the gym's value is cleared.
    public var defaultSeconds: Seconds
    /// The owner set it in this session.
    public var isSetInGym: Bool
    /// `1:30`, or `Off`.
    public var text: String
    /// `Default (2:00)`, the picker's item.
    public var defaultText: String

    public init(kind: SlotRestKind, seconds: Seconds, defaultSeconds: Seconds, isSetInGym: Bool, text: String,
                defaultText: String)
    {
        self.kind = kind
        self.seconds = seconds
        self.defaultSeconds = defaultSeconds
        self.isSetInGym = isSetInGym
        self.text = text
        self.defaultText = defaultText
    }

    /// The rest picker's presets (§7.6), then "Default (…)" and "Custom…".
    public static let presets: [Seconds] = [0, 30, 60, 90, 120, 150, 180]
}

/// A row's state in the grid (§7.6).
public enum RowState: String, Equatable, Sendable {
    case pending
    /// The row the session points at: a leading accent bar.
    case upNext
    /// A segment whose earlier link is pending: its fields are disabled.
    case locked
    case done
    /// Its timed set runs.
    case timing
}

/// One row of the set grid.
public struct SetRowPresentation: Equatable, Sendable, Identifiable {
    public var id: RowID
    public var slot: String
    /// `W`, `2`, `2a`, `M`, `1L`, with the spoken form, the F badge and Top or Back-off.
    public var label: SetLabel
    public var state: RowState
    /// The set-kind menu's current choice; nil on a segment.
    public var kind: SetKind?
    /// The menu's items §4.3 allows for the row; the rest are disabled.
    public var allowedKinds: [SetKind]
    public var previous: PreviousSet?
    /// The middle columns' cells, in the card's column order.
    public var cells: [GridCell]
    /// The RPE typed or logged, on any row (a timed or distance set too); never ghosted.
    public var rpe: Decimal?
    /// `9.5`.
    public var rpeText: String?
    /// The RPE cell as the Rows screen shows it (§4.14): nothing, the plan's
    /// pre-fill on a pending row (shown apart from a value the owner gave),
    /// or a value with its method.
    public var rpeEntry: RowRPE
    /// `8`: the pre-fill's text, when `rpeEntry` is a pre-fill.
    public var rpePrefillText: String?
    /// `RPE 8`: a small label under a row whose grid has no RPE column (a
    /// timed or distance set) once an RPE is typed or logged; nil otherwise.
    public var effortText: String?
    /// The row's menu offers "Rate effort" (`rateEffortTitle`), which opens
    /// the RPE sheet: rows whose grid has no RPE column, unless locked.
    public var canRateEffort: Bool
    /// The RPE control's VoiceOver label: `Plank, set 1, RPE`.
    public var rpeAccessibilityLabel: String
    /// A row with a timer: its play button, countdown or stopwatch.
    public var timer: TimedSetState?
    /// The suggestion for a pending row (its text is the ghost's popover).
    public var suggestion: Suggestion?
    /// The reason line under the Up next row only (§5.6).
    public var reason: String?
    /// After a chain's last mini-set: "Stop: mini-set 4 got 3, first was 4".
    public var stopText: String?
    /// What a tick or the Live Activity's Done commits (§5.5); nil when a
    /// required value has neither a typed nor a ghost value. With
    /// `PresenterSettings.prefillsRPE`, a row with no RPE of its own commits
    /// its pre-fill as `prefilled`.
    public var commitValues: SetValues?
    /// What Stop sends for a running timed set: the typed duration only, so
    /// the engine fills the time run.
    public var stopValues: SetValues?
    /// Required values with nothing to commit: their fields are outlined.
    public var missing: [ValueField]
    /// The tick is enabled.
    public var canTick: Bool
    /// "Rest after this set…": the per-set override.
    public var restOverride: Seconds?
    public var notes: String?
    public var isSegment: Bool

    public init(id: RowID, slot: String, label: SetLabel, state: RowState, kind: SetKind?, allowedKinds: [SetKind],
                previous: PreviousSet?, cells: [GridCell], rpe: Decimal?, rpeText: String?, rpeEntry: RowRPE = .none,
                effortText: String? = nil, canRateEffort: Bool = false, rpeAccessibilityLabel: String = "RPE",
                timer: TimedSetState?,
                suggestion: Suggestion?, reason: String?, stopText: String?, commitValues: SetValues?, stopValues: SetValues?,
                missing: [ValueField], canTick: Bool, restOverride: Seconds?, notes: String?, isSegment: Bool)
    {
        self.id = id
        self.slot = slot
        self.label = label
        self.state = state
        self.kind = kind
        self.allowedKinds = allowedKinds
        self.previous = previous
        self.cells = cells
        self.rpe = rpe
        self.rpeText = rpeText
        self.rpeEntry = rpeEntry
        if case let .prefill(value) = rpeEntry { rpePrefillText = Format.rpe(value) }
        self.effortText = effortText
        self.canRateEffort = canRateEffort
        self.rpeAccessibilityLabel = rpeAccessibilityLabel
        self.timer = timer
        self.suggestion = suggestion
        self.reason = reason
        self.stopText = stopText
        self.commitValues = commitValues
        self.stopValues = stopValues
        self.missing = missing
        self.canTick = canTick
        self.restOverride = restOverride
        self.notes = notes
        self.isSegment = isSegment
    }

    public var isDone: Bool { state == .done }

    public func cell(_ column: GridColumn) -> GridCell? {
        cells.first { $0.column == column }
    }

    /// The row menu's item that opens the RPE sheet on a row with no RPE column.
    public static let rateEffortTitle = "Rate effort"

    /// The RPE control's VoiceOver value: `9.5`, or `not rated`.
    public var rpeAccessibilityValue: String { rpeText ?? "not rated" }

    /// What the Table's RPE sheet sends for Done on `value`, or for Clear
    /// (nil), by §4.14: Done is a rating (`enteringRPE`) whatever the row
    /// held, so a copied or pre-filled value the sheet opened on becomes the
    /// owner's when they confirm it; nothing when the row already holds that
    /// value as `entered`. Clear sends `clearingRPE` only when the row holds
    /// a value. Equality never decides the method.
    public func rpePatch(for value: Decimal?) -> SetValuesPatch? {
        guard let value else {
            if case .value = rpeEntry { return .clearingRPE }
            return nil
        }
        return rpeEntry == .value(value, .entered) ? nil : .enteringRPE(value)
    }
}

/// One value cell (§5.5): a typed or performed value in black, else the ghost in grey.
public struct GridCell: Equatable, Sendable {
    public var column: GridColumn
    public var field: ValueField
    /// The text shown: `40`, `11`, `1:07`, `≈15`, `to failure`; nil when empty.
    /// A load in another unit than the column's carries its unit: `83.91 kg`.
    public var text: String?
    /// Grey: a ghost value, not typed.
    public var isGhost: Bool
    /// A `rirstop` hint (`≈15`) or a placeholder: shown, never committed.
    public var isHint: Bool
    /// Required, with nothing to commit: outlined.
    public var isMissing: Bool
    /// The typed value as the field edits it (`40`, `1:07`); nil when nothing is typed.
    public var entered: String?
    /// The field's VoiceOver label: the exercise, the set and the field with
    /// its unit, `Leg Press (Machine), set 1, load in kilograms`.
    public var accessibilityLabel: String

    public init(column: GridColumn, field: ValueField, text: String?, isGhost: Bool, isHint: Bool, isMissing: Bool,
                entered: String?, accessibilityLabel: String = "")
    {
        self.column = column
        self.field = field
        self.text = text
        self.isGhost = isGhost
        self.isHint = isHint
        self.isMissing = isMissing
        self.entered = entered
        self.accessibilityLabel = accessibilityLabel
    }

    /// The field's VoiceOver value: `draft` while one is being typed, else
    /// the typed or done value, else the ghost as `suggested 40`, a hint as
    /// `≈15, a hint`, and with nothing to show `required, empty` or `empty`.
    /// Pass `draft` only when the field has one: an empty draft is a value
    /// cleared while typing and reads as the empty field shows, the ghost or
    /// `empty`, never the value it replaced.
    public func accessibilityValue(draft: String? = nil) -> String {
        if let draft {
            if !draft.isEmpty { return draft }
            // The field shows the ghost or hint over it, if the cell has one.
            if isGhost || isHint, let text { return isHint ? "\(text), a hint" : "suggested \(text)" }
            return isMissing ? "required, empty" : "empty"
        }
        if let entered, !entered.isEmpty { return entered }
        if let text {
            if isHint { return "\(text), a hint" }
            return isGhost ? "suggested \(text)" : text
        }
        return isMissing ? "required, empty" : "empty"
    }
}

/// A timed row's TIME cell (§7.6), or a carry's stopwatch below its row.
public enum TimedSetState: Equatable, Sendable {
    /// The play button. `target` is what Play sends: the shown duration to
    /// count down to, nil for a stopwatch.
    case ready(target: Seconds?)
    /// Counting: down to `endsAt`, then on past it (orange, `+0:05`); up from
    /// `startedAt` when `endsAt` is nil.
    case running(startedAt: Date, endsAt: Date?)
    case done(duration: Seconds?)
}

/// What a pending row commits if Finish marks it done.
public struct UntickedRow: Equatable, Sendable {
    public var row: RowID
    /// Nil when nothing complete can be committed: Finish drops the row. A
    /// pre-filled RPE as the row's `commitValues` has it.
    public var values: SetValues?

    public init(row: RowID, values: SetValues?) {
        self.row = row
        self.values = values
    }
}

/// The Up next row as every surface describes it (§5.6).
public struct UpNextPresentation: Equatable, Sendable {
    public var row: RowID
    public var slot: String
    /// `Chest Press (Machine)`.
    public var exercise: String
    /// `A2` when grouped.
    public var groupLabel: String?
    public var label: SetLabel
    /// Its round in a group block.
    public var round: Int?
    /// The values it shows: `40 kg × 11`; empty when none.
    public var values: String
    /// `Next: Chest Press (Machine), set 1: 40 kg × 11`.
    public var line: String
    /// The suggestion's text, shown under the row.
    public var reason: String?

    public init(row: RowID, slot: String, exercise: String, groupLabel: String?, label: SetLabel, round: Int?, values: String,
                line: String, reason: String?)
    {
        self.row = row
        self.slot = slot
        self.exercise = exercise
        self.groupLabel = groupLabel
        self.label = label
        self.round = round
        self.values = values
        self.line = line
        self.reason = reason
    }

    /// `Chest Press (Machine)`, or `Bird Dog (A2)` in a group.
    public var exerciseWithGroup: String {
        groupLabel.map { "\(exercise) (\($0))" } ?? exercise
    }
}

/// The rest bar's forms (§7.6).
public enum RestBarPresentation: Equatable, Sendable {
    /// No countdown: "Next: Seated Row (Machine), set 1: 50 kg × 9"; a tap scrolls to it.
    case upNext(String)
    /// A rest, a pause, a transition or the standalone timer counting down.
    case resting(RestCountdownPresentation)
    /// For ten seconds after a rest ends: "Rest over. Go: set 2, 35 kg × 14", tinted green.
    case restOver(String)
    /// A rest of 0 after the last tick: "Go: Bird Dog (A2), round 1".
    case go(String)
    /// "Plank, set 1", its countdown or stopwatch, and Stop.
    case timedSet(TimedSetBar)
    /// "All sets done" and Finish.
    case allDone
    /// A session with no exercise yet: "Add an exercise to begin"; a tap
    /// opens the exercise picker.
    case empty
}

/// A countdown in the rest bar.
public struct RestCountdownPresentation: Equatable, Sendable {
    /// `Rest`, `Warm-up rest`, `Round rest`, `Rest before next exercise`,
    /// `Switch sides`, `Pause`, `Go: Bird Dog (A2) in`, `Timer`.
    public var label: String
    /// Nil for the standalone timer.
    public var slot: RestSlot?
    public var startedAt: Date
    public var endsAt: Date
    /// The Up next line under the pill.
    public var next: String?
    /// The end alerts with a notification; else a haptic only.
    public var notifies: Bool

    public init(label: String, slot: RestSlot?, startedAt: Date, endsAt: Date, next: String?, notifies: Bool) {
        self.label = label
        self.slot = slot
        self.startedAt = startedAt
        self.endsAt = endsAt
        self.next = next
        self.notifies = notifies
    }

    public var isManual: Bool { slot == nil }
}

public struct TimedSetBar: Equatable, Sendable {
    public var row: RowID
    /// `Plank, set 1`.
    public var label: String
    public var startedAt: Date
    /// Nil for a stopwatch.
    public var endsAt: Date?

    public init(row: RowID, label: String, startedAt: Date, endsAt: Date?) {
        self.row = row
        self.label = label
        self.startedAt = startedAt
        self.endsAt = endsAt
    }
}

/// The Live Activity's content (§9.1), every string preformatted, because the
/// widget does not link WorkoutsKit. The app copies it into
/// `WorkoutsActivityAttributes.ContentState` and adds its effects revision.
public struct LiveActivityContent: Equatable, Sendable {
    /// The activity's title: the routine's name or "Training session".
    public var title: String
    public var clockStart: Date
    public var clockPausedAt: Date?
    /// The Up next row's exercise; empty when every set is done.
    public var exercise: String
    public var groupLabel: String?
    /// `Set 2: 35 kg × 14`, or `All sets done`.
    public var next: String
    /// The row Done completes, or the row Stop stops while a timed set runs.
    public var nextRowID: RowID?
    /// Done can tick the Up next row as shown, or Stop can stop the timed
    /// set: false when a value must be typed first (a drop to failure, an
    /// exercise with nothing to suggest, a carry's load), so the Lock Screen
    /// offers no button that would do nothing.
    public var canComplete: Bool
    public var countdown: LiveActivityCountdown?
    public var setsDone: Int
    public var setsPlanned: Int

    public init(title: String, clockStart: Date, clockPausedAt: Date?, exercise: String, groupLabel: String?, next: String,
                nextRowID: RowID?, canComplete: Bool = true, countdown: LiveActivityCountdown?, setsDone: Int, setsPlanned: Int)
    {
        self.title = title
        self.clockStart = clockStart
        self.clockPausedAt = clockPausedAt
        self.exercise = exercise
        self.groupLabel = groupLabel
        self.next = next
        self.nextRowID = nextRowID
        self.canComplete = canComplete
        self.countdown = countdown
        self.setsDone = setsDone
        self.setsPlanned = setsPlanned
    }
}

/// The Live Activity's countdown. `kind`'s raw values are the app's
/// `ContentState.Countdown.Kind`'s.
public struct LiveActivityCountdown: Equatable, Sendable {
    public enum Kind: String, Equatable, Sendable, CaseIterable {
        case rest, roundRest, blockRest, transition, pause, sideSwitch, timedSet, manual
    }

    public var kind: Kind
    /// `Rest`, `Round rest`, `Plank, set 1`: short, it sits above the countdown.
    public var label: String
    public var startedAt: Date
    /// Nil for a stopwatch.
    public var endsAt: Date?

    public init(kind: Kind, label: String, startedAt: Date, endsAt: Date?) {
        self.kind = kind
        self.label = label
        self.startedAt = startedAt
        self.endsAt = endsAt
    }
}

/// The countdown notification's copy (§9.2).
public struct CountdownNotification: Equatable, Sendable {
    /// `Rest over`, `Round 2`, `Go`, `Time`, `Timer done`.
    public var title: String
    /// `Next: Chest Press (Machine), set 2: 35 kg × 14`; nil for the standalone timer.
    public var body: String?
    /// The countdown's end.
    public var fireDate: Date

    public init(title: String, body: String?, fireDate: Date) {
        self.title = title
        self.body = body
        self.fireDate = fireDate
    }
}

/// The tab bar's in-progress accessory (§7.5).
public struct AccessoryPresentation: Equatable, Sendable {
    /// The routine's name or "Training session".
    public var title: String
    /// `Chest Press (Machine), set 2`; nil when every set is done.
    public var detail: String?
    public var isPaused: Bool
    /// A running rest's end, for "Rest 1:16"; nil when none runs or it is over.
    public var restEndsAt: Date?

    public init(title: String, detail: String?, isPaused: Bool, restEndsAt: Date?) {
        self.title = title
        self.detail = detail
        self.isPaused = isPaused
        self.restEndsAt = restEndsAt
    }
}
