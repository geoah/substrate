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
        let _ = Meter.shared.count(.body, "Turn")
        switch turn.body {
        case let .user(user): UserTurnView(turn: user)
        case let .trigger(words):
            Label(words, systemImage: "bolt.fill")
                .font(.footnote.weight(.medium))
                .foregroundStyle(.quietText)
                .frame(maxWidth: .infinity)
        case .assistant:
            // A reply is laid out as its pieces, which the transcript makes
            // lazy rows of; here they stand in one.
            VStack(alignment: .leading, spacing: 0) {
                let pieces = TranscriptPiece.pieces(of: [turn])
                ForEach(pieces) { piece in
                    TranscriptPieceView(piece: piece, cards: cards, expandThinking: expandThinking)
                        .padding(.top, piece.id == pieces.first?.id ? 0 : TranscriptPiece.spacing)
                }
            }
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
                .foregroundStyle(.quietText)
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
                    Text(turn.aboutLabel).font(.caption).foregroundStyle(.quietText)
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
            // Sent while a reply was being written, and not read yet.
            if let queued = turn.queued { QueuedMark(words: queued) }
        }
    }
}

/// One row of a reply's text: its blocks from one page's start to the next,
/// under the model's thinking where it is the first. A long reply is
/// several of these, so the lazy stack measures rows of about a screen and
/// a flush lays out the last of them alone. Drawn again only when its own
/// blocks change.
struct ReplyPageView: View, Equatable {
    /// The turn's id.
    let id: String
    let reply: AssistantTurn
    let page: Int
    let expandThinking: Bool
    /// What stands under this reply where it is the chat's last: its long
    /// press offers the same, and its last row draws it (`shows`).
    var actions: ReplyActions?
    var shows = false
    /// What Try Again and Continue do.
    var act: (FailureAction) -> Void = { _ in }
    /// The Thinking section as its reader left it: open, shut, or as the
    /// setting says.
    @State private var thinkingOpen: Bool?
    /// The row's height while the model's thinking showed in its window:
    /// kept for the answer that follows.
    @State private var held: CGFloat = 0

    /// The window on the model's thinking shows: it is thinking, in the
    /// first row of its reply, and its reader has not opened the whole.
    private var windowed: Bool {
        page == 0 && reply.reasoning?.thinking == true && !(thinkingOpen ?? expandThinking)
    }

    private var rows: ArraySlice<MarkdownRow> {
        let all = reply.blocks.rows
        guard page < reply.pages.count else { return [] }
        let start = min(reply.pages[page], all.count)
        let end = page + 1 < reply.pages.count ? min(reply.pages[page + 1], all.count) : all.count
        return all[start ..< max(start, end)]
    }

    static func == (lhs: ReplyPageView, rhs: ReplyPageView) -> Bool {
        guard lhs.id == rhs.id, lhs.page == rhs.page, lhs.reply.live == rhs.reply.live, lhs.rows == rhs.rows,
            lhs.reply.missing == rhs.reply.missing, lhs.actions == rhs.actions, lhs.shows == rhs.shows
        else { return false }
        // A finished reply's menu shares its whole text; one being written
        // reads the text when the menu is used.
        if !lhs.reply.live, lhs.reply.text != rhs.reply.text { return false }
        guard lhs.page == 0 else { return true }
        return lhs.reply.reasoning == rhs.reply.reasoning && lhs.reply.typing == rhs.reply.typing
            && (lhs.reply.text == nil) == (rhs.reply.text == nil) && lhs.expandThinking == rhs.expandThinking
            && lhs.reply.tools.isEmpty == rhs.reply.tools.isEmpty
    }

