import WorkoutsCore
import SwiftUI

/// A timed set's TIME cell (DESIGN.md §7.6): a play button beside the
/// duration; while it runs, Stop beside the countdown, which turns orange
/// and counts on past the target; a stopwatch counts up. A long press on
/// Stop offers "Cancel timer". Reaching the target does nothing by itself.
struct TimedCell: View {
    let row: SetRowPresentation
    let card: ExerciseCard
    let layout: GridLayout
    var focus: FocusState<CoverModel.Field?>.Binding

    @Environment(SessionController.self) private var session

    var body: some View {
        HStack(spacing: 4) {
            switch row.timer {
            case .ready?:
                TimerButton(row: row, exercise: card.name, running: false)
                durationField
            case let .running(startedAt, endsAt)?:
                TimerButton(row: row, exercise: card.name, running: true)
                RunningTime(startedAt: startedAt, endsAt: endsAt)
                    .frame(maxWidth: .infinity, minHeight: layout.cellHeight)
            case .done?, nil:
                durationField
            }
        }
    }

    @ViewBuilder
    private var durationField: some View {
        if let cell = row.cell(.time) {
            ValueCell(row: row, cell: cell, card: card, layout: layout, focus: focus)
        }
    }
}

/// Play or Stop for a row's timer.
struct TimerButton: View {
    let row: SetRowPresentation
    /// The exercise, so VoiceOver says whose timer it is.
    let exercise: String
    let running: Bool

    @Environment(SessionController.self) private var session
    @Environment(CoverModel.self) private var cover

    var body: some View {
        Button {
            let id = row.id
            Task {
                // A duration still being typed is the target Play counts down to.
                await cover.commitRow(id, session: session)
                await session.send(running ? .stopTimedSet(id) : .startTimedSet(id))
            }
        } label: {
            Image(systemName: running ? "stop.fill" : "play.fill")
                .font(.body.weight(.semibold))
                .foregroundStyle(running ? Color.white : Color.accentColor)
                .frame(width: 36, height: 36)
                .background(running ? Color.accentColor : Color(.tertiarySystemFill), in: .circle)
                // The circle stays 36 pt; the target is the 44 pt minimum.
                .frame(minWidth: 44, minHeight: 44)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .disabled(!running && (session.presentation?.timedRow != nil || row.state == .locked))
        .contextMenu {
            if running {
                Button("Cancel timer", systemImage: "xmark", role: .destructive) {
                    session.run(.cancelTimedSet(row.id))
                }
            }
        }
        .accessibilityLabel(running ? "Stop \(exercise), \(row.label.spoken)" : "Start the timer for \(exercise), \(row.label.spoken)")
    }
}

/// The time a running timer shows: what is left, then `+0:05` in orange
/// past the target; a stopwatch counts up (DESIGN.md §7.6).
///
/// The digits are `Text(timerInterval:)`, which the system ticks; the switch
/// from countdown to overtime is a `TimelineView` that redraws every second
/// from the start, so it flips at the target to the second. Never an
/// `.explicit([endsAt])` schedule: SwiftUI draws its first entry at once,
/// even in the future, which put a fresh timer in overtime ("+0:00", frozen).
struct RunningTime: View {
    let startedAt: Date
    let endsAt: Date?
    /// Past the target: orange in the grid, white on the bar's orange pill.
    var overtimeColor: Color = .orange
    /// "left" with the countdown; the bar's pill has its own label.
    var showsLeft = true
    var font: Font = .body.monospacedDigit().weight(.semibold)

    var body: some View {
        Group {
            if let endsAt {
                TimelineView(.periodic(from: startedAt, by: 1)) { context in
                    if context.date < endsAt {
                        countdown(to: endsAt)
                    } else {
                        // `+` and the time past the target, counting up from it.
                        HStack(spacing: 0) {
                            Text("+")
                            Text(timerInterval: endsAt ... Date.distantFuture, countsDown: false)
                        }
                        .foregroundStyle(overtimeColor)
                        .accessibilityElement(children: .ignore)
                        .accessibilityLabel("Past the target by")
                        .accessibilityValue(Text(timerInterval: endsAt ... Date.distantFuture, countsDown: false))
                    }
                }
            } else {
                Text(timerInterval: startedAt ... Date.distantFuture, countsDown: false)
            }
        }
        .font(font)
        .lineLimit(1)
        .minimumScaleFactor(0.6)
    }

    @ViewBuilder
    private func countdown(to endsAt: Date) -> some View {
        if showsLeft {
            // "1:07 left" on one line when the cell has the room; else "left"
            // small under the time, which the 44 pt cell holds, so the time
            // is never shrunk to make room for the word.
            ViewThatFits(in: .horizontal) {
                HStack(alignment: .firstTextBaseline, spacing: 3) {
                    CountdownText(start: startedAt, end: endsAt)
                    Text("left")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                VStack(spacing: -1) {
                    CountdownText(start: startedAt, end: endsAt)
                    Text("left")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Time left")
            .accessibilityValue(Text(timerInterval: min(startedAt, endsAt) ... endsAt, countsDown: true))
        } else {
            CountdownText(start: startedAt, end: endsAt)
        }
    }
}

/// A carry's stopwatch, under its row (§7.6): the row has no TIME column.
struct CarryStopwatch: View {
    let row: SetRowPresentation
    let exercise: String
    let timer: TimedSetState
    var focus: FocusState<CoverModel.Field?>.Binding

    var body: some View {
        HStack(spacing: 8) {
            switch timer {
            case .ready:
                TimerButton(row: row, exercise: exercise, running: false)
                Text("Stopwatch")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            case let .running(startedAt, endsAt):
                TimerButton(row: row, exercise: exercise, running: true)
                RunningTime(startedAt: startedAt, endsAt: endsAt)
            case let .done(duration):
                if let duration {
                    Text(Format.duration(duration))
                        .font(.footnote.monospacedDigit())
                        .foregroundStyle(.secondary)
                }
            }
        }
    }
}
