import SwiftUI
import UniformTypeIdentifiers
import WorkoutsCore
import WorkoutsSync

/// Settings → Import → Hevy: an account read through Hevy's API with its API
/// key (workouts, routines and the account's own exercises), or the workout
/// export file Hevy writes, read into sessions and routines (`HevyImport`),
/// shown before anything is saved, then added. Importing again adds only
/// what is new.
struct HevyImportView: View {
    @Environment(AppModel.self) private var app
    @Environment(SubstrateLink.self) private var link
    @State private var apiKey = ""
    @State private var choosing = false
    @State private var stage: Stage = .start
    /// What was read, kept so a change of the choices below plans again
    /// without reading Hevy again.
    @State private var read: Read?
    @State private var pairs: HevyImport.PairedWeights = .each
    @State private var zone: TimeZone = .current

    private enum Read {
        case account(HevyAPI.Export, library: [ExerciseID: Exercise])
        case file(name: String, HevyCSV.File, library: [ExerciseID: Exercise])
    }

    private enum Stage {
        case start
        case reading(String)
        case ready(source: String, plan: HevyImport.Plan)
        case importing(source: String, plan: HevyImport.Plan)
        case done(imported: Int, exercises: Int, routines: Int, routineProblem: String?)
        case failed(String)
    }

    var body: some View {
        Form {
            Section {
                SecureField("API key", text: $apiKey)
                    .textContentType(.password)
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                Button {
                    readAccount()
                } label: {
                    Label("Read from Hevy", systemImage: "arrow.down.circle")
                }
                .disabled(apiKey.trimmingCharacters(in: .whitespaces).isEmpty || isBusy)
            } header: {
                Text("Hevy account")
            } footer: {
                Text("Reads your workouts, routines, body measurements and the exercises you made in Hevy. Get the key on hevy.com \u{2192} Settings \u{2192} Developer (it needs Hevy Pro). Nothing is written to Hevy, and the key is not kept.")
            }

            Section {
                Picker("A pair of dumbbells was logged as", selection: $pairs) {
                    Text("One dumbbell's weight").tag(HevyImport.PairedWeights.each)
                    Text("Both together").tag(HevyImport.PairedWeights.together)
                }
            } header: {
                Text("How you logged")
            } footer: {
                Text("Substrate Workouts counts one dumbbell or kettlebell of a pair. If you logged the two together in Hevy (40 kg for two 20 kg dumbbells), choose Both together and they are halved on import.")
            }
            .onChange(of: pairs) { _, _ in replan() }

            Section {
                Button {
                    choosing = true
                } label: {
                    Label("Choose the CSV file", systemImage: "doc.badge.plus")
                }
                .disabled(isBusy)
            } header: {
                Text("Or an export file")
            } footer: {
                Text("In Hevy: Profile \u{2192} Settings \u{2192} Export & Import Data \u{2192} Export Data \u{2192} Export Workouts. The file holds workouts only, no routines. Nothing is saved until you import.")
            }

            switch stage {
            case .start:
                EmptyView()
            case let .reading(step):
                Section { ProgressView(step) }
            case let .ready(source, plan), let .importing(source, plan):
                summary(source: source, plan: plan)
                importSection(plan)
            case let .done(imported, exercises, routines, problem):
                doneSection(imported: imported, exercises: exercises, routines: routines, problem: problem)
            case let .failed(reason):
                Section {
                    Label(reason, systemImage: "exclamationmark.triangle")
                        .foregroundStyle(.orange)
                }
            }
        }
        .navigationTitle("Import from Hevy")
        .navigationBarTitleDisplayMode(.inline)
        .fileImporter(isPresented: $choosing, allowedContentTypes: [.commaSeparatedText, .plainText, .text]) { result in
            switch result {
            case let .success(url): readFile(url)
            case let .failure(error): stage = .failed(error.localizedDescription)
            }
        }
    }

    private var isBusy: Bool {
        switch stage {
        case .reading, .importing: true
        default: false
        }
    }

    // MARK: - Sections

