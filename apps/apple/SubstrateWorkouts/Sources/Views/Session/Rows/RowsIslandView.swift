import WorkoutsCore
import SwiftUI

/// The floating island at the bottom of the Rows layout (DESIGN.md §7.6a),
/// on Liquid Glass: a stack of up to three parts, each drawn only when it
/// applies (`RowsIsland.parts(editorInView:)`).
///
/// 1. The rest: a ring and the time left, `of 2:00`, −15, +15 and Skip.
/// 2. The next set, compact: its name and RPE (a tap brings the full editor
///    into view), its values with steppers (a tap on a number types it) and
///    Log; Start or Stop and the clock for a timed set. Drawn only while
///    the open editor is scrolled out of view, so logging never needs a
///    scroll back, and never twice on screen.
/// 3. The set after that, on one line; a tap opens it when it can open now,
///    else it only reads (no chevron).
///
/// Parts come and go with a fade and a small rise (a fade alone under Reduce
/// Motion). Nothing here counts up for a set that is not timed.
struct RowsIslandView: View {
    let island: RowsIsland
    /// The open editor is on screen: the list shows the next set, the island leaves it out.
    let editorInView: Bool
    let perform: (RowsAction) -> Void
    /// Brings the open set's full editor into view.
    let showOpenRow: () -> Void
    let finish: () -> Void
    let addExercise: () -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var typeSize

    private var parts: [RowsIslandPart] { island.parts(editorInView: editorInView) }

    var body: some View {
        if !parts.isEmpty {
            VStack(spacing: 0) {
                ForEach(Array(parts.enumerated()), id: \.element) { index, part in
                    if index > 0 {
                        Divider().padding(.horizontal, 14)
                    }
                    partView(part)
                        .transition(transition)
                }
            }
            .glassEffect(.regular, in: .rect(cornerRadius: 28))
            .animation(reduceMotion ? .easeInOut(duration: 0.2) : .smooth(duration: 0.3), value: parts)
            .accessibilityElement(children: .contain)
        }
    }

    private var transition: AnyTransition {
        reduceMotion ? .opacity : .opacity.combined(with: .scale(scale: 0.96, anchor: .bottom))
    }

    @ViewBuilder
    private func partView(_ part: RowsIslandPart) -> some View {
        switch part {
        case .rest:
            if let rest = island.rest { RowsRestPartView(rest: rest, perform: perform) }
        case .next:
            if let next = island.next { RowsNextPartView(next: next, perform: perform, showOpenRow: showOpenRow) }
        case .after:
            if let after = island.after { afterLine(after) }
        case .allDone:
            if case let .allDone(title, detail) = island.end {
                HStack(spacing: 10) {
                    Image(systemName: "checkmark")
                        .font(.body.weight(.bold))
                        .foregroundStyle(.white)
                        .frame(width: 36, height: 36)
                        .background(.green, in: .circle)
                        .accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(title).font(.subheadline.weight(.semibold))
                        Text(detail).font(.footnote).foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 4)
                    Button("Finish", action: finish)
                        .buttonStyle(RowsFilledButtonStyle(fill: .accentColor, minHeight: 44))
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
            }
        case .empty:
            Button(action: addExercise) {
                Label("Add an exercise to begin", systemImage: "plus")
                    .font(.subheadline.weight(.semibold))
                    .frame(maxWidth: .infinity, minHeight: 52)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .foregroundStyle(Color.accentColor)
        }
    }

    /// `Then  Calf Press (Machine), set 1, 60 kg × 12  ›`: a tap opens that set.
    @ViewBuilder
    private func afterLine(_ after: RowsAfterPart) -> some View {
        let content = HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(after.label)
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.accentColor)
            Text(after.text)
                .font(.subheadline)
                .foregroundStyle(.primary)
                .lineLimit(typeSize.isAccessibilitySize ? 2 : 1)
                .truncationMode(.middle)
            Spacer(minLength: 4)
            if after.action != nil {
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
            }
        }
        .padding(.horizontal, 16)
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .contentShape(.rect)
        if let action = after.action {
            Button { perform(action) } label: { content }
                .buttonStyle(.plain)
                .accessibilityLabel(after.sentence)
                .accessibilityHint("Opens that set")
        } else {
            content
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(after.sentence)
        }
    }
}

