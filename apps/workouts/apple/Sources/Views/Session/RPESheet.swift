import WorkoutsCore
import SwiftUI

/// The RPE sheet (DESIGN.md §7.6): 6 to 10 in half steps on the RIR-based
/// scale with Hevy's wording, a −0.5 and a +0.5 beside the value, a slider
/// and a tappable scale, Clear and Done. While a rest runs, its countdown and
/// a compact −15, +15 and Skip stay at the top, since the rest bar steps
/// aside while a sheet is up. A timed or distance set ("Rate effort") gets
/// no reps-in-reserve line. The system's sheet glass, no custom background.
struct RPESheet: View {
    let rowID: RowID

    @Environment(SessionController.self) private var session
    @Environment(\.dismiss) private var dismiss
    @Environment(\.dynamicTypeSize) private var typeSize
    @State private var value: Decimal?
    @State private var slider: Double = 8
    @State private var showsHelp = false

    private var row: SetRowPresentation? { session.presentation?.row(rowID) }
    private var measurement: MeasurementType? { row.flatMap { session.presentation?.card($0.slot)?.measurement } }

    private static let lowest: Decimal = 6
    private static let highest: Decimal = 10
    private static let step = Decimal(string: "0.5") ?? 0.5

    var body: some View {
        VStack(spacing: 10) {
            header
            HStack(spacing: 12) {
                stepButton(by: -Self.step)
                valueText
                    .frame(maxWidth: .infinity)
                stepButton(by: Self.step)
            }
            // With no RPE yet the thumb is only a resting place: dimmed, and
            // Done waits for a value, so what Done saves is what is shown.
            Slider(value: $slider, in: 6 ... 10, step: 0.5) {
                Text("RPE")
            } onEditingChanged: { _ in
                value = Decimal(slider)
            }
            .tint(value == nil ? .secondary : .accentColor)
            .opacity(value == nil ? 0.4 : 1)
            .accessibilityValue(value.map(Format.rpe) ?? "not rated")
            scale
            HStack {
                Button("Clear") {
                    send(nil)
                }
                .buttonStyle(.bordered)
                .controlSize(.large)
                Spacer()
                Button("Done") {
                    send(value)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .disabled(value == nil)
            }
        }
        .padding(.horizontal, 20)
        .padding(.top, 8)
        // §7.6's height; past the default text size it opens at half the
        // screen, so Done is never below the edge.
        .presentationDetents(typeSize > .large ? [.medium, .large] : [.height(320), .large])
        .animation(.snappy, value: value)
        .onChange(of: slider) { _, new in value = Decimal(new) }
        .onAppear {
            value = row?.rpe
            if let rpe = row?.rpe { slider = NSDecimalNumber(decimal: rpe).doubleValue }
        }
    }

    /// The value, its headline and, for a rep set, the reps in reserve.
    private var valueText: some View {
        VStack(spacing: 4) {
            Text(value.map(Format.rpe) ?? " ")
                .font(.system(.largeTitle, design: .rounded, weight: .bold).monospacedDigit())
                .contentTransition(.numericText())
            Text(value.flatMap(RPEScaleText.headline) ?? RPEScaleText.emptyHeadline)
                .font(.headline)
            if let reserve {
                Text(reserve)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
        .multilineTextAlignment(.center)
        .accessibilityElement(children: .combine)
    }

    /// No line for a timed or distance set: reps in reserve mean nothing there.
    private var reserve: String? {
        guard let value else { return RPEScaleText.emptyReserve }
        guard let measurement else { return RPEScaleText.reserve(value) }
        return RPEScaleText.reserve(value, measurement: measurement)
    }

    /// −0.5 or +0.5, 44 pt. With no value yet it moves from the slider's
    /// resting place.
    private func stepButton(by delta: Decimal) -> some View {
        let current = value ?? Decimal(slider)
        let next = min(Self.highest, max(Self.lowest, current + delta))
        let less = delta < 0
        return Button {
            value = next
            slider = NSDecimalNumber(decimal: next).doubleValue
        } label: {
            Image(systemName: less ? "minus" : "plus")
                .font(.title3.weight(.semibold))
                .frame(width: 44, height: 44)
                .background(Color(.tertiarySystemFill), in: .circle)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .foregroundStyle(.tint)
        .disabled(value != nil && next == value)
        .accessibilityLabel(less ? "RPE half a point lower" : "RPE half a point higher")
    }

    /// The values, each a tap target.
    private var scale: some View {
        HStack(spacing: 0) {
            ForEach(RPEScaleText.values, id: \.self) { rpe in
                Button {
                    value = rpe
                    slider = NSDecimalNumber(decimal: rpe).doubleValue
                } label: {
                    Text(Format.rpe(rpe))
                        .font(.footnote.monospacedDigit().weight(value == rpe ? .bold : .regular))
                        .foregroundStyle(value == rpe ? Color.accentColor : .secondary)
                        .frame(maxWidth: .infinity, minHeight: 36)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("RPE \(Format.rpe(rpe))")
            }
        }
    }

    /// The rest, when one runs, and the scale's help.
    private var header: some View {
        HStack(spacing: 8) {
            if case let .resting(rest)? = session.presentation?.restBar {
                RPESheetRest(rest: rest)
            } else {
                Spacer()
            }
            Button {
                showsHelp = true
            } label: {
                Image(systemName: "questionmark.circle")
                    .font(.title3)
                    .frame(width: 44, height: 44)
            }
            .accessibilityLabel("About RPE")
            .popover(isPresented: $showsHelp) {
                Text("RPE says how hard the set was by the reps left in you: 10, none left; 9, one; 8, two; 7, three; 6, four or more. A half step means maybe one more.")
                    .font(.footnote)
                    .padding()
                    .frame(idealWidth: 300)
                    .presentationCompactAdaptation(.popover)
            }
        }
    }

    /// Done rates the value shown, even one copied from PREVIOUS or
    /// pre-filled in Rows (§4.14); `SetRowPresentation.rpePatch` decides.
    private func send(_ rpe: Decimal?) {
        if let patch = row?.rpePatch(for: rpe) {
            session.run(.edit(rowID, patch))
        }
        dismiss()
    }
}

/// The running rest at the top of the RPE sheet: its label and countdown,
/// then a compact −15, +15 and Skip (§7.6). Content in a sheet, so plain
/// fills, not glass; each control is 44 pt tall.
///
/// At the default text sizes the sheet is 320 pt, so the controls always sit
/// beside the countdown and a long label ("Go: Leg Press (Machine) (A2) in",
/// a superset's transition) takes two lines instead: stacked, the row would
/// be about 50 pt taller and push Done to the sheet's edge. Past the default
/// size the sheet opens at half the screen and the controls may go under.
private struct RPESheetRest: View {
    let rest: RestCountdownPresentation

    @Environment(SessionController.self) private var session
    @Environment(\.dynamicTypeSize) private var typeSize

    /// The 320 pt sheet (RPESheet's detents): one row, always.
    private var oneRow: Bool { typeSize <= .large }

    var body: some View {
        if oneRow {
            HStack(spacing: 8) {
                countdown
                    .frame(maxWidth: .infinity, alignment: .leading)
                controls
            }
        } else {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) {
                    countdown
                        .frame(maxWidth: .infinity, alignment: .leading)
                    controls
                }
                VStack(alignment: .leading, spacing: 6) {
                    countdown
                    controls
                }
            }
        }
    }

    private var countdown: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(rest.label)
                .font(.caption.weight(.medium))
                .lineLimit(oneRow ? 2 : 1)
                .minimumScaleFactor(0.8)
            CountdownText(start: rest.startedAt, end: rest.endsAt)
                .font(.headline.monospacedDigit())
        }
        .foregroundStyle(.tint)
        .accessibilityElement(children: .combine)
    }

    private var controls: some View {
        HStack(spacing: 6) {
            Button("\u{2212}15") { session.run(.adjustRest(-15)) }
                .buttonStyle(CompactPillButtonStyle(prominent: false))
                .accessibilityLabel("Rest 15 seconds less")
            Button("+15") { session.run(.adjustRest(15)) }
                .buttonStyle(CompactPillButtonStyle(prominent: false))
                .accessibilityLabel("Rest 15 seconds more")
            Button("Skip") { session.run(.skipRest) }
                .buttonStyle(CompactPillButtonStyle(prominent: true))
                .accessibilityLabel("Skip the rest")
        }
        .fixedSize()
    }
}

/// A small pill button that still meets the 44 pt target: a plain fill, or
/// the accent colour with white text for the main action.
private struct CompactPillButtonStyle: ButtonStyle {
    let prominent: Bool

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.subheadline.weight(.semibold).monospacedDigit())
            .foregroundStyle(prominent ? AnyShapeStyle(Color.white) : AnyShapeStyle(.tint))
            .padding(.horizontal, 12)
            .frame(minWidth: 44, minHeight: 44)
            .background(prominent ? AnyShapeStyle(.tint) : AnyShapeStyle(Color(.tertiarySystemFill)), in: .capsule)
            .opacity(configuration.isPressed ? 0.6 : 1)
            .contentShape(.capsule)
    }
}
