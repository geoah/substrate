import SwiftUI
import WorkoutsCore

/// The set open in place (DESIGN.md §7.6a): a tinted panel with a header
/// line (the set and why today's numbers), each value with last time beside
/// it and its steppers under the thumb, the RPE stops, a timed set's clock
/// and track, and a Log button that names exactly what it saves.
/// A done set being fixed uses the same panel, neutral, with Save and
/// Cancel; Delete set is in its `…`, as on every set (owner, 2026-11-02).
/// Every number, word and patch comes from `RowsEditor`.
struct RowsEditorPanel: View {
    let editor: RowsEditor
    /// The row as the Table reads it, for the set menu; nil hides the `…`.
    let row: SetRowPresentation?
    /// A rest runs before this set: the ring is hollow.
    var isResting = false
    /// A drop or a mini-set: the line hangs off the one above.
    var role: RowsRowRole = .working
    /// `Set 2`, `Bird Dog, set 1`: the set as said aloud, for the undo toast.
    var name = ""
    /// The open set's Log button came into view or left it: the island docks
    /// the set while it is out (§7.6a). Nil for a fix.
    var onLogVisibility: ((Bool) -> Void)?

    @Environment(SessionModel.self) private var session
    @Environment(CoverModel.self) private var cover
    @Environment(RowsScreenModel.self) private var screen
    @Environment(\.dynamicTypeSize) private var typeSize
    /// The widest value and its unit, so `last` lines up across the fields.
    @State private var valueWidths: [ValueField: CGFloat] = [:]
    @ScaledMetric(relativeTo: .title) private var minValueWidth: CGFloat = 84
    @ScaledMetric(relativeTo: .body) private var indent: CGFloat = 32

