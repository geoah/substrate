import WorkoutsCore
import SwiftUI

/// The rest bar (DESIGN.md §7.6, §8): the one custom glass element, in the
/// cover's bottom safe-area bar. Its pill morphs between the forms the
/// presenter gives: the Up next chip, a countdown with −15, +15 and Skip,
/// "Rest over" in green, "Go", a timed set with Stop, "All sets done" with
/// Finish, and "Add an exercise" in an empty session. The pill is the accent
/// colour while resting, green when rest is over, orange past a timed set's
/// target; what it says is white on all of them. Under a running rest, the
/// next set sits on its own glass, its values in bold. At the accessibility
/// text sizes the controls go under the pill instead of beside it.
struct RestBar: View {
    let bar: RestBarPresentation
    /// The Up next row, so the "Next" lines can set its values apart.
    var upNext: UpNextPresentation?
    let adjust: (Seconds) -> Void
    let skip: () -> Void
    let stop: (RowID) -> Void
    let finish: () -> Void
    let showUpNext: () -> Void
    let addExercise: () -> Void

    @Namespace private var glass
    @Environment(\.dynamicTypeSize) private var typeSize

    /// Accessibility sizes: the pill on one row, its controls on the next.
    private var stacked: Bool { typeSize.isAccessibilitySize }
    /// Lines a chip's text may take before it is cut.
    private var chipLines: Int { stacked ? 4 : 2 }

    var body: some View {
        // Glass closer than the container's spacing melts into one shape:
        // kept under the stacks' spacing, the controls stay separate pieces.
        // The morph between forms comes from the glass IDs, not this.
        GlassEffectContainer(spacing: 6) {
            switch bar {
            case let .upNext(line):
                Button(action: showUpNext) {
                    NextLine(line: line, upNext: upNext, lines: chipLines)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 8)
                        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.interactive(), in: .capsule)
                .glassEffectID("countdown", in: glass)
                .accessibilityHint("Shows the next set")
            case let .resting(rest):
                resting(rest)
            case let .restOver(text):
                Button(action: showUpNext) {
                    Label(text, systemImage: "checkmark.circle.fill")
                        .font(.subheadline.weight(.semibold))
                        .lineLimit(chipLines)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 12)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.tint(.green).interactive(), in: .capsule)
                .glassEffectID("countdown", in: glass)
            case let .go(text):
                Button(action: showUpNext) {
                    Label(text, systemImage: "arrow.right.circle.fill")
                        .font(.subheadline.weight(.semibold))
                        .lineLimit(chipLines)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 12)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.tint(.green).interactive(), in: .capsule)
                .glassEffectID("countdown", in: glass)
            case let .timedSet(timed):
                timedSet(timed)
            case .empty:
                Button(action: addExercise) {
                    Label("Add an exercise to begin", systemImage: "plus.circle.fill")
                        .font(.subheadline.weight(.semibold))
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 12)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.interactive(), in: .capsule)
                .glassEffectID("countdown", in: glass)
            case .allDone:
                pillAndAction {
                    Text("All sets done")
                        .font(.subheadline.weight(.semibold))
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 12)
                        .glassEffect(.regular, in: .capsule)
                        .glassEffectID("countdown", in: glass)
                } action: {
                    Button(action: finish) {
                        Text("Finish")
                            .frame(maxWidth: stacked ? .infinity : nil)
                    }
                    .font(.headline)
                    .buttonStyle(.glassProminent)
                    .controlSize(.large)
                }
            }
        }
        .animation(.smooth, value: bar)
        .animation(.smooth, value: stacked)
    }

    /// A pill and its button: side by side, or the button under the pill at
    /// the accessibility sizes.
    private func pillAndAction<Pill: View, Action: View>(@ViewBuilder pill: () -> Pill,
                                                         @ViewBuilder action: () -> Action) -> some View
    {
        let layout = stacked ? AnyLayout(VStackLayout(spacing: 10)) : AnyLayout(HStackLayout(spacing: 12))
        return layout {
            pill()
            action()
        }
    }

    private func resting(_ rest: RestCountdownPresentation) -> some View {
        VStack(spacing: 10) {
            if stacked {
                // The pill full width, then −15, +15 and Skip sharing a row.
                countdownPill(rest)
                HStack(spacing: 10) {
                    lessButton
                    moreButton
                    skipButton
                }
            } else {
                HStack(spacing: 10) {
                    lessButton
                    countdownPill(rest)
                    moreButton
                    skipButton
                }
            }
            if let next = rest.next {
                // The quickest way to the next set while there is time to get
                // ready, on glass of its own so it reads over the rows.
                Button(action: showUpNext) {
                    NextLine(line: next, upNext: upNext, lines: chipLines)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 6)
                        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.interactive(), in: .capsule)
                .glassEffectID("next", in: glass)
                .accessibilityHint("Shows the next set")
            }
        }
    }

    private func countdownPill(_ rest: RestCountdownPresentation) -> some View {
        // The label above the time: beside it there is no room for
        // "Rest before next exercise" or "Go: Bird Dog (A2) in".
        VStack(spacing: 2) {
            Text(rest.label)
                .font(.caption.weight(.medium))
                .lineLimit(stacked ? 2 : 1)
                .minimumScaleFactor(0.85)
                .multilineTextAlignment(.center)
            CountdownText(start: rest.startedAt, end: rest.endsAt)
                .font(.title3.weight(.semibold).monospacedDigit())
            ProgressView(timerInterval: min(rest.startedAt, rest.endsAt) ... rest.endsAt, countsDown: true) {
                EmptyView()
            } currentValueLabel: {
                EmptyView()
            }
            .progressViewStyle(.linear)
            .tint(.white.opacity(0.85))
        }
        .foregroundStyle(.white)
        .padding(.horizontal, 14)
        .padding(.vertical, 6)
        .frame(maxWidth: .infinity)
        .glassEffect(.regular.tint(.accentColor), in: .capsule)
        .glassEffectID("countdown", in: glass)
        .accessibilityElement(children: .combine)
    }

    private var lessButton: some View {
        Button {
            adjust(-15)
        } label: {
            Text("\u{2212}15")
                .font(.headline.monospacedDigit())
                .frame(minWidth: 36, maxWidth: stacked ? .infinity : nil, minHeight: 32)
        }
        .buttonStyle(.glass)
        .accessibilityLabel("Rest 15 seconds less")
    }

    private var moreButton: some View {
        Button {
            adjust(15)
        } label: {
            Text("+15")
                .font(.headline.monospacedDigit())
                .frame(minWidth: 36, maxWidth: stacked ? .infinity : nil, minHeight: 32)
        }
        .buttonStyle(.glass)
        .accessibilityLabel("Rest 15 seconds more")
    }

    private var skipButton: some View {
        Button(action: skip) {
            Text("Skip")
                .frame(maxWidth: stacked ? .infinity : nil, minHeight: 32)
        }
        .font(.headline)
        .buttonStyle(.glassProminent)
        .accessibilityLabel("Skip the rest")
    }

    private func timedSet(_ timed: TimedSetBar) -> some View {
        pillAndAction {
            // Redrawn every second from the start, so the tint turns orange at
            // the target to the second. An `.explicit([endsAt])` schedule
            // would draw its overtime branch at once (§7.6).
            TimelineView(.periodic(from: timed.startedAt, by: 1)) { context in
                let over = timed.endsAt.map { context.date >= $0 } ?? false
                HStack(spacing: 8) {
                    Text(timed.label)
                        .font(.subheadline.weight(.medium))
                        .lineLimit(stacked ? 2 : 1)
                        .minimumScaleFactor(0.7)
                    Spacer(minLength: 4)
                    RunningTime(startedAt: timed.startedAt, endsAt: timed.endsAt, overtimeColor: .white, showsLeft: false,
                                font: .title3.weight(.semibold).monospacedDigit())
                }
                .foregroundStyle(.white)
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
                .frame(maxWidth: .infinity)
                .glassEffect(.regular.tint(over ? .orange : .accentColor), in: .capsule)
                .glassEffectID("countdown", in: glass)
            }
        } action: {
            Button {
                stop(timed.row)
            } label: {
                Text("Stop")
                    .frame(maxWidth: stacked ? .infinity : nil)
            }
            .font(.headline)
            .buttonStyle(.glassProminent)
            .controlSize(.large)
            .accessibilityLabel("Stop \(timed.label)")
        }
    }
}

