import SwiftUI

/// One turn of the transcript, by role. Equatable, so a flush of the live
/// turn redraws that turn and not every one above it.
struct TurnView: View, Equatable {
    let turn: Turn
    let agent: Agent?
    /// The assistant turn the stream is writing.
    var live = false

    var body: some View {
        switch turn.role {
        case .user: UserTurn(turn: turn)
        case .assistant: AssistantTurn(turn: turn, agent: agent, live: live)
        case .summary: SummaryLine(turn: turn)
        case .system: SystemLine(turn: turn)
        }
    }
}

private struct UserTurn: View {
    let turn: Turn

    var body: some View {
        if let delivery = deliveryNotice(turn) {
            Label("When \(delivery.title ?? kindDisplayName(delivery.kind)) changed", systemImage: "bolt.fill")
                .font(.footnote.weight(.medium))
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity)
        } else {
            HStack {
                Spacer(minLength: 48)
                Text(turn.content)
                    .textSelection(.enabled)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(Color.accentColor.opacity(0.16), in: .rect(cornerRadius: 20))
                    .opacity(turn.pending ? 0.7 : 1)
                    .contextMenu {
                        Button("Copy", systemImage: "doc.on.doc") { copyToPasteboard(turn.content) }
                    }
            }
            .accessibilityLabel("You: \(turn.content)")
        }
    }
}