    private var tint: Color { RowsStyle.tint(editor.tint) }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            if let cue = editor.cue {
                // The slot's cue, beside the set it is for; the notes above the
                // block stay the longer rationale.
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Image(systemName: "lightbulb")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(tint)
                        .accessibilityHidden(true)
                    Text(cue)
                        .font(.footnote.weight(.medium))
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .padding(.leading, typeSize.isAccessibilitySize ? 0 : indent)
                .padding(.bottom, 4)
                .accessibilityElement(children: .combine)
                .accessibilityLabel("Cue: \(cue)")
            }
            // Running, the clock and the track replace the duration field.
            if let timer = editor.timer, timer.phase != .ready {
                RowsTimerView(timer: timer, tint: tint)
                    .padding(.leading, typeSize.isAccessibilitySize ? 0 : indent)
            }
            ForEach(editor.fields) { field in
                RowsFieldRow(
                    field: field,
                    tint: tint,
                    valueWidth: max(minValueWidth, valueWidths.values.max() ?? 0),
                    typing: screen.typing.flatMap { $0.row == editor.row && $0.field == field.field ? $0.text : nil },
                    onKeypad: { screen.openKeypad(field, in: editor) },
                    onPatch: { screen.apply($0, row: editor.row, mode: editor.mode, session: session) },
                    onMeasure: { valueWidths[field.field] = $0 }
                )
                .padding(.leading, typeSize.isAccessibilitySize ? 0 : indent)
            }
            // Ready, the track under the duration shows the goal and last time.
            if let timer = editor.timer, timer.phase == .ready {
                RowsTimerView(timer: timer, tint: tint)
                    .padding(.leading, typeSize.isAccessibilitySize ? 0 : indent)
            }
            if let effort = editor.effort, !effort.isCollapsed || screen.openStrips.contains(editor.row) {
                // A third value under load and reps, built like them, so the
                // number lines up with theirs and steps under the same thumb.
                RowsEffortRow(
                    effort: effort,
                    tint: tint,
                    valueWidth: max(minValueWidth, valueWidths.values.max() ?? 0),
                    onPatch: { screen.step($0, in: editor, session: session) }
                )
                .padding(.leading, typeSize.isAccessibilitySize ? 0 : indent)
            }
            buttons
                .padding(.top, 10)
        }
        .padding(.top, 6)
        .padding(.horizontal, 12)
        .padding(.bottom, 10)
        .background(background, in: .rect(cornerRadius: RowsStyle.editorRadius))
        .overlay {
            RoundedRectangle(cornerRadius: RowsStyle.editorRadius)
                .strokeBorder(stroke, lineWidth: editor.tint == .neutral ? 1.5 : 1)
        }
        .padding(.horizontal, 4)
        .padding(.top, 2)
        .padding(.bottom, 4)
        .accessibilityElement(children: .contain)
    }

    private var background: Color {
        switch editor.tint {
        case .accent: Color.accentColor.opacity(0.08)
        case .orange: Color.orange.opacity(0.09)
        case .neutral: RowsStyle.card
        }
    }

    private var stroke: Color {
        switch editor.tint {
        case .accent: Color.accentColor.opacity(0.24)
        case .orange: Color.orange.opacity(0.32)
        case .neutral: Color(.tertiaryLabel)
        }
    }

    // MARK: - Header

    /// The note beside the title when it fits, else on its own line under
    /// it. The RPE meaning shown for a moment takes the note's place without
    /// changing which: the panel never jumps.
    @ViewBuilder
    private var header: some View {
        if typeSize.isAccessibilitySize {
            // The title first, never squeezed to a letter a line by the
            // controls beside it; the controls under it, at the trailing edge.
            VStack(alignment: .leading, spacing: 2) {
                titleGroup
                    .fixedSize(horizontal: false, vertical: true)
                HStack(spacing: 8) {
                    Spacer(minLength: 0)
                    trailing
                }
                noteSlot(lines: nil)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(minHeight: 36)
        } else {
            compactHeader
        }
    }

    private var compactHeader: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 8) {
                titleGroup
                noteSlot(lines: 1)
                trailing
            }
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 8) {
                    titleGroup
                    Spacer(minLength: 4)
                    trailing
                }
                noteSlot(lines: typeSize.isAccessibilitySize ? nil : 2)
                    .padding(.leading, typeSize.isAccessibilitySize ? 0 : indent)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(minHeight: 36)
    }

    /// The note, sized by itself; the flash is drawn over its place.
    private func noteSlot(lines: Int?) -> some View {
        let flash = screen.flash.flatMap { $0.row == editor.row ? $0.text : nil }
        return note(lines: lines)
            .opacity(flash == nil ? 1 : 0)
            .frame(maxWidth: .infinity, alignment: .leading)
            .overlay(alignment: .leading) {
                if let flash {
                    Text(flash)
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(tint)
                        .lineLimit(lines)
                        .minimumScaleFactor(0.8)
                        .transition(.opacity)
                }
            }
            .animation(.smooth(duration: 0.2), value: flash)
    }

    private var titleGroup: some View {
        HStack(spacing: 10) {
            RowsGlyph(kind: glyph)
            HStack(alignment: .center, spacing: 7) {
                RowsSetLabel(
                    text: editor.setLabel, isWarmup: role == .warmup,
                    emphasis: editor.mode == .fix ? .done : .now)
                if let caption = editor.caption {
                    Text(caption)
                        .font(.body.weight(.semibold))
                        .foregroundStyle(role == .drop ? RowsStyle.orange : Color.primary)
                        .lineLimit(2)
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(editor.title)
            .accessibilityAddTraits(.isHeader)
        }
    }

    private var glyph: RowsGlyph.Kind {
        if editor.mode == .fix { return role == .warmup ? .warmupDone : .done }
        if role == .drop { return .hook }
        if isResting { return .waiting }
        return editor.tint == .orange ? .warmupNow : .now
    }

    @ViewBuilder
    private func note(lines: Int?) -> some View {
        if let note = editor.note {
            switch note.kind {
            case .reason:
                HStack(alignment: .firstTextBaseline, spacing: 4) {
                    Image(systemName: "sparkle")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(tint)
                        .accessibilityHidden(true)
                    Text(note.text)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(lines)
                }
            case .status:
                Text(note.text)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            case .running:
                HStack(spacing: 5) {
                    Circle().fill(.red).frame(width: 8, height: 8)
                    Text(note.text)
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(RowsStyle.red)
                }
            }
        }
    }

    @ViewBuilder
    private var trailing: some View {
        HStack(spacing: 8) {
            if let effort = editor.effort, effort.isCollapsed, !screen.openStrips.contains(editor.row) {
                Button {
                    screen.openStrips.insert(editor.row)
                } label: {
                    // A symbol and a short word, never wrapped.
                    Label(effort.addTitle, systemImage: "plus")
                        .labelStyle(.titleAndIcon)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(tint)
                        .lineLimit(1)
                        .fixedSize()
                        .padding(.horizontal, 10)
                        .frame(minHeight: 30)
                        .background(tint.opacity(0.13), in: .capsule)
                        .frame(minHeight: 44)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Add reps left")
            }
            if let trailingNote = editor.trailingNote {
                Text(trailingNote)
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(RowsStyle.orange)
                    .lineLimit(1)
            }
            if editor.mode == .fix, let cancel = editor.cancelTitle {
                Button(cancel) { screen.cancelFix() }
                    .font(.body)
                    .foregroundStyle(Color.accentColor)
                    // One word, never wrapped: the header moves its note down first.
                    .lineLimit(1)
                    .fixedSize()
                    .frame(minHeight: 44)
            }
            if let row {
                Menu {
                    SetMenuItems(
                        row: row,
                        untick: editor.mode == .fix ? { screen.untick(editor.row, name: name, session: session) } : nil,
                        delete: { screen.delete(editor.row, name: name, session: session) })
                } label: {
                    Image(systemName: "ellipsis")
                        .font(.subheadline.weight(.bold))
                        .foregroundStyle(tint)
                        .frame(width: 32, height: 32)
                        .background(tint.opacity(0.13), in: .circle)
                        .frame(width: 44, height: 44)
                        .contentShape(.rect)
                }
                .accessibilityLabel("\(editor.title) options")
                .accessibilityHint(editor.mode == .fix ? "Untick, delete, set kind, rest and note" : "Delete, set kind, rest and note")
            }
        }
    }

    // MARK: - Buttons

    @ViewBuilder
    private var buttons: some View {
        switch editor.mode {
        case .fix:
            primaryButton(fullWidth: true)
        case .log:
            if editor.timer != nil, let secondary = editor.secondary {
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 8) {
                        secondaryButton(secondary, fullWidth: false)
                        primaryButton(fullWidth: true)
                    }
                    VStack(spacing: 8) {
                        primaryButton(fullWidth: true)
                        secondaryButton(secondary, fullWidth: true)
                    }
                }
            } else {
                VStack(spacing: 4) {
                    primaryButton(fullWidth: true)
                    if let secondary = editor.secondary {
                        Button(secondary.title) { run(secondary.action) }
                            .font(.subheadline.weight(.medium))
                            .foregroundStyle(tint)
                            .frame(maxWidth: .infinity, minHeight: 40)
                            .disabled(!secondary.isEnabled)
                            .accessibilityLabel(secondary.accessibilityLabel)
                    }
                }
            }
        }
    }

    private func primaryButton(fullWidth: Bool) -> some View {
        let primary = editor.primary
        return Button {
            run(primary.action)
        } label: {
            Label(primary.title, systemImage: primary.action.symbol)
                .labelStyle(.titleAndIcon)
                .lineLimit(2)
                .multilineTextAlignment(.center)
                .frame(maxWidth: fullWidth ? .infinity : nil)
        }
        .buttonStyle(RowsFilledButtonStyle(fill: RowsStyle.buttonFill(editor.tint)))
        .disabled(!primary.isEnabled)
        .accessibilityLabel(primary.accessibilityLabel)
        // The dock follows this button alone, in whichever layout shows it:
        // not the secondary action beside or under it, not the padding.
        .modifier(RowsLogVisibility(viewport: screen.listViewport, action: onLogVisibility))
    }

    private func secondaryButton(_ button: RowsButton, fullWidth: Bool) -> some View {
        Button {
            run(button.action)
        } label: {
            Text(button.title)
                .lineLimit(2)
                .multilineTextAlignment(.center)
                .frame(maxWidth: fullWidth ? .infinity : nil)
        }
        .buttonStyle(RowsTintedButtonStyle(tint: tint))
        .disabled(!button.isEnabled)
        .accessibilityLabel(button.accessibilityLabel)
    }

    private func run(_ action: RowsAction) {
        screen.perform(action, session: session, cover: cover, loggedText: editor.loggedText)
    }
}

