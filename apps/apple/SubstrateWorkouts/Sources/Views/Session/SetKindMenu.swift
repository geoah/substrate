import WorkoutsCore
import SwiftUI

/// The SET label and its menu (DESIGN.md §7.6): the kinds §4.3 allows for
/// the row (the rest disabled), "About set types…", "Rate effort…" on a row
/// whose grid has no RPE column, the rest after this set, a note, Remove.
/// The label reads `W`, `2`, `2a`, `M`, `1L`, with a red F for a failure or
/// AMRAP set and Top or Back-off under the number.
struct SetKindMenu: View {
    let row: SetRowPresentation
    let card: ExerciseCard
    let layout: GridLayout

    var body: some View {
        Menu {
            SetMenuItems(row: row)
        } label: {
            label
        }
        .accessibilityLabel("\(row.label.spoken.capitalizedSentence)\(row.label.failureBadge ? ", to failure" : "")\(row.label.roleText.map { ", \($0)" } ?? "")")
        .accessibilityHint(row.canRateEffort ? "Set kind, effort, rest and note" : "Set kind, rest and note")
    }

    private var label: some View {
        VStack(spacing: 0) {
            HStack(spacing: 2) {
                Text(row.label.short)
                    .font(.body.weight(.semibold).monospacedDigit())
                    .foregroundStyle(color)
                if row.label.failureBadge {
                    Text("F")
                        .font(.caption2.weight(.heavy))
                        .foregroundStyle(.white)
                        .padding(.horizontal, 3)
                        .background(.red, in: .rect(cornerRadius: 3))
                }
            }
            if let role = row.label.roleText {
                // "Back-off" in a 36 pt cell: shrinks rather than cut at AX1.
                Text(role)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .minimumScaleFactor(0.5)
            }
        }
        .lineLimit(1)
        .minimumScaleFactor(0.6)
        .frame(width: layout.set, height: layout.cellHeight)
        .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 8))
    }

    private var color: Color {
        if row.label.short == "W" { return GroupPalette.warmup }
        if row.isSegment { return .blue }
        return .primary
    }
}

/// A set's menu items (DESIGN.md §7.6): the kinds §4.3 allows (the rest
/// disabled), "About set types…", "Rate effort…" on a row whose grid has no
/// RPE column, the rest after this set, a note, Remove. The Table's SET label
/// and the Rows editor's `…` open the same items.
struct SetMenuItems: View {
    let row: SetRowPresentation

    @Environment(SessionController.self) private var session
    @Environment(CoverModel.self) private var cover

    var body: some View {
        if row.kind != nil {
            ForEach(SetKind.allCases, id: \.self) { kind in
                Button {
                    session.run(.touchRow(row.id))
                    session.run(.changeSetKind(row.id, kind))
                } label: {
                    if row.kind == kind {
                        Label(kind.label, systemImage: "checkmark")
                    } else {
                        Text(kind.label)
                    }
                }
                .disabled(!row.allowedKinds.contains(kind) || row.kind == kind)
            }
            Button("About set types\u{2026}", systemImage: "questionmark.circle") {
                cover.sheet = .setTypes
            }
            Divider()
        }
        if row.canRateEffort {
            // A timed or distance set has no RPE column: its RPE is rated here (§7.6).
            Button("\(SetRowPresentation.rateEffortTitle)\u{2026}", systemImage: "gauge.with.dots.needle.33percent") {
                session.run(.touchRow(row.id))
                cover.sheet = .rpe(row.id)
            }
        }
        if row.kind != nil {
            Menu("Rest after this set…", systemImage: "timer") {
                RestPresetItems(current: row.restOverride, defaultText: "Default") { seconds in
                    session.run(.setRowRest(row.id, seconds))
                } custom: {
                    cover.sheet = .rest(.row(row.id))
                }
            }
        }
        Button("Note…", systemImage: "note.text") {
            cover.sheet = .note(.row(row.id))
        }
        Button("Remove set", systemImage: "trash", role: .destructive) {
            if row.isDone {
                cover.removeDoneRow = row.id
            } else {
                session.run(.removeSet(row.id))
            }
        }
    }
}
