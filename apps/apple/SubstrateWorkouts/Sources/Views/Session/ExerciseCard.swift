import WorkoutsCore
import SwiftUI

/// One exercise in the session (DESIGN.md §7.6): its name (a tap opens the
/// exercise), the `…` menu, the group label, the Rest and Note chips, the
/// notes, the set grid and "Add set". Content: no glass.
struct ExerciseCardView: View {
    let card: ExerciseCard
    /// The group block the card sits in; nil for a plain exercise.
    let group: GroupHeader?
    /// Every card of the session, for "Add to superset…".
    let allCards: [ExerciseCard]
    var focus: FocusState<CoverModel.Field?>.Binding

    @Environment(SessionController.self) private var session
    @Environment(CoverModel.self) private var cover

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                if let label = card.groupLabel {
                    Text(label)
                        .font(.headline)
                        .foregroundStyle(GroupPalette.color(card.colorIndex))
                }
                NavigationLink(value: card.exercise) {
                    Text(card.name)
                        .font(.headline)
                        .multilineTextAlignment(.leading)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.tint)
                .accessibilityAddTraits(.isHeader)
                .accessibilityHint("Shows the exercise's history")
                exerciseMenu
            }
            chips
            if let notes = card.planNotes {
                Text(notes)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            if let notes = card.notes {
                Text(notes)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            SetGridView(card: card, focus: focus)
            Button {
                session.run(.addSet(slot: card.slot, role: .working))
            } label: {
                Label("Add set", systemImage: "plus")
                    .frame(maxWidth: .infinity, minHeight: 32)
            }
            .buttonStyle(.bordered)
            .accessibilityLabel("Add a set to \(card.name)")
        }
        .padding(12)
        .cardBackground()
        .id(card.slot)
    }

    // MARK: - Chips

    private var chips: some View {
        HStack(spacing: 8) {
            Menu {
                restChipItems
            } label: {
                Label(card.restChip, systemImage: "timer")
                    .font(.subheadline)
                    .monospacedDigit()
            }
            .menuStyle(.button)
            .buttonStyle(.bordered)
            .accessibilityLabel("Rest for \(card.name), \(card.restChip)")
            .accessibilityHint("Changes the rest")
            Button {
                cover.sheet = .note(.slot(card.slot))
            } label: {
                Label("Note", systemImage: "note.text.badge.plus")
                    .font(.subheadline)
            }
            .buttonStyle(.bordered)
            .accessibilityLabel("Note for \(card.name)")
            if card.isLocal {
                Text(ExerciseRow.localText)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .lineLimit(1)
    }

    /// The chip's rest: a plain exercise's set rest; a member's transition
    /// to the next member; the last member's round rest.
    @ViewBuilder
    private var restChipItems: some View {
        if let group, group.members.last == card.slot {
            RestPresetItems(current: group.roundRest, defaultText: "Default") { seconds in
                session.run(RestTarget.roundRest(group: group.key).command(seconds))
            } custom: {
                cover.sheet = .rest(.roundRest(group: group.key))
            }
        } else {
            let kind: SlotRestKind = group == nil ? .setRest : .transition
            let option = card.rests.first { $0.kind == kind }
            RestPresetItems(current: option?.seconds, defaultText: option?.defaultText) { seconds in
                session.run(.setSlotRest(slot: card.slot, kind: kind, seconds: seconds))
            } custom: {
                cover.sheet = .rest(.slot(card.slot, kind))
            }
        }
    }

    // MARK: - The exercise menu

    private var exerciseMenu: some View {
        Menu {
            ExerciseMenuItems(card: card, group: group, allCards: allCards)
        } label: {
            Image(systemName: "ellipsis")
                .font(.body.weight(.semibold))
                .frame(minWidth: 44, minHeight: 44)
                .contentShape(.rect)
        }
        .accessibilityLabel("\(card.name) options")
    }
}

/// A group's block: its header and its member cards, joined by the group's
/// coloured bracket (§7.6).
struct GroupBlockView: View {
    let group: GroupHeader
    let cards: [ExerciseCard]
    let allCards: [ExerciseCard]
    var focus: FocusState<CoverModel.Field?>.Binding

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            RoundedRectangle(cornerRadius: 2)
                .fill(GroupPalette.color(group.colorIndex))
                .frame(width: 4)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 10) {
                GroupHeaderView(group: group)
                ForEach(cards) { card in
                    ExerciseCardView(card: card, group: group, allCards: allCards, focus: focus)
                }
            }
        }
    }
}

/// An exercise's menu items (DESIGN.md §7.6): reorder, replace, the
/// superset, rests, a warm-up, an exercise after it, a note, Remove. The
/// Table card's `…` and the Rows block's `…` open the same items.
struct ExerciseMenuItems: View {
    let card: ExerciseCard
    /// The group the exercise is in; nil for a plain exercise.
    let group: GroupHeader?
    /// Every card of the session, for "Add to superset…".
    let allCards: [ExerciseCard]

    @Environment(SessionController.self) private var session
    @Environment(CoverModel.self) private var cover

    var body: some View {
        Button("Reorder exercises", systemImage: "arrow.up.arrow.down") { cover.sheet = .reorder }
        Button("Replace exercise", systemImage: "arrow.2.squarepath") { cover.sheet = .replaceExercise(slot: card.slot) }
        if card.group == nil {
            let others = allCards.filter { $0.slot != card.slot }
            if !others.isEmpty {
                Menu("Add to superset…", systemImage: "link") {
                    ForEach(others) { other in
                        Button(other.groupLabel.map { "\($0) \(other.name)" } ?? other.name) {
                            session.run(.groupExercises(slot: card.slot, partner: other.slot))
                        }
                    }
                }
            }
        } else {
            Button("Remove from superset", systemImage: "xmark.circle") {
                session.run(.ungroupExercise(slot: card.slot))
            }
        }
        Menu("Rest…", systemImage: "timer") {
            ForEach(card.rests, id: \.kind) { option in
                Menu("\(option.kind.label): \(option.text)") {
                    RestPresetItems(current: option.seconds, defaultText: option.defaultText) { seconds in
                        session.run(.setSlotRest(slot: card.slot, kind: option.kind, seconds: seconds))
                    } custom: {
                        cover.sheet = .rest(.slot(card.slot, option.kind))
                    }
                }
            }
        }
        Button("Add warm-up set", systemImage: "flame") {
            session.run(.addSet(slot: card.slot, role: .warmup))
        }
        Button("Add exercise after this", systemImage: "plus") {
            cover.sheet = .addExercise(after: card.slot)
        }
        Button("Note…", systemImage: "note.text") { cover.sheet = .note(.slot(card.slot)) }
        Divider()
        Button("Remove exercise", systemImage: "trash", role: .destructive) {
            if let question = card.removeQuestion {
                cover.removeExercise = (card.slot, question)
            } else {
                session.run(.removeExercise(slot: card.slot))
            }
        }
    }
}
