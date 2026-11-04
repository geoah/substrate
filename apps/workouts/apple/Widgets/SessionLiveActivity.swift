import ActivityKit
import AppIntents
import SwiftUI
import WidgetKit

/// The training session on the Lock Screen, in the Dynamic Island and, through
/// the `.small` family, in the watch's Smart Stack (DESIGN.md §9.1).
///
/// Everything is drawn from the content state. Counting views tick with no app
/// code, and the content's `staleDate` is the countdown's end, so a finished
/// rest flips to "Rest over" while the app is suspended: a stale or finished
/// state never shows a live 0:00 with buttons.
struct SessionLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: WorkoutsActivityAttributes.self) { context in
            SessionActivityView(
                title: context.attributes.title,
                phase: SessionPhase(context.state, isStale: context.isStale)
            )
            .widgetURL(WorkoutsLinks.session)
        } dynamicIsland: { context in
            let phase = SessionPhase(context.state, isStale: context.isStale)
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    ExpandedLeading(phase: phase)
                        .dynamicIsland(verticalPlacement: .belowIfTooWide)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    ExpandedTrailing(phase: phase)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    ExpandedBottom(phase: phase)
                }
            } compactLeading: {
                CompactLeading(phase: phase)
            } compactTrailing: {
                CompactTrailing(phase: phase)
            } minimal: {
                MinimalView(phase: phase)
            }
            .keylineTint(phase.tint)
            .widgetURL(WorkoutsLinks.session)
        }
        .supplementalActivityFamilies([.small])
    }
}

// MARK: - Phase

typealias SessionContent = WorkoutsActivityAttributes.ContentState
typealias SessionCountdown = WorkoutsActivityAttributes.ContentState.Countdown

/// What the Live Activity shows: the content state read together with whether
/// its countdown has ended.
struct SessionPhase {
    enum Mode: Equatable {
        /// Nothing counts down: Up next and Done.
        case working
        /// A rest, transition, pause, side switch or timer is running: −15 s,
        /// +15 s and Skip (Skip alone for a stopwatch).
        case counting
        /// That countdown has ended: "Rest over", Up next and Done.
        case over
        /// A timed set counts down to its target, or up when it has none: Stop.
        case timing
        /// A timed set past its target keeps counting (it never completes
        /// itself, DESIGN.md §4.3): the time held so far and Stop.
        case targetReached
        /// Saved: the summary, no buttons, no clocks.
        case finished
    }

    let state: SessionContent
    let mode: Mode

    init(_ state: SessionContent, isStale: Bool, now: Date = .now) {
        self.state = state
        if state.finishedSummary != nil {
            mode = .finished
            return
        }
        guard let countdown = state.countdown else {
            mode = .working
            return
        }
        // The system marks the content stale at its staleDate, the
        // countdown's end; the clock check covers a render after the end
        // that arrives before the stale flag.
        let ended = countdown.endsAt.map { isStale || $0 <= now } ?? false
        if countdown.kind == .timedSet {
            mode = ended ? .targetReached : .timing
        } else {
            mode = ended ? .over : .counting
        }
    }

    var countdown: SessionCountdown? { state.countdown }

    /// Whether the countdown has an end to move (a stopwatch has none).
    var canAdjust: Bool { mode == .counting && countdown?.endsAt != nil }

    /// The rest bar's colours (DESIGN.md §8): the accent while counting, green
    /// once rest is over, orange past a timed set's target.
    var tint: Color {
        switch mode {
        case .working, .counting, .timing: .workoutsAccent
        case .over, .finished: .green
        case .targetReached: .orange
        }
    }

    /// The headline when a countdown has ended.
    var overTitle: String {
        switch countdown?.kind {
        case .transition, .pause, .sideSwitch: "Go"
        case .manual: "Timer done"
        default: "Rest over"
        }
    }