    var body: some View {
        let _ = Meter.shared.count(.body, "ReplyPage")
        VStack(alignment: .leading, spacing: 10) {
            if page == 0, let reasoning = reply.reasoning {
                ReasoningView(reasoning: reasoning, expandByDefault: expandThinking, open: $thinkingOpen)
            }
            if reply.text != nil {
                MarkdownView(rows: rows, missing: Set(reply.missing))
                    .textSelection(.enabled)
                    .accessibilityAddTraits(reply.live ? .updatesFrequently : [])
                    .contextMenu { ReplyMenu(id: id, reply: reply, actions: actions, act: act) }
            } else if page == 0, reply.typing {
                TypingDots()
            }
            if shows, let actions {
                ReplyActionsRow(actions: actions, text: reply.text, act: act).equatable()
            }
        }
        // The window gives its height to the answer: while the answer is
        // the last thing written the row stays as tall as it was with the
        // window in it, so the answer starts in the window's place and the
        // page does not jump. Let go of when something stands under it (a
        // tool's row, which then takes that place) or the reply ends.
        .onGeometryChange(for: CGFloat.self) {
            $0.size.height
        } action: { height in
            if windowed, height != held { held = height }
        }
        .frame(minHeight: holds ? held : nil, alignment: .topLeading)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// The row keeps the height it had with the thinking's window in it.
    private var holds: Bool {
        reply.live && !windowed && page == 0 && reply.tools.isEmpty && reply.sources == nil
    }

}

/// What stands under a reply's text: the pages it cites, the records it
/// links, and its tool calls with what each left.
struct ReplyTailView: View, Equatable {
    let reply: AssistantTurn
    let cards: [String: CardPresentation]
    /// What stands under this reply where it is the chat's last, drawn
    /// here: under everything else that stands under its text.
    var actions: ReplyActions?
    /// What Try Again and Continue do.
    var act: (FailureAction) -> Void = { _ in }

    static func == (lhs: ReplyTailView, rhs: ReplyTailView) -> Bool {
        // The row under it shares the reply's whole text once it is whole.
        lhs.actions == rhs.actions && (lhs.actions?.whole != true || lhs.reply.text == rhs.reply.text)
            && lhs.reply.sources == rhs.reply.sources && lhs.reply.linked == rhs.reply.linked
            && lhs.reply.linkedLabel == rhs.reply.linkedLabel && lhs.reply.tools == rhs.reply.tools
            && lhs.reply.tools.compactMap(\.cardID).map { lhs.cards[$0] } == rhs.reply.tools.compactMap(\.cardID).map { rhs.cards[$0] }
    }