/// The rest part: the ring and the time left centred on one axis, `of 2:00`
/// on the countdown's baseline, then −15, +15 and Skip. At larger text sizes
/// the buttons go under the time.
struct RowsRestPartView: View {
    let rest: RowsRestPart
    let perform: (RowsAction) -> Void

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 8) {
                clock(showsTotal: true)
                Spacer(minLength: 4)
                controls
            }
            HStack(spacing: 8) {
                clock(showsTotal: false)
                Spacer(minLength: 4)
                controls
            }
            VStack(alignment: .leading, spacing: 8) {
                clock(showsTotal: true)
                controls
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }

    private func clock(showsTotal: Bool) -> some View {
        // The ring has no baseline: centre it against the text, and keep the
        // text's own baseline between the countdown and its total.
        HStack(alignment: .center, spacing: 8) {
            RowsRestRing(startedAt: rest.startedAt, endsAt: rest.endsAt)
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                // The widest the countdown gets, so it never pushes the buttons.
                ReservedWidth(template: rest.endsAt.timeIntervalSince(rest.startedAt) >= 600 ? "00:00" : "0:00") {
                    CountdownText(start: rest.startedAt, end: rest.endsAt)
                }
                .font(.title.weight(.semibold).monospacedDigit())
                .fixedSize()
                if showsTotal {
                    Text(rest.totalText)
                        .font(.subheadline.monospacedDigit())
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .fixedSize()
                }
            }
        }
        // The label says what the rest is; the value is the time left and the
        // total, so VoiceOver reads the countdown as the Table's bar does.
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(rest.title)
        // "1:48 left of 2:30".
        .accessibilityValue(Text("\(Text(timerInterval: min(rest.startedAt, rest.endsAt) ... rest.endsAt, countsDown: true)) left \(rest.totalText)"))
    }

    private var controls: some View {
        HStack(spacing: 8) {
            small(rest.minus)
            small(rest.plus)
            Button {
                perform(rest.skip.action)
            } label: {
                Text(rest.skip.title)
            }
            .buttonStyle(RowsFilledButtonStyle(fill: .accentColor, minHeight: 44, horizontalPadding: 14))
            .accessibilityLabel(rest.skip.accessibilityLabel)
        }
    }

    private func small(_ button: RowsButton) -> some View {
        Button {
            perform(button.action)
        } label: {
            Text(button.title)
                .font(.body.weight(.semibold).monospacedDigit())
                .foregroundStyle(Color.accentColor)
                .frame(minWidth: 52, minHeight: 44)
                .background(RowsStyle.card, in: .capsule)
                .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(button.accessibilityLabel)
    }
}

/// The next set, compact: `A2 Bird Dog  Round 1  @ 8  ⌃` over its values
/// with steppers and Log. When the row is too narrow the steppers go and a tap
/// on a number types it; when even that does not fit (the accessibility
/// sizes, a narrow phone) the clock, each value and Log take a line each.
/// Every target stays at least 44 pt: the layout gives way, never the targets.
struct RowsNextPartView: View {
    let next: RowsNextPart
    let perform: (RowsAction) -> Void
    let showOpenRow: () -> Void