    /// What a timed set's clock means right now.
    var timedCaption: String {
        if mode == .targetReached { return "Target reached" }
        return countdown?.endsAt == nil ? "Time held" : "Time left"
    }

    /// The range a progress bar empties over, never inverted: a reversed
    /// `ClosedRange` traps and would blank the activity.
    var countdownRange: ClosedRange<Date>? {
        guard mode == .counting, let countdown, let end = countdown.endsAt else { return nil }
        return countdown.startedAt ... max(countdown.startedAt, end)
    }
}

extension Color {
    /// The app's AccentColor. The extension cannot read the app's asset
    /// catalogue, so SubstrateWorkoutsWidgets/Assets.xcassets repeats it.
    static let workoutsAccent = Color("AccentColor")
}

// MARK: - Shared pieces

/// The running countdown: down to its end, or up from its start for a
/// stopwatch and for a timed set past its target (the time held so far).
struct CountdownText: View {
    let phase: SessionPhase

    var body: some View {
        if let countdown = phase.countdown {
            if phase.mode != .targetReached, let end = countdown.endsAt {
                Text(timerInterval: countdown.startedAt ... max(countdown.startedAt, end), countsDown: true)
                    .monospacedDigit()
            } else {
                Text(timerInterval: countdown.startedAt ... Date.distantFuture, countsDown: false)
                    .monospacedDigit()
            }
        }
    }
}

/// The session's elapsed time, frozen while the session clock is paused.
struct SessionClock: View {
    let state: SessionContent

    var body: some View {
        Text(
            timerInterval: state.clockStart ... Date.distantFuture,
            pauseTime: state.clockPausedAt,
            countsDown: false
        )
        .monospacedDigit()
    }
}

/// The app glyph inside a ring that fills as sets are done.
struct SetsRing: View {
    let phase: SessionPhase
    var size: CGFloat = 22

    private var fraction: Double {
        let state = phase.state
        guard state.setsPlanned > 0 else { return phase.mode == .finished ? 1 : 0 }
        return min(1, Double(state.setsDone) / Double(state.setsPlanned))
    }

    var body: some View {
        ZStack {
            Circle()
                .stroke(phase.tint.opacity(0.25), lineWidth: size / 9)
            Circle()
                .trim(from: 0, to: fraction)
                .stroke(phase.tint, style: StrokeStyle(lineWidth: size / 9, lineCap: .round))
                .rotationEffect(.degrees(-90))
            Image(systemName: "dumbbell.fill")
                .font(.system(size: size * 0.4, weight: .bold))
                .foregroundStyle(phase.tint)
        }
        .frame(width: size, height: size)
        .accessibilityElement()
        .accessibilityLabel("\(phase.state.setsDone) of \(phase.state.setsPlanned) working sets done")
    }
}

/// The Up next set's label, "W" or "2", as the app's lines draw it: one
/// size and shape, orange for a warm-up, never a group letter or a group's
/// colour (owner, 2026-11-02).
struct SetLabelChip: View {
    let label: String
    let isWarmup: Bool
    let tint: Color
    var size: CGFloat = 20

    var body: some View {
        let color = isWarmup ? Color.orange : tint
        let spoken = isWarmup ? "Warm-up" : label.allSatisfy(\.isNumber) ? "Set \(label)" : nil
        Text(label)
            .font(.system(size: size * 0.6, weight: .bold))
            .monospacedDigit()
            .lineLimit(1)
            .minimumScaleFactor(0.6)
            .foregroundStyle(color)
            .frame(width: size * 1.35, height: size)
            .background(color.opacity(0.2), in: .rect(cornerRadius: size * 0.3))
            .accessibilityLabel(spoken ?? "")
            .accessibilityHidden(spoken == nil)
    }
}

/// The Up next exercise, after its set label.
struct ExerciseLine: View {
    let phase: SessionPhase
    var font: Font = .headline
    var chipSize: CGFloat = 20

