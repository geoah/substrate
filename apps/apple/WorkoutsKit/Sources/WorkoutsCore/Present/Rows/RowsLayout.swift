import Foundation

// The Rows session layout (DESIGN §7.6, the owner's chosen design): each set
// is one line that reads like a sentence, the set to do now is open in place
// as an editor, and a floating capsule says what is happening. Everything a
// Rows view shows is here as plain values; the views compute no rule. They
// send `RowsAction`s, which map one to one onto the controller's commands,
// and `SetValuesPatch`es, the same edits the Table layout sends.

/// The session as the Rows layout shows it.
public struct RowsLayout: Equatable, Sendable {
    /// Display order, as `SessionPresentation.blocks`: a plain exercise or a group.
    public var blocks: [RowsBlock]
    /// The floating capsule at the bottom.
    public var capsule: RowsCapsule
    /// The row open in place: Up next, or the timed row while its timer runs.
    /// Nil when every set is done. Scroll to it after a Log.
    public var openRow: RowID?

    public init(blocks: [RowsBlock], capsule: RowsCapsule, openRow: RowID?) {
        self.blocks = blocks
        self.capsule = capsule
        self.openRow = openRow
    }

    /// No session yet.
    public static let empty = RowsLayout(blocks: [], capsule: .empty, openRow: nil)

    /// A set row wherever it sits: a block's own line, a folded warm-up, a mini-set chip.
    public func item(_ row: RowID) -> RowsSetItem? {
        for block in blocks {
            if let item = block.item(row) { return item }
        }
        return nil
    }
}

/// One block: a plain exercise, or a group with its members interleaved by round.
public struct RowsBlock: Equatable, Sendable, Identifiable {
    /// `ExerciseBlock.id`: the slot's key, or `group-<key>`.
    public var id: String
    /// The slots in the block, in member order. The block's `…` menu reads
    /// `SessionPresentation.card(slot)` for the rest picker, notes and the rest.
    public var slots: [String]
    /// The group's key; nil for a plain exercise.
    public var group: String?
    /// The group's colour, 0 to `GroupHeader.paletteSize − 1`.
    public var colorIndex: Int?
    /// `Leg Press (Machine)`, `Superset A`.
    public var title: String
    /// `6–10 reps at RPE 8, rest 2:30`, `10–15 reps at RPE 9.5, rest 1:30, set 2 ends in a drop`,
    /// `Glute Bridge and Bird Dog, 2 rounds`; nil when there is nothing to say.
    public var subtitle: String?
    /// The block's lines in the order they are done.
    public var items: [RowsItem]
    /// The `+ Add set` buttons: one for a plain exercise, one per member in a group.
    public var addSets: [RowsButton]
    /// Every row is done: the block folds to its one-line `summary` until opened.
    public var isFinished: Bool
    /// What a finished block's line shows; nil until the block is finished.
    public var summary: RowsBlockSummary?

    public init(id: String, slots: [String], group: String?, colorIndex: Int?, title: String, subtitle: String?,
                items: [RowsItem], addSets: [RowsButton], isFinished: Bool, summary: RowsBlockSummary?)
    {
        self.id = id
        self.slots = slots
        self.group = group
        self.colorIndex = colorIndex
        self.title = title
        self.subtitle = subtitle
        self.items = items
        self.addSets = addSets
        self.isFinished = isFinished
        self.summary = summary
    }

    /// A set row of this block wherever it sits.
    public func item(_ row: RowID) -> RowsSetItem? {
        for item in items {
            switch item {
            case let .set(set) where set.id == row: return set
            case let .warmups(fold): if let set = fold.sets.first(where: { $0.id == row }) { return set }
            case let .miniSets(line): if let set = line.sets.first(where: { $0.id == row }) { return set }
            default: continue
            }
        }
        return nil
    }

    /// The set rows in order, folded ones included.
    public var setItems: [RowsSetItem] {
        items.flatMap { item -> [RowsSetItem] in
            switch item {
            case let .set(set): [set]
            case let .warmups(fold): fold.sets
            case let .miniSets(line): line.sets
            case .round, .connector, .chainHeader: []
            }
        }
    }
}

