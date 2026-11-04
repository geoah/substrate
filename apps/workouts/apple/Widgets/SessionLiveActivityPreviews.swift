import SwiftUI
import WidgetKit

// Hand-built content states for every phase, until the countdown coordinator
// builds them from the session presentation. Times are relative to the
// preview's launch so the counting views tick.

extension WorkoutsActivityAttributes {
    static let preview = WorkoutsActivityAttributes(
        sessionID: "019a3c5e-7a00-7000-8000-000000000001",
        title: "Fully Body Collection #1"
    )
}

extension WorkoutsActivityAttributes.ContentState {
    private static let start = Date.now.addingTimeInterval(-24 * 60)

    static let resting = Self(
        clockStart: start,
        exercise: "Chest Press (Machine)",
        setLabel: "2",
        values: "35 kg × 14",
        next: "Set 2: 35 kg × 14",
        nextRowID: "row-chest-2",
        countdown: .init(kind: .rest, label: "Rest", startedAt: .now.addingTimeInterval(-44), endsAt: .now.addingTimeInterval(76)),
        setsDone: 8,
        setsPlanned: 24,
        revision: 12
    )

    static let roundRest = Self(
        clockStart: start,
        exercise: "Glute Bridge",
        setLabel: "2",
        values: "× 15",
        next: "Set 2: × 15",
        nextRowID: "row-bridge-2",
        countdown: .init(kind: .roundRest, label: "Round rest", startedAt: .now.addingTimeInterval(-20), endsAt: .now.addingTimeInterval(70)),
        setsDone: 3,
        setsPlanned: 24,
        revision: 5
    )

    /// The countdown's end has passed: what the system shows once the
    /// content goes stale.
    static let restOver = Self(
        clockStart: start,
        exercise: "Chest Press (Machine)",
        setLabel: "2",
        values: "35 kg × 14",
        next: "Set 2: 35 kg × 14",
        nextRowID: "row-chest-2",
        countdown: .init(kind: .rest, label: "Rest", startedAt: .now.addingTimeInterval(-130), endsAt: .now.addingTimeInterval(-10)),
        setsDone: 8,
        setsPlanned: 24,
        revision: 12
    )

    static let working = Self(
        clockStart: start,
        exercise: "Vertical Traction (Machine)",
        setLabel: "1",
        values: "45 kg × 12",
        next: "Set 1: 45 kg × 12",
        nextRowID: "row-pull-1",
        setsDone: 9,
        setsPlanned: 24,
        revision: 14
    )

    static let paused = Self(
        clockStart: start,
        clockPausedAt: .now.addingTimeInterval(-60),
        exercise: "Seated Row (Machine)",
        setLabel: "W",
        isWarmup: true,
        values: "30 kg × 12",
        next: "Warm-up: 30 kg × 12",
        nextRowID: "row-row-1",
        setsDone: 10,
        setsPlanned: 24,
        revision: 15
    )

    static let timedSet = Self(
        clockStart: start,
        exercise: "Plank",
        setLabel: "1",
        values: "1:07",
        next: "Set 1: 1:07",
        nextRowID: "row-plank-1",
        countdown: .init(kind: .timedSet, label: "Plank, set 1", startedAt: .now.addingTimeInterval(-25), endsAt: .now.addingTimeInterval(42)),
        setsDone: 18,
        setsPlanned: 24,
        revision: 30
    )

    static let targetReached = Self(
        clockStart: start,
        exercise: "Plank",
        setLabel: "1",
        values: "1:07",
        next: "Set 1: 1:07",
        nextRowID: "row-plank-1",
        countdown: .init(kind: .timedSet, label: "Plank, set 1", startedAt: .now.addingTimeInterval(-72), endsAt: .now.addingTimeInterval(-5)),
        setsDone: 18,
        setsPlanned: 24,
        revision: 31
    )

    static let allDone = Self(
        clockStart: start,
        exercise: "",
        next: "All sets done",
        setsDone: 24,
        setsPlanned: 24,
        revision: 40
    )

    static let finished = Self(
        clockStart: start,
        exercise: "",
        next: "All sets done",
        setsDone: 24,
        setsPlanned: 24,
        finishedSummary: "Saved: 46 min, 24 sets",
        revision: 41
    )

    /// The owner's screenshot (2026-11-02): a superset's transition, the
    /// next member's name long enough to be cut.
    static let transition = Self(
        clockStart: start,
        exercise: "Seated Leg Curl (Machine)",
        setLabel: "2",
        values: "40 kg × 10",
        next: "Set 2: 40 kg × 10",
        nextRowID: "row-curl-2",
        countdown: .init(
            kind: .transition, label: "Go: Seated Leg Curl (Machine)", startedAt: .now.addingTimeInterval(-5),
            endsAt: .now.addingTimeInterval(10)),
        setsDone: 11,
        setsPlanned: 24,
        revision: 20
    )

    static let all: [Self] = [
        .resting, .roundRest, .transition, .restOver, .working, .paused, .timedSet, .targetReached, .allDone, .finished,
    ]
}

#Preview("Lock Screen", as: .content, using: WorkoutsActivityAttributes.preview) {
    SessionLiveActivity()
} contentStates: {
    WorkoutsActivityAttributes.ContentState.resting
    WorkoutsActivityAttributes.ContentState.roundRest
    WorkoutsActivityAttributes.ContentState.restOver
    WorkoutsActivityAttributes.ContentState.working
    WorkoutsActivityAttributes.ContentState.paused
    WorkoutsActivityAttributes.ContentState.timedSet
    WorkoutsActivityAttributes.ContentState.targetReached
    WorkoutsActivityAttributes.ContentState.allDone
    WorkoutsActivityAttributes.ContentState.finished
}

#Preview("Island expanded", as: .dynamicIsland(.expanded), using: WorkoutsActivityAttributes.preview) {
    SessionLiveActivity()
} contentStates: {
    WorkoutsActivityAttributes.ContentState.resting
    WorkoutsActivityAttributes.ContentState.restOver
    WorkoutsActivityAttributes.ContentState.working
    WorkoutsActivityAttributes.ContentState.timedSet
    WorkoutsActivityAttributes.ContentState.finished
}

#Preview("Island compact", as: .dynamicIsland(.compact), using: WorkoutsActivityAttributes.preview) {
    SessionLiveActivity()
} contentStates: {
    WorkoutsActivityAttributes.ContentState.resting
    WorkoutsActivityAttributes.ContentState.roundRest
    WorkoutsActivityAttributes.ContentState.restOver
    WorkoutsActivityAttributes.ContentState.working
    WorkoutsActivityAttributes.ContentState.targetReached
}

#Preview("Island minimal", as: .dynamicIsland(.minimal), using: WorkoutsActivityAttributes.preview) {
    SessionLiveActivity()
} contentStates: {
    WorkoutsActivityAttributes.ContentState.resting
    WorkoutsActivityAttributes.ContentState.restOver
    WorkoutsActivityAttributes.ContentState.working
}

// The `.small` family at the watch Smart Stack's size, every phase: the
// smallest watch's slot less the system's inset, so nothing here may clip.
#Preview("Watch", traits: .sizeThatFitsLayout) {
    VStack(spacing: 12) {
        ForEach(Array(WorkoutsActivityAttributes.ContentState.all.enumerated()), id: \.offset) { _, state in
            SmallActivityView(phase: SessionPhase(state, isStale: false))
                .frame(width: 146, height: 54)
                .padding(10)
                .background(.black, in: .rect(cornerRadius: 16))
        }
    }
    .padding()
    .environment(\.colorScheme, .dark)
}
