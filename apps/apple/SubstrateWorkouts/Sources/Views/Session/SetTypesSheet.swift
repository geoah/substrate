import WorkoutsCore
import SwiftUI

/// "About set types…" in a row's set-kind menu (DESIGN.md §7.6): what each
/// kind is, what Workouts adds for it, how rest works around it and when it
/// stops (§4.7, §4.9). Plain words; each kind's name over its text, never in
/// columns. The system's sheet glass.
struct SetTypesSheet: View {
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                ForEach(SetTypeHelp.all) { entry in
                    SetTypeHelpRow(entry: entry)
                }
            }
            .listStyle(.insetGrouped)
            .navigationTitle("Set types")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
        .presentationDetents([.medium, .large])
    }
}

/// One kind: the grid's label for it, its name, then what it does.
private struct SetTypeHelpRow: View {
    let entry: SetTypeHelp

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                badge
                Text(entry.name)
                    .font(.headline)
            }
            Text(entry.text)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }

    /// As the SET cell shows it: `W` in orange, a red `F`, `2a`, `M`.
    @ViewBuilder
    private var badge: some View {
        if entry.kind == .failure {
            Text("F")
                .font(.caption.weight(.heavy))
                .foregroundStyle(.white)
                .padding(.horizontal, 4)
                .background(.red, in: .rect(cornerRadius: 3))
                .accessibilityHidden(true)
        } else {
            Text(entry.badge)
                .font(.subheadline.weight(.semibold).monospacedDigit())
                .foregroundStyle(entry.kind == .warmup ? GroupPalette.warmup : .secondary)
                .accessibilityHidden(true)
        }
    }
}

/// The help's words, one entry per kind of the menu, in the menu's order.
struct SetTypeHelp: Identifiable {
    let kind: SetKind
    let badge: String
    let text: String

    var id: SetKind { kind }
    var name: String { kind.label }

    static let all: [SetTypeHelp] = [
        SetTypeHelp(
            kind: .warmup, badge: "W",
            text: "A lighter set before the work. It is not a working set and is left out of volume unless Settings counts it. The warm-up rest follows it (0:45 unless changed)."
        ),
        SetTypeHelp(
            kind: .working, badge: "1",
            text: "A normal set. It counts as a working set and in volume, and the exercise\u{2019}s rest (its Rest chip) follows it."
        ),
        SetTypeHelp(
            kind: .failure, badge: "F",
            text: "A working set taken until no clean rep is left. It keeps its number with a red F; the rest after it is the exercise\u{2019}s."
        ),
        SetTypeHelp(
            kind: .drop, badge: "2a",
            text: "Straight after the set, lighten the load and go again. Workouts adds the drop as its own row, 20 % lighter unless the routine says otherwise, to failure. There is no rest between the set and its drop; the exercise\u{2019}s rest starts after the last drop."
        ),
        SetTypeHelp(
            kind: .myo, badge: "M",
            text: "One set close to failure, then short mini-sets with the same load, 0:15 apart. Each tick adds the next mini-set, about a quarter of the first set\u{2019}s reps. Stop when a mini-set gets fewer reps than the first mini-set, or after 5."
        ),
        SetTypeHelp(
            kind: .myomatch, badge: "M",
            text: "Match the reps of your previous working set with the same load, in pieces. After this set Workouts adds mini-sets, 0:15 apart, until this set and its mini-sets reach that many reps, or after 5."
        ),
    ]
}

#if DEBUG
#Preview {
    Color.clear.sheet(isPresented: .constant(true)) {
        SetTypesSheet()
    }
}
#endif