/// A finished block's one line: `110 kg × 8, 7, 8` and `+5 kg`.
public struct RowsBlockSummary: Equatable, Sendable {
    /// The working sets, a repeated load written once: `110 kg × 8, 7, 8`,
    /// `40 kg × 11, 35 kg × 14, drop 25 kg × 12`, `Glute Bridge 16, 16; Bird Dog 11, 11 per side`.
    public var text: String
    /// The headline change against last time (`+5 kg`, `+1 rep`, `as last time`, `first time`).
    public var change: RowsChange
    /// VoiceOver: `Leg Press (Machine), done: 110 kilograms, 8, 7 and 8 reps. 5 kilograms more than last time.`
    public var sentence: String

    public init(text: String, change: RowsChange, sentence: String) {
        self.text = text
        self.change = change
        self.sentence = sentence
    }
}

/// One line of a block.
public enum RowsItem: Equatable, Sendable, Identifiable {
    /// A set: a head, a side, a drop or a mini-set.
    case set(RowsSetItem)
    /// Every warm-up of the block once all are done, on one line.
    case warmups(RowsWarmupFold)
    /// `Round 1 of 2`, in a group.
    case round(RowsRoundHeader)
    /// A transition, a round rest or the rest before the next exercise, in a group.
    case connector(RowsConnector)
    /// `Set 2 as myo-reps, 0:15 between mini-sets`, above a chain's head.
    case chainHeader(RowsChainHeader)
    /// A chain's done mini-sets or clusters as chips: `Mini-sets 4 4 4`.
    case miniSets(RowsMiniSets)

    public var id: String {
        switch self {
        case let .set(item): item.id.rawValue
        case let .warmups(fold): fold.id
        case let .round(header): header.id
        case let .connector(connector): connector.id
        case let .chainHeader(header): header.id
        case let .miniSets(line): line.id
        }
    }
}

/// Where a set row stands.
public enum RowsRowState: String, Equatable, Sendable {
    case done
    /// The set to do now: open in place with its editor.
    case now
    /// Pending, after the open row: muted, with last time.
    case next
    /// Pending, passed over (the owner went ahead out of order): a grey "later" tag.
    case later
    /// A segment whose earlier link is pending (a second side before the first).
    case locked
}

/// What kind of set a row is: its colour and its words.
public enum RowsRowRole: String, Equatable, Sendable {
    case working
    /// Orange.
    case warmup
    /// Orange, hanging off its set.
    case drop
    /// A myo, myo-match or rest-pause mini-set, hanging off its activation.
    case miniSet
    /// A cluster after the first.
    case cluster
    /// The second side of a one-side-at-a-time set.
    case side
}

/// A small tag after a row's values.
public struct RowsTag: Equatable, Sendable {
    public enum Style: String, Equatable, Sendable {
        /// Tinted: `up next`.
        case accent
        /// Grey: `later`, `top set`, `back-off`.
        case grey
        /// Orange: `warm-up`, `+ drop`.
        case orange
        /// Red: `to failure`.
        case red
    }

    public var text: String
    public var style: Style

    public init(text: String, style: Style) {
        self.text = text
        self.style = style
    }
}

/// One part of a change against last time.
public struct RowsChange: Equatable, Sendable {
    /// `+5 kg`, `−2 reps`, `+0:05`, `as last time`, `first time`.
    public var text: String
    /// Green when true; every loss and every neutral word is grey.
    public var isGain: Bool

    public init(text: String, isGain: Bool) {
        self.text = text
        self.isGain = isGain
    }
}

/// The small mark after a done set's change: how it compares with last time
/// overall, by estimated one-rep max (adjusted for RPE when both sets carry a
/// rating), within 1 % counting as the same. A model, not a fact: shown grey
/// unless better, never red.
public enum RowsVerdict: String, Equatable, Sendable {
    /// Green `↗`.
    case better
    /// Grey `=`.
    case same
    /// Grey `↘`.
    case below

    /// The mark's glyph.
    public var symbol: String {
        switch self {
        case .better: "\u{2197}"
        case .same: "="
        case .below: "\u{2198}"
        }
    }

    /// What VoiceOver says: `better than last time`.
    public var spoken: String {
        switch self {
        case .better: "better than last time"
        case .same: "about the same as last time"
        case .below: "below last time"
        }
    }
}

/// What a tap on a row does.
public enum RowsTap: Equatable, Sendable {
    /// Nothing: the open row, a locked row.
    case none
    /// A done row: open `fix` in place (view state, no command until Save).
    case fix
    /// A later or upcoming row: do it now (`touchRow`); it opens and becomes the current set.
    case doNow(RowsAction)
}