    @ViewBuilder
    private func summary(source: String, plan: HevyImport.Plan) -> some View {
        Section {
            LabeledContent("Read from", value: source)
            LabeledContent("Workouts to import", value: Format.grouped(plan.sessions.count))
            LabeledContent("Sets", value: Format.grouped(plan.setCount))
            if let first = plan.sessions.first?.session.at, let last = plan.sessions.last?.session.at {
                LabeledContent("From", value: Format.dayWithYear(first, in: plan.timeZone))
                LabeledContent("To", value: Format.dayWithYear(last, in: plan.timeZone))
            }
            if case .file? = read {
                NavigationLink {
                    ZonePicker(zone: $zone)
                } label: {
                    LabeledContent("Times read in", value: zone.localizedName(for: .generic, locale: .current) ?? zone.identifier)
                }
            }
            if !plan.measurements.isEmpty {
                LabeledContent("Body measurements", value: Format.grouped(plan.measurements.count))
            }
        } header: {
            Text("Workouts")
        } footer: {
            if case .file? = read {
                Text("The file's times have no zone: they are read in the zone above, the one you trained in.")
            }
        }
        .onChange(of: zone) { _, _ in replan() }

        if !plan.routines.isEmpty || !plan.routinesHeld.isEmpty {
            Section {
                NavigationLink {
                    NameList(title: "Routines to add", names: plan.routines.map { routine in
                        routine.folder.map { "\(routine.name) (\($0))" } ?? routine.name
                    })
                } label: {
                    LabeledContent("Routines to add", value: "\(plan.routines.count)")
                }
                .disabled(plan.routines.isEmpty)
                if !plan.routinesHeld.isEmpty {
                    NavigationLink {
                        NameList(title: "Routines already here", names: plan.routinesHeld.sorted())
                    } label: {
                        LabeledContent("Already here", value: "\(plan.routinesHeld.count)")
                    }
                }
            } header: {
                Text("Routines")
            } footer: {
                Text(link.session == nil
                    ? "Routines and body measurements live on your substrate: sign in to one, then import again to add them."
                    : "Each routine keeps its folder, exercises, supersets, sets and rests. A routine here already is left as it is. Workouts started from a routine are linked to it.")
            }
        }

        Section {
            NavigationLink {
                NameList(title: "New exercises", names: plan.exercises.map(\.name).sorted())
            } label: {
                LabeledContent("New exercises", value: "\(plan.exercises.count)")
            }
            .disabled(plan.exercises.isEmpty)
            NavigationLink {
                NameList(title: "Matched exercises", names: plan.matched.map { title, id in
                    let name = app.exercises[id]?.name ?? id.rawValue
                    return name == title ? title : "\(title) \u{2192} \(name)"
                }.sorted())
            } label: {
                LabeledContent("Matched to your exercises", value: "\(plan.matched.count)")
            }
            .disabled(plan.matched.isEmpty)
        } header: {
            Text("Exercises")
        } footer: {
            Text("A Hevy exercise is matched to yours by Hevy's id, else by name. The rest come from the exercise library, movement and muscles included, or from your own Hevy exercises.")
        }

        if !plan.skipped.isEmpty || !plan.problems.isEmpty || !plan.weightsNotRecorded.isEmpty {
            Section {
                if !plan.skipped.isEmpty {
                    NavigationLink {
                        SkippedList(skipped: plan.skipped, timeZone: plan.timeZone)
                    } label: {
                        LabeledContent("Workouts skipped", value: "\(plan.skipped.count)")
                    }
                }
                if !plan.weightsNotRecorded.isEmpty {
                    NavigationLink {
                        NameList(title: "Weights not recorded", names: plan.weightsNotRecorded.sorted { $0.key < $1.key }.map { title, count in
                            "\(title): \(Format.count(count, "set"))"
                        })
                    } label: {
                        LabeledContent("Weights not recorded", value: "\(plan.weightsNotRecorded.values.reduce(0, +))")
                    }
                }
                if !plan.problems.isEmpty {
                    NavigationLink {
                        ProblemList(problems: plan.problems)
                    } label: {
                        LabeledContent("Rows that could not be read", value: "\(plan.problems.count)")
                    }
                }
            } header: {
                Text("Left out")
            } footer: {
                Text("A workout already imported, deleted from History, or overlapping a session you logged here is skipped. A weight is not recorded on a set whose matched exercise counts reps or time only.")
            }
        }
    }