private struct AssistantTurn: View {
    @Environment(ChatStore.self) private var store
    let turn: Turn
    let agent: Agent?
    let live: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let reasoning = turn.reasoning, !reasoning.isEmpty {
                ReasoningView(text: reasoning, millis: turn.reasoningMillis, thinking: turn.thinking && live)
            }
            if !turn.content.isEmpty {
                MarkdownView(text: turn.content, live: live)
                    .textSelection(.enabled)
                    .accessibilityAddTraits(live ? .updatesFrequently : [])
                    .contextMenu {
                        Button("Copy", systemImage: "doc.on.doc") { copyToPasteboard(turn.content) }
                        ShareLink(item: turn.content)
                    }
            } else if live, turn.tools.isEmpty, turn.reasoning == nil {
                TypingDots()
            }
            ForEach(Array(turn.tools.enumerated()), id: \.offset) { _, call in
                ToolCallRow(call: call, agent: agent)
                if let request = requestID(of: call) {
                    ProposalCard(id: request)
                } else if let interaction = interactionID(of: call) {
                    InteractionCard(id: interaction)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// "Thinking…" while the model reasons, then "Thought for 6 s", folded.
struct ReasoningView: View {
    @Environment(ChatStore.self) private var store
    let text: String
    let millis: Int?
    let thinking: Bool
    @State private var open: Bool?

    var body: some View {
        let expanded = open ?? store.prefs.expandThinking
        VStack(alignment: .leading, spacing: 8) {
            Button {
                withAnimation(.snappy) { open = !expanded }
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: "brain")
                        .symbolEffect(.pulse, isActive: thinking)
                    if thinking {
                        Text("Thinking…").shimmer()
                    } else {
                        Text(thoughtFor)
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
                Text(text)
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

    private var thoughtFor: String {
        guard let millis, millis > 0 else { return "Thought" }
        let seconds = max(1, Int((Double(millis) / 1000).rounded()))
        return seconds < 60 ? "Thought for \(seconds) s" : "Thought for \(seconds / 60) min \(seconds % 60) s"
    }
}

/// One tool call in the console's words, opening to what it found, changed
/// or failed on.
struct ToolCallRow: View {
    let call: ToolCall
    let agent: Agent?
    @State private var open = false
    @State private var raw = false

    var body: some View {
        let resolved = resolveTool(agent: agent, name: call.name, callable: call.callable)
        let details = toolDetails(call, resolved)
        VStack(alignment: .leading, spacing: 8) {
            Button {
                withAnimation(.snappy) { open.toggle() }
            } label: {
                HStack(spacing: 8) {
                    status
                        .frame(width: 18)
                    Text(toolSummary(call, resolved))
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.leading)
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
                    ForEach(Array(details.enumerated()), id: \.offset) { _, detail in DetailLine(detail: detail) }
                    DisclosureGroup("Details", isExpanded: $raw) {
                        VStack(alignment: .leading, spacing: 6) {
                            LabeledRaw(label: "Tool", value: call.callable ?? call.name)
                            if !call.arguments.isEmpty { LabeledRaw(label: "Arguments", value: pretty(call.arguments)) }
                            if let output = call.output { LabeledRaw(label: "Output", value: pretty(output)) }
                        }
                    }
                    .font(.caption)
                }
                .padding(.leading, 26)
            }
        }
    }

    @ViewBuilder private var status: some View {
        switch call.ok {
        case nil: ProgressView().controlSize(.mini)
        case true?: Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
        case false?: Image(systemName: "xmark.circle.fill").foregroundStyle(.red)
        }
    }

    private func pretty(_ text: String) -> String {
        guard let value = JSONValue.parse(text), case .object = value,
              let data = try? JSONSerialization.data(withJSONObject: value.any, options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes])
        else { return text }
        return String(decoding: data, as: UTF8.self)
    }
}

private struct DetailLine: View {
    let detail: ToolDetail

    var body: some View {
        switch detail {
        case let .failed(reason):
            Label(reason ?? "It failed.", systemImage: "xmark.octagon")
                .font(.footnote)
                .foregroundStyle(.red)
        case let .found(records, total):
            VStack(alignment: .leading, spacing: 3) {
                Text(total == 0 ? "Found nothing." : "Found \(plural(total, "record")).")
                    .font(.footnote).foregroundStyle(.secondary)
                ForEach(records, id: \.self) { r in
                    Text("\(kindDisplayName(r.kind)) · \(r.id)").font(.caption.monospaced()).foregroundStyle(.secondary)
                }
            }
        case let .changed(records):
            VStack(alignment: .leading, spacing: 3) {
                ForEach(records, id: \.self) { r in
                    Label("\(verb(r.op)) \(kindDisplayName(r.kind).lowercasedFirst) \(r.id)", systemImage: "square.and.pencil")
                        .font(.footnote).foregroundStyle(.secondary)
                }
            }
        case let .wrote(records, kinds, more):
            Text(wroteSentence(records: records, kinds: kinds, moreKinds: more))
                .font(.footnote).foregroundStyle(.secondary)
        case let .reply(agent, text):
            VStack(alignment: .leading, spacing: 4) {
                Text("\(agentName(agent)) said").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                MarkdownView(text: text).font(.footnote)
            }
            .card()
        case let .output(raw):
            Text(raw).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(12).textSelection(.enabled)
        }
    }

    private func verb(_ op: String?) -> String {
        switch op {
        case "put": "Saved"
        case "delete": "Deleted"
        default: "Changed"
        }
    }
}

private struct LabeledRaw: View {
    let label: String
    let value: String

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label).font(.caption2.weight(.semibold)).foregroundStyle(.tertiary)
            Text(value)
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
                .lineLimit(40)
        }
    }
}

private struct SummaryLine: View {
    let turn: Turn
    @State private var open = false

    var body: some View {
        VStack(spacing: 8) {
            HStack(spacing: 10) {
                Rectangle().fill(.quaternary).frame(height: 1)
                Button {
                    withAnimation(.snappy) { open.toggle() }
                } label: {
                    Label("Earlier messages were summarized", systemImage: "rectangle.compress.vertical")
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

private struct SystemLine: View {
    let turn: Turn

    var body: some View {
        Group {
            if let decision = decisionNotice(turn) {
                Label(decision.accepted ? (decision.adjusted ? "You applied the suggestion, with your changes" : "You applied the suggestion")
                    : "You dismissed the suggestion",
                    systemImage: decision.accepted ? "checkmark.seal" : "xmark.seal")
            } else if let answer = interactionNotice(turn) {
                Label(answer.answered ? "You answered" : "You dismissed the questions",
                      systemImage: answer.answered ? "checkmark.bubble" : "xmark.bubble")
            } else {
                Text(turn.content).lineLimit(3)
            }
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity)
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