/// One set's line.
public struct RowsSetItem: Equatable, Sendable, Identifiable {
    public var id: RowID
    public var slot: String
    public var state: RowsRowState
    public var role: RowsRowRole
    /// `Set 1`, `Warm-up 2`, `Activation`, `Drop`, `Mini-set 3`, `Set 1 left`; in a group, the
    /// exercise's name (`Glute Bridge`) beside its `badge`.
    public var title: String
    /// `A1`, in a group.
    public var badge: String?
    /// Drawn hanging off the line above (a drop, a mini-set, a cluster).
    public var isChained: Bool
    /// `110 kg × 8`, `16 reps`, `11 per side`, `0:58 hold`, `25 kg to failure`: done values, or
    /// what the row will log (typed over suggested).
    public var values: String
    /// A pending row's values differ from the suggestion (typed or copied): tint them.
    public var valuesChanged: Bool
    /// `@ 8`: the RPE a done row holds; nil on pending rows and when none.
    public var rpe: String?
    /// The done row's RPE is the plan's, logged as shown, not tapped.
    public var rpeIsPrefilled: Bool
    /// Tags after the values: `+ drop`, `later`, `to failure`, `top set`, `warm-up`.
    public var tags: [RowsTag]
    /// The tag the open row shows while it is drawn collapsed (another row is
    /// being fixed): `up next`.
    public var collapsedTag: RowsTag?
    /// Pending rows: `last 105 × 10 @ 9.5`. Nil when there is no last time.
    public var last: String?
    /// Done rows: the change against last time, in order (`+5 kg`, `−2 reps`), or one
    /// neutral word (`as last time`, `first time`). Empty on pending rows.
    public var changes: [RowsChange]
    /// Done rows with a last time to compare.
    public var verdict: RowsVerdict?
    /// The row as one sentence, for VoiceOver.
    public var sentence: String
    public var tap: RowsTap
    /// The editor, on the open row only.
    public var editor: RowsEditor?
    /// Done rows: the fix editor's start (tap the row, change, Save).
    public var fix: RowsFix?

    public init(id: RowID, slot: String, state: RowsRowState, role: RowsRowRole, title: String, badge: String?,
                isChained: Bool, values: String, valuesChanged: Bool, rpe: String?, rpeIsPrefilled: Bool, tags: [RowsTag],
                collapsedTag: RowsTag?, last: String?, changes: [RowsChange], verdict: RowsVerdict?, sentence: String,
                tap: RowsTap, editor: RowsEditor?, fix: RowsFix?)
    {
        self.id = id
        self.slot = slot
        self.state = state
        self.role = role
        self.title = title
        self.badge = badge
        self.isChained = isChained
        self.values = values
        self.valuesChanged = valuesChanged
        self.rpe = rpe
        self.rpeIsPrefilled = rpeIsPrefilled
        self.tags = tags
        self.collapsedTag = collapsedTag
        self.last = last
        self.changes = changes
        self.verdict = verdict
        self.sentence = sentence
        self.tap = tap
        self.editor = editor
        self.fix = fix
    }
}

/// The warm-ups on one line once all are done: `Warm-ups 40 kg × 12, 70 kg × 6, as last time`.
public struct RowsWarmupFold: Equatable, Sendable, Identifiable {
    public var id: String
    /// `Warm-ups`, or `Warm-up` for one.
    public var title: String
    /// `40 kg × 12, 70 kg × 6`.
    public var values: String
    /// `as last time`, or `last 40 × 12, 60 × 6`; nil when none was done last time.
    public var comparison: RowsChange?
    /// The folded rows, for unfolding and fixing one.
    public var sets: [RowsSetItem]
    public var sentence: String

    public init(id: String, title: String, values: String, comparison: RowsChange?, sets: [RowsSetItem], sentence: String) {
        self.id = id
        self.title = title
        self.values = values
        self.comparison = comparison
        self.sets = sets
        self.sentence = sentence
    }
}

/// `Round 1 of 2`.
public struct RowsRoundHeader: Equatable, Sendable, Identifiable {
    public var id: String
    public var round: Int
    public var rounds: Int
    public var title: String
    /// Every set of the round is done.
    public var isDone: Bool

    public init(id: String, round: Int, rounds: Int, title: String, isDone: Bool) {
        self.id = id
        self.round = round
        self.rounds = rounds
        self.title = title
        self.isDone = isDone
    }
}