/// "Next: Leg Press (Machine), warm-up 2" with its values, `70 kg × 6`, in
/// bold (§7.6): on one line when it fits, else the values on a line of
/// their own so they are never the part cut off. A line that is not Up
/// next's is shown as it comes.
struct NextLine: View {
    let line: String
    let upNext: UpNextPresentation?
    var lines = 2

    var body: some View {
        Group {
            if let upNext, upNext.line == line {
                let what = Text("\(Text("Next:").foregroundStyle(.secondary)) \(upNext.exerciseWithGroup), \(upNext.label.spoken)")
                if upNext.values.isEmpty {
                    what.lineLimit(lines)
                } else {
                    let values = Text(upNext.values)
                        .font(.subheadline.weight(.semibold).monospacedDigit())
                    ViewThatFits(in: .horizontal) {
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            what.lineLimit(1)
                            Spacer(minLength: 4)
                            values.lineLimit(1)
                        }
                        VStack(alignment: .leading, spacing: 1) {
                            what.lineLimit(max(1, lines - 1))
                            values.lineLimit(lines)
                        }
                    }
                }
            } else {
                Text(line)
                    .lineLimit(lines)
            }
        }
        .font(.subheadline)
        .foregroundStyle(.primary)
        .multilineTextAlignment(.leading)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(line)
    }
}

extension RestBarPresentation {
    /// A rest or a timed set is counting.
    var isCounting: Bool {
        switch self {
        case .resting, .timedSet: true
        case .upNext, .restOver, .go, .allDone, .empty: false
        }
    }
}