/// Reports whether the primary button (Log, Start, Stop) is in the list's viewport, on screen:
/// between the list's top and the bar over its foot (`RowsDock.isInView`). Geometry
/// reports on appearing too, so a panel laid out again says where it is at
/// once; a lazy list dropping it counts as out of view.
private struct RowsLogVisibility: ViewModifier {
    let viewport: ClosedRange<Double>
    let action: ((Bool) -> Void)?

    func body(content: Content) -> some View {
        if let action {
            content
                .onGeometryChange(for: Bool.self) { proxy in
                    let frame = proxy.frame(in: .global)
                    return RowsDock.isInView(Double(frame.minY) ... Double(frame.maxY), viewport: viewport)
                } action: { visible in
                    action(visible)
                }
                .onDisappear { action(false) }
        } else {
            content
        }
    }
}

// MARK: - A value field

/// One value: the big number (a tap opens the keypad), its unit, last time
/// at a fixed position, the reset chip, and the `−step +step` stepper. At
/// the accessibility text sizes the stepper goes under the number.
struct RowsFieldRow: View {
    let field: RowsField
    let tint: Color
    let valueWidth: CGFloat
    /// The keypad's text while it types into this field.
    let typing: String?
    let onKeypad: () -> Void
    let onPatch: (SetValuesPatch) -> Void
    let onMeasure: (CGFloat) -> Void

    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .title) private var valueSize: CGFloat = 26

    var body: some View {
        if typeSize.isAccessibilitySize {
            VStack(alignment: .leading, spacing: 6) {
                value
                HStack(spacing: 8) {
                    last
                    resetChip
                }
                RowsStepper(minus: field.minus, plus: field.plus, tint: tint, onPatch: onPatch)
            }
            .padding(.vertical, 6)
        } else {
            // `last` and the reset chip side by side, or the chip under
            // `last` when the row is too narrow: neither is ever cut.
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) {
                    // `last` on the number's baseline, not centred on its row.
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        value
                            .frame(minWidth: valueWidth, alignment: .leading)
                        last
                    }
                    resetChip
                    Spacer(minLength: 4)
                    RowsStepper(minus: field.minus, plus: field.plus, tint: tint, onPatch: onPatch)
                }
                HStack(spacing: 8) {
                    value
                        .frame(minWidth: valueWidth, alignment: .leading)
                    VStack(alignment: .leading, spacing: 2) {
                        last
                        resetChip
                    }
                    Spacer(minLength: 4)
                    RowsStepper(minus: field.minus, plus: field.plus, tint: tint, onPatch: onPatch)
                }
                // Larger text: the number beside its stepper, last time under them.
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 8) {
                        value
                        Spacer(minLength: 4)
                        RowsStepper(minus: field.minus, plus: field.plus, tint: tint, onPatch: onPatch)
                    }
                    HStack(spacing: 8) {
                        last
                        resetChip
                    }
                }
                .padding(.vertical, 4)
            }
            .frame(minHeight: 48)
        }
    }

    private var value: some View {
        Button(action: onKeypad) {
            HStack(alignment: .firstTextBaseline, spacing: 5) {
                number
                if let unit = field.unit, showsUnit {
                    Text(unit)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
            .onGeometryChange(for: CGFloat.self) {
                $0.size.width
            } action: {
                onMeasure($0)
            }
            // The number is about 31 pt tall; the target is 44 (the row is 48).
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(field.accessibilityLabel)
        .accessibilityValue(typing.map { $0.isEmpty ? field.accessibilityValue : "typing \($0)" } ?? field.accessibilityValue)
        .accessibilityHint("Opens a keypad")
    }

    /// A hint such as `≈15` reads with its unit, and so does an empty field
    /// that must be filled (`– reps` on a drop to failure): it says what it wants.
    private var showsUnit: Bool {
        field.value != nil || typing != nil || field.placeholder?.first == "\u{2248}" || field.isMissing
    }

    @ViewBuilder
    private var number: some View {
        let font = Font.system(size: valueSize, weight: .semibold).monospacedDigit()
        if let typing {
            HStack(spacing: 2) {
                Text(typing.isEmpty ? field.value ?? "" : typing)
                    .font(font)
                    .foregroundStyle(typing.isEmpty ? Color.secondary : Color.primary)
                Capsule()
                    .fill(tint)
                    .frame(width: 2, height: valueSize * 0.85)
            }
            .padding(.horizontal, 5)
            .padding(.vertical, 3)
            .background(RowsStyle.card, in: .rect(cornerRadius: 9))
            .overlay(RoundedRectangle(cornerRadius: 9).strokeBorder(tint, lineWidth: 2))
            .padding(.leading, -5)
        } else if let value = field.value {
            Text(value)
                .font(font)
                .foregroundStyle(field.isChanged ? tint : Color.primary)
                .underline(pattern: .dot, color: (field.isChanged ? tint : Color.primary).opacity(0.25))
                .lineLimit(1)
        } else {
            Text(field.placeholder ?? "\u{2013}")
                .font(.title3.italic())
                .foregroundStyle(GhostValueStyle.color)
                .underline(pattern: .dot, color: Color.primary.opacity(0.25))
                .lineLimit(1)
                .padding(.horizontal, field.isMissing ? 5 : 0)
                .padding(.vertical, field.isMissing ? 2 : 0)
                .overlay {
                    if field.isMissing {
                        RoundedRectangle(cornerRadius: 8).strokeBorder(RowsStyle.orange, lineWidth: 1.5)
                    }
                }
        }
    }

    @ViewBuilder
    private var last: some View {
        if let last = field.last {
            Text(last)
                .font(.subheadline.monospacedDigit())
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
    }

    @ViewBuilder
    private var resetChip: some View {
        if let reset = field.reset {
            Button {
                onPatch(reset.patch)
            } label: {
                HStack(spacing: 3) {
                    Image(systemName: "arrow.uturn.backward")
                        .font(.caption2.weight(.bold))
                    Text(reset.text)
                        .monospacedDigit()
                }
                .font(.footnote.weight(.semibold))
                .foregroundStyle(tint)
                .padding(.horizontal, 7)
                .padding(.vertical, 3)
                .background(tint.opacity(0.13), in: .capsule)
                .fixedSize()
                // A bigger target than the chip, without growing the row.
                .contentShape(.rect.inset(by: -10))
            }
            .buttonStyle(.plain)
            .lineLimit(1)
            .accessibilityLabel(reset.accessibilityLabel)
        }
    }
}

/// `−5 | +5` in one tinted capsule, under the right thumb. Holding a side
/// repeats it; each step is a light tick.
struct RowsStepper: View {
    let minus: RowsStep?
    let plus: RowsStep?
    let tint: Color
    let onPatch: (SetValuesPatch) -> Void

    @ScaledMetric(relativeTo: .body) private var width: CGFloat = 56
    @ScaledMetric(relativeTo: .body) private var height: CGFloat = 44

    var body: some View {
        HStack(spacing: 0) {
            side(minus, fallback: plus.map { "\u{2212}" + $0.title.dropFirst() })
            Rectangle()
                .fill(tint.opacity(0.25))
                .frame(width: 1, height: height * 0.45)
            side(plus, fallback: nil)
        }
        .background(tint.opacity(0.13), in: .capsule)
    }

    private func side(_ step: RowsStep?, fallback: String?) -> some View {
        Button {
            if let step { onPatch(step.patch) }
        } label: {
            Text(step?.title ?? fallback ?? "")
                .font(.body.weight(.semibold).monospacedDigit())
                .foregroundStyle(step == nil ? Color(.tertiaryLabel) : tint)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
                .frame(minWidth: width, minHeight: height)
                .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .buttonRepeatBehavior(.enabled)
        .disabled(step == nil)
        .accessibilityLabel(step?.accessibilityLabel ?? "")
        .accessibilityHidden(step == nil)
    }
}

// MARK: - Reps left

/// Reps left, a third value under load and reps: the number in a capsule,
/// hollow while it is the plan's value or a copy and filled once it is the
/// lifter's (a tap on the hollow one rates it as it is), `reps left`, last
/// time and the reset chip, and `−1 | +1` under the thumb. Under it, why the
/// set ended, a menu. Where one row cannot hold them (larger text) last time
/// and the chip go under the number; the steppers never give way.
struct RowsEffortRow: View {
    let effort: RowsEffort
    let tint: Color
    let valueWidth: CGFloat
    let onPatch: (SetValuesPatch) -> Void

    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .title) private var valueSize: CGFloat = 26

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            if typeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: 6) {
                    value
                    HStack(spacing: 8) {
                        last
                        resetChip
                    }
                    RowsStepper(minus: effort.minus, plus: effort.plus, tint: tint, onPatch: onPatch)
                }
                .padding(.vertical, 6)
            } else {
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 8) {
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            value
                                .frame(minWidth: valueWidth, alignment: .leading)
                            last
                        }
                        resetChip
                        Spacer(minLength: 4)
                        RowsStepper(minus: effort.minus, plus: effort.plus, tint: tint, onPatch: onPatch)
                    }
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 8) {
                            value
                            Spacer(minLength: 4)
                            RowsStepper(minus: effort.minus, plus: effort.plus, tint: tint, onPatch: onPatch)
                        }
                        HStack(spacing: 8) {
                            last
                            resetChip
                        }
                    }
                    .padding(.vertical, 4)
                }
                .frame(minHeight: 48)
            }
            if let stop = effort.stop {
                RowsStopMenuChip(menu: stop, tint: tint, onPatch: onPatch)
            }
        }
    }

    /// The number in its capsule and `reps left` after it.
    private var value: some View {
        HStack(alignment: .firstTextBaseline, spacing: 5) {
            Button {
                if let rate = effort.rate { onPatch(rate) }
            } label: {
                Text(effort.value ?? "\u{2013}")
                    .font(.system(size: valueSize, weight: .semibold).monospacedDigit())
                    .foregroundStyle(effort.value == nil ? Color.secondary : effort.isRated ? Color.white : tint)
                    .lineLimit(1)
                    .padding(.horizontal, 10)
                    .frame(minWidth: 44, minHeight: 44)
                    .background {
                        if effort.isRated {
                            Capsule().fill(tint)
                        } else if effort.value != nil {
                            Capsule().strokeBorder(tint, style: StrokeStyle(lineWidth: 1.5, dash: [4, 3]))
                        }
                    }
                    .contentShape(.capsule)
            }
            .buttonStyle(.plain)
            .disabled(effort.rate == nil)
            .accessibilityLabel(effort.accessibilityLabel)
            .accessibilityValue(effort.accessibilityValue)
            .accessibilityHint(effort.rate == nil ? "" : "Rates it as shown")
            Text(effort.unit)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .fixedSize()
                .accessibilityHidden(true)
        }
    }

    @ViewBuilder
    private var last: some View {
        if let last = effort.last {
            Text(last)
                .font(.subheadline.monospacedDigit())
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
    }

    @ViewBuilder
    private var resetChip: some View {
        if let reset = effort.reset {
            Button {
                onPatch(reset.patch)
            } label: {
                Label(reset.text, systemImage: "arrow.counterclockwise")
                    .labelStyle(.titleAndIcon)
                    .font(.footnote.weight(.semibold).monospacedDigit())
                    .foregroundStyle(tint)
                    .lineLimit(1)
                    .fixedSize()
                    .padding(.horizontal, 8)
                    .frame(minHeight: 28)
                    .background(tint.opacity(0.13), in: .capsule)
                    .frame(minHeight: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(reset.accessibilityLabel)
        }
    }
}

/// Why the set ended: `Why stop?` with a chevron, or the reason chosen
/// (pain in orange); a tap lists the three reasons and, with one chosen,
/// `No reason`.
struct RowsStopMenuChip: View {
    let menu: RowsStopMenu
    let tint: Color
    let onPatch: (SetValuesPatch) -> Void

    var body: some View {
        Menu {
            ForEach(menu.options) { option in
                Button {
                    onPatch(option.patch)
                } label: {
                    if option.isSelected {
                        Label(option.title, systemImage: "checkmark")
                    } else {
                        Text(option.title)
                    }
                }
            }
            if let clear = menu.clear {
                Divider()
                Button("No reason") { onPatch(clear) }
            }
        } label: {
            HStack(spacing: 4) {
                Text(menu.title)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.caption2.weight(.semibold))
                    .accessibilityHidden(true)
            }
            .font(.footnote.weight(.semibold))
            .foregroundStyle(color)
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, 10)
            .frame(minHeight: 30)
            .background(color.opacity(menu.selected == nil ? 0.08 : 0.15), in: .capsule)
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(menu.accessibilityLabel)
        .accessibilityValue(menu.accessibilityValue)
    }

    private var color: Color {
        switch menu.selected {
        case .pain?: RowsStyle.orange
        case nil: .secondary
        default: tint
        }
    }
}

// MARK: - Timed sets

/// A timed set's clock and track: the time held, a line beside it ("0:21 to
/// beat last time"), and the track with last time below and the goal above,
/// so the two labels never collide. Ready, the track shows where they are.
struct RowsTimerView: View {
    let timer: RowsTimer
    let tint: Color

    var body: some View {
        switch timer.phase {
        case .ready:
            RowsTrack(timer: timer, elapsed: 0, tint: tint)
                .padding(.vertical, 4)
        case let .running(startedAt, _):
            TimelineView(.periodic(from: startedAt, by: 1)) { context in
                let elapsed = max(0, Int(context.date.timeIntervalSince(startedAt)))
                VStack(alignment: .leading, spacing: 0) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        ReservedWidth(template: "00:00", alignment: .leading) {
                            Text(Format.duration(elapsed))
                        }
                        .font(.title.weight(.semibold).monospacedDigit())
                        .fixedSize()
                        Spacer(minLength: 4)
                        if let caption = timer.caption(elapsed: elapsed) {
                            Text(caption)
                                .font(.subheadline.monospacedDigit())
                                .foregroundStyle(.secondary)
                                .lineLimit(1)
                                .minimumScaleFactor(0.8)
                        }
                    }
                    .accessibilityElement(children: .combine)
                    RowsTrack(timer: timer, elapsed: elapsed, tint: tint)
                }
                .padding(.top, 4)
            }
        }
    }
}

