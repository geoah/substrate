import ChatKit
import SubstrateKit
import SwiftUI

/// The agent's questions, inline in the thread that asked them, while they
/// wait: once answered or dismissed they are the mark on the call that
/// asked (`ResolvedCardView`). Answering is one patch with the version the
/// person read; the engine checks every selection against the stored option
/// values. What is picked stays here until it is sent. Beside the options
/// stands "Answer in Your Own Words", which takes the card off the
/// transcript and puts the cursor in the composer: what is sent from there
/// is the answer.
struct QuestionCardView: View {
    @Environment(ChatHost.self) private var host
    @Environment(\.conversationKey) private var conversation
    let id: String
    let card: QuestionCard
    @State private var picked: [String: Set<String>] = [:]

    var body: some View {
        VStack(alignment: .leading, spacing: ChatSpace.blocks) {
            Label(card.title, systemImage: "questionmark.bubble")
                .font(ChatType.heading)
            if card.loading {
                ProgressView().controlSize(.small)
            }
            ForEach(card.questions) { question in
                VStack(alignment: .leading, spacing: 0) {
                    Text(question.prompt).font(ChatType.detail)
                    ForEach(question.options) { option in
                        let chosen = card.pending ? picked[question.id, default: []].contains(option.value) : option.chosen
                        Button {
                            toggle(question, option.value)
                        } label: {
                            HStack(alignment: .firstTextBaseline, spacing: ChatSpace.beside) {
                                Image(
                                    systemName: question.multi
                                        ? (chosen ? "checkmark.square.fill" : "square")
                                        : (chosen ? "largecircle.fill.circle" : "circle")
                                )
                                .ink(chosen ? .accent : .quiet, on: .card)
                                VStack(alignment: .leading, spacing: ChatSpace.pair) {
                                    Text(option.label)
                                    if let description = option.description {
                                        Text(description).font(ChatType.caption).ink(.quiet, on: .card)
                                    }
                                }
                                Spacer(minLength: 0)
                            }
                            // An option is a whole target, and the next
                            // one starts where it ends.
                            .frame(minHeight: ChatSize.target)
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        .disabled(!card.pending || card.busy != nil)
                    }
                }
            }
            if card.pending, let conversation, let words = card.words {
                // An option like the others, and the last: prose, in chat.
                Button {
                    host.send(.answerInWords(id), to: conversation)
                } label: {
                    HStack(alignment: .firstTextBaseline, spacing: ChatSpace.beside) {
                        Image(systemName: "square.and.pencil")
                        Text(words)
                        Spacer(minLength: 0)
                    }
                    .ink(.accent, on: .card)
                    .frame(minHeight: ChatSize.target)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .disabled(card.busy != nil)
                .accessibilityHint("Closes these questions and puts the cursor in the message field")
            }
            if let failure = card.failure {
                CardFailureView(failure: failure)
            }
            if card.pending {
                ButtonRow {
                    Button {
                        host.send(.answer(question: id, picks: picked.mapValues { Array($0).sorted() }))
                    } label: {
                        if card.busy == "answer" { ProgressView().controlSize(.small) } else { Text(card.answer) }
                    }
                    .buttonStyle(.borderedProminent)
                    .prominentTint()
                    .touchTarget()
                    .disabled(card.busy != nil || !card.questions.allSatisfy { !(picked[$0.id]?.isEmpty ?? true) })
                    Button(card.dismiss) { host.send(.dismissQuestions(id)) }
                        .buttonStyle(.bordered)
                        .touchTarget()
                        .disabled(card.busy != nil)
                }
            }
        }
        .card(card.pending ? .caution : nil)
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
        VStack(alignment: .leading, spacing: ChatSpace.parts) {
            ChangeHeaderView(heading: card.panel.heading)
            if let rationale = card.rationale {
                Text(rationale).font(ChatType.note).ink(.quiet, on: .card)
            }
            ForEach(card.values) { line in
                VStack(alignment: .leading, spacing: ChatSpace.pair) {
                    Text(line.label).font(ChatType.label).ink(.quiet, on: .card)
                    if let before = line.before {
                        RecordValueView(before, struck: true).font(ChatType.note).lineLimit(4)
                    }
                    RecordValueView(line.value).font(ChatType.note).lineLimit(6)
                }
            }
            if let failure = card.failure {
                CardFailureView(failure: failure)
            }
            if let console = card.console.flatMap(URL.init(string:)) {
                // Without `decide:` the request is not the app's to decide.
                if let note = card.consoleNote {
                    Text(note).font(ChatType.note).ink(.quiet, on: .card)
                }
                ConsoleLink(title: "Open in the Console", destination: console)
                    .font(ChatType.noteStrong)
            } else {
                ButtonRow {
                    Button {
                        host.send(.decide(suggestion: id, accept: true))
                    } label: {
                        if card.busy == "accept" { ProgressView().controlSize(.small) } else { Text(card.apply) }
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(card.destructive ? .destructiveFill : .accentFill)
                    .touchTarget()
                    Button(card.dismiss) { host.send(.decide(suggestion: id, accept: false)) }
                        .buttonStyle(.bordered)
                        .touchTarget()
                }
                .disabled(card.busy != nil)
            }
        }
        .card(.informative)
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
        VStack(alignment: .leading, spacing: ChatSpace.parts) {
            HStack(spacing: ChatSpace.words) {
                Label(card.title, systemImage: symbol)
                    .font(ChatType.heading)
                    .ink(card.tone == .failed ? .tone(.critical) : .label, on: .card)
                Spacer(minLength: 0)
                if card.tone == .running { ProgressView().controlSize(.small) }
            }
            if card.loading {
                ProgressView().controlSize(.small)
            } else {
                // One sentence, so the tool's whole name wraps with it and
                // is never cut.
                Text("\(Text(card.asks)) \(Text(card.tool).monospaced())")
                    .font(ChatType.note)
                    .ink(.quiet, on: .card)
                    .textSelection(.enabled)
            }
            if !card.arguments.isEmpty {
                VStack(alignment: .leading, spacing: ChatSpace.items) {
                    ForEach(card.arguments) { argument in
                        VStack(alignment: .leading, spacing: ChatSpace.pair) {
                            Text(argument.label).font(ChatType.label).ink(.quiet, on: .box)
                            // The model wrote it: plain text, never Markdown.
                            // While it waits every line shows: the owner
                            // approves all of it, not what fits.
                            Text(verbatim: argument.value)
                                .font(ChatType.note)
                                .lineLimit(card.pending ? nil : 3)
                                .textSelection(.enabled)
                        }
                    }
                }
                .padding(ChatSpace.inner)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(ChatColours.boxStyle, in: .rect(cornerRadius: ChatRadius.block))
            }
            if let error = card.error {
                Text(verbatim: error).font(ChatType.note).ink(.tone(.critical), on: .card)
            }
            if let failure = card.failure {
                CardFailureView(failure: failure)
            }
            if card.pending {
                ButtonRow {
                    Button {
                        host.send(.decideApproval(id, approve: true))
                    } label: {
                        if card.busy == "approve" { ProgressView().controlSize(.small) } else { Text(card.approve) }
                    }
                    .buttonStyle(.borderedProminent)
                    .prominentTint()
                    .touchTarget()
                    Button {
                        host.send(.decideApproval(id, approve: false))
                    } label: {
                        if card.busy == "decline" { ProgressView().controlSize(.small) } else { Text(card.decline) }
                    }
                    .buttonStyle(.bordered)
                    .touchTarget()
                }
                .disabled(card.busy != nil)
            }
        }
        .card(tone)
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

    private var tone: ScreenTone? {
        switch card.tone {
        case .waiting: .caution
        case .running: .informative
        case .done: .positive
        case .failed: .critical
        case .declined: nil
        }
    }
}

extension EnvironmentValues {
    /// A call's row is drawn opened where its card ended: for a picture of
    /// what the row opens to. False in the app, where a tap opens it.
    @Entry var opensEndedCards = false
    /// Puts the chat at one of its messages, by the message's id: set by
    /// the conversation's view, which knows its window.
    @Entry var showMessage: (@MainActor (String) -> Void)?
}

/// A card's answer or decision that did not go through, in the card: the one
/// component every failure is drawn by, with the button that tries again
/// where trying again can help.
struct CardFailureView: View {
    @Environment(ChatHost.self) private var host
    let failure: FailurePresentation

    var body: some View {
        FailureView(failure: failure) { action in
            if case let .retryCard(reference) = action { host.send(.retryCard(reference)) }
        }
    }
}

/// How a card ended, under the call that asked, when its row is opened: each
/// thing that was asked with what was answered, or the change as it was
/// applied or not taken, then when. Who ended it is the row's own line. What
/// was asked is on the card's record, read when this is first shown.
struct ResolvedCardView: View {
    @Environment(ChatHost.self) private var host
    @Environment(\.showMessage) private var showMessage
    let resolution: CardResolution

    var body: some View {
        VStack(alignment: .leading, spacing: ChatSpace.items) {
            if resolution.loading {
                ProgressView().controlSize(.small)
            }
            ForEach(Array(resolution.facts.enumerated()), id: \.offset) { _, fact in
                VStack(alignment: .leading, spacing: ChatSpace.pair) {
                    Text(verbatim: fact.label).font(ChatType.caption).ink(.quiet, on: .page)
                    // The model's words and the person's choice: plain text.
                    Text(verbatim: fact.value).font(ChatType.note).textSelection(.enabled)
                }
                .accessibilityElement(children: .combine)
            }
            if let panel = resolution.panel {
                ChangePanel(panel: panel)
            }
            if let message = resolution.repliedIn, let showMessage {
                // Answered in a message: the chat is put at it.
                Button(ResolvedCardView.showMessage, systemImage: "arrow.down.message") { showMessage(message) }
                    .font(ChatType.note)
                    .buttonStyle(.borderless)
                    .touchTarget()
            }
            if let at = resolution.at {
                VStack(alignment: .leading, spacing: ChatSpace.pair) {
                    Text(ResolvedCardView.whenLabel).font(ChatType.caption).ink(.quiet, on: .page)
                    Text(at.formatted(date: .abbreviated, time: .shortened)).font(ChatType.note)
                }
                .accessibilityElement(children: .combine)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .task(id: resolution.card) {
            if resolution.loading { host.send(.loadCard(resolution.card)) }
        }
    }

    /// Over the instant the card ended, where its record says one. Who
    /// ended it and how is the row's own line, above.
    static let whenLabel = "When"
    /// On questions answered in a message: puts the chat at that message.
    static let showMessage = "Show Message"
}

/// Under a message sent while a reply was being written: it waits for the
/// assistant's next step.
struct QueuedMark: View {
    let words: String

    var body: some View {
        Label(words, systemImage: "clock")
            .font(ChatType.caption)
            .ink(.quiet, on: .page)
            .frame(maxWidth: .infinity, alignment: .trailing)
            .accessibilityLabel(words)
    }
}

/// What the composer says while it holds the answer to a question in the
/// person's own words: which question, and the way back to its options.
struct AnsweringNoteView: View {
    let note: AnsweringNote
    /// Whether the question is drawn under the title: the composer leaves
    /// it out at the accessibility text sizes, where the title, the way
    /// back and the field come first.
    var asking = true
    let back: () -> Void

    var body: some View {
        SplitRow(alignment: .firstTextBaseline) {
            HStack(alignment: .firstTextBaseline, spacing: ChatSpace.beside) {
                Image(systemName: "square.and.pencil")
                    .imageScale(.small)
                    .ink(.quiet, on: .field)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: ChatSpace.pair) {
                    Text(note.title).font(ChatType.label).fixedSize(horizontal: false, vertical: true)
                    if asking, let prompt = note.prompt {
                        Text(verbatim: prompt).font(ChatType.caption).ink(.quiet, on: .field).lineLimit(2).fixedSize(horizontal: false, vertical: true)
                    }
                }
                .accessibilityElement(children: .combine)
                .accessibilityLabel([note.title, note.prompt].compactMap { $0 }.joined(separator: ", "))
            }
        } trailing: {
            Button(note.back, action: back)
                .font(ChatType.label)
                .buttonStyle(.borderless)
                .touchTarget()
                .accessibilityHint("Puts the questions back in the chat")
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, ChatSpace.edge)
    }
}

/// "Changed [Groceries]", "Suggests adding a task": the op's mark, the verb,
/// and the record as its pill where there is one to open.
struct ChangeHeaderView: View {
    let heading: ChangeHeading
    @Scaled(.markColumn) private var column
    @Environment(\.dynamicTypeSize) private var size

    var body: some View {
        SplitRow {
            if size.isAccessibilitySize {
                // The verb over what it is done to, where one line cannot
                // hold both.
                HStack(alignment: .firstTextBaseline, spacing: ChatSpace.words) {
                    mark
                    VStack(alignment: .leading, spacing: ChatSpace.mark) {
                        Text(heading.verb)
                        subject
                    }
                }
            } else {
                HStack(alignment: .center, spacing: ChatSpace.words) {
                    mark
                    Text(heading.verb).fixedSize()
                    subject
                }
            }
        } trailing: {
            if let badge = heading.badge, let label = heading.badgeLabel {
                let applied = badge == .applied
                Label(label, systemImage: applied ? "checkmark" : "xmark")
                    .font(ChatType.label)
                    .ink(applied ? .tone(.positive) : .tone(.quiet), on: .badge)
                    .padding(.horizontal, ChatSpace.pillSide).padding(.vertical, ChatSpace.pillEdge)
                    .background(ChatColours.badgeStyle(applied ? .positive : .quiet), in: .capsule)
                    .fixedSize()
            }
        }
        .font(ChatType.heading)
    }

    private var mark: some View {
        Image(systemName: symbol)
            .growing(to: .markColumn)
            .frame(width: column)
    }

    /// The record as its pill, or the words that say what changes.
    @ViewBuilder private var subject: some View {
        if let record = heading.record {
            RecordPill(record, title: heading.recordTitle)
        } else {
            Text(heading.words).lineLimit(size.isAccessibilitySize ? 3 : 1)
        }
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
        VStack(alignment: .leading, spacing: ChatSpace.parts) {
            ChangeHeaderView(heading: panel.heading)
            if let note = panel.note {
                Text(note).font(ChatType.note).ink(.quiet, on: .card)
            }
            ForEach(panel.values) { line in
                VStack(alignment: .leading, spacing: ChatSpace.pair) {
                    Text(line.label).font(ChatType.label).ink(.quiet, on: .card)
                    RecordValueView(line.value, struck: panel.struck)
                        .font(ChatType.note)
                        .lineLimit(6)
                }
            }
            if let more = panel.more {
                Text(more).font(ChatType.caption).ink(.quiet, on: .card)
            }
        }
        .card(tone)
        .accessibilityElement(children: .combine)
    }

    private var tone: ScreenTone? {
        switch panel.tint {
        case .plain: nil
        case .applied: .positive
        case .suggested: .informative
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
        .frame(minWidth: ChatSize.sheet.width, minHeight: ChatSize.sheet.height)
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