/// A thin line between a group's sets: `0:00, straight to A2`, `Rest 1:00, then round 2`,
/// `Rest 1:00, then Plank`.
public struct RowsConnector: Equatable, Sendable, Identifiable {
    public enum Kind: String, Equatable, Sendable {
        /// To the next member of the round: an arrow.
        case transition
        /// After a round: a timer.
        case roundRest
        /// After the group's last round, before the next exercise: a timer.
        case blockRest
    }

    public enum Phase: String, Equatable, Sendable {
        case upcoming
        /// Its countdown runs now.
        case running
        case passed
    }

    public var id: String
    public var kind: Kind
    public var seconds: Seconds
    /// Bold, tinted for a rest: `0:00`, `Rest 1:00`.
    public var lead: String
    /// `straight to A2`, `then round 2`, `then Plank`.
    public var text: String
    public var phase: Phase
    /// `No rest, straight to A2`.
    public var sentence: String

    public init(id: String, kind: Kind, seconds: Seconds, lead: String, text: String, phase: Phase, sentence: String) {
        self.id = id
        self.kind = kind
        self.seconds = seconds
        self.lead = lead
        self.text = text
        self.phase = phase
        self.sentence = sentence
    }
}

/// `Set 2 as myo-reps, 0:15 between mini-sets`.
public struct RowsChainHeader: Equatable, Sendable, Identifiable {
    public var id: String
    public var head: RowID
    public var text: String

    public init(id: String, head: RowID, text: String) {
        self.id = id
        self.head = head
        self.text = text
    }
}

/// A chain's done mini-sets as chips, one line: `Mini-sets 4 4 4, first time`.
public struct RowsMiniSets: Equatable, Sendable, Identifiable {
    public var id: String
    /// `Mini-sets`, `Clusters`.
    public var title: String
    /// One chip per done mini-set, in order: its reps (`4`). A tap fixes it (`sets`).
    public var chips: [RowsChip]
    /// `as last time`, `last 4, 4, 4, 3`, `first time`.
    public var comparison: RowsChange?
    /// Once the chain has ended: `Stop: mini-set 4 got 3, first was 4`.
    public var stopText: String?
    /// The done mini-sets, for fixing one.
    public var sets: [RowsSetItem]
    public var sentence: String

    public init(id: String, title: String, chips: [RowsChip], comparison: RowsChange?, stopText: String?, sets: [RowsSetItem],
                sentence: String)
    {
        self.id = id
        self.title = title
        self.chips = chips
        self.comparison = comparison
        self.stopText = stopText
        self.sets = sets
        self.sentence = sentence
    }
}

/// A chip that stands for one row.
public struct RowsChip: Equatable, Sendable, Identifiable {
    public var row: RowID
    public var text: String
    public var id: RowID { row }

    public init(row: RowID, text: String) {
        self.row = row
        self.text = text
    }
}

/// What a Rows control asks of the session: one `SessionCommand` each.
public enum RowsAction: Equatable, Sendable {
    /// `.tick(row)`: commits `SetRowPresentation.commitValues`, after the entry check.
    case tick(RowID)
    /// `.touchRow(row)`: do this row now.
    case touch(RowID)
    /// `.edit(row, patch)`: a fix's Save.
    case edit(RowID, SetValuesPatch)
    /// `.removeSet(row)`: Delete set, or End myo-reps on a pending mini-set.
    case removeSet(RowID)
    /// `.addSet(slot:role:)`.
    case addSet(slot: String, role: Role)
    case startTimedSet(RowID)
    case stopTimedSet(RowID)
    case cancelTimedSet(RowID)
    /// `.adjustRest(seconds)`: −15 and +15.
    case adjustRest(Seconds)
    case skipRest
}

/// A button: its words and what it sends.
public struct RowsButton: Equatable, Sendable {
    public var title: String
    /// VoiceOver's label when it says more than the title.
    public var accessibilityLabel: String
    public var action: RowsAction
    public var isEnabled: Bool

    public init(title: String, accessibilityLabel: String? = nil, action: RowsAction, isEnabled: Bool = true) {
        self.title = title
        self.accessibilityLabel = accessibilityLabel ?? title
        self.action = action
        self.isEnabled = isEnabled
    }
}

// MARK: - The capsule

