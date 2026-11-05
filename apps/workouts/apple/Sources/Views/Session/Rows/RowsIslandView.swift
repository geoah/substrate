import SwiftUI
import WorkoutsCore

/// The floating island at the bottom of the Rows layout (DESIGN.md §7.6a),
/// on Liquid Glass: a stack of up to three parts, each drawn only when it
/// applies (`RowsIsland.parts(editorInView:)`).
///
/// 1. The rest: a ring and the time left, `of 2:00`, −15, +15 and Skip.
/// 2. The next set, compact: its name (a tap brings the full editor into
///    view), its values with steppers where they fit (a tap on a number
///    types it), its reps left (a tap opens their steps in the island) and a Log
///    check; Start or Stop and the clock for a timed set. At the
///    accessibility sizes only the name and Log (or Stop and its clock).
///    Drawn only while the open editor is scrolled out of view, so logging
///    never needs a scroll back, and never twice on screen.
/// 3. The set after that, on one line; a tap takes the list to it and marks
///    it for a moment. It never opens it (owner, 2026-11-02).
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
    /// Takes the list to a set and marks it ("Then …").
    let showLine: (RowID) -> Void
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

    /// `Then  Calf Press (Machine), set 1, 60 kg × 12  ›`: a tap takes the
    /// list to that set; the open set stays open.
    @ViewBuilder
    private func afterLine(_ after: RowsAfterPart) -> some View {
        let content = HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(after.label)
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.accentColor)
            // Wraps between words rather than cutting a name in the middle;
            // only what two lines (three at the accessibility sizes) cannot
            // hold is cut, at its end.
            Text(after.text)
                .font(.subheadline)
                .foregroundStyle(.primary)
                .lineLimit(typeSize.isAccessibilitySize ? 3 : 2)
                .truncationMode(.tail)
            Spacer(minLength: 4)
            if after.row != nil {
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
            }
        }
        .padding(.horizontal, 16)
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .contentShape(.rect)
        if let row = after.row {
            Button {
                showLine(row)
            } label: {
                content
            }
            .buttonStyle(.plain)
            .accessibilityLabel(after.sentence)
            .accessibilityHint("Shows that set in the list")
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
                CountdownText(start: rest.startedAt, end: rest.endsAt, alignment: .trailing)
                    .font(.title.weight(.semibold).monospacedDigit())
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

/// The next set, compact: `A2 Bird Dog  Round 1  ⌃` on a line of its own
/// (the name cut in its middle; a tap brings the full editor into view),
/// then its values, each with − and + (a tap on a number types it), its reps
/// left (a tap opens their steps in the island) and Log as a check, on one
/// row where they fit. Where they do not, the values take the first row and
/// reps left the second, with Log at its right edge; narrower still (larger text)
/// each value takes a line of its own, still with its steppers, and the
/// number shrinks between them before a stepper ever goes: the steppers
/// never give way to reps left (owner, 2026-11-02). A timed set's clock leads
/// the values. At the accessibility sizes it docks only the name and Log, or
/// Stop and its clock, so the island never takes half the screen. Every
/// target stays at least 44 pt: the layout gives way, never the targets.
struct RowsNextPartView: View {
    let next: RowsNextPart
    let perform: (RowsAction) -> Void
    let showOpenRow: () -> Void

    @Environment(SessionModel.self) private var session
    @Environment(CoverModel.self) private var cover
    @Environment(RowsScreenModel.self) private var screen
    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .body) private var scaledStepWidth: CGFloat = 44

    private var stepWidth: CGFloat { max(44, scaledStepWidth) }

    private var tint: Color { RowsStyle.tint(next.tint) }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            title
            if typeSize.isAccessibilitySize {
                HStack(spacing: 8) {
                    clock
                    Spacer(minLength: 0)
                    primary
                }
            } else {
                ViewThatFits(in: .horizontal) {
                    // Wide: the values, reps left and Log on one row, packed
                    // (4 pt gaps, Log right-aligned without a spacer's two
                    // gaps) so a load and reps fit Plus and Pro Max phones.
                    HStack(spacing: 4) {
                        HStack(spacing: 4) {
                            valuesRow
                            rpeButton
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        primary
                    }
                    // Narrow: the values, then reps left with Log at the right edge.
                    VStack(alignment: .leading, spacing: 6) {
                        HStack(spacing: 6) {
                            valuesRow
                        }
                        logRow
                    }
                    // Larger text: each value on a line of its own, steppers kept.
                    VStack(alignment: .leading, spacing: 6) {
                        stacked
                        logRow
                    }
                }
                if screen.islandRPE == next.row, let effort = showsEffort {
                    effortPicker(effort)
                        .transition(.opacity)
                }
            }
        }
        .padding(.leading, 12)
        .padding(.trailing, 10)
        .padding(.top, 2)
        .padding(.bottom, 10)
        // Its full height always: the bar never squeezes a stacked set's targets.
        .fixedSize(horizontal: false, vertical: true)
        .accessibilityElement(children: .contain)
    }

    /// The name and the set on a line of their own (a tap brings the full
    /// editor into view); at the accessibility sizes the name takes a line
    /// of its own and the set the next, so it is never cut to nothing.
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
                            chevron
                        }
                    }
                } else {
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        badge
                        // A long name wraps between its words, never cut in its middle.
                        exerciseName
                            .lineLimit(2)
                            .truncationMode(.tail)
                        setLabel
                        chevron
                        Spacer(minLength: 0)
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

    private var badge: some View {
        RowsSetLabel(text: next.setLabel, isWarmup: next.isWarmup, emphasis: .now)
    }

    private var exerciseName: some View {
        Text(next.exercise)
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(.primary)
    }

    @ViewBuilder
    private var setLabel: some View {
        if !next.label.isEmpty {
            Text(next.label)
                .font(.subheadline)
                .foregroundStyle(Color.secondary)
                .lineLimit(1)
                .layoutPriority(1)
        }
    }

    private var chevron: some View {
        Image(systemName: "chevron.up")
            .font(.footnote.weight(.semibold))
            .foregroundStyle(.tertiary)
            .accessibilityHidden(true)
    }

    /// The clock and each value with its steppers, side by side.
    @ViewBuilder
    private var valuesRow: some View {
        clock
        ForEach(next.fields) { field in
            compactField(field, steppers: true)
        }
    }

    /// Reps left and, at the row's right edge, Log.
    private var logRow: some View {
        HStack(spacing: 6) {
            rpeButton
            Spacer(minLength: 0)
            primary
        }
    }

    /// The values when one row cannot hold them: the clock, then each value
    /// on a line of its own, still with its steppers; the number shrinks
    /// between them before a stepper ever goes.
    private var stacked: some View {
        VStack(alignment: .leading, spacing: 6) {
            clock
            ForEach(next.fields) { field in
                ViewThatFits(in: .horizontal) {
                    compactField(field, steppers: true)
                    compactField(field, steppers: true, fitsWidth: true)
                }
            }
        }
    }

    /// The reps left the island shows: as the panel shows them, so a
    /// warm-up's or a timed set's stay out until the owner opens them there.
    private var showsEffort: RowsEffort? {
        guard let effort = next.effort, !effort.isCollapsed || screen.openStrips.contains(next.row) else { return nil }
        return effort
    }

    /// `2 left`, filled once rated and outlined while it is the plan's; a tap
    /// opens the steps and why the set ended.
    @ViewBuilder
    private var rpeButton: some View {
        if let effort = showsEffort {
            let open = screen.islandRPE == next.row
            Button {
                withAnimation(.smooth(duration: 0.2)) { screen.islandRPE = open ? nil : next.row }
            } label: {
                Text(next.rpe ?? effort.addTitle)
                    .font(.body.weight(.semibold).monospacedDigit())
                    .foregroundStyle(next.rpe == nil ? Color.secondary : tint)
                    .lineLimit(1)
                    .fixedSize()
                    .padding(.horizontal, 8)
                    .frame(minWidth: 44, minHeight: 44)
                    .background(tint.opacity(effort.isRated || open ? 0.13 : 0), in: .capsule)
                    .overlay {
                        if !effort.isRated, !open {
                            Capsule().strokeBorder(tint.opacity(0.45), style: StrokeStyle(lineWidth: 1.5, dash: [4, 3]))
                        }
                    }
                    .contentShape(.capsule)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Reps left")
            .accessibilityValue(effort.accessibilityValue)
            .accessibilityHint(open ? "Hides the steps" : "Shows the steps and why the set ended")
        }
    }

    /// Reps left in the island: `−1 | +1` either side of the number, and why
    /// the set ended beside them; the steps keep it open, the reason closes it.
    private func effortPicker(_ effort: RowsEffort) -> some View {
        let label = Text("\(effort.value ?? "\u{2013}") \(effort.unit)")
            .font(.subheadline.weight(.semibold).monospacedDigit())
            .foregroundStyle(effort.value == nil ? Color.secondary : tint)
            .lineLimit(1)
            .fixedSize()
            .accessibilityHidden(true)
        let steps = RowsStepper(minus: effort.minus, plus: effort.plus, tint: tint) { patch in
            screen.apply(patch, row: next.row, mode: .log, session: session)
        }
        let stop = effort.stop.map { menu in
            RowsStopMenuChip(menu: menu, tint: tint) { patch in
                screen.apply(patch, row: next.row, mode: .log, session: session)
                withAnimation(.smooth(duration: 0.2)) { screen.islandRPE = nil }
            }
        }
        return ViewThatFits(in: .horizontal) {
            HStack(spacing: 8) {
                label
                steps
                Spacer(minLength: 0)
                stop
            }
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 8) {
                    label
                    Spacer(minLength: 0)
                    steps
                }
                stop
            }
        }
        .padding(.top, 2)
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
                // Between steppers the 44 pt step targets give the number its air.
                .padding(.horizontal, steppers ? 0 : 10)
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

    /// The check (play, stop for a timed set), alone: the room goes to the values and reps left.
    private var primary: some View {
        Button {
            screen.perform(next.primary.action, session: session, cover: cover, loggedText: next.loggedText)
        } label: {
            Image(systemName: next.primary.action.symbol)
                .font(.body.weight(.bold))
                .frame(width: 20, height: 20)
        }
        // Exactly 44 pt wide: 20 for the glyph, 12 either side.
        .buttonStyle(RowsFilledButtonStyle(fill: RowsStyle.buttonFill(next.tint), minHeight: 44, horizontalPadding: 12))
        .frame(minWidth: 44, minHeight: 44)
        .disabled(!next.primary.isEnabled)
        // "Log", then what it logs: `105 kg × 10 @ 8`.
        .accessibilityLabel(next.primary.title)
        .accessibilityValue(spokenValues)
    }

    private var spokenValues: String {
        let label = next.primary.accessibilityLabel
        guard label.hasPrefix(next.primary.title) else { return label }
        return String(label.dropFirst(next.primary.title.count)).trimmingCharacters(in: .whitespaces)
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
