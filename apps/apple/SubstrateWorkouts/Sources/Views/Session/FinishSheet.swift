import WorkoutsCore
import SwiftUI

/// The Finish sheet (DESIGN.md §7.7): editable start and end, clamped as the
/// engine will; the duration that follows them; an optional session RPE on
/// the CR10 scale; what happens to unticked sets, with the dialog's choice
/// still open to change and a way back to fill in the ones missing a value;
/// the summary as labelled values, which is what Save writes; notes; and
/// where the session will be saved. Save never waits for Apple Health.
struct FinishSheet: View {
    /// Back to the session, to the first set missing a value; the sheet closes first.
    let fillMissing: () -> Void

    @Environment(SessionController.self) private var session
    @Environment(\.dismiss) private var dismiss
    /// What Save does with unticked rows: the dialog's choice, changeable here.
    @State private var policy: PendingSetPolicy
    @State private var start = Date.now
    @State private var end = Date.now
    @State private var sessionRPE: Int?
    @State private var notes = ""
    @State private var saving = false
    @State private var failed = false
    @State private var loaded = false
    /// Saved: the sheet is closing and keeps showing what was saved.
    @State private var closed = false
    /// The summary for the dates and policy shown, worked out when they
    /// change (not on every keystroke in Notes). Kept through Save: the
    /// session is gone (or is the next one) while the sheet closes, and the
    /// sheet keeps showing what was saved.
    @State private var summary: FinishSummary?

    init(policy: PendingSetPolicy, fillMissing: @escaping () -> Void = {}) {
        _policy = State(initialValue: policy)
        self.fillMissing = fillMissing
    }

    var body: some View {
        NavigationStack {
            if let summary {
                form(summary)
            } else {
                ContentUnavailableView("No session in progress", systemImage: "dumbbell")
            }
        }
        .presentationDetents([.large])
        .interactiveDismissDisabled(saving)
        .onAppear(perform: load)
        .onChange(of: start) { refresh() }
        .onChange(of: end) { refresh() }
        .onChange(of: policy) { refresh() }
        .onChange(of: session.presentation) { refresh() }
    }

    private func form(_ summary: FinishSummary) -> some View {
        Form {
            Section {
                Text(summary.title)
                    .font(.headline)
                DatePicker("Started", selection: $start, in: ...(summary.latestStart ?? end))
                DatePicker("Ended", selection: $end, in: (summary.earliestEnd ?? start)...)
                LabelledValueView(label: "Duration", value: summary.durationText)
            }
            Section {
                rpeButtons
                if let sessionRPE, let word = SessionRPEText.word(sessionRPE) {
                    Text(word.capitalizedSentence)
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(.secondary)
                }
            } header: {
                HStack {
                    Text("How hard was the session?")
                    Spacer()
                    if sessionRPE != nil {
                        Button("Skip") { sessionRPE = nil }
                            .font(.subheadline)
                            .textCase(nil)
                    }
                }
            }
            if summary.untickedCount > 0 {
                untickedSection(summary)
            }
            Section {
                ForEach(summary.rows) { row in
                    LabelledValueView(row)
                }
            } footer: {
                // What the choice above does to the unticked sets, under the numbers it gives.
                if let pending = summary.pendingText {
                    Text(pending)
                }
            }
            Section {
                TextField("How it went", text: $notes, axis: .vertical)
                    .lineLimit(2 ... 6)
            } header: {
                Text("Notes")
            } footer: {
                Text(summary.savedLine)
            }
        }
        .navigationTitle("Finish session")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("Cancel", role: .cancel) {
                    session.startAfterFinish = nil
                    dismiss()
                }
                .disabled(saving)
            }
            ToolbarItem(placement: .confirmationAction) {
                Button("Save", action: save)
                    .disabled(saving)
            }
        }
        .alert("The session could not be saved", isPresented: $failed) {
            Button("OK", role: .cancel) {}
        } message: {
            if session.journalWriteFailed {
                Text("It is still open, but recent changes could not be stored on this iPhone. Free some storage, then try Save again.")
            } else {
                Text("It is still open. Try Save again; nothing was lost.")
            }
        }
    }

    /// "3 sets are not ticked": the dialog's two choices, the chosen one
    /// ticked, and, when some cannot be marked done, the way back to them.
    private func untickedSection(_ summary: FinishSummary) -> some View {
        Section {
            Picker(summary.untickedTitle ?? "Unticked sets", selection: $policy) {
                Text(FinishSummary.dropTitle).tag(PendingSetPolicy.drop)
                Text(FinishSummary.markDoneTitle).tag(PendingSetPolicy.markDone)
            }
            .pickerStyle(.inline)
            .labelsHidden()
            .disabled(saving)
            if summary.projection.incompleteSets > 0 {
                Button {
                    session.startAfterFinish = nil
                    dismiss()
                    fillMissing()
                } label: {
                    Label(FinishSheet.fillMissingTitle, systemImage: "square.and.pencil")
                }
                .disabled(saving)
            }
        } header: {
            if let title = summary.untickedTitle {
                Text(title)
                    .textCase(nil)
            }
        } footer: {
            if let message = summary.untickedMessage {
                Text(message)
            }
        }
    }

    /// The way back from Finish to the first set with a value missing.
    static let fillMissingTitle = "Fill in the missing values"

    /// Ten buttons for the CR10 scale, two rows of five so each is a good
    /// thumb target. 0 is not offered: Apple's effort score runs from 1.
    private var rpeButtons: some View {
        Grid(horizontalSpacing: 8, verticalSpacing: 8) {
            ForEach([Array(1 ... 5), Array(6 ... 10)], id: \.self) { values in
                GridRow {
                    ForEach(values, id: \.self) { value in
                        Button {
                            sessionRPE = value
                        } label: {
                            Text("\(value)")
                                .font(.body.monospacedDigit().weight(.semibold))
                                .frame(maxWidth: .infinity, minHeight: 40)
                        }
                        .buttonStyle(.bordered)
                        .tint(sessionRPE == value ? .accentColor : .secondary)
                        .accessibilityLabel("\(value), \(SessionRPEText.word(value) ?? "")")
                        .accessibilityAddTraits(sessionRPE == value ? .isSelected : [])
                    }
                }
            }
        }
        .padding(.vertical, 4)
    }

    private func load() {
        guard !loaded, let first = session.finishSummary(end: .now, policy: policy) else { return }
        loaded = true
        summary = first
        start = first.start
        end = first.end
        notes = session.state?.session.notes ?? ""
        sessionRPE = session.state?.session.sessionRPE.map { NSDecimalNumber(decimal: $0).intValue }
    }

    /// The summary again for the dates and policy shown. Not while saving:
    /// the sheet keeps what it is saving.
    private func refresh() {
        guard loaded, !saving, !closed, let fresh = session.finishSummary(start: start, end: end, policy: policy) else { return }
        summary = fresh
    }

    private func save() {
        saving = true
        Task {
            let done = await session.finish(start: start, end: end, policy: policy, sessionRPE: sessionRPE, notes: notes)
            if done {
                closed = true
                saving = false
                dismiss()
            } else {
                saving = false
                failed = true
                refresh()
            }
        }
    }
}

#if DEBUG
#Preview {
    let app = PreviewData.app()
    Color.clear.sheet(isPresented: .constant(true)) {
        FinishSheet(policy: .drop)
            .environment(app)
            .environment(PreviewData.session(app))
    }
}
#endif