/// The floating capsule at the bottom of the Rows layout (glass): the rest
/// countdown, or in words what to do now.
public enum RowsCapsule: Equatable, Sendable {
    /// A rest, a pause, a transition or the standalone timer counts down.
    case resting(RowsRestCapsule)
    /// The open row is up: `Set 2 is up`, `Rest 2:30 done`.
    case upNext(RowsNowCapsule)
    /// No rest after the last set: go straight to the open row. `A2 Bird Dog now, no rest`,
    /// `Drop to 25 kg now`.
    case go(RowsNowCapsule)
    /// The owner went ahead out of order; a passed-over set waits.
    case waiting(RowsWaitingCapsule)
    /// A timed set runs: what comes after it.
    case timing(RowsTimingCapsule)
    /// Every set is done: Finish.
    case allDone(title: String, detail: String)
    /// No exercise yet: a tap opens the exercise picker.
    case empty
}

/// The capsule while a countdown runs.
public struct RowsRestCapsule: Equatable, Sendable {
    public var kind: LiveActivityCountdown.Kind
    /// `Rest, then set 3`, `Round rest, then round 2`, `Pause, then mini-set 3`, `Timer`.
    public var title: String
    public var startedAt: Date
    public var endsAt: Date
    /// `of 2:30`, beside the countdown.
    public var totalText: String
    /// `After set 3`, tinted; nil for the standalone timer or with nothing after.
    public var afterLabel: String?
    /// `Chest Press (Machine), warm-up, 25 kg × 15`.
    public var afterText: String?
    /// −15, +15 and Skip.
    public var minus: RowsButton
    public var plus: RowsButton
    public var skip: RowsButton

    public init(kind: LiveActivityCountdown.Kind, title: String, startedAt: Date, endsAt: Date, totalText: String,
                afterLabel: String?, afterText: String?, minus: RowsButton, plus: RowsButton, skip: RowsButton)
    {
        self.kind = kind
        self.title = title
        self.startedAt = startedAt
        self.endsAt = endsAt
        self.totalText = totalText
        self.afterLabel = afterLabel
        self.afterText = afterText
        self.minus = minus
        self.plus = plus
        self.skip = skip
    }
}

/// The capsule's colour and glyph.
public enum RowsCapsuleTone: String, Equatable, Sendable {
    /// A tinted arrow or check.
    case accent
    /// A green check: a rest just ended.
    case restDone
    /// Orange: a warm-up or a drop.
    case orange
}

/// The capsule when no countdown runs: what is up and a clock counting up.
public struct RowsNowCapsule: Equatable, Sendable {
    public var row: RowID
    public var tone: RowsCapsuleTone
    /// `Set 2 is up`, `Leg Press (Machine), warm-up 1 is up`, `A2 Bird Dog now, no rest`, `Drop to 25 kg now`.
    public var title: String
    /// `Rest 2:30 done`, `Rest 1:00 after, then warm-up 2, 70 kg × 6`, `Then rest 1:00, then round 2`.
    public var detail: String?
    /// The clock on the right counts up from here: the last tick, else the session's start.
    public var since: Date
    /// Under that clock: `since set 1`, `since A1`, `session`.
    public var sinceLabel: String
    /// The session clock is paused (`session` only): the clock stands at
    /// this instant, as the title's does.
    public var pausedAt: Date?

    public init(row: RowID, tone: RowsCapsuleTone, title: String, detail: String?, since: Date, sinceLabel: String,
                pausedAt: Date? = nil)
    {
        self.row = row
        self.tone = tone
        self.title = title
        self.detail = detail
        self.since = since
        self.sinceLabel = sinceLabel
        self.pausedAt = pausedAt
    }
}

/// A passed-over set waits while the owner does another.
public struct RowsWaitingCapsule: Equatable, Sendable {
    /// The waiting row.
    public var row: RowID
    /// `Leg Press (Machine), set 3 is waiting`.
    public var title: String
    /// `Go back to it when you are ready`.
    public var detail: String
    /// `Go back`: do the waiting row now.
    public var goBack: RowsButton

    public init(row: RowID, title: String, detail: String, goBack: RowsButton) {
        self.row = row
        self.title = title
        self.detail = detail
        self.goBack = goBack
    }
}

/// The capsule while a timed set runs: what comes after it (the clock is in the open row).
public struct RowsTimingCapsule: Equatable, Sendable {
    public var row: RowID
    /// `Then rest 0:15`, `Then no rest`, `Last set`.
    public var title: String
    /// `Set 2, 0:58 hold, last 0:53`.
    public var detail: String?
    public var startedAt: Date
    /// Nil for a stopwatch.
    public var endsAt: Date?

    public init(row: RowID, title: String, detail: String?, startedAt: Date, endsAt: Date?) {
        self.row = row
        self.title = title
        self.detail = detail
        self.startedAt = startedAt
        self.endsAt = endsAt
    }
}
