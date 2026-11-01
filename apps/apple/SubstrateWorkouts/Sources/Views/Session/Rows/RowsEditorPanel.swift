import WorkoutsCore
import SwiftUI

/// The set open in place (DESIGN.md §7.6a): a tinted panel with a header
/// line (the set and why today's numbers), each value with last time beside
/// it and its steppers under the thumb, the full-width RPE strip, a timed
/// set's clock and track, and a Log button that names exactly what it saves.
/// A done set being fixed uses the same panel, neutral, with Save, Cancel
/// and Delete set. Every number, word and patch comes from `RowsEditor`.
struct RowsEditorPanel: View {
    let editor: RowsEditor
    /// The row as the Table reads it, for the set menu; nil hides the `…`.
    let row: SetRowPresentation?
    /// A rest runs before this set: the ring is hollow.
    var isResting = false
    /// A drop or a mini-set: the line hangs off the one above.
    var role: RowsRowRole = .working
    var groupColor: Color = .accentColor

    @Environment(SessionController.self) private var session
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
            if let strip = editor.rpe, !strip.isCollapsed || screen.openStrips.contains(editor.row) {
                // The panel's full inner width: a ninth of it is 44 pt on a 440 pt iPhone.
                RowsRPEStripView(strip: strip, tint: tint) { cell in
                    screen.pick(cell, in: editor, session: session)
                }
                .padding(.horizontal, -12)
                .padding(.top, 8)
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
    private var header: some View {
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
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                if let badge = editor.badge {
                    RowsBadge(text: badge, color: groupColor)
                }
                Text(editor.title)
                    .font(.body.weight(.semibold))
                    .foregroundStyle(editor.tint == .orange ? RowsStyle.orange : Color.primary)
                    .lineLimit(2)
            }
            .accessibilityElement(children: .combine)
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
            if let strip = editor.rpe, strip.isCollapsed, !screen.openStrips.contains(editor.row) {
                Button {
                    screen.openStrips.insert(editor.row)
                } label: {
                    Text(strip.addTitle)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(tint)
                        .padding(.horizontal, 11)
                        .frame(minHeight: 30)
                        .background(tint.opacity(0.13), in: .capsule)
                        .frame(minHeight: 44)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Add an RPE")
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
                    .frame(minHeight: 44)
            } else if let row {
                Menu {
                    SetMenuItems(row: row)
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
                .accessibilityHint("Set kind, rest and note")
            }
        }
    }

    // MARK: - Buttons

    @ViewBuilder
    private var buttons: some View {
        switch editor.mode {
        case .fix:
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 12) {
                    deleteButton
                    Spacer(minLength: 0)
                    primaryButton(fullWidth: false)
                }
                VStack(spacing: 8) {
                    primaryButton(fullWidth: true)
                    deleteButton
                }
            }
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

    @ViewBuilder
    private var deleteButton: some View {
        if let delete = editor.delete {
            Button(delete.title, role: .destructive) { run(delete.action) }
                .font(.subheadline.weight(.medium))
                .foregroundStyle(RowsStyle.red)
                .frame(minHeight: 44)
                .accessibilityLabel(delete.accessibilityLabel)
        }
    }

    private func run(_ action: RowsAction) {
        let logged = editor.badge.map { "\($0) \(editor.title) logged" } ?? "\(editor.title) logged"
        screen.perform(action, session: session, cover: cover, loggedText: logged)
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
                    value
                        .frame(minWidth: valueWidth, alignment: .leading)
                    last
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
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { onMeasure($0) }
            // The number is about 31 pt tall; the target is 44 (the row is 48).
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(field.accessibilityLabel)
        .accessibilityValue(typing.map { $0.isEmpty ? field.accessibilityValue : "typing \($0)" } ?? field.accessibilityValue)
        .accessibilityHint("Opens a keypad")
    }

    /// A hint such as `≈15` reads with its unit; words such as `to failure` do not.
    private var showsUnit: Bool {
        field.value != nil || typing != nil || field.placeholder?.first == "\u{2248}"
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

// MARK: - RPE

/// The full-width RPE strip, 6 to 10 in half steps: the plan's value an
/// outlined, hollow cell; a rating filled solid inside its cell. A tap on any
/// cell rates it (the outlined one too); a tap on the filled cell clears it.
/// Each cell is 48 pt tall and a ninth of the strip wide: 44 pt on a 440 pt
/// iPhone, 40 pt on a 402 pt one, 39 pt on a 390 pt one. It takes two rows
/// at the accessibility sizes, and where a cell would be narrower than 38 pt
/// (a 375 pt iPhone).
struct RowsRPEStripView: View {
    let strip: RowsRPEStrip
    let tint: Color
    let onPick: (RowsRPECell) -> Void

    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .body) private var height: CGFloat = 48

    /// The narrowest a cell gets in one row.
    private static let narrowestCell: CGFloat = 38

    var body: some View {
        Group {
            if typeSize.isAccessibilitySize {
                twoRows
            } else {
                // One row when nine cells of 38 pt fit, else two; chosen in
                // layout, so the strip never redraws from one to the other.
                ViewThatFits(in: .horizontal) {
                    row(strip.cells, narrowest: Self.narrowestCell)
                    twoRows
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(strip.accessibilityLabel)
        .accessibilityValue(strip.accessibilityValue)
    }

    private var twoRows: some View {
        VStack(spacing: 4) {
            row(Array(strip.cells.prefix(5)))
            row(Array(strip.cells.dropFirst(5)))
        }
    }

    private func row(_ cells: [RowsRPECell], narrowest: CGFloat = 0) -> some View {
        HStack(spacing: 0) {
            ForEach(cells) { cell in
                Button {
                    onPick(cell)
                } label: {
                    Text(cell.text)
                        .font(cell.isHalf ? .subheadline : .body.weight(.semibold))
                        .monospacedDigit()
                        .foregroundStyle(foreground(cell))
                        .lineLimit(1)
                        .minimumScaleFactor(0.7)
                        .frame(minWidth: narrowest, maxWidth: .infinity, minHeight: height)
                        .background {
                            switch cell.style {
                            case .filled:
                                RoundedRectangle(cornerRadius: 11).fill(tint).padding(3)
                            case .outlined:
                                RoundedRectangle(cornerRadius: 10).strokeBorder(tint, lineWidth: 2).padding(4)
                            case .plain:
                                EmptyView()
                            }
                        }
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(cell.meaning)
                .accessibilityValue(cell.style == .filled ? "rated" : cell.style == .outlined ? "shown, not rated" : "")
                .accessibilityAddTraits(cell.style == .filled ? .isSelected : [])
            }
        }
        .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 14))
    }

    private func foreground(_ cell: RowsRPECell) -> Color {
        switch cell.style {
        case .filled: .white
        case .outlined: tint
        case .plain: .primary
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

    var body: some View {
        VStack(spacing: 2) {
            label(timer.goal, text: timer.goalText, color: tint, weight: .semibold)
            GeometryReader { geometry in
                let width = geometry.size.width
                ZStack(alignment: .leading) {
                    Capsule().fill(Color(.tertiarySystemFill))
                    Capsule().fill(tint)
                        .frame(width: max(0, width * timer.fraction(elapsed)))
                        .animation(.linear(duration: 1), value: elapsed)
                    if let goal = timer.goal {
                        tick(at: width * timer.fraction(goal), color: tint)
                    }
                    if let last = timer.last {
                        tick(at: width * timer.fraction(last), color: .secondary)
                    }
                }
            }
            .frame(height: 8)
            ZStack(alignment: .leading) {
                Text(Format.duration(0))
                    .font(.footnote.monospacedDigit())
                    .foregroundStyle(.secondary)
                label(timer.last, text: timer.lastText, color: .secondary, weight: .regular)
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([timer.goalText, timer.lastText].compactMap { $0 }.joined(separator: ", "))
    }

    private func tick(at x: CGFloat, color: Color) -> some View {
        RoundedRectangle(cornerRadius: 1)
            .fill(color)
            .frame(width: 2, height: 18)
            .offset(x: x - 1)
    }

    /// A label centred on its tick, kept inside the track.
    private func label(_ seconds: Seconds?, text: String?, color: Color, weight: Font.Weight) -> some View {
        GeometryReader { geometry in
            if let seconds, let text {
                let x = geometry.size.width * timer.fraction(seconds)
                Text(text)
                    .font(.footnote.weight(weight).monospacedDigit())
                    .foregroundStyle(color)
                    .lineLimit(1)
                    .fixedSize()
                    .position(x: min(max(x, 36), max(36, geometry.size.width - 36)), y: geometry.size.height / 2)
            }
        }
        .frame(height: labelHeight)
    }
}
