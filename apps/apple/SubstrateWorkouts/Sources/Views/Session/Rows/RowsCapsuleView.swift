import WorkoutsCore
import SwiftUI

/// The floating capsule at the bottom of the Rows layout (DESIGN.md §7.6a),
/// on Liquid Glass: while a rest counts down, the countdown with −15, +15
/// and Skip and what comes after; otherwise what is up now, in words, with
/// a clock counting up since the last set. It replaces the Table's rest bar;
/// the countdown coordinator, the notifications and the Live Activity are
/// the same for both.
struct RowsCapsuleView: View {
    let capsule: RowsCapsule
    let layout: RowsLayout
    let perform: (RowsAction) -> Void
    /// Brings the open row into view (a tap on the capsule).
    let showOpenRow: () -> Void
    let finish: () -> Void
    let addExercise: () -> Void

    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        Group {
            switch capsule {
            case let .resting(rest):
                RowsRestCapsuleView(rest: rest, perform: perform)
            case let .upNext(now), let .go(now):
                nowCapsule(now)
            case let .waiting(waiting):
                waitingCapsule(waiting)
            case let .timing(timing):
                capsuleRow {
                    icon("timer", fill: .accentColor)
                    texts(timing.title, timing.detail)
                    Spacer(minLength: 0)
                }
                .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 30))
                .onTapGesture(perform: showOpenRow)
                .accessibilityElement(children: .combine)
                .accessibilityAddTraits(.isButton)
                .accessibilityHint("Shows the timed set")
            case let .allDone(title, detail):
                capsuleRow {
                    icon("checkmark", fill: .green)
                    texts(title, detail)
                    Spacer(minLength: 4)
                    Button("Finish", action: finish)
                        .buttonStyle(RowsFilledButtonStyle(fill: .accentColor, minHeight: 44))
                }
                .glassEffect(.regular, in: .rect(cornerRadius: 30))
            case .empty:
                Button(action: addExercise) {
                    capsuleRow {
                        icon("plus", fill: .accentColor)
                        texts("Add an exercise to begin", nil)
                        Spacer(minLength: 0)
                    }
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 30))
            }
        }
        .animation(.smooth(duration: 0.25), value: capsule)
    }

    // MARK: - Forms

    private func nowCapsule(_ now: RowsNowCapsule) -> some View {
        Button(action: showOpenRow) {
            capsuleRow {
                icon(symbol(for: now), fill: color(for: now.tone))
                texts(now.title, now.detail)
                Spacer(minLength: 4)
                VStack(alignment: .trailing, spacing: 0) {
                    ReservedWidth(template: "00:00") {
                        // The session's clock stands still while it is paused, as the title's does.
                        Text(timerInterval: now.since ... Date.distantFuture, pauseTime: now.pausedAt, countsDown: false)
                    }
                    .font(.title2.weight(.semibold).monospacedDigit())
                    .fixedSize()
                    Text(now.sinceLabel)
                        .font(.footnote.weight(.medium))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                .padding(.trailing, 4)
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .glassEffect(glass(for: now.tone), in: .rect(cornerRadius: 30))
        .accessibilityElement(children: .combine)
        .accessibilityHint("Shows the set")
    }

    private func waitingCapsule(_ waiting: RowsWaitingCapsule) -> some View {
        capsuleRow {
            icon("pause.fill", fill: Color(.systemGray))
            texts(waiting.title, waiting.detail)
            Spacer(minLength: 4)
            Button {
                perform(waiting.goBack.action)
            } label: {
                Label(waiting.goBack.title, systemImage: "chevron.left")
                    .labelStyle(.titleAndIcon)
                    .font(.body.weight(.semibold))
                    .foregroundStyle(Color.accentColor)
                    .padding(.horizontal, 14)
                    .frame(minHeight: 44)
                    .background(RowsStyle.card, in: .capsule)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(waiting.goBack.accessibilityLabel)
        }
        .glassEffect(.regular, in: .rect(cornerRadius: 30))
    }

    private func capsuleRow(@ViewBuilder _ content: () -> some View) -> some View {
        HStack(spacing: 10) {
            content()
        }
        .padding(.leading, 12)
        .padding(.trailing, 10)
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity, minHeight: 60, alignment: .leading)
    }

    private func icon(_ symbol: String, fill: Color) -> some View {
        Image(systemName: symbol)
            .font(.body.weight(.bold))
            .foregroundStyle(.white)
            .frame(width: 36, height: 36)
            .background(fill, in: .circle)
            .accessibilityHidden(true)
    }

    private func texts(_ title: String, _ detail: String?) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(title)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.primary)
                .lineLimit(typeSize.isAccessibilitySize ? 4 : 2)
            if let detail {
                Text(detail)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .lineLimit(typeSize.isAccessibilitySize ? 4 : 2)
            }
        }
        .multilineTextAlignment(.leading)
    }

    private func symbol(for now: RowsNowCapsule) -> String {
        switch now.tone {
        case .restDone: return "checkmark"
        case .accent: return "arrow.right"
        case .orange: return layout.item(now.row)?.role == .warmup ? "flame.fill" : "arrow.down"
        }
    }

    private func color(for tone: RowsCapsuleTone) -> Color {
        switch tone {
        case .restDone: .green
        case .accent: .accentColor
        case .orange: .orange
        }
    }

    private func glass(for tone: RowsCapsuleTone) -> Glass {
        tone == .orange ? .regular.tint(.orange.opacity(0.18)).interactive() : .regular.interactive()
    }
}

/// The capsule while a countdown runs: a ring and the time left, `of 2:30`,
/// −15, +15 and Skip, and under a hairline what comes after the next set.
/// At larger text sizes the buttons go under the time.
struct RowsRestCapsuleView: View {
    let rest: RowsRestCapsule
    let perform: (RowsAction) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
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
            Divider()
                .padding(.top, 8)
            Group {
                if let label = rest.afterLabel {
                    Text("\(Text(label).font(.footnote.weight(.semibold)).foregroundStyle(Color.accentColor))  \(rest.afterText ?? "")")
                        .font(.subheadline)
                } else {
                    Text(rest.title)
                        .font(.subheadline)
                }
            }
            .lineLimit(2)
            .padding(.top, 8)
            .padding(.horizontal, 4)
            .accessibilityElement(children: .combine)
        }
        .padding(.horizontal, 12)
        .padding(.top, 10)
        .padding(.bottom, 8)
        .glassEffect(.regular, in: .rect(cornerRadius: 28))
    }

    private func clock(showsTotal: Bool) -> some View {
        HStack(alignment: .lastTextBaseline, spacing: 8) {
            RowsRestRing(startedAt: rest.startedAt, endsAt: rest.endsAt)
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
        // The label says what the rest is for; the value is the time left and
        // the total, so VoiceOver reads the countdown as the Table's bar does.
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
/// capsule was: the nearby steps of the exercise's grid as chips that set
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

extension RowsCapsule {
    /// A rest or a timed set is counting: the capsule stays on a page pushed inside the cover.
    var isCounting: Bool {
        switch self {
        case .resting, .timing: true
        case .upNext, .go, .waiting, .allDone, .empty: false
        }
    }
}
