import WorkoutsCore
import SwiftUI

/// A group of the routine: its kind, rounds and round rest, and its members
/// to reorder, open, take out or add to.
struct GroupEditorView: View {
    let model: RoutineEditorModel
    let groupKey: String

    @Environment(AppModel.self) private var app
    @Environment(SubstrateLink.self) private var link
    @Environment(\.dismiss) private var dismiss
    @State private var adding = false
    @State private var customRest = false

    /// `Tri-set A, 2 rounds`.
    static func title(_ group: RoutineGroup) -> String {
        var text = "\(group.groupKind.label) \(group.key.uppercased())"
        if let rounds = group.rounds { text += ", \(Format.count(rounds, "round"))" }
        return text
    }

    var body: some View {
        if let group = model.editor.group(groupKey) {
            List {
                values(group)
                members
                Section {
                    Button("Ungroup", systemImage: "rectangle.split.1x2") {
                        model.change { $0.ungroup(groupKey) }
                        dismiss()
                    }
                } footer: {
                    Text("The exercises stay in the routine, each on its own.")
                }
            }
            .listStyle(.insetGrouped)
            .navigationTitle(Self.title(group))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { EditButton() }
            .sheet(isPresented: $adding) {
                ExercisePicker(title: "Add to \(group.groupKind.label.lowercased()) \(groupKey.uppercased())") { exercise in
                    model.change { $0.addExercise(exercise, toGroup: groupKey) }
                    sendIfLocal(exercise, link: link)
                }
            }
            .sheet(isPresented: $customRest) {
                RestPickerSheet(title: "Rest after each round", initial: group.roundRest ?? 90) { seconds in
                    model.change { $0.updateGroup(groupKey) { $0.roundRest = seconds } }
                }
            }
        } else {
            ContentUnavailableView("Group removed", systemImage: "rectangle.split.1x2")
        }
    }

    private func values(_ group: RoutineGroup) -> some View {
        Section {
            Picker("Kind", selection: Binding { group.groupKind } set: { kind in
                model.change { $0.updateGroup(groupKey) { $0.groupKind = kind } }
            }) {
                ForEach(GroupKind.allCases, id: \.self) { Text($0.label).tag($0) }
            }
            Picker("Pairing", selection: Binding { group.pairing } set: { pairing in
                model.change { $0.updateGroup(groupKey) { $0.pairing = pairing } }
            }) {
                Text("None").tag(Pairing?.none)
                ForEach(Pairing.allCases, id: \.self) { Text($0.label).tag(Pairing?.some($0)) }
            }
            Stepper(value: Binding { group.rounds ?? maxWorkingSets } set: { rounds in
                model.change { $0.updateGroup(groupKey) { $0.rounds = rounds } }
            }, in: 1 ... 20) {
                LabeledContent("Rounds", value: group.rounds.map(String.init) ?? "One per set")
            }
            RestMenuRow(title: "Rest after each round", current: group.roundRest, select: { seconds in
                model.change { $0.updateGroup(groupKey) { $0.roundRest = seconds } }
            }, custom: { customRest = true })
        }
    }

    private var maxWorkingSets: Int {
        model.editor.members(of: groupKey).compactMap { model.editor.slot($0)?.sets.count { $0.role == .working } }.max() ?? 1
    }

    private var members: some View {
        let members = model.editor.members(of: groupKey)
        return Section("Exercises") {
            ForEach(Array(members.enumerated()), id: \.element) { position, key in
                NavigationLink(value: RoutineEditorRoute.slot(key)) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(groupMemberLabel(group: groupKey, position: position + 1))
                            .font(.subheadline.weight(.bold))
                            .foregroundStyle(.secondary)
                        Text(name(key))
                    }
                }
                .swipeActions(edge: .leading) {
                    Button("Leave Group", systemImage: "arrow.down.right") {
                        withAnimation { model.change { $0.leaveGroup(key) } }
                    }
                }
            }
            .onMove { source, destination in model.change { $0.moveMembers(of: groupKey, from: source, to: destination) } }
            .onDelete { offsets in model.change { $0.removeMembers(of: groupKey, at: offsets) } }
            Button {
                adding = true
            } label: {
                Label("Add exercise", systemImage: "plus")
            }
        }
    }

    private func name(_ slotKey: String) -> String {
        guard let slot = model.editor.slot(slotKey) else { return slotKey }
        return app.exercises[slot.exercise]?.name ?? slot.exercise.rawValue
    }
}

/// A rest setting as a row with its presets in a menu: the value, or
/// "Default" when the routine leaves it to the app.
struct RestMenuRow: View {
    let title: String
    let current: Seconds?
    let select: (Seconds?) -> Void
    let custom: () -> Void

    var body: some View {
        Menu {
            RestPresetItems(current: current, defaultText: "Default", select: select, custom: custom)
        } label: {
            LabeledContent(title, value: current.map(Format.rest) ?? "Default")
                .foregroundStyle(.primary)
        }
    }
}