    @Environment(SessionController.self) private var session
    @Environment(CoverModel.self) private var cover
    @Environment(RowsScreenModel.self) private var screen
    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .body) private var scaledStepWidth: CGFloat = 44

    private var stepWidth: CGFloat { max(44, scaledStepWidth) }

    private var tint: Color { RowsStyle.tint(next.tint) }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            title
            ViewThatFits(in: .horizontal) {
                controls(steppers: true)
                controls(steppers: false)
                stacked
            }
        }
        .padding(.leading, 14)
        .padding(.trailing, 10)
        .padding(.top, 2)
        .padding(.bottom, 10)
        // Its full height always: the bar never squeezes a stacked set's targets.
        .fixedSize(horizontal: false, vertical: true)
        .accessibilityElement(children: .contain)
    }

    /// The name, the set and its RPE on one line; at the accessibility sizes
    /// the name takes a line of its own, so it is never cut to nothing.
    private var title: some View {
        Button(action: showOpenRow) {
            Group {
                if typeSize.isAccessibilitySize {
                    VStack(alignment: .leading, spacing: 0) {
                        HStack(alignment: .firstTextBaseline, spacing: 6) {
                            badge
                            exerciseName.lineLimit(2)
                        }
                        HStack(alignment: .firstTextBaseline, spacing: 6) {
                            setLabel
                            Spacer(minLength: 4)
                            trailing
                        }
                    }
                } else {
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        badge
                        exerciseName.lineLimit(1)
                        setLabel
                        Spacer(minLength: 4)
                        trailing
                    }
                }
            }
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(next.sentence)
        .accessibilityHint("Shows every option for this set")
    }

    @ViewBuilder
    private var badge: some View {
        if let badge = next.badge {
            RowsBadge(text: badge, color: tint)
        }
    }

    private var exerciseName: some View {
        Text(next.exercise)
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(.primary)
    }

    private var setLabel: some View {
        Text(next.label)
            .font(.subheadline)
            .foregroundStyle(next.tint == .orange ? RowsStyle.orange : Color.secondary)
            .lineLimit(1)
            .layoutPriority(1)
    }

    @ViewBuilder
    private var trailing: some View {
        if let rpe = next.rpe {
            Text(rpe)
                .font(.subheadline.weight(.medium).monospacedDigit())
                .foregroundStyle(.secondary)
                .fixedSize()
                .accessibilityLabel("RPE \(rpe.dropFirst(2))")
        }
        Image(systemName: "chevron.up")
            .font(.footnote.weight(.semibold))
            .foregroundStyle(.tertiary)
            .accessibilityHidden(true)
    }

    private func controls(steppers: Bool) -> some View {
        HStack(spacing: 8) {
            values(steppers: steppers)
            Spacer(minLength: 0)
            primary
        }
    }

    /// The clock, each value and Log on a line of their own; a value keeps
    /// its steppers where its line has room for them.
    private var stacked: some View {
        VStack(alignment: .leading, spacing: 6) {
            clock
            ForEach(next.fields) { field in
                ViewThatFits(in: .horizontal) {
                    compactField(field, steppers: true)
                    compactField(field, steppers: false)
                    compactField(field, steppers: false, fitsWidth: true)
                }
            }
            primary
        }
    }

    @ViewBuilder
    private func values(steppers: Bool) -> some View {
        clock
        ForEach(next.fields) { field in
            compactField(field, steppers: steppers)
        }
    }

    @ViewBuilder
    private var clock: some View {
        if let start = next.timerStartedAt {
            TimelineView(.periodic(from: start, by: 1)) { context in
                HStack(spacing: 6) {
                    Circle().fill(.red).frame(width: 8, height: 8)
                    Text(Format.duration(max(0, Int(context.date.timeIntervalSince(start)))))
                        .font(.title2.weight(.semibold).monospacedDigit())
                        .fixedSize()
                }
            }
            .accessibilityElement(children: .combine)
        }
    }

    /// `− 105 kg +` in one tinted capsule; without steppers the number alone, a tap types it.
    /// `fitsWidth`: the last resort on a line of its own, the number shrinks to the line.
    private func compactField(_ field: RowsField, steppers: Bool, fitsWidth: Bool = false) -> some View {
        let typing = screen.typing.flatMap { $0.row == next.row && $0.field == field.field ? $0.text : nil }
        return HStack(spacing: 0) {
            if steppers { step(field.minus, fallback: "\u{2212}") }
            Button {
                screen.openKeypad(field, row: next.row, mode: .log, fromIsland: true)
            } label: {
                HStack(alignment: .firstTextBaseline, spacing: 3) {
                    Text(typing.flatMap { $0.isEmpty ? nil : $0 } ?? field.value ?? field.placeholder ?? "\u{2013}")
                        .font(.body.weight(.semibold).monospacedDigit())
                        .foregroundStyle(field.isChanged ? tint : Color.primary)
                    if let unit = field.unit, field.value != nil {
                        Text(unit)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
                .lineLimit(1)
                .minimumScaleFactor(fitsWidth ? 0.5 : 1)
                .fixedSize(horizontal: !fitsWidth, vertical: false)
                .padding(.horizontal, steppers ? 2 : 12)
                .frame(minWidth: 44, minHeight: 44)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(field.accessibilityLabel)
            .accessibilityValue(field.accessibilityValue)
            .accessibilityHint("Opens a keypad")
            if steppers { step(field.plus, fallback: "+") }
        }
        .background(tint.opacity(0.13), in: .capsule)
        .overlay {
            if field.isMissing {
                Capsule().strokeBorder(RowsStyle.orange, lineWidth: 1.5)
            }
        }
    }

    private func step(_ step: RowsStep?, fallback: String) -> some View {
        Button {
            if let step { screen.apply(step.patch, row: next.row, mode: .log, session: session) }
        } label: {
            Text(step?.symbol ?? fallback)
                .font(.body.weight(.semibold))
                .foregroundStyle(step == nil ? Color(.tertiaryLabel) : tint)
                .frame(width: stepWidth, height: 44)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .buttonRepeatBehavior(.enabled)
        .disabled(step == nil)
        .accessibilityLabel(step?.accessibilityLabel ?? "")
        .accessibilityHidden(step == nil)
    }

    private var primary: some View {
        Button {
            screen.perform(next.primary.action, session: session, cover: cover, loggedText: next.loggedText)
        } label: {
            Label(next.primary.title, systemImage: next.primary.action.symbol)
                .labelStyle(.titleAndIcon)
                .lineLimit(1)
                .fixedSize()
        }
        .buttonStyle(RowsFilledButtonStyle(fill: RowsStyle.buttonFill(next.tint), minHeight: 44, horizontalPadding: 12))
        .disabled(!next.primary.isEnabled)
        .accessibilityLabel(next.primary.accessibilityLabel)
    }
}

/// The rest's ring: what is left of it, redrawn once a second.
struct RowsRestRing: View {
    let startedAt: Date
    let endsAt: Date

    @ScaledMetric(relativeTo: .title) private var size: CGFloat = 36

    var body: some View {
        TimelineView(.periodic(from: startedAt, by: 1)) { context in
            let total = max(1, endsAt.timeIntervalSince(startedAt))
            let left = min(1, max(0, endsAt.timeIntervalSince(context.date) / total))
            ZStack {
                Circle().stroke(Color(.tertiarySystemFill), lineWidth: 4)
                Circle()
                    .trim(from: 0, to: left)
                    .stroke(Color.accentColor, style: StrokeStyle(lineWidth: 4, lineCap: .round))
                    .rotationEffect(.degrees(-90))
                    .animation(.linear(duration: 1), value: left)
            }
        }
        .frame(width: size, height: size)
        .padding(2)
        .accessibilityHidden(true)
    }
}

/// The keypad for a big jump (DESIGN.md §7.6a), floating on glass where the
/// island was: the nearby steps of the exercise's grid as chips that set
/// the value at once, digits, and a confirm button that names the value.
struct RowsKeypadPanel: View {
    let typing: RowsScreenModel.Typing
    let tint: Color
    let onKey: (String) -> Void
    let onPatch: (SetValuesPatch) -> Void
    let onCancel: () -> Void

    @ScaledMetric(relativeTo: .title2) private var keyHeight: CGFloat = 46

    private var keypad: RowsKeypad { typing.keypad }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(keypad.chipsTitle ?? keypad.title)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .padding(.leading, 8)
            if !keypad.chips.isEmpty {
                HStack(spacing: 6) {
                    ForEach(keypad.chips) { chip in
                        let on = typing.text.isEmpty ? chip.isCurrent : chip.text == typing.text
                        Button {
                            onPatch(chip.patch)
                        } label: {
                            Text(chip.text)
                                .font(.subheadline.weight(.semibold).monospacedDigit())
                                .foregroundStyle(on ? Color.white : Color.primary)
                                .lineLimit(1)
                                .minimumScaleFactor(0.7)
                                .frame(maxWidth: .infinity, minHeight: 44)
                                .background(on ? tint : keyFill, in: .capsule)
                                .contentShape(.capsule)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("Set \(chip.text)")
                    }
                }
            }
            Grid(horizontalSpacing: 7, verticalSpacing: 7) {
                ForEach([["1", "2", "3"], ["4", "5", "6"], ["7", "8", "9"], [".", "0", "delete"]], id: \.self) { keys in
                    GridRow {
                        ForEach(keys, id: \.self) { key in
                            keyButton(key)
                        }
                    }
                }
            }
            HStack(spacing: 8) {
                Button("Cancel", action: onCancel)
                    .buttonStyle(RowsTintedButtonStyle(tint: tint))
                let patch = keypad.patch(typing.text)
                Button {
                    if let patch { onPatch(patch) }
                } label: {
                    Text(keypad.confirmTitle(typing.text))
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(RowsFilledButtonStyle(fill: tint))
                .disabled(patch == nil)
            }
        }
        .padding(12)
        .padding(.bottom, 4)
        .glassEffect(.regular, in: .rect(cornerRadius: 34))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(keypad.title)
    }

    private var keyFill: Color { RowsStyle.card }

    @ViewBuilder
    private func keyButton(_ key: String) -> some View {
        let disabled = key == "." && !keypad.allowsDecimal
        Button {
            onKey(key)
        } label: {
            Group {
                if key == "delete" {
                    Image(systemName: "delete.left")
                        .font(.title3)
                } else {
                    Text(key)
                        .font(.title2.weight(.medium))
                }
            }
            .foregroundStyle(disabled ? Color(.quaternaryLabel) : Color.primary)
            .frame(maxWidth: .infinity, minHeight: keyHeight)
            .background(keyFill, in: .capsule)
            .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .disabled(disabled)
        .accessibilityLabel(key == "delete" ? "Delete" : key == "." ? "Point" : key)
    }
}

/// `Set 2 logged  Undo`, for five seconds after a Log.
struct RowsUndoToast: View {
    let undo: RowsScreenModel.Undo
    let onUndo: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            Text(undo.text)
                .font(.subheadline)
                .lineLimit(1)
            Spacer(minLength: 4)
            Button("Undo", action: onUndo)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.accentColor)
                .frame(minHeight: 44)
                .accessibilityLabel("Undo, \(undo.text)")
        }
        .padding(.horizontal, 18)
        .frame(minHeight: 44)
        .glassEffect(.regular, in: .capsule)
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }
}

