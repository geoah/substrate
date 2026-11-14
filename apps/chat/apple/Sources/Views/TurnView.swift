import ChatKit
import SubstrateKit
import SwiftUI

/// One turn of the transcript, by what it shows. Equatable, so a flush of
/// the live turn redraws that turn and not every one above it.
struct TurnView: View, Equatable {
    let turn: TurnPresentation
    /// The cards the turns land, by id: a card draws as its record now
    /// stands, which a turn does not hold.
    let cards: [String: CardPresentation]
    let expandThinking: Bool

    static func == (lhs: TurnView, rhs: TurnView) -> Bool {
        lhs.turn == rhs.turn && lhs.expandThinking == rhs.expandThinking && lhs.cardsShown == rhs.cardsShown
    }

    /// The cards this turn shows, as they stand.
    private var cardsShown: [CardPresentation?] {
        switch turn.body {
        case let .assistant(assistant): assistant.tools.compactMap(\.cardID).map { cards[$0] }
        case let .decision(_, suggestion): [cards[suggestion]]
        default: []
        }
    }

    var body: some View {
        switch turn.body {
        case let .user(user): UserTurnView(turn: user)
        case let .trigger(words):
            Label(words, systemImage: "bolt.fill")
                .font(.footnote.weight(.medium))
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity)
        case let .assistant(assistant): AssistantTurnView(turn: assistant, cards: cards, expandThinking: expandThinking)
        case let .summary(summary): SummaryLine(turn: summary)
        case let .decision(line, suggestion):
            VStack(alignment: .leading, spacing: 10) {
                OutcomeRow(line: line)
                if let card = cards[suggestion]?.suggestion { SuggestionCardView(id: suggestion, card: card) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        case let .answer(line), let .jobEnded(line), let .approvalEnded(line): OutcomeRow(line: line)
        case let .system(text):
            Text(text)
                .lineLimit(3)
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity)
        }
    }
}

extension ToolRowPresentation {
    var cardID: String? {
        switch attachment {
        case let .suggestion(id)?, let .questions(id)?, let .approval(id)?: id
        default: nil
        }
    }
}

private struct UserTurnView: View {
    let turn: UserTurn

    var body: some View {
        VStack(alignment: .trailing, spacing: 6) {
            if !turn.about.isEmpty {
                HStack(spacing: 6) {
                    Spacer(minLength: 48)
                    Text(turn.aboutLabel).font(.caption).foregroundStyle(.secondary)
                    ForEach(turn.about) { RecordPill($0) }
                }
                .font(.footnote)
                .accessibilityElement(children: .combine)
            }
            HStack {
                Spacer(minLength: 48)
                Text(turn.text)
                    .textSelection(.enabled)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(Color.accentColor.opacity(0.16), in: .rect(cornerRadius: 20))
                    .opacity(turn.pending ? 0.7 : 1)
                    .contextMenu {
                        Button("Copy", systemImage: "doc.on.doc") { copyToPasteboard(turn.text) }
                    }
            }
            .accessibilityLabel("You: \(turn.text)")
        }
    }
}

