import WorkoutsCore
import SwiftUI

/// One exercise of the routine: which exercise, its planned sets to add,
/// remove, reorder and open, its rests and its notes.
struct SlotEditorView: View {
    let model: RoutineEditorModel
    let slotKey: String

    @Environment(AppModel.self) private var app
    @Environment(SubstrateLink.self) private var link
    @Environment(\.dismiss) private var dismiss
    @State private var replacing = false
    @State private var customRest: RestField?

    enum RestField: String, Identifiable {
        case sets = "Rest between sets"
        case warmups = "Rest after warm-ups"
        case transition = "Rest to the next exercise"
        var id: String { rawValue }
    }

    var body: some View {
        if let slot = model.editor.slot(slotKey) {
            let exercise = app.exercises[slot.exercise]
            List {
                Section {
                    Button {
                        replacing = true
                    } label: {
                        LabeledContent("Exercise", value: exercise?.name ?? slot.exercise.rawValue)
                            .foregroundStyle(.primary)
                    }
                    if let key = slot.group, let group = model.editor.group(key) {
                        LabeledContent("In", value: GroupEditorView.title(group))
                        Button("Leave \(group.groupKind.label.lowercased()) \(key.uppercased())", systemImage: "arrow.down.right") {
                            model.change { $0.leaveGroup(slotKey) }
                        }
                    }
                }
                sets(slot, measurement: exercise?.measurement ?? .weightreps)
                rests(slot)
                Section("Notes") {
                    TextField("Seat height, cues", text: Binding { slot.notes ?? "" } set: { value in
                        model.change { $0.update(slotKey) { $0.notes = RoutineEditorText.text(value) } }
                    }, axis: .vertical)
                        .lineLimit(1 ... 6)
                        .font(.subheadline)
                }
                Section {
                    Button("Remove from Routine", systemImage: "trash", role: .destructive) {
                        model.change { $0.removeSlot(slotKey) }
                        dismiss()
                    }
                }
            }
            .listStyle(.insetGrouped)
            .navigationTitle(exercise?.name ?? slot.exercise.rawValue)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { EditButton() }
            .sheet(isPresented: $replacing) {
                ExercisePicker(title: "Replace exercise") { picked in
                    model.change { $0.update(slotKey) { $0.exercise = picked.id } }
                    sendIfLocal(picked, link: link)
                }
            }
            .sheet(item: $customRest) { field in
                RestPickerSheet(title: field.rawValue, initial: value(field, slot) ?? 90) { seconds in
                    set(field, seconds)
                }
            }
        } else {
            ContentUnavailableView("Exercise removed", systemImage: "dumbbell")
        }
    }

    private func sets(_ slot: RoutineSlot, measurement: MeasurementType) -> some View {
        Section("Sets") {
            ForEach(Array(slot.sets.enumerated()), id: \.element.key) { _, planned in
                NavigationLink(value: RoutineEditorRoute.set(slot: slotKey, set: planned.key)) {
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Text(PlannedSetEditorView.label(planned, in: slot))
                            .font(.subheadline.weight(.semibold).monospacedDigit())
                            .foregroundStyle(planned.role == .warmup ? GroupPalette.warmup : .primary)
                            .frame(minWidth: 24, alignment: .leading)
                        Text(RoutinePresenter.plannedSetText(planned, measurement: measurement))
                            .font(.subheadline)
                    }
                }
            }
            .onMove { source, destination in model.change { $0.moveSets(of: slotKey, from: source, to: destination) } }
            .onDelete { offsets in model.change { $0.removeSets(of: slotKey, at: offsets) } }
            HStack {
                Button("Add set", systemImage: "plus") { model.change { $0.addSet(to: slotKey) } }
                Spacer()
                Button("Add warm-up") { model.change { $0.addSet(to: slotKey, role: .warmup) } }
                    .foregroundStyle(GroupPalette.warmup)
            }
            .buttonStyle(.borderless)
            .font(.subheadline)
        }
    }

    private func rests(_ slot: RoutineSlot) -> some View {
        Section("Rest") {
            RestMenuRow(title: RestField.sets.rawValue, current: slot.setRest, select: { set(.sets, $0) },
                        custom: { customRest = .sets })
            if slot.sets.contains(where: { $0.role == .warmup }) {
                RestMenuRow(title: RestField.warmups.rawValue, current: slot.warmupRest, select: { set(.warmups, $0) },
                            custom: { customRest = .warmups })
            }
            if slot.group != nil {
                RestMenuRow(title: RestField.transition.rawValue, current: slot.transition, select: { set(.transition, $0) },
                            custom: { customRest = .transition })
            }
        }
    }

    private func value(_ field: RestField, _ slot: RoutineSlot) -> Seconds? {
        switch field {
        case .sets: slot.setRest
        case .warmups: slot.warmupRest
        case .transition: slot.transition
        }
    }

    private func set(_ field: RestField, _ seconds: Seconds?) {
        model.change { editor in
            editor.update(slotKey) { slot in
                switch field {
                case .sets: slot.setRest = seconds
                case .warmups: slot.warmupRest = seconds
                case .transition: slot.transition = seconds
                }
            }
        }
    }
}