    @ViewBuilder
    private func importSection(_ plan: HevyImport.Plan) -> some View {
        let routines = link.session == nil ? 0 : plan.routines.count
        Section {
            Button {
                run(plan)
            } label: {
                HStack {
                    Text(title(workouts: plan.sessions.count, routines: routines))
                    Spacer()
                    if case .importing = stage { ProgressView() }
                }
            }
            .disabled((plan.sessions.isEmpty && routines == 0) || isBusy)
        } footer: {
            Text("Imported workouts join History and the suggestions. They are not saved to Apple Health; Hevy may have saved its own. \(destination)")
        }
    }

    private func title(workouts: Int, routines: Int) -> String {
        switch (workouts, routines) {
        case (0, 0): "Nothing new to import"
        case (_, 0): "Import \(Format.count(workouts, "workout"))"
        case (0, _): "Import \(Format.count(routines, "routine"))"
        default: "Import \(Format.count(workouts, "workout")) and \(Format.count(routines, "routine"))"
        }
    }

    private func doneSection(imported: Int, exercises: Int, routines: Int, problem: String?) -> some View {
        Section {
            Label("Imported \(Format.count(imported, "workout"))", systemImage: "checkmark.circle")
            if routines > 0 {
                LabeledContent("Routines added", value: "\(routines)")
            }
            if exercises > 0 {
                LabeledContent("Exercises added", value: "\(exercises)")
            }
            if let problem {
                Label("Routines not added: \(problem)", systemImage: "exclamationmark.triangle")
                    .foregroundStyle(.orange)
            }
        } footer: {
            Text(destination)
        }
    }

    private var destination: String {
        if let host = link.host { return "They are sent to \(host) at the next sync." }
        return "They stay on this iPhone until you sign in to a substrate."
    }

    // MARK: - Actions

    private struct Inputs: Sendable {
        var exercises: [ExerciseID: Exercise]
        var routines: [RoutineID: Routine]
        var history: [SessionRecords]
        var deleted: Set<String>
        var adapter: String
    }

    private var inputs: Inputs {
        Inputs(exercises: app.exercises, routines: app.catalog?.routines ?? [:], history: app.history.map(\.records),
               deleted: (app.tombstones?.all ?? []).union(link.tombstoneIDs), adapter: SessionController.adapter)
    }

    private func readAccount() {
        let client = HevyClient(apiKey: apiKey)
        stage = .reading("Reading your Hevy account\u{2026}")
        Task {
            do {
                let library = try await Task.detached(priority: .userInitiated) { try ExerciseLibrary.bundled().catalog().exercises }.value
                let known = Set(library.values.flatMap(\.codes).filter { $0.system == "hevy" }.map(\.code))
                let export = try await client.export(known: known) { step in
                    Task { @MainActor in stage = .reading(step) }
                }
                read = .account(export, library: library)
                replan()
            } catch {
                stage = .failed(error.localizedDescription)
            }
        }
    }

    private func readFile(_ url: URL) {
        stage = .reading("Reading the file\u{2026}")
        Task {
            let result = await Task.detached(priority: .userInitiated) { () -> Result<(HevyCSV.File, [ExerciseID: Exercise]), any Error> in
                let scoped = url.startAccessingSecurityScopedResource()
                defer { if scoped { url.stopAccessingSecurityScopedResource() } }
                do {
                    let data = try Data(contentsOf: url)
                    guard let text = String(data: data, encoding: .utf8) ?? String(data: data, encoding: .isoLatin1) else {
                        throw HevyImportError.unreadableText
                    }
                    return .success((try HevyCSV.read(text), (try? ExerciseLibrary.bundled().catalog().exercises) ?? [:]))
                } catch {
                    return .failure(error)
                }
            }.value
            switch result {
            case let .success((file, library)):
                read = .file(name: url.lastPathComponent, file, library: library)
                replan()
            case let .failure(error):
                stage = .failed(error.localizedDescription)
            }
        }
    }