    var body: some View {
        let _ = Meter.shared.count(.body, "ReplyTail")
        let turn = reply
        VStack(alignment: .leading, spacing: 10) {
            if let sources = turn.sources {
                SourceListView(list: sources)
            }
            if !turn.linked.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    Text(turn.linkedLabel)
                        .font(.caption.weight(.medium))
                        .foregroundStyle(.quietText)
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
            if let actions {
                ReplyActionsRow(actions: actions, text: reply.text, act: act).equatable()
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// One lazy row of the transcript: a turn, or one of the rows a reply is
/// laid out in (each page of its text, then what stands under it). The
/// stack spaces its rows 22 points apart; a reply's rows stand as close as
/// its blocks and its parts did inside one row, so `closer` is taken back.
struct TranscriptPiece: Identifiable {
    enum Part {
        case turn
        case page(Int)
        case tail
        /// The card of a turn that failed, under its last row.
        case failure(FailurePresentation)
        /// The line above the first turn its reader had not seen.
        case newLine(NewLine)
    }

    /// The transcript's spacing, the Markdown blocks' and a turn's parts'.
    static let spacing: CGFloat = 22
    static let blockSpacing: CGFloat = 12
    static let partSpacing: CGFloat = 10

    let id: String
    let turn: TurnPresentation
    let part: Part
    let closer: CGFloat
    /// What stands under this turn's reply, where it is the chat's last:
    /// on each of the reply's rows, for its long press, and drawn by the
    /// last of them (`drawsActions`). Part of that row, not a row of its
    /// own, so it is there from the reply's first words and takes nothing
    /// from the stack when the next reply has it.
    var actions: ReplyActions?
    var drawsActions = false

    /// - Parameters:
    ///   - failures: by a turn's id, the card drawn under it. It is a row
    ///     of its own, so every element of the stack is one view: a lazy
    ///     stack walks all of its elements at each update to count the
    ///     views of one that may be one or two.
    ///   - newLine: the line drawn above the first turn that was not read,
    ///     a row of its own for the same reason, and the row such a chat
    ///     opens at.
    ///   - actions: what stands under the chat's last reply.
    static func pieces(
        of turns: [TurnPresentation], failures: [String: FailurePresentation] = [:], newLine: NewLine? = nil, actions: ReplyActions? = nil
    ) -> [TranscriptPiece] {
        var out: [TranscriptPiece] = []
        out.reserveCapacity(turns.count)
        for turn in turns {
            if let newLine, newLine.turn == turn.id {
                out.append(TranscriptPiece(id: "\(turn.id)/new", turn: turn, part: .newLine(newLine), closer: 0))
            }
            defer {
                if let failure = failures[turn.id] {
                    out.append(TranscriptPiece(id: "\(turn.id)/failure", turn: turn, part: .failure(failure), closer: 0))
                }
            }
            guard case let .assistant(reply) = turn.body else {
                // Under a turn that is no reply only Try Again stands, for
                // a run that ended before the assistant said anything.
                let under = actions.flatMap { $0.turn == turn.id ? $0 : nil }
                out.append(TranscriptPiece(id: turn.id, turn: turn, part: .turn, closer: 0, actions: under, drawsActions: under != nil))
                continue
            }
            let tailed = reply.sources != nil || !reply.linked.isEmpty || !reply.tools.isEmpty
            let headed = reply.reasoning != nil || reply.text != nil || reply.typing || !tailed
            let first = out.count
            defer {
                if let actions, actions.turn == turn.id, out.count > first {
                    for index in first ..< out.count { out[index].actions = actions }
                    out[out.count - 1].drawsActions = true
                }
            }
            if headed {
                // The first keeps the turn's id: the row it was before it
                // grew another.
                out.append(TranscriptPiece(id: turn.id, turn: turn, part: .page(0), closer: 0))
                if reply.text != nil {
                    for page in reply.pages.indices.dropFirst() {
                        out.append(TranscriptPiece(id: "\(turn.id)/\(page)", turn: turn, part: .page(page), closer: spacing - blockSpacing))
                    }
                }
            }
            if tailed {
                out.append(TranscriptPiece(id: "\(turn.id)/tail", turn: turn, part: .tail, closer: headed ? spacing - partSpacing : 0))
            }
        }
        return out
    }
}

extension TranscriptPiece {
    /// The turn of the last of `rows` (the ids of the rows on screen) in
    /// the transcript's order: the last turn a reader can see.
    static func lastTurn(among rows: [String], of turns: [TurnPresentation]) -> String? {
        let onScreen = Set(rows)
        return pieces(of: turns).last { onScreen.contains($0.id) || onScreen.contains("\($0.turn.id)/failure") }?.turn.id
    }
}

/// A piece as its view.
struct TranscriptPieceView: View {
    let piece: TranscriptPiece
    let cards: [String: CardPresentation]
    let expandThinking: Bool
    /// What a failure's button does.
    var act: (FailureAction) -> Void = { _ in }

    var body: some View {
        switch (piece.part, piece.turn.body) {
        case let (.failure(failure), _):
            FailureView(failure: failure, act: act)
        case let (.newLine(line), _):
            NewLineView(line: line).padding(.top, 8)
        case let (.page(page), .assistant(reply)):
            ReplyPageView(
                id: piece.turn.id, reply: reply, page: page, expandThinking: expandThinking, actions: piece.actions, shows: piece.drawsActions, act: act
            )
            .equatable()
            .padding(.top, -piece.closer)
        case let (.tail, .assistant(reply)):
            ReplyTailView(reply: reply, cards: cards, actions: piece.drawsActions ? piece.actions : nil, act: act)
                .equatable()
                .padding(.top, -piece.closer)
        default:
            if piece.drawsActions, let actions = piece.actions {
                // One view all the same: the turn, and Try Again under it
                // on the side the turn stands on.
                VStack(spacing: TranscriptPiece.partSpacing) {
                    TurnView(turn: piece.turn, cards: cards, expandThinking: expandThinking)
                        .equatable()
                    ReplyActionsRow(actions: actions, text: nil, trailing: piece.turn.isUsers, act: act)
                        .equatable()
                }
            } else {
                TurnView(turn: piece.turn, cards: cards, expandThinking: expandThinking)
                    .equatable()
            }
        }
    }
}

extension TurnPresentation {
    /// The person's own message, drawn on the trailing side.
    var isUsers: Bool {
        if case .user = body { true } else { false }
    }
}

extension EnvironmentValues {
    /// The conversation the view is in.
    @Entry var conversationKey: ChildKey?
}

/// "Thinking…" while the model reasons, over a window of fixed height that
/// shows the newest of its thinking; then "Thought for 6 s", folded. Opened,
/// it is the whole text, while it thinks and after.
struct ReasoningView: View {
    let reasoning: ReasoningPresentation
    let expandByDefault: Bool
    /// Kept by the row, which outlives a change of what stands in it.
    @Binding var open: Bool?
    /// About three lines of the thinking, at the text's size.
    @ScaledMetric(relativeTo: .footnote) private var window = 58.0

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
                .foregroundStyle(.quietText)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityValue(expanded ? "Shown" : "Hidden")
            if expanded {
                Text(reasoning.text)
                    .font(.subheadline)
                    .foregroundStyle(.quietText)
                    .textSelection(.enabled)
                    .padding(.leading, 12)
                    .overlay(alignment: .leading) {
                        Rectangle().fill(.quaternary).frame(width: 2)
                    }
            } else if reasoning.thinking {
                // The newest lines, the older ones fading out at the top:
                // where the thinking has got to, in a height that holds.
                Text(reasoning.tail)
                    .font(.footnote)
                    .foregroundStyle(.quietText)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .frame(height: window, alignment: .bottom)
                    .clipped()
                    .mask {
                        LinearGradient(stops: [.init(color: .clear, location: 0), .init(color: .black, location: 0.45)], startPoint: .top, endPoint: .bottom)
                    }
                    .padding(.leading, 12)
                    .overlay(alignment: .leading) {
                        Rectangle().fill(.quaternary).frame(width: 2)
                    }
                    // The label says it is thinking; its words are read
                    // when the section is opened.
                    .accessibilityHidden(true)
            }
        }
    }
}

/// Opens a thread in the window the action came from: a sub-agent's own
/// conversation, from the call that ran it.
///
/// A value that compares, never a closure: the transcript sets it at each of
/// its draws, and one that could not be compared read as changed each time,
/// to every view under it.
struct OpenThreadAction: Equatable {
    let host: ChatHost
    let scene: String

    static func == (lhs: OpenThreadAction, rhs: OpenThreadAction) -> Bool { lhs.host === rhs.host && lhs.scene == rhs.scene }

    @MainActor func callAsFunction(_ thread: String) { host.send(.show(thread: thread, scene: scene)) }
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
    @Environment(\.opensEndedCards) private var opensEnded
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
                            .foregroundStyle(.quietText)
                            .multilineTextAlignment(.leading)
                        if let label = tool.statusLabel {
                            Text(label)
                                .font(.caption.weight(.medium))
                                .foregroundStyle(tool.status == .held ? ScreenTone.caution.textColor : Color.quietText)
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
            if open || (opensEnded && tool.resolution != nil) {
                VStack(alignment: .leading, spacing: 8) {
                    // A card that ended: what was asked and what was answered.
                    if let resolution = tool.resolution { ResolvedCardView(resolution: resolution) }
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
                                    Text(line.label).font(.caption2.weight(.semibold)).foregroundStyle(.quietText)
                                    Text(line.value)
                                        .font(.caption.monospaced())
                                        .foregroundStyle(.quietText)
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
        case .declined: Image(systemName: "xmark.circle.fill").foregroundStyle(.secondary)
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
                .foregroundStyle(ScreenTone.critical.textColor)
        case let .found(words, records):
            VStack(alignment: .leading, spacing: 6) {
                Text(words).font(.footnote).foregroundStyle(.quietText)
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
            Text(words).font(.footnote).foregroundStyle(.quietText)
        case let .reply(heading, text):
            VStack(alignment: .leading, spacing: 4) {
                Text(heading).font(.caption.weight(.semibold)).foregroundStyle(.quietText)
                MarkdownView(text: text).font(.footnote)
            }
            .card()
        case let .output(raw):
            Text(raw).font(.caption.monospaced()).foregroundStyle(.quietText).lineLimit(12).textSelection(.enabled)
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
                Text(lead).foregroundStyle(.quietText)
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
                    Text(more).foregroundStyle(.quietText)
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
                        .foregroundStyle(.quietText)
                        .fixedSize()
                }
                .buttonStyle(.plain)
                .disabled(turn.content.isEmpty)
                Rectangle().fill(.quaternary).frame(height: 1)
            }
            if open, !turn.content.isEmpty {
                MarkdownView(text: turn.content).font(.footnote).foregroundStyle(.quietText).card()
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
                .foregroundStyle(.quietText)
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