enum RoutineEditorText {
    /// Blank text is no text.
    static func text(_ value: String) -> String? {
        value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : value
    }
}

/// One planned set: its role, the rep or time target, the effort target
/// and the rest after it.
struct PlannedSetEditorView: View {
    let model: RoutineEditorModel
    let slotKey: String
    let setKey: String

    @Environment(AppModel.self) private var app
    @State private var customRest = false

    /// `W` for a warm-up, else the working set's number.
    static func label(_ planned: PlannedSet, in slot: RoutineSlot) -> String {
        guard planned.role == .working else { return "W" }
        let working = slot.sets.filter { $0.role == .working }
        return String((working.firstIndex { $0.key == planned.key } ?? 0) + 1)
    }

    enum Effort: String, CaseIterable {
        case none = "None", rpe = "RPE", rir = "RIR"
    }

    var body: some View {
        if let slot = model.editor.slot(slotKey), let planned = slot.sets.first(where: { $0.key == setKey }) {
            let measurement = app.exercises[slot.exercise]?.measurement ?? .weightreps
            Form {
                Section {
                    Picker("Role", selection: binding(planned.role) { $0.role = $1 }) {
                        Text("Warm-up").tag(Role.warmup)
                        Text("Working").tag(Role.working)
                    }
                    .pickerStyle(.segmented)
                    if planned.role == .working {
                        Picker("Label", selection: binding(planned.roleLabel) { $0.roleLabel = $1 }) {
                            Text("None").tag(RoleLabel?.none)
                            Text("Top set").tag(RoleLabel?.some(.top))
                            Text("Back-off").tag(RoleLabel?.some(.backoff))
                        }
                    }
                    if planned.structure == .straight {
                        Picker("Ends", selection: binding(ending(planned)) { set, ending in set.termination = ending }) {
                            Text("As planned").tag(Termination?.none)
                            Text("As many as possible").tag(Termination?.some(.amrap))
                            Text("At failure").tag(Termination?.some(.failure))
                            if let other = planned.termination, ![.amrap, .failure].contains(other) {
                                Text(other.rawValue).tag(Termination?.some(other))
                            }
                        }
                    } else {
                        LabeledContent("Type", value: RoutinePresenter.plannedSetText(planned, measurement: measurement))
                    }
                }
                target(planned, measurement: measurement)
                effort(planned)
                Section {
                    RestMenuRow(title: "Rest after this set", current: planned.rest, select: { seconds in
                        update { $0.rest = seconds }
                    }, custom: { customRest = true })
                } footer: {
                    Text("Default uses the exercise\u{2019}s rest between sets.")
                }
            }
            .navigationTitle(planned.role == .warmup ? "Warm-up" : "Set \(Self.label(planned, in: slot))")
            .navigationBarTitleDisplayMode(.inline)
            .sheet(isPresented: $customRest) {
                RestPickerSheet(title: "Rest after this set", initial: planned.rest ?? 90) { seconds in update { $0.rest = seconds } }
            }
        } else {
            ContentUnavailableView("Set removed", systemImage: "list.number")
        }
    }

    private func ending(_ planned: PlannedSet) -> Termination? {
        [.reps, .reprange].contains(planned.termination) ? nil : planned.termination
    }

