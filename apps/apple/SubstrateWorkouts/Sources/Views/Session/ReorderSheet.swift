import WorkoutsCore
import SwiftUI

/// Reorder exercises (DESIGN.md §7.6): a `List` in edit mode, names only.
/// A group shows as a "Superset A" row with its members indented under it;
/// the group moves as a block, a member moves only within its group, and a
/// plain exercise dropped inside a group lands before it. Done sends the new
/// order; the engine keeps every group together.
struct ReorderSheet: View {
    let blocks: [ExerciseBlock]
    let done: ([String]) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var items: [Item] = []

    /// A row of the list: a group's header, or an exercise (a member when `group` is set).
    struct Item: Identifiable, Equatable {
        enum Kind: Equatable { case group(String, title: String, color: Int), slot(String, name: String, group: String?) }
        var kind: Kind

        var id: String {
            switch kind {
            case let .group(key, _, _): "group-\(key)"
            case let .slot(key, _, _): key
            }
        }

        var groupKey: String? {
            switch kind {
            case let .group(key, _, _): key
            case let .slot(_, _, group): group
            }
        }
    }

    var body: some View {
        NavigationStack {
            List {
                ForEach(items) { item in
                    switch item.kind {
                    case let .group(_, title, color):
                        Text(title)
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(GroupPalette.color(color))
                    case let .slot(_, name, group):
                        Text(name)
                            .padding(.leading, group == nil ? 0 : 20)
                    }
                }
                .onMove(perform: move)
            }
            .environment(\.editMode, .constant(.active))
            .navigationTitle("Reorder exercises")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") {
                        done(slotOrder)
                        dismiss()
                    }
                }
            }
        }
        .presentationDetents([.large])
        .onAppear { items = Self.items(blocks) }
    }

    static func items(_ blocks: [ExerciseBlock]) -> [Item] {
        blocks.flatMap { block -> [Item] in
            guard let group = block.group else {
                return block.cards.map { Item(kind: .slot($0.slot, name: $0.name, group: nil)) }
            }
            let header = Item(kind: .group(group.key, title: "\(group.kind.label) \(group.key.uppercased())", color: group.colorIndex))
            return [header] + block.cards.map { Item(kind: .slot($0.slot, name: $0.name, group: group.key)) }
        }
    }

    private var slotOrder: [String] {
        items.compactMap { item in
            if case let .slot(key, _, _) = item.kind { return key }
            return nil
        }
    }

    /// The range of a group's header and members.
    private func range(ofGroup key: String, in list: [Item]) -> Range<Int>? {
        guard let first = list.firstIndex(where: { $0.groupKey == key }),
              let last = list.lastIndex(where: { $0.groupKey == key })
        else { return nil }
        return first ..< last + 1
    }

    private func move(from source: IndexSet, to destination: Int) {
        guard let index = source.first else { return }
        let item = items[index]
        var list = items
        switch item.kind {
        case let .slot(_, _, group?):
            // A member stays inside its group.
            guard let range = range(ofGroup: group, in: list) else { return }
            let target = min(max(destination, range.lowerBound + 1), range.upperBound)
            list.move(fromOffsets: source, toOffset: target)
        case .group, .slot(_, _, nil):
            // The whole block moves, and never into another group.
            let block = item.groupKey.flatMap { range(ofGroup: $0, in: list) } ?? index ..< index + 1
            let moving = Array(list[block])
            list.removeSubrange(block)
            var target = destination > block.lowerBound ? destination - block.count : destination
            target = min(max(target, 0), list.count)
            if target < list.count, let inside = list[target].groupKey, case .slot = list[target].kind,
               let range = range(ofGroup: inside, in: list)
            {
                target = range.lowerBound
            }
            list.insert(contentsOf: moving, at: target)
        }
        items = list
    }
}

/// A note on the session, an exercise or a set (DESIGN.md §7.6). Empty text clears it.
struct NoteSheet: View {
    let title: String
    let initial: String?
    let save: (String?) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var text = ""
    @FocusState private var focused: Bool

    var body: some View {
        NavigationStack {
            TextField("Note", text: $text, axis: .vertical)
                .lineLimit(4 ... 12)
                .focused($focused)
                .padding()
                .frame(maxHeight: .infinity, alignment: .top)
                .navigationTitle(title)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel", role: .cancel) { dismiss() }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") {
                            let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
                            save(trimmed.isEmpty ? nil : trimmed)
                            dismiss()
                        }
                    }
                }
        }
        .presentationDetents([.medium, .large])
        .onAppear {
            text = initial ?? ""
            focused = true
        }
    }
}