    var body: some View {
        if !phase.state.exercise.isEmpty {
            HStack(spacing: 6) {
                if let label = phase.state.setLabel {
                    // The accent, never the phase's green or orange: orange means a warm-up.
                    SetLabelChip(label: label, isWarmup: phase.state.isWarmup, tint: .workoutsAccent, size: chipSize)
                }
                Text(phase.state.exercise)
                    .font(font)
                    .lineLimit(1)
            }
        }
    }
}

/// "Next" and the Up next set's values, or "All sets done" on its own. The
/// values ("105 kg × 10") are what the owner looks for between sets, so they
/// are primary and medium weight; the label beside the exercise says which
/// set, and the word "Next" is a quiet label.
struct NextLine: View {
    let phase: SessionPhase
    var font: Font = .subheadline

    var body: some View {
        Group {
            if phase.state.nextRowID == nil {
                Text(phase.state.next)
                    .fontWeight(.medium)
                    .foregroundStyle(.primary)
            } else {
                HStack(alignment: .firstTextBaseline, spacing: 5) {
                    Text("Next")
                        .foregroundStyle(.secondary)
                    Text(phase.state.values.isEmpty ? phase.state.next : phase.state.values)
                        .fontWeight(.medium)
                        .foregroundStyle(.primary)
                }
                .accessibilityElement(children: .combine)
            }
        }
        .font(font)
        .lineLimit(1)
        .minimumScaleFactor(0.85)
    }
}

// MARK: - Buttons

// Tinted fills rather than glass: the Lock Screen platter is already the
// system's glass, and DESIGN.md §8 allows no glass on glass.

/// −15 s or +15 s on the running countdown.
struct AdjustButton: View {
    let phase: SessionPhase
    let seconds: Int

    var body: some View {
        if phase.canAdjust {
            Button(intent: AdjustRestIntent(seconds: seconds)) {
                Text(seconds < 0 ? "\u{2212}\(-seconds)s" : "+\(seconds)s")
                    .monospacedDigit()
            }
            .font(.subheadline.weight(.semibold))
            .buttonStyle(.bordered)
            .buttonBorderShape(.capsule)
            .tint(phase.tint)
            .accessibilityLabel(seconds < 0 ? "Rest \(-seconds) seconds less" : "Rest \(seconds) seconds more")
        }
    }
}

/// Skip: ends the running countdown now.
struct SkipButton: View {
    let phase: SessionPhase

    var body: some View {
        Button(intent: SkipRestIntent()) {
            Text("Skip")
        }
        .font(.subheadline.weight(.semibold))
        .buttonStyle(.borderedProminent)
        .buttonBorderShape(.capsule)
        .tint(phase.tint)
    }
}

/// Done: ticks the Up next set with the values it shows. None when a value
/// must be typed in the app first: a tap on the activity opens it there.
struct DoneButton: View {
    let phase: SessionPhase

    var body: some View {
        if let rowID = phase.state.nextRowID, phase.state.canComplete {
            Button(intent: CompleteSetIntent(rowID: rowID)) {
                Label("Done", systemImage: "checkmark")
            }
            .font(.subheadline.weight(.semibold))
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.capsule)
            .tint(phase.tint)
        }
    }
}

/// Stop: completes the running timed set. None when a value other than the
/// time must be typed first (a weighted plank's load): Stop would be refused.
struct StopButton: View {
    let phase: SessionPhase

    var body: some View {
        if let rowID = phase.state.nextRowID, phase.state.canComplete {
            Button(intent: StopTimedSetIntent(rowID: rowID)) {
                Label("Stop", systemImage: "stop.fill")
            }
            .font(.subheadline.weight(.semibold))
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.capsule)
            .tint(phase.tint)
        }
    }
}

// MARK: - Lock Screen and the watch

