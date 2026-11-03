import WorkoutsCore
import SwiftUI

/// One set of the grid (DESIGN.md §7.6). States: pending (ghost values in a
/// readable grey, regular weight, fields bordered), Up next (a leading accent
/// bar and the reason line), locked (a segment waiting for its link: fields
/// disabled), done (tinted, values bold, the tick filled green) and timing.
/// Swipe left removes it; a done set asks first.
struct SetRowView: View {
    let row: SetRowPresentation
    let card: ExerciseCard
    let layout: GridLayout
    var focus: FocusState<CoverModel.Field?>.Binding

    @Environment(SessionController.self) private var session
    @Environment(CoverModel.self) private var cover

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: layout.spacing) {
                SetKindMenu(row: row, card: card, layout: layout)
                if layout.stacksPrevious {
                    Spacer(minLength: 0)
                } else {
                    PreviousCell(row: row)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                ForEach(card.columns, id: \.self) { column in
                    if column == .time {
                        TimedCell(row: row, card: card, layout: layout, focus: focus)
                            .frame(width: layout.time)
                    } else if column.field != nil, let cell = row.cell(column) {
                        ValueCell(row: row, cell: cell, card: card, layout: layout, focus: focus)
                            .frame(width: layout.value)
                    }
                }
                if card.columns.contains(.rpe) {
                    RPECell(row: row, layout: layout)
                }
                TickButton(row: row, exercise: card.name, layout: layout, focus: focus)
            }
            .padding(.vertical, 3)
            .padding(.horizontal, 6)
            .background(rowBackground, in: .rect(cornerRadius: 10))
            .overlay(alignment: .leading) {
                if row.state == .upNext || row.state == .timing {
                    Capsule()
                        .fill(.tint)
                        .frame(width: 3)
                        .padding(.vertical, 6)
                        .accessibilityHidden(true)
                }
            }
            extras
                .padding(.leading, layout.set + layout.spacing + 6)
                .padding(.trailing, 6)
        }
        .opacity(row.state == .locked ? 0.5 : 1)
        .swipeActions(edge: .trailing, allowsFullSwipe: !row.isDone) {
            Button(role: .destructive) {
                if row.isDone {
                    cover.removeDoneRow = row.id
                } else {
                    session.run(.removeSet(row.id))
                }
            } label: {
                Label("Remove", systemImage: "trash")
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(row.label.spoken.capitalizedSentence)
    }

    private var rowBackground: Color {
        switch row.state {
        case .done: Color.green.opacity(0.14)
        case .upNext, .timing: Color.accentColor.opacity(0.06)
        case .pending, .locked: .clear
        }
    }

    private var isFocusedRow: Bool { focus.wrappedValue?.row == row.id }

    /// Lines under the row: PREVIOUS at the largest text sizes, the reason
    /// line (Up next only), a focused row's suggestion, the stop text, the
    /// RPE of a timed or distance set (its grid has no RPE column), a carry's
    /// stopwatch and the set's note.
    @ViewBuilder
    private var extras: some View {
        if layout.stacksPrevious, let previous = row.previous {
            Button {
                session.run(.copyPrevious(row.id))
            } label: {
                Text("Previous: \(previous.text)")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            .disabled(row.isDone || row.state == .locked)
        }
        if let reason = row.reason {
            Text(reason)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        } else if isFocusedRow, row.state != .done, let text = row.suggestion?.text {
            // §5.6's popover, as a line, so the keypad stays up.
            Text(text)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        if let stop = row.stopText {
            Label(stop, systemImage: "hand.raised")
                .font(.footnote)
                .foregroundStyle(.orange)
        }
        if let effort = row.effortText {
            // Under the row, so the TIME column keeps its width (§7.6).
            Text(effort)
                .font(.footnote.monospacedDigit())
                .foregroundStyle(.secondary)
                .accessibilityLabel(row.rpeAccessibilityLabel)
                .accessibilityValue(row.rpeAccessibilityValue)
        }
        if card.measurement.hasStopwatchBelowRow, let timer = row.timer, !row.isDone {
            CarryStopwatch(row: row, exercise: card.name, timer: timer, focus: focus)
        }
        if let notes = row.notes {
            Text(notes)
                .font(.footnote)
                .italic()
                .foregroundStyle(.secondary)
        }
    }
}

/// PREVIOUS (§5.4): last time's same set; a tap copies it into the row, RPE left out.
struct PreviousCell: View {
    let row: SetRowPresentation

    @Environment(SessionController.self) private var session

    var body: some View {
        if let previous = row.previous {
            Button {
                session.run(.copyPrevious(row.id))
            } label: {
                VStack(alignment: .leading, spacing: 0) {
                    Text(previous.text)
                        .font(.footnote.monospacedDigit())
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                        .minimumScaleFactor(0.8)
                    if previous.deload {
                        Text("deload")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
                .frame(maxWidth: .infinity, minHeight: 36, alignment: .leading)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .disabled(row.isDone || row.state == .locked)
            .accessibilityLabel("Previous, \(previous.text)\(previous.deload ? ", deload" : "")")
            .accessibilityHint(row.isDone ? "" : "Copies these values into the set")
        } else {
            Text("\u{2013}")
                .font(.footnote)
                .foregroundStyle(.tertiary)
                .accessibilityLabel("No previous set")
        }
    }
}

/// A value field: what was typed or done in black (done values bold), else
/// the ghost value as an overlay in a readable grey, regular weight, italic
/// for a hint (§5.5, §7.6). Not the text field's prompt: the system draws a
/// prompt in the placeholder grey, too faint in the gym and nearly invisible
/// in dark mode. A required field with nothing to commit is outlined.
struct ValueCell: View {
    let row: SetRowPresentation
    let cell: GridCell
    let card: ExerciseCard
    let layout: GridLayout
    var focus: FocusState<CoverModel.Field?>.Binding

    @Environment(CoverModel.self) private var cover

    private var field: CoverModel.Field { CoverModel.Field(row: row.id, column: cell.column) }

    var body: some View {
        let value = row.isDone ? cell.text : cell.entered
        let shown = cover.text(field, value: value)
        TextField(
            cell.column.title(unit: card.unit),
            text: Binding(get: { cover.text(field, value: value) }, set: { cover.setDraft($0, for: field) }),
            // An empty prompt: the ghost is the overlay below, never the title.
            prompt: Text(verbatim: "")
        )
        .keyboardType(GridValue.keyboard(for: cell.field))
        .multilineTextAlignment(.center)
        .font(.body.monospacedDigit().weight(row.isDone ? .semibold : .regular))
        .foregroundStyle(.primary)
        .lineLimit(1)
        // A load in another unit ("83.91 kg") still fits the cell at AX1.
        .minimumScaleFactor(0.5)
        .focused(focus, equals: field)
        .frame(maxWidth: .infinity, minHeight: layout.cellHeight)
        .background(fill, in: .rect(cornerRadius: 8))
        .overlay {
            if shown.isEmpty, let ghost {
                Text(ghost)
                    .font(.body.monospacedDigit())
                    .italic(cell.isHint)
                    .foregroundStyle(GhostValueStyle.color)
                    .lineLimit(1)
                    .minimumScaleFactor(0.5)
                    .padding(.horizontal, 3)
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
            }
        }
        .overlay {
            RoundedRectangle(cornerRadius: 8)
                .strokeBorder(border, lineWidth: cell.isMissing ? 1.5 : 1)
        }
        .disabled(row.state == .locked)
        .accessibilityLabel(cell.accessibilityLabel)
        // While typing, the draft (cleared: the ghost or "empty"); else what
        // the cell shows, the ghost as "suggested 40".
        .accessibilityValue(cell.accessibilityValue(draft: cover.draftText(field)))
    }

    /// The ghost or hint the empty field shows.
    private var ghost: String? {
        guard cell.isGhost || cell.isHint, let text = cell.text else { return nil }
        // "to failure" is wider than the cell: the wireframe's short form
        // (§7.6). VoiceOver still reads the whole words.
        return text == GhostValues.toFailure ? "to fail." : text
    }

    private var fill: Color {
        row.isDone ? .clear : Color(.systemBackground).opacity(0.6)
    }

    private var border: Color {
        if cell.isMissing { return .orange }
        if focus.wrappedValue == field { return .accentColor }
        return row.isDone ? .clear : Color(.separator)
    }
}

/// The RPE cell: the value, or a gauge to open the sheet (§7.6).
struct RPECell: View {
    let row: SetRowPresentation
    let layout: GridLayout

    @Environment(SessionController.self) private var session
    @Environment(CoverModel.self) private var cover

    var body: some View {
        Button {
            session.run(.touchRow(row.id))
            cover.sheet = .rpe(row.id)
        } label: {
            Group {
                if let rpe = row.rpeText {
                    Text(rpe)
                        .font(.body.monospacedDigit().weight(row.isDone ? .semibold : .regular))
                        .foregroundStyle(.primary)
                } else {
                    Image(systemName: "gauge.with.dots.needle.33percent")
                        .foregroundStyle(.secondary)
                }
            }
            .lineLimit(1)
            .minimumScaleFactor(0.7)
            .frame(width: layout.rpe, height: layout.cellHeight)
            .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 8))
        }
        .buttonStyle(.plain)
        .disabled(row.state == .locked)
        .accessibilityLabel(row.rpeAccessibilityLabel)
        .accessibilityValue(row.rpeAccessibilityValue)
    }
}

/// The tick (§7.6): commits what the row shows, after the entry check; a
/// filled tick unticks. While its timed set runs it is Stop.
struct TickButton: View {
    let row: SetRowPresentation
    /// The exercise, so VoiceOver says whose set it ticks.
    let exercise: String
    let layout: GridLayout
    var focus: FocusState<CoverModel.Field?>.Binding

    @Environment(SessionController.self) private var session
    @Environment(CoverModel.self) private var cover

    var body: some View {
        Button(action: tap) {
            Image(systemName: "checkmark")
                .font(.body.weight(.bold))
                .foregroundStyle(row.isDone ? Color.white : row.canTick || typedCouldComplete ? Color.primary : Color(.tertiaryLabel))
                .frame(width: layout.tick, height: layout.cellHeight)
                .background(row.isDone ? Color.green : Color(.tertiarySystemFill), in: .rect(cornerRadius: 8))
        }
        .buttonStyle(.plain)
        .disabled(!(row.isDone || row.canTick || typedCouldComplete))
        .accessibilityLabel(tickLabel)
    }

    private var tickLabel: String {
        let set = "\(exercise), \(row.label.spoken)"
        if row.isDone { return "Undo \(set)" }
        return row.state == .timing ? "Stop \(set)" : "Complete \(set)"
    }

    /// Only a value is missing and one is being typed: the tick sends it
    /// first, and the engine says no if it is still not complete.
    private var typedCouldComplete: Bool {
        !row.isDone && row.state != .locked && !row.missing.isEmpty && cover.hasDraft(forRow: row.id)
    }

    private func tap() {
        let id = row.id
        if row.isDone {
            session.run(.untick(id))
            return
        }
        let stop = row.state == .timing
        if focus.wrappedValue?.row == id { focus.wrappedValue = nil }
        Task {
            // A value still being typed is what the row shows: send it first.
            await cover.commitRow(id, session: session)
            await session.send(stop ? .stopTimedSet(id) : .tick(id))
        }
    }
}

extension String {
    /// "set 2" as a label reads "Set 2".
    var capitalizedSentence: String { prefix(1).uppercased() + dropFirst() }
}