    @ViewBuilder
    private func target(_ planned: PlannedSet, measurement: MeasurementType) -> some View {
        Section("Target") {
            if measurement.isTimedHold || planned.durationMin != nil {
                let low = planned.durationMin ?? 30
                let high = planned.durationMax ?? low
                Stepper(value: Binding { low } set: { value in update { $0.durationMin = value; $0.durationMax = max(value, $0.durationMax ?? value) } },
                        in: 5 ... 3600, step: 5) {
                    LabeledContent("From", value: Format.duration(low))
                }
                Stepper(value: Binding { high } set: { value in update { $0.durationMax = value; $0.durationMin = min(value, $0.durationMin ?? value) } },
                        in: 5 ... 3600, step: 5) {
                    LabeledContent("To", value: Format.duration(high))
                }
            } else if let low = planned.repsMin ?? planned.repsMax {
                let high = planned.repsMax ?? low
                Stepper(value: Binding { low } set: { value in update { $0.repsMin = value; $0.repsMax = max(value, $0.repsMax ?? value) } },
                        in: 1 ... 100) {
                    LabeledContent("Reps from", value: "\(low)")
                }
                Stepper(value: Binding { high } set: { value in update { $0.repsMax = value; $0.repsMin = min(value, $0.repsMin ?? value) } },
                        in: 1 ... 100) {
                    LabeledContent("Reps to", value: "\(high)")
                }
                Button("No rep target", role: .destructive) { update { $0.repsMin = nil; $0.repsMax = nil } }
                    .font(.subheadline)
            } else {
                Button("Add a rep target", systemImage: "plus") { update { $0.repsMin = 8; $0.repsMax = 12 } }
            }
        }
    }

    @ViewBuilder
    private func effort(_ planned: PlannedSet) -> some View {
        let mode: Effort = planned.rpeMin != nil || planned.rpeMax != nil ? .rpe : planned.rirMin != nil || planned.rirMax != nil ? .rir : .none
        Section("Effort") {
            Picker("Effort", selection: Binding { mode } set: { next in setEffort(next, planned) }) {
                ForEach(Effort.allCases, id: \.self) { Text($0.rawValue).tag($0) }
            }
            .pickerStyle(.segmented)
            switch mode {
            case .rpe:
                effortSteppers(low: planned.rpeMin ?? planned.rpeMax ?? 8, high: planned.rpeMax ?? planned.rpeMin ?? 8, range: 1 ... 10,
                               name: "RPE") { low, high in update { $0.rpeMin = low; $0.rpeMax = high } }
            case .rir:
                effortSteppers(low: planned.rirMin ?? planned.rirMax ?? 2, high: planned.rirMax ?? planned.rirMin ?? 2, range: 0 ... 10,
                               name: "RIR") { low, high in update { $0.rirMin = low; $0.rirMax = high } }
            case .none:
                EmptyView()
            }
        }
    }

    private func effortSteppers(low: Decimal, high: Decimal, range: ClosedRange<Double>, name: String,
                                set: @escaping (Decimal, Decimal) -> Void) -> some View
    {
        let lowValue = NSDecimalNumber(decimal: low).doubleValue
        let highValue = NSDecimalNumber(decimal: high).doubleValue
        return Group {
            Stepper(value: Binding { lowValue } set: { value in set(Decimal(value), Decimal(max(value, highValue))) }, in: range, step: 0.5) {
                LabeledContent("\(name) from", value: Format.number(low))
            }
            Stepper(value: Binding { highValue } set: { value in set(Decimal(min(value, lowValue)), Decimal(value)) }, in: range, step: 0.5) {
                LabeledContent("\(name) to", value: Format.number(high))
            }
        }
    }

    private func setEffort(_ mode: Effort, _ planned: PlannedSet) {
        update { set in
            switch mode {
            case .none:
                set.rpeMin = nil; set.rpeMax = nil; set.rirMin = nil; set.rirMax = nil
            case .rpe:
                set.rirMin = nil; set.rirMax = nil
                if set.rpeMax == nil { set.rpeMin = 7; set.rpeMax = 8 }
            case .rir:
                set.rpeMin = nil; set.rpeMax = nil
                if set.rirMax == nil { set.rirMin = 1; set.rirMax = 2 }
            }
        }
    }

    private func update(_ change: @escaping (inout PlannedSet) -> Void) {
        model.change { $0.updateSet(slotKey, setKey, change) }
    }

    private func binding<Value>(_ value: Value, _ write: @escaping (inout PlannedSet, Value) -> Void) -> Binding<Value> {
        Binding { value } set: { next in update { write(&$0, next) } }
    }
}