private struct AssistantTurnView: View {
    let turn: AssistantTurn
    let cards: [String: CardPresentation]
    let expandThinking: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let reasoning = turn.reasoning {
                ReasoningView(reasoning: reasoning, expandByDefault: expandThinking)
            }
            if let text = turn.text {
                MarkdownView(text: text, live: turn.live, missing: Set(turn.missing))
                    .textSelection(.enabled)
                    .accessibilityAddTraits(turn.live ? .updatesFrequently : [])
                    .contextMenu {
                        Button("Copy", systemImage: "doc.on.doc") { copyToPasteboard(text) }
                        ShareLink(item: text)
                    }
            } else if turn.typing {
                TypingDots()
            }
            if let sources = turn.sources {
                SourceListView(list: sources)
            }
            if !turn.linked.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    Text(turn.linkedLabel)
                        .font(.caption.weight(.medium))
                        .foregroundStyle(.secondary)
                    WrapLayout(spacing: 6) {
                        ForEach(turn.linked) { RecordPill($0) }
                    }
                    .font(.footnote)
                }
                .accessibilityElement(children: .contain)
                .accessibilityLabel(turn.linkedLabel)
            }
            ForEach(turn.tools) { tool in
                ToolCallRow(tool: tool)
                switch tool.attachment {
                case let .suggestion(id)?:
                    if let card = cards[id]?.suggestion { SuggestionCardView(id: id, card: card) }
                case let .questions(id)?:
                    if let card = cards[id]?.question { QuestionCardView(id: id, card: card) }
                case let .approval(id)?:
                    if let card = cards[id]?.approval { ApprovalCardView(id: id, card: card) }
                case let .change(panel)?:
                    ChangePanel(panel: panel)
                case nil:
                    EmptyView()
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// "Thinking…" while the model reasons, then "Thought for 6 s", folded.
struct ReasoningView: View {
    let reasoning: ReasoningPresentation
    let expandByDefault: Bool
    @State private var open: Bool?

    var body: some View {
        let expanded = open ?? expandByDefault
        VStack(alignment: .leading, spacing: 8) {
            Button {
                withAnimation(.snappy) { open = !expanded }
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: "brain")
                        .symbolEffect(.pulse, isActive: reasoning.thinking)
                    if reasoning.thinking {
                        Text(reasoning.label).shimmer()
                    } else {
                        Text(reasoning.label)
                    }
                    Image(systemName: "chevron.right")
                        .font(.caption2.weight(.semibold))
                        .rotationEffect(.degrees(expanded ? 90 : 0))
                }
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            if expanded {
                Text(reasoning.text)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .padding(.leading, 12)
                    .overlay(alignment: .leading) {
                        Rectangle().fill(.quaternary).frame(width: 2)
                    }
            }
        }
    }
}

/// Opens a thread in the window the action came from: a sub-agent's own
/// conversation, from the call that ran it.
struct OpenThreadAction {
    let handler: @MainActor (String) -> Void

    @MainActor func callAsFunction(_ thread: String) { handler(thread) }
}

extension EnvironmentValues {
    @Entry var openThread: OpenThreadAction?
}

/// One tool call in the console's words, opening to what it found, changed
/// or failed on; the pages a search found or a call read, and the records
/// it found or changed where there are several, show under it, open or not.
struct ToolCallRow: View {
    @Environment(\.openThread) private var openThread
    @Environment(\.recordDirectory) private var directory
    let tool: ToolRowPresentation
    @State private var open = false
    @State private var raw = false

    /// The line, naming the record it worked on once its heading is read
    /// where nothing in the chat carried it.
    private var summary: String {
        guard let unnamed = tool.unnamed, let title = directory?.title(for: unnamed.record) else { return tool.summary }
        return unnamed.line(titled: title)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Button {
                withAnimation(.snappy) { open.toggle() }
            } label: {
                HStack(spacing: 8) {
                    status
                        .frame(width: 18)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(summary)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.leading)
                        if let label = tool.statusLabel {
                            Text(label)
                                .font(.caption.weight(.medium))
                                .foregroundStyle(tool.status == .held ? Color.orange : Color.secondary)
                        }
                    }
                    Spacer(minLength: 0)
                    Image(systemName: "chevron.right")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(.tertiary)
                        .rotationEffect(.degrees(open ? 90 : 0))
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityHint(open ? "Hides the details" : "Shows the details")
            if open {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(Array(tool.details.enumerated()), id: \.offset) { _, detail in DetailLine(detail: detail) }
                    if let link = tool.thread, let openThread {
                        Button(link.label, systemImage: "bubble.left.and.bubble.right") { openThread(link.thread) }
                            .font(.footnote)
                            .buttonStyle(.borderless)
                    }
                    DisclosureGroup("Details", isExpanded: $raw) {
                        VStack(alignment: .leading, spacing: 6) {
                            ForEach(tool.raw, id: \.self) { line in
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(line.label).font(.caption2.weight(.semibold)).foregroundStyle(.tertiary)
                                    Text(line.value)
                                        .font(.caption.monospaced())
                                        .foregroundStyle(.secondary)
                                        .textSelection(.enabled)
                                        .lineLimit(40)
                                }
                            }
                        }
                    }
                    .font(.caption)
                }
                .padding(.leading, 26)
            }
            if let sources = tool.sources {
                SourceListView(list: sources)
                    .padding(.leading, 26)
            }
            ForEach(Array(tool.chips.enumerated()), id: \.offset) { _, chips in
                RecordChipsView(chips: chips)
                    .padding(.leading, 26)
            }
        }
        .task(id: tool.unnamed?.record) {
            if let record = tool.unnamed?.record { await directory?.resolve(record) }
        }
    }

    @ViewBuilder private var status: some View {
        switch tool.status {
        case .running, .working: ProgressView().controlSize(.mini)
        case .done: Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
        case .failed: Image(systemName: "xmark.circle.fill").foregroundStyle(.red)
        case .held: Image(systemName: "hourglass.circle.fill").foregroundStyle(.orange)
        }
    }
}