/// 0:00 at the left, the fill so far, the goal's tick labelled above and
/// last time's labelled below.
struct RowsTrack: View {
    let timer: RowsTimer
    let elapsed: Seconds
    let tint: Color

    @ScaledMetric(relativeTo: .footnote) private var labelHeight: CGFloat = 18
    /// "0:00" in the footnote font and the gap after it: last time's label
    /// starts past it, never on it.
    @ScaledMetric(relativeTo: .footnote) private var zeroWidth: CGFloat = 36

    private static let trackHeight: CGFloat = 8
    private static let tickHeight: CGFloat = 18

    var body: some View {
        VStack(spacing: 0) {
            label(timer.goal, text: timer.goalText, color: tint, weight: .semibold, from: 0)
            // The ticks stand out of the track by the same amount above and
            // below, and the row is their height, so they never reach a label.
            GeometryReader { geometry in
                let width = geometry.size.width
                ZStack(alignment: .leading) {
                    Capsule().fill(Color(.tertiarySystemFill))
                        .frame(height: Self.trackHeight)
                    Capsule().fill(tint)
                        .frame(width: max(0, width * timer.fraction(elapsed)), height: Self.trackHeight)
                        .animation(.linear(duration: 1), value: elapsed)
                    if let goal = timer.goal {
                        tick(at: width * timer.fraction(goal), color: tint)
                    }
                    if let last = timer.last {
                        tick(at: width * timer.fraction(last), color: .secondary)
                    }
                }
                .frame(width: width, height: geometry.size.height, alignment: .leading)
            }
            .frame(height: Self.tickHeight)
            ZStack(alignment: .leading) {
                Text(Format.duration(0))
                    .font(.footnote.monospacedDigit())
                    .foregroundStyle(.secondary)
                label(timer.last, text: timer.lastText, color: .secondary, weight: .regular, from: zeroWidth)
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([timer.goalText, timer.lastText].compactMap { $0 }.joined(separator: ", "))
    }

    private func tick(at x: CGFloat, color: Color) -> some View {
        RoundedRectangle(cornerRadius: 1)
            .fill(color)
            .frame(width: 2, height: Self.tickHeight)
            .offset(x: x - 1)
    }

    /// A label centred on its tick, kept inside the track and right of
    /// `from`, by its own measured width.
    private func label(_ seconds: Seconds?, text: String?, color: Color, weight: Font.Weight, from: CGFloat)
        -> some View
    {
        GeometryReader { geometry in
            if let seconds, let text {
                let width = geometry.size.width
                let x = width * timer.fraction(seconds)
                Text(text)
                    .font(.footnote.weight(weight).monospacedDigit())
                    .foregroundStyle(color)
                    .lineLimit(1)
                    .fixedSize()
                    .alignmentGuide(.leading) { d in
                        -min(max(x - d.width / 2, from), max(from, width - d.width))
                    }
                    .frame(width: width, height: geometry.size.height, alignment: .leading)
            }
        }
        .frame(height: labelHeight)
    }
}