/// Picks the layout for the family the system asks for: `.medium` on the
/// Lock Screen and in StandBy, `.small` in the watch's Smart Stack.
struct SessionActivityView: View {
    @Environment(\.activityFamily) private var family
    let title: String
    let phase: SessionPhase

    var body: some View {
        switch family {
        case .small:
            SmallActivityView(phase: phase)
        case .medium:
            LockScreenView(title: title, phase: phase)
        @unknown default:
            LockScreenView(title: title, phase: phase)
        }
    }
}

struct LockScreenView: View {
    let title: String
    let phase: SessionPhase

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            header
            switch phase.mode {
            case .counting:
                counting
            case .over, .working:
                upNext
            case .timing, .targetReached:
                timed
            case .finished:
                if let summary = phase.state.finishedSummary {
                    Label {
                        Text(summary).foregroundStyle(.primary)
                    } icon: {
                        Image(systemName: "checkmark.circle.fill").foregroundStyle(phase.tint)
                    }
                    .font(.headline)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 16)
        .padding(.vertical, 14)
        // The system background (Liquid Glass on the Lock Screen) is kept;
        // only the system's own dismiss control takes the accent.
        .activitySystemActionForegroundColor(.workoutsAccent)
    }

    private var header: some View {
        HStack(spacing: 8) {
            SetsRing(phase: phase)
            Text(title)
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.secondary)
                .lineLimit(1)
            Spacer(minLength: 8)
            if phase.mode != .finished {
                if phase.state.clockPausedAt != nil {
                    Image(systemName: "pause.fill")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .accessibilityLabel("Paused")
                }
                SessionClock(state: phase.state)
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.trailing)
                    .frame(width: 64, alignment: .trailing)
            }
        }
    }

    /// Resting: what comes next, the bar emptying, and the controls around
    /// the countdown.
    @ViewBuilder private var counting: some View {
        VStack(alignment: .leading, spacing: 2) {
            ExerciseLine(phase: phase)
            NextLine(phase: phase)
        }
        if let range = phase.countdownRange {
            ProgressView(timerInterval: range, countsDown: true) {
                EmptyView()
            } currentValueLabel: {
                EmptyView()
            }
            .tint(phase.tint)
        }
        // [−15s]   Rest 1:16   [+15s] [Skip], as DESIGN.md §9.1 draws it.
        HStack(spacing: 8) {
            AdjustButton(phase: phase, seconds: -15)
            Spacer(minLength: 4)
            VStack(spacing: 0) {
                Text(phase.countdown?.label ?? "")
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                CountdownText(phase: phase)
                    .font(.system(.title2, design: .rounded, weight: .semibold))
                    .foregroundStyle(phase.tint)
                    .multilineTextAlignment(.center)
                    .minimumScaleFactor(0.7)
                    .frame(width: 88)
            }
            Spacer(minLength: 4)
            AdjustButton(phase: phase, seconds: 15)
            SkipButton(phase: phase)
        }
    }

    /// Rest over, or nothing counting: what to do now and Done.
    @ViewBuilder private var upNext: some View {
        if phase.mode == .over {
            Label(phase.overTitle, systemImage: "checkmark.circle.fill")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(phase.tint)
        }
        HStack(alignment: .bottom, spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                ExerciseLine(phase: phase)
                NextLine(phase: phase)
            }
            Spacer(minLength: 0)
            DoneButton(phase: phase)
        }
    }

    /// A timed set: its name, the time left (or held) and Stop.
    @ViewBuilder private var timed: some View {
        HStack(alignment: .bottom, spacing: 12) {
            VStack(alignment: .leading, spacing: 0) {
                Text(phase.countdown?.label ?? phase.state.exercise)
                    .font(.headline)
                    .lineLimit(1)
                Text(phase.timedCaption)
                    .font(.caption.weight(.medium))
                    .foregroundStyle(phase.mode == .targetReached ? phase.tint : .secondary)
                    .padding(.top, 2)
                CountdownText(phase: phase)
                    .font(.system(.title, design: .rounded, weight: .semibold))
                    .foregroundStyle(phase.tint)
                    .minimumScaleFactor(0.7)
            }
            Spacer(minLength: 0)
            StopButton(phase: phase)
        }
    }
}