private struct DetailLine: View {
    let detail: ToolDetailPresentation

    var body: some View {
        switch detail {
        case let .failed(reason):
            Label(reason, systemImage: "xmark.octagon")
                .font(.footnote)
                .foregroundStyle(.red)
        case let .found(words, records):
            VStack(alignment: .leading, spacing: 6) {
                Text(words).font(.footnote).foregroundStyle(.secondary)
                WrapLayout(spacing: 6) {
                    ForEach(records) { RecordPill($0) }
                }
                .font(.footnote)
            }
        case let .changed(runs):
            VStack(alignment: .leading, spacing: 6) {
                ForEach(Array(runs.enumerated()), id: \.offset) { _, chips in RecordChipsView(chips: chips) }
            }
        case let .wrote(words):
            Text(words).font(.footnote).foregroundStyle(.secondary)
        case let .reply(heading, text):
            VStack(alignment: .leading, spacing: 4) {
                Text(heading).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                MarkdownView(text: text).font(.footnote)
            }
            .card()
        case let .output(raw):
            Text(raw).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(12).textSelection(.enabled)
        }
    }
}

/// Records a call found or changed, as pills: the first few, then "and N
/// more", which shows the rest the call's answer holds.
private struct RecordChipsView: View {
    let chips: RecordChips
    @State private var all = false

    var body: some View {
        let shown = all ? chips.records : Array(chips.records.prefix(RecordChips.first))
        WrapLayout(spacing: 6) {
            if let lead = chips.lead {
                Text(lead).foregroundStyle(.secondary)
            }
            ForEach(Array(shown.enumerated()), id: \.offset) { _, chip in
                RecordPill(chip.record, title: chip.title)
            }
            if let more = chips.more(shown: shown.count) {
                if chips.records.count > shown.count {
                    Button(more) { withAnimation(.snappy) { all = true } }
                        .buttonStyle(.borderless)
                        .accessibilityHint("Shows the rest")
                } else {
                    Text(more).foregroundStyle(.tertiary)
                }
            }
        }
        .font(.footnote)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(chips.label)
    }
}

private struct SummaryLine: View {
    let turn: SummaryTurn
    @State private var open = false

    var body: some View {
        VStack(spacing: 8) {
            HStack(spacing: 10) {
                Rectangle().fill(.quaternary).frame(height: 1)
                Button {
                    withAnimation(.snappy) { open.toggle() }
                } label: {
                    Label(turn.label, systemImage: "rectangle.compress.vertical")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize()
                }
                .buttonStyle(.plain)
                .disabled(turn.content.isEmpty)
                Rectangle().fill(.quaternary).frame(height: 1)
            }
            if open, !turn.content.isEmpty {
                MarkdownView(text: turn.content).font(.footnote).foregroundStyle(.secondary).card()
            }
        }
    }
}

/// What the owner did with a card, in the tool rows' shape: a mark, then
/// the words.
private struct OutcomeRow: View {
    let line: OutcomeLine

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: line.done ? "checkmark.circle.fill" : "xmark.circle.fill")
                .foregroundStyle(line.done ? Color.green : Color.secondary)
                .frame(width: 18)
            Text(line.text)
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

/// Three dots while a reply has not begun.
struct TypingDots: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var phase = 0

    var body: some View {
        HStack(spacing: 5) {
            ForEach(0 ..< 3) { i in
                Circle()
                    .fill(.secondary)
                    .frame(width: 7, height: 7)
                    .opacity(phase == i ? 1 : 0.35)
            }
        }
        .padding(.vertical, 6)
        .task {
            guard !reduceMotion else { return }
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(320))
                withAnimation(.easeInOut(duration: 0.25)) { phase = (phase + 1) % 3 }
            }
        }
        .accessibilityLabel("Answering")
    }
}
