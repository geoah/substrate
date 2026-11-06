import SwiftUI
import WorkoutsCore

/// A planned set open in place on the routine page: the panel a session
/// opens on its set (DESIGN.md §7.6a), for the plan's targets. The header
/// (the label and the set), each target with its steppers, reps left among
/// them, the rest after the set, and Done; its `…` holds
/// the set's role, its place, More options (its label, how it ends) and
/// Delete. Every number and step comes from `RoutinePlanSetEditor`; each
/// step is kept in the draft at once.
struct PlanSetPanel<MenuItems: View>: View {
    let editor: RoutinePlanSetEditor
    let apply: (RoutinePlanChange) -> Void
    let customRest: () -> Void
    let close: () -> Void
    @ViewBuilder var menuItems: () -> MenuItems

    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .body) private var indent: CGFloat = 32

    private var tint: Color { editor.isWarmup ? RowsStyle.orange : .accentColor }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            Group {
                if let structure = editor.structure {
                    Text(structure)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .padding(.bottom, 4)
                }
                ForEach(editor.fields) { field in
                    PlanFieldRow(field: field, tint: tint) { apply($0) }
                }
                if let clear = editor.clearEffort {
                    Button {
                        Haptics.shared.play(.step)
                        apply(clear)
                    } label: {
                        Label("No target", systemImage: "xmark")
                            .labelStyle(.titleAndIcon)
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(tint)
                            .padding(.horizontal, 10)
                            .frame(minHeight: 28)
                            .background(tint.opacity(0.13), in: .capsule)
                            .frame(minHeight: 44)
                            .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("No reps left target")
                }
                rest
                    .padding(.top, 6)
            }
            .padding(.leading, typeSize.isAccessibilitySize ? 0 : indent)
            buttons
                .padding(.top, 10)
        }
        .padding(.top, 6)
        .padding(.horizontal, 12)
        .padding(.bottom, 10)
        .background(tint.opacity(editor.isWarmup ? 0.09 : 0.08), in: .rect(cornerRadius: RowsStyle.editorRadius))
        .overlay {
            RoundedRectangle(cornerRadius: RowsStyle.editorRadius)
                .strokeBorder(tint.opacity(editor.isWarmup ? 0.32 : 0.24), lineWidth: 1)
        }
        .padding(.horizontal, 4)
        .padding(.top, 2)
        .padding(.bottom, 4)
        .accessibilityElement(children: .contain)
    }

    private var header: some View {
        HStack(spacing: 8) {
            RowsGlyph(kind: editor.isWarmup ? .warmupNow : .now)
            RowsSetLabel(text: editor.setLabel, isWarmup: editor.isWarmup, emphasis: .now)
            Text(editor.title)
                .font(.body.weight(.semibold))
                .lineLimit(typeSize.isAccessibilitySize ? nil : 2)
                .accessibilityAddTraits(.isHeader)
            Spacer(minLength: 4)
            Menu {
                menuItems()
            } label: {
                RowsGlassIcon(systemImage: "ellipsis")
            }
            .rowsGlassCircle(tint: .secondary)
            .accessibilityLabel("\(editor.title) options")
        }
        .frame(minHeight: 44)
    }

    /// The rest after the set: its own, or the exercise's, with the presets.
    private var rest: some View {
        HStack(spacing: 8) {
            Text("Rest after")
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Spacer(minLength: 4)
            Menu {
                RestPresetItems(
                    current: editor.rest, defaultText: "Default (\(Format.rest(editor.restDefault)))",
                    select: { apply(.rest($0)) }, custom: customRest)
            } label: {
                HStack(spacing: 4) {
                    Text(editor.restText)
                        .font(.body.weight(.semibold).monospacedDigit())
                    Image(systemName: "chevron.up.chevron.down")
                        .font(.caption.weight(.semibold))
                }
                .foregroundStyle(tint)
                .padding(.horizontal, 14)
                .frame(minHeight: 44)
                .background(tint.opacity(0.13), in: .capsule)
                .contentShape(.capsule)
            }
            .accessibilityLabel("Rest after this set")
            .accessibilityValue(editor.restText)
        }
    }

    private var buttons: some View {
        Button(action: close) {
            Label("Done", systemImage: "checkmark")
                .frame(maxWidth: .infinity)
        }
        .buttonStyle(RowsFilledButtonStyle(fill: editor.isWarmup ? RowsStyle.orangeButton : .accentColor))
        .accessibilityHint("Closes the set; your changes are kept")
    }
}

/// One target: its label over the number and unit, and its stepper.
private struct PlanFieldRow: View {
    let field: RoutinePlanField
    let tint: Color
    let apply: (RoutinePlanChange) -> Void

    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .title) private var valueSize: CGFloat = 26

    var body: some View {
        let layout =
            typeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 6))
            : AnyLayout(HStackLayout(alignment: .center, spacing: 8))
        layout {
            VStack(alignment: .leading, spacing: 0) {
                Text(field.label)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                HStack(alignment: .firstTextBaseline, spacing: 5) {
                    Text(field.value)
                        .font(.system(size: valueSize, weight: .semibold).monospacedDigit())
                        .italic(!field.isSet)
                        .foregroundStyle(field.isSet ? Color.primary : Color.secondary)
                        .lineLimit(1)
                    if let unit = field.unit {
                        Text(unit)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(field.accessibilityLabel)
            .accessibilityValue(field.accessibilityValue)
            if !typeSize.isAccessibilitySize { Spacer(minLength: 4) }
            PlanStepper(minus: field.minus, plus: field.plus, tint: tint, apply: apply)
        }
        .frame(minHeight: 52)
    }
}

/// `−5 | +5` in one tinted capsule, as a session's stepper; holding a side
/// repeats it, and each step is a light tick.
private struct PlanStepper: View {
    let minus: RoutinePlanStep?
    let plus: RoutinePlanStep?
    let tint: Color
    let apply: (RoutinePlanChange) -> Void

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

    private func side(_ step: RoutinePlanStep?, fallback: String?) -> some View {
        Button {
            guard let step else { return }
            Haptics.shared.play(.step)
            apply(step.change)
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