/// The watch's Smart Stack: one thing, in two lines, so it never clips
/// (owner, 2026-11-02: four lines were cut off at the top and the bottom).
/// Resting, the countdown and what comes next; otherwise the set to do now,
/// its label and exercise over its values. No buttons, no session clock. The
/// system insets the view, so it adds no padding of its own.
struct SmallActivityView: View {
    let phase: SessionPhase

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            switch phase.mode {
            case .counting:
                // A transition's label names the exercise the line under it shows.
                countdownRow(caption: phase.countdown?.kind == .transition ? "Go" : phase.countdown?.label ?? "")
                ExerciseLine(phase: phase, font: Self.lineFont, chipSize: 16)
                    .minimumScaleFactor(0.8)
            case .timing, .targetReached:
                countdownRow(caption: phase.timedCaption)
                Text(phase.countdown?.label ?? phase.state.exercise)
                    .font(Self.lineFont)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
            case .over, .working:
                if phase.state.nextRowID == nil {
                    allDone
                } else {
                    ExerciseLine(phase: phase, font: Self.lineFont, chipSize: 16)
                        .minimumScaleFactor(0.8)
                    Text(phase.state.values.isEmpty ? phase.state.next : phase.state.values)
                        .font(.system(size: 20, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                        .foregroundStyle(phase.mode == .over ? phase.tint : .primary)
                        .lineLimit(1)
                        .minimumScaleFactor(0.6)
                }
            case .finished:
                Label(phase.state.finishedSummary ?? "", systemImage: "checkmark.circle.fill")
                    .font(Self.lineFont)
                    .foregroundStyle(phase.tint)
                    .lineLimit(2)
                    .minimumScaleFactor(0.8)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
    }

    /// Fixed sizes: the slot does not grow with the watch's text size, so
    /// text styles that do would clip it again (the owner's complaint).
    private static let lineFont = Font.system(size: 13, weight: .semibold)

    /// The countdown large, its word beside it: `1:16  Rest`.
    private func countdownRow(caption: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            CountdownText(phase: phase)
                .font(.system(size: 24, weight: .semibold, design: .rounded))
                .foregroundStyle(phase.tint)
                .lineLimit(1)
                .minimumScaleFactor(0.6)
                .layoutPriority(1)
            Text(caption)
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(phase.mode == .targetReached ? phase.tint : .secondary)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
        }
    }

    private var allDone: some View {
        HStack(spacing: 6) {
            SetsRing(phase: phase, size: 18)
            Text(phase.state.next)
                .font(Self.lineFont)
                .lineLimit(2)
                .minimumScaleFactor(0.8)
        }
    }
}

// MARK: - Dynamic Island

struct CompactLeading: View {
    let phase: SessionPhase

    var body: some View {
        if let label = phase.state.setLabel, phase.mode != .finished {
            SetLabelChip(label: label, isWarmup: phase.state.isWarmup, tint: .workoutsAccent, size: 18)
        } else {
            Image(systemName: phase.mode == .finished ? "checkmark.circle.fill" : "dumbbell.fill")
                .foregroundStyle(phase.tint)
        }
    }
}

struct CompactTrailing: View {
    let phase: SessionPhase

