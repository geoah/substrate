import ChatKit
import SubstrateKit
import SwiftUI

/// The agent's questions, inline in the thread that asked them. Answering is
/// one patch with the version the owner read; the engine checks every
/// selection against the stored option values. What the owner picks stays
/// here until it is sent.
struct QuestionCardView: View {
    @Environment(ChatHost.self) private var host
    let id: String
    let card: QuestionCard
    @State private var picked: [String: Set<String>] = [:]

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label(card.title, systemImage: "questionmark.bubble")
                .font(.subheadline.weight(.semibold))
            if card.loading {
                ProgressView().controlSize(.small)
            }
            ForEach(card.questions) { question in
                VStack(alignment: .leading, spacing: 6) {
                    Text(question.prompt).font(.subheadline)
                    ForEach(question.options) { option in
                        let chosen = card.pending ? picked[question.id, default: []].contains(option.value) : option.chosen
                        Button {
                            toggle(question, option.value)
                        } label: {
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                Image(
                                    systemName: question.multi
                                        ? (chosen ? "checkmark.square.fill" : "square")
                                        : (chosen ? "largecircle.fill.circle" : "circle")
                                )
                                .foregroundStyle(chosen ? Color.accentColor : .secondary)
                                VStack(alignment: .leading, spacing: 1) {
                                    Text(option.label)
                                    if let description = option.description {
                                        Text(description).font(.caption).foregroundStyle(.secondary)
                                    }
                                }
                                Spacer(minLength: 0)
                            }
                            .padding(.vertical, 4)
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        .disabled(!card.pending || card.busy != nil)
                    }
                }
            }
            if let problem = card.problem {
                Text(problem).font(.footnote).foregroundStyle(.red)
            }
            if card.pending {
                HStack {
                    Button {
                        host.send(.answer(question: id, picks: picked.mapValues { Array($0).sorted() }))
                    } label: {
                        if card.busy == "answer" { ProgressView().controlSize(.small) } else { Text(card.answer) }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(card.busy != nil || !card.questions.allSatisfy { !(picked[$0.id]?.isEmpty ?? true) })
                    Button(card.dismiss) { host.send(.dismissQuestions(id)) }
                        .buttonStyle(.bordered)
                        .disabled(card.busy != nil)
                }
            }
        }
        .card(tint: card.pending ? .orange : nil)
        .task(id: id) { host.send(.loadCard(.question(id))) }
    }

    private func toggle(_ question: QuestionCard.Question, _ value: String) {
        if question.multi {
            picked[question.id, default: []].formSymmetricDifference([value])
        } else {
            picked[question.id] = [value]
        }
    }
}

/// A suggested change: the record, each property before and after, Apply and
/// Dismiss; once decided, the change that was applied or the one that was
/// not.
struct SuggestionCardView: View {
    @Environment(ChatHost.self) private var host
    let id: String
    let card: SuggestionCard

    var body: some View {
        Group {
            if card.decided {
                ChangePanel(panel: card.panel)
            } else {
                proposed
            }
        }
        .task(id: id) { host.send(.loadCard(.suggestion(id))) }
    }

    private var proposed: some View {
        VStack(alignment: .leading, spacing: 10) {
            ChangeHeaderView(heading: card.panel.heading)
            if let rationale = card.rationale {
                Text(rationale).font(.footnote).foregroundStyle(.secondary)
            }
            ForEach(card.values) { line in
                VStack(alignment: .leading, spacing: 3) {
                    Text(line.label).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                    if let before = line.before {
                        RecordValueView(before, struck: true).font(.footnote).lineLimit(4)
                    }
                    RecordValueView(line.value).font(.footnote).lineLimit(6)
                }
            }
            if let problem = card.problem {
                Text(problem).font(.footnote).foregroundStyle(.red)
            }
            if let console = card.console.flatMap(URL.init(string:)) {
                // Without `decide:` the request is not the app's to decide.
                if let note = card.consoleNote {
                    Text(note).font(.footnote).foregroundStyle(.secondary)
                }
                ConsoleLink(title: "Open in the Console", destination: console)
                    .font(.footnote.weight(.semibold))
            } else {
                HStack {
                    Button {
                        host.send(.decide(suggestion: id, accept: true))
                    } label: {
                        if card.busy == "accept" { ProgressView().controlSize(.small) } else { Text(card.apply) }
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(card.destructive ? .red : .accentColor)
                    Button(card.dismiss) { host.send(.decide(suggestion: id, accept: false)) }
                        .buttonStyle(.bordered)
                }
                .disabled(card.busy != nil)
            }
        }
        .card(tint: .blue)
    }
}

/// A call an agent wants to make that waits for the owner: who asks, the
/// tool, exactly what it would run with, then Approve or Decline; once
/// decided, where it ended. Approving is one patch on the version read, and
/// the engine runs the stored call, never anything the app sends.
struct ApprovalCardView: View {
    @Environment(ChatHost.self) private var host
    let id: String
    let card: ApprovalCard

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                Label(card.title, systemImage: symbol)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(card.tone == .failed ? Color.red : Color.primary)
                Spacer(minLength: 0)
                if card.tone == .running { ProgressView().controlSize(.small) }
            }
            if card.loading {
                ProgressView().controlSize(.small)
            } else {
                HStack(alignment: .firstTextBaseline, spacing: 4) {
                    Text(card.asks)
                    Text(card.tool).monospaced()
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
            }
            if !card.arguments.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(card.arguments) { argument in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(argument.label).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                            // The model wrote it: plain text, never Markdown.
                            // While it waits every line shows: the owner
                            // approves all of it, not what fits.
                            Text(verbatim: argument.value)
                                .font(.footnote)
                                .lineLimit(card.pending ? nil : 3)
                                .textSelection(.enabled)
                        }
                    }
                }
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(.secondary.opacity(0.08), in: .rect(cornerRadius: 10))
            }
            if let error = card.error {
                Text(verbatim: error).font(.footnote).foregroundStyle(.red)
            }
            if let problem = card.problem {
                Text(problem).font(.footnote).foregroundStyle(.red)
            }
            if card.pending {
                HStack {
                    Button {
                        host.send(.decideApproval(id, approve: true))
                    } label: {
                        if card.busy == "approve" { ProgressView().controlSize(.small) } else { Text(card.approve) }
                    }
                    .buttonStyle(.borderedProminent)
                    Button {
                        host.send(.decideApproval(id, approve: false))
                    } label: {
                        if card.busy == "decline" { ProgressView().controlSize(.small) } else { Text(card.decline) }
                    }
                    .buttonStyle(.bordered)
                }
                .disabled(card.busy != nil)
            }
        }
        .card(tint: tint)
        .task(id: id) { host.send(.loadCard(.approval(id))) }
    }

    private var symbol: String {
        switch card.tone {
        case .waiting: "hand.raised"
        case .running: "play.circle"
        case .done: "checkmark.circle"
        case .failed: "exclamationmark.triangle"
        case .declined: "xmark.circle"
        }
    }

    private var tint: Color? {
        switch card.tone {
        case .waiting: .orange
        case .running: .blue
        case .done: .green
        case .failed: .red
        case .declined: nil
        }
    }
}

