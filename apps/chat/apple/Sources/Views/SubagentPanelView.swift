import ChatKit
import SubstrateKit
import SwiftUI

/// A call to another assistant as a card of its own: who was asked and how
/// it stands, then what it was asked, what it answered or why it stopped,
/// and what it changed, each a line or two. One tap opens the whole of each,
/// the kinds it wrote, its conversation, and under a quiet disclosure the
/// raw call.
struct SubagentPanelView: View {
    @Environment(\.openThread) private var openThread
    let panel: SubagentPanel
    @State private var open = false
    @State private var raw = false
    @Scaled(.markColumn) private var column

    var body: some View {
        VStack(alignment: .leading, spacing: ChatSpace.items) {
            Button {
                ChatMotion.animate(.fold) { open.toggle() }
            } label: {
                header.touchTarget()
            }
            .buttonStyle(.plain)
            .accessibilityHint(open ? "Hides the details" : "Shows the details")
            Group {
                if open { whole } else { lines }
            }
            .padding(.leading, column + ChatSpace.beside)
        }
        .card()
    }

    private var header: some View {
        HStack(spacing: ChatSpace.beside) {
            mark
                .growing(to: .markColumn)
                .frame(width: column)
            VStack(alignment: .leading, spacing: ChatSpace.pair) {
                Text(panel.name)
                    .font(ChatType.detail)
                    .fontWeight(ChatType.strong)
                    .ink(.label, on: .card)
                SubagentStatus(panel: panel)
            }
            Spacer(minLength: 0)
            Image(systemName: "chevron.right")
                .font(ChatType.smallLabel)
                .ink(.quiet, on: .card)
                .rotationEffect(.degrees(open ? 90 : 0))
        }
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private var mark: some View {
        switch panel.state {
        case .running, .working: ProgressView().controlSize(.mini)
        case .finished: Image(systemName: "checkmark.circle.fill").ink(.mark(.positive), on: .card)
        case .stopped: Image(systemName: "exclamationmark.circle.fill").ink(.mark(.caution), on: .card)
        case .failed: Image(systemName: "xmark.circle.fill").ink(.mark(.critical), on: .card)
        }
    }

    /// Closed: a line or two of each, its label leading it.
    private var lines: some View {
        VStack(alignment: .leading, spacing: ChatSpace.lines) {
            PanelLine(label: panel.askedLabel, value: panel.asked, limit: 2)
            if let label = panel.outcomeLabel, let outcome = panel.outcome {
                PanelLine(label: label, value: outcome, limit: 2)
            }
            if let changed = panel.changed { PanelLine(label: "Changed", value: changed, limit: 1) }
        }
    }

    /// Open: the whole of each, under its label.
    private var whole: some View {
        VStack(alignment: .leading, spacing: ChatSpace.items) {
            PanelSection(label: panel.askedLabel) {
                Text(panel.request.isEmpty ? panel.asked : panel.request)
                    .font(ChatType.note)
                    .ink(.label, on: .card)
                    .textSelection(.enabled)
            }
            if let reply = panel.reply, panel.state == .finished {
                PanelSection(label: panel.outcomeLabel ?? "Answered") {
                    MarkdownView(text: reply).font(ChatType.note)
                }
            }
            if let stopped = panel.stopped {
                PanelSection(label: panel.outcomeLabel ?? "Stopped") {
                    Text(stopped)
                        .font(ChatType.note)
                        .ink(panel.state == .failed ? .tone(.critical) : .label, on: .card)
                        .textSelection(.enabled)
                    if let failed = panel.failedCall {
                        Text(failed.label).font(ChatType.label).ink(.quiet, on: .card)
                        Text(failed.error)
                            .font(ChatType.verbatim)
                            .ink(.tone(.critical), on: .card)
                            .textSelection(.enabled)
                    }
                    if let reply = panel.reply {
                        MarkdownView(text: reply).font(ChatType.note)
                    }
                }
            }
            if let changed = panel.changed {
                PanelSection(label: "Changed") {
                    Text(changed).font(ChatType.note).ink(.label, on: .card)
                    if !panel.kinds.isEmpty {
                        WrapLayout(spacing: ChatSpace.words) {
                            ForEach(panel.kinds, id: \.self) { KindPill(kind: $0) }
                            if let more = panel.moreKinds { Text(more).ink(.quiet, on: .card) }
                        }
                        .font(ChatType.note)
                        .accessibilityElement(children: .contain)
                        .accessibilityLabel("Kinds it wrote")
                    }
                }
            }
            if let link = panel.thread, let openThread {
                Button(link.label, systemImage: "bubble.left.and.bubble.right") { openThread(link.thread) }
                    .font(ChatType.note)
                    .buttonStyle(.borderless)
                    .touchTarget()
            }
            DisclosureGroup(panel.rawLabel, isExpanded: $raw) {
                VStack(alignment: .leading, spacing: ChatSpace.lines) {
                    ForEach(panel.raw, id: \.self) { line in
                        VStack(alignment: .leading, spacing: ChatSpace.pair) {
                            Text(line.label).font(ChatType.smallLabel).ink(.quiet, on: .card)
                            Text(line.value)
                                .font(ChatType.verbatim)
                                .ink(.quiet, on: .card)
                                .textSelection(.enabled)
                                .lineLimit(40)
                        }
                    }
                }
            }
            .font(ChatType.caption)
            .ink(.quiet, on: .card)
        }
    }
}

/// The state beside the name, and while the helper runs its clock, which
/// ticks once a second here alone: the transcript around it is not drawn
/// again.
private struct SubagentStatus: View {
    let panel: SubagentPanel

    var body: some View {
        if let since = panel.clock.since {
            TimelineView(.periodic(from: since, by: 1)) { context in line(at: context.date) }
        } else {
            line(at: Date())
        }
    }

    private func line(at now: Date) -> some View {
        Text(panel.status(at: now))
            .font(ChatType.label)
            .monospacedDigit()
            .ink(panel.state == .stopped ? .tone(.caution) : panel.state == .failed ? .tone(.critical) : .quiet, on: .card)
            .accessibilityLabel(panel.spoken(at: now))
    }
}

/// One fact of a closed panel: its label, then a line or two of its value.
private struct PanelLine: View {
    let label: String
    let value: String
    let limit: Int

    var body: some View {
        Text("\(Text(label).fontWeight(ChatType.strong).ink(.quiet, on: .card))  \(Text(value).ink(.label, on: .card))")
            .font(ChatType.note)
            .lineLimit(limit)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// One fact of an open panel: its label over the whole of it.
private struct PanelSection<Content: View>: View {
    let label: String
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: ChatSpace.lines) {
            Text(label).font(ChatType.label).ink(.quiet, on: .card)
            content
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .contain)
    }
}

/// A kind as a pill: its tile and its name, the record pill's shape
/// without a record to open.
private struct KindPill: View {
    let kind: String

    var body: some View {
        HStack(spacing: RecordPillStyle.spacing) {
            KindGlyph(kind: kind, size: RecordPillStyle.tile)
            Text(kindDisplayPlural(kind)).ink(.label, on: .pill).lineLimit(1)
        }
        .padding(.leading, RecordPillStyle.leading)
        .padding(.trailing, ChatSpace.pillSide)
        .padding(.vertical, ChatSpace.pillEdge)
        .background(ChatColours.pillStyle, in: .capsule)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(kindDisplayPlural(kind)), \(kind)")
    }
}