    var body: some View {
        switch phase.mode {
        case .counting, .timing, .targetReached:
            CountdownText(phase: phase)
                .font(.caption.weight(.semibold))
                .foregroundStyle(phase.tint)
                .multilineTextAlignment(.trailing)
                .frame(maxWidth: 44)
        case .over:
            Text("Go")
                .font(.caption.weight(.bold))
                .foregroundStyle(phase.tint)
        case .working:
            // Elapsed session time when nothing counts down.
            SessionClock(state: phase.state)
                .font(.caption.weight(.semibold))
                .multilineTextAlignment(.trailing)
                .frame(maxWidth: 52)
        case .finished:
            Text("Saved")
                .font(.caption.weight(.semibold))
                .foregroundStyle(phase.tint)
        }
    }
}

/// Shown when another app's activity shares the island. The countdown when
/// one runs, else the glyph: elapsed time would not fit.
struct MinimalView: View {
    let phase: SessionPhase

    var body: some View {
        switch phase.mode {
        case .counting, .timing, .targetReached:
            CountdownText(phase: phase)
                .font(.caption2.weight(.semibold))
                .foregroundStyle(phase.tint)
                .multilineTextAlignment(.center)
                .minimumScaleFactor(0.6)
                .frame(maxWidth: 36)
        case .over:
            Text("Go")
                .font(.caption2.weight(.bold))
                .foregroundStyle(phase.tint)
        case .working:
            Image(systemName: "dumbbell.fill")
                .foregroundStyle(phase.tint)
        case .finished:
            Image(systemName: "checkmark")
                .foregroundStyle(phase.tint)
        }
    }
}

struct ExpandedLeading: View {
    let phase: SessionPhase

    var body: some View {
        if phase.mode == .finished {
            SetsRing(phase: phase, size: 28)
        } else {
            VStack(alignment: .leading, spacing: 2) {
                ExerciseLine(phase: phase)
                if phase.mode == .over {
                    Text(phase.overTitle)
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(phase.tint)
                }
            }
            .padding(.leading, 4)
        }
    }
}

struct ExpandedTrailing: View {
    let phase: SessionPhase

    var body: some View {
        Group {
            switch phase.mode {
            case .counting, .timing, .targetReached:
                CountdownText(phase: phase)
                    .font(.system(.title2, design: .rounded, weight: .semibold))
                    .foregroundStyle(phase.tint)
                    .multilineTextAlignment(.trailing)
                    .minimumScaleFactor(0.7)
                    .frame(maxWidth: 96, alignment: .trailing)
            case .over:
                Text("Go")
                    .font(.system(.title2, design: .rounded, weight: .bold))
                    .foregroundStyle(phase.tint)
            case .working:
                SessionClock(state: phase.state)
                    .font(.system(.title3, design: .rounded, weight: .semibold))
                    .multilineTextAlignment(.trailing)
                    .frame(maxWidth: 96, alignment: .trailing)
            case .finished:
                EmptyView()
            }
        }
        .padding(.trailing, 4)
    }
}

struct ExpandedBottom: View {
    let phase: SessionPhase

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            switch phase.mode {
            case .counting:
                NextLine(phase: phase)
                // The countdown sits in the trailing region already.
                HStack(spacing: 8) {
                    AdjustButton(phase: phase, seconds: -15)
                    AdjustButton(phase: phase, seconds: 15)
                    Spacer(minLength: 0)
                    SkipButton(phase: phase)
                }
            case .over, .working:
                HStack(alignment: .center, spacing: 12) {
                    NextLine(phase: phase)
                    Spacer(minLength: 0)
                    DoneButton(phase: phase)
                }
            case .timing, .targetReached:
                HStack(alignment: .center, spacing: 12) {
                    VStack(alignment: .leading, spacing: 0) {
                        Text(phase.countdown?.label ?? phase.state.exercise)
                            .font(.subheadline.weight(.semibold))
                            .lineLimit(1)
                        Text(phase.timedCaption)
                            .font(.caption)
                            .foregroundStyle(phase.mode == .targetReached ? phase.tint : .secondary)
                    }
                    Spacer(minLength: 0)
                    StopButton(phase: phase)
                }
            case .finished:
                Text(phase.state.finishedSummary ?? "")
                    .font(.headline)
                    .lineLimit(2)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 4)
    }
}