/// "Changed [Groceries]", "Suggests adding a task": the op's mark, the verb,
/// and the record as its pill where there is one to open.
struct ChangeHeaderView: View {
    let heading: ChangeHeading

    var body: some View {
        HStack(alignment: .center, spacing: 6) {
            Image(systemName: symbol)
                .frame(width: 20)
            Text(heading.verb)
                .fixedSize()
            if let record = heading.record {
                RecordPill(record, title: heading.recordTitle)
            } else {
                Text(heading.words).lineLimit(1)
            }
            Spacer(minLength: 8)
            if let badge = heading.badge, let label = heading.badgeLabel {
                let applied = badge == .applied
                Label(label, systemImage: applied ? "checkmark" : "xmark")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(applied ? Color.green : Color.secondary)
                    .padding(.horizontal, 8).padding(.vertical, 3)
                    .background((applied ? Color.green : Color.secondary).opacity(0.15), in: .capsule)
                    .fixedSize()
            }
        }
        .font(.subheadline.weight(.semibold))
    }

    private var symbol: String {
        switch heading.symbol {
        case .delete: "trash"
        case .create: "plus.square"
        case .edit: "square.and.pencil"
        }
    }
}

/// What a change wrote: the record, then each value it set.
struct ChangePanel: View {
    let panel: ChangePanelPresentation

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ChangeHeaderView(heading: panel.heading)
            if let note = panel.note {
                Text(note).font(.footnote).foregroundStyle(.secondary)
            }
            ForEach(panel.values) { line in
                VStack(alignment: .leading, spacing: 3) {
                    Text(line.label).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                    RecordValueView(line.value, struck: panel.struck)
                        .font(.footnote)
                        .lineLimit(6)
                }
            }
            if let more = panel.more {
                Text(more).font(.caption).foregroundStyle(.secondary)
            }
        }
        .card(tint: tint)
        .accessibilityElement(children: .combine)
    }

    private var tint: Color? {
        switch panel.tint {
        case .plain: nil
        case .applied: .green
        case .suggested: .blue
        }
    }
}

/// Writes a provider's API key. The key lives in the field until it is sent:
/// never kept, never logged.
struct ProviderKeySheet: View {
    @Environment(ChatHost.self) private var host
    @Environment(\.dismiss) private var dismiss
    let sheet: KeySheet
    @State private var key = ""
    @State private var busy = false
    /// Save was pressed once: a problem is the server's answer to it.
    @State private var tried = false

    var body: some View {
        NavigationStack {
            Form {
                if let console = sheet.console.flatMap(URL.init(string:)) {
                    Section {
                        Text(sheet.consoleNote)
                        ConsoleLink(title: sheet.consoleLink, destination: console)
                    } header: {
                        Text(sheet.header)
                    } footer: {
                        Text(sheet.footer)
                    }
                } else {
                    Section {
                        SecureField("API key", text: $key)
                            .textContentType(.password)
                            #if os(iOS)
                        .textInputAutocapitalization(.never)
                            #endif
                            .autocorrectionDisabled()
                    } header: {
                        Text(sheet.header)
                    } footer: {
                        Text(sheet.footer)
                    }
                    if let problem = host.chats?.keys[sheet.id]?.problem, busy == false, tried {
                        Section { ProblemBox(message: problem) }
                    }
                }
            }
            .formStyle(.grouped)
            .navigationTitle(sheet.title)
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                if sheet.console == nil {
                    ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Save") { save() }
                            .disabled(key.trimmingCharacters(in: .whitespaces).isEmpty || busy)
                    }
                } else {
                    ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
                }
            }
        }
        .frame(minWidth: 380, minHeight: 260)
        .onChange(of: host.savedKey) { _, saved in
            if busy, saved == sheet.id { dismiss() }
        }
        .onChange(of: host.chats?.keys[sheet.id]?.problem) { _, problem in
            if problem != nil { busy = false }
        }
    }

    private func save() {
        busy = true
        tried = true
        host.addKey(provider: sheet.id, key: key)
    }
}
