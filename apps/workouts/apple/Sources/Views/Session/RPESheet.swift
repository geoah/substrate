import SwiftUI
import WorkoutsCore

/// The reps-left sheet (DESIGN.md §7.6): 0 to 4+ reps left, a −1 and a +1
/// beside the value, a tappable scale, Clear and Done. The value is kept as
/// the RIR-based RPE every set is logged with (RPE = 10 − reps left). While a
/// rest runs, its countdown and a compact −15, +15 and Skip stay at the top,
/// since the rest bar steps aside while a sheet is up. A timed or distance
/// set ("Rate effort") gets no reps line. The system's sheet glass, no custom
/// background.
struct RPESheet: View {
    let rowID: RowID

    @Environment(SessionModel.self) private var session
    @Environment(\.dismiss) private var dismiss
    @Environment(\.dynamicTypeSize) private var typeSize
    @State private var value: Decimal?
    @State private var showsHelp = false

    private var row: SetRowPresentation? { session.presentation?.row(rowID) }
    private var measurement: MeasurementType? { row.flatMap { session.presentation?.card($0.slot)?.measurement } }

    /// RPE 10 to 6: 0 to 4+ reps left, whole reps.
    private static let scaleValues: [Decimal] = [10, 9, 8, 7, 6]

    var body: some View {
        VStack(spacing: 10) {
            header
            HStack(spacing: 12) {
                stepButton(fewer: true)
                valueText
                    .frame(maxWidth: .infinity)
                stepButton(fewer: false)
            }
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
        .onAppear {
            value = row?.rpe
        }
    }

    /// The reps left, their headline and, for a rep set, what they mean.
    private var valueText: some View {
        VStack(spacing: 4) {
            Text(value.map { measurement?.recordsReps == false ? RPEScaleText.headline($0) ?? "" : "\(RowsEffort.number($0)) left" } ?? " ")
                .font(.system(.largeTitle, design: .rounded, weight: .bold).monospacedDigit())
                .contentTransition(.numericText())
            Text(value.flatMap(RPEScaleText.headline) ?? "Reps left")
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

    /// No line for a timed or distance set: reps mean nothing there.
    private var reserve: String? {
        guard let value else { return "Tap how many more good reps you had in you" }
        guard let measurement else { return RPEScaleText.reserve(value) }
        return RPEScaleText.reserve(value, measurement: measurement)
    }

    /// −1 or +1 rep left, 44 pt. With no value yet, one either side of 2 left.
    private func stepButton(fewer: Bool) -> some View {
        let next = RowsEffort.stepped(value, fewer: fewer)
        return Button {
            if let next { value = next }
        } label: {
            Image(systemName: fewer ? "minus" : "plus")
                .font(.title3.weight(.semibold))
                .frame(width: 44, height: 44)
                .background(Color(.tertiarySystemFill), in: .circle)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .foregroundStyle(.tint)
        .disabled(next == nil)
        .accessibilityLabel(fewer ? "1 rep left fewer" : "1 rep left more")
    }

    /// 0 to 4+, each a tap target.
    private var scale: some View {
        HStack(spacing: 0) {
            ForEach(Self.scaleValues, id: \.self) { rpe in
                Button {
                    value = rpe
                } label: {
                    Text(RowsEffort.number(rpe))
                        .font(.body.monospacedDigit().weight(value == rpe ? .bold : .regular))
                        .foregroundStyle(value == rpe ? Color.accentColor : .secondary)
                        .frame(maxWidth: .infinity, minHeight: 44)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(RowsEffort.number(rpe)) reps left")
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
            .accessibilityLabel("About reps left")
            .popover(isPresented: $showsHelp) {
                Text(
                    "Reps left says how hard the set was: how many more good reps you had in you. 0, none; 1, one more; 4+, four or more."
                )
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

/// The running rest at the top of the reps-left sheet: its label and countdown,
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

    @Environment(SessionModel.self) private var session
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
            CountdownText(start: rest.startedAt, end: rest.endsAt, alignment: .leading)
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