    /// The plan for what was read, with the choices as they stand.
    private func replan() {
        guard let read else { return }
        if case .importing = stage { return }
        let inputs = inputs
        switch read {
        case let .account(export, library):
            let plan = HevyImport.plan(export, exercises: inputs.exercises, routines: inputs.routines, history: inputs.history,
                                       deleted: inputs.deleted, timeZone: .current, adapter: inputs.adapter, library: library, pairs: pairs)
            stage = .ready(source: "Your Hevy account", plan: plan)
        case let .file(name, file, library):
            let plan = HevyImport.plan(file, exercises: inputs.exercises, history: inputs.history, deleted: inputs.deleted, timeZone: zone,
                                       adapter: inputs.adapter, library: library, pairs: pairs)
            stage = .ready(source: name, plan: plan)
        }
    }

    private func run(_ plan: HevyImport.Plan) {
        guard case let .ready(source, _) = stage else { return }
        stage = .importing(source: source, plan: plan)
        Task {
            do {
                try await app.importSessions(plan.sessions, exercises: plan.exercises)
                var routines = 0
                var problem: String?
                if link.session != nil, !plan.routines.isEmpty || !plan.measurements.isEmpty {
                    do {
                        try await link.importRecords(routines: plan.routines, measurements: plan.measurements, exercises: plan.exercises)
                        routines = plan.routines.count
                    } catch {
                        problem = error.localizedDescription
                    }
                }
                stage = .done(imported: plan.sessions.count, exercises: plan.exercises.count, routines: routines, routineProblem: problem)
            } catch {
                stage = .failed(error.localizedDescription)
            }
            await link.syncAfterRunning()
        }
    }
}

/// Every zone the system knows, searchable: the one a CSV's wall-clock
/// times were kept in.
private struct ZonePicker: View {
    @Binding var zone: TimeZone
    @Environment(\.dismiss) private var dismiss
    @State private var query = ""

    var body: some View {
        let zones = TimeZone.knownTimeZoneIdentifiers.filter { query.isEmpty || $0.localizedCaseInsensitiveContains(query) }
        List(zones, id: \.self) { identifier in
            Button {
                if let picked = TimeZone(identifier: identifier) { zone = picked }
                dismiss()
            } label: {
                HStack {
                    Text(identifier.replacingOccurrences(of: "_", with: " "))
                    Spacer()
                    if identifier == zone.identifier { Image(systemName: "checkmark").foregroundStyle(.tint) }
                }
            }
            .foregroundStyle(.primary)
        }
        .searchable(text: $query, prompt: "Search time zones")
        .navigationTitle("Time zone")
        .navigationBarTitleDisplayMode(.inline)
    }
}

private struct NameList: View {
    let title: String
    let names: [String]

    var body: some View {
        List(names, id: \.self) { Text($0) }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
    }
}

private struct SkippedList: View {
    let skipped: [HevyImport.Skipped]
    let timeZone: TimeZone

    var body: some View {
        List(Array(skipped.enumerated()), id: \.offset) { _, item in
            VStack(alignment: .leading, spacing: 2) {
                Text(item.title)
                    .font(.subheadline.weight(.semibold))
                Text(Format.dayAndTime(item.start, in: timeZone))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Text(reason(item.reason))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .navigationTitle("Workouts skipped")
        .navigationBarTitleDisplayMode(.inline)
    }

    private func reason(_ skip: HevyImport.Skip) -> String {
        switch skip {
        case .imported: "Imported before"
        case .deleted: "Deleted from History"
        case .overlaps: "Overlaps a session in History"
        }
    }
}

private struct ProblemList: View {
    let problems: [HevyImport.Problem]

    var body: some View {
        List(problems, id: \.self) { problem in
            VStack(alignment: .leading, spacing: 2) {
                Text(problem.line > 0 ? "Line \(problem.line)" : "Hevy")
                    .font(.subheadline.weight(.semibold))
                Text(problem.reason)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .navigationTitle("Rows that could not be read")
        .navigationBarTitleDisplayMode(.inline)
    }
}

#if DEBUG
#Preview {
    NavigationStack {
        HevyImportView()
    }
    .environment(PreviewData.app())
    .environment(SubstrateLink.shared)
}
#endif
