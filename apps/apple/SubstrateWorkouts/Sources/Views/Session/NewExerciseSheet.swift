import WorkoutsCore
import SwiftUI

/// "New exercise…" (DESIGN.md §7.6): name, what it records, equipment, sides
/// and rest class. It lives on this iPhone ("On this iPhone"); its id is the
/// name's slug, `-2`, `-3` when taken.
struct NewExerciseSheet: View {
    let initialName: String
    let created: (Exercise) -> Void

    @Environment(AppModel.self) private var app
    @State private var name = ""
    @State private var measurement: MeasurementType = .weightreps
    @State private var equipment: Equipment = .machine
    @State private var laterality: Laterality = .bilateral
    @State private var restClass: RestClass?
    @State private var failure: String?

    private var trimmed: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        Form {
            Section {
                TextField("Name", text: $name)
                    .textInputAutocapitalization(.words)
            }
            Section {
                Picker("Records", selection: $measurement) {
                    ForEach(MeasurementType.allCases, id: \.self) { Text($0.label).tag($0) }
                }
                Picker("Equipment", selection: $equipment) {
                    ForEach(Equipment.allCases, id: \.self) { Text($0.label).tag($0) }
                }
                Picker("Sides", selection: $laterality) {
                    ForEach(Laterality.allCases, id: \.self) { Text($0.label).tag($0) }
                }
                Picker("Rest class", selection: $restClass) {
                    Text("None").tag(RestClass?.none)
                    ForEach(RestClass.allCases, id: \.self) { Text($0.label).tag(RestClass?.some($0)) }
                }
            } footer: {
                Text("Kept on this iPhone. The rest class picks the default rest between sets.")
            }
        }
        .navigationTitle("New exercise")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Add", action: save)
                    .disabled(trimmed.isEmpty)
            }
        }
        .alert("The exercise could not be saved", isPresented: Binding { failure != nil } set: { if !$0 { failure = nil } }) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(failure ?? "")
        }
        .onAppear { if name.isEmpty { name = initialName } }
    }

    private func save() {
        let id = ExerciseID.slug(for: trimmed, taken: Set(app.exercises.keys))
        let exercise = Exercise(id: id, name: trimmed, measurement: measurement, equipment: equipment, laterality: laterality,
                                restClass: restClass, isLocal: true)
        do {
            created(try app.addLocalExercise(exercise))
        } catch {
            failure = error.localizedDescription
        }
    }
}

#if DEBUG
#Preview {
    NavigationStack {
        NewExerciseSheet(initialName: "Cable Fly") { _ in }
    }
    .environment(PreviewData.app())
}
#endif
