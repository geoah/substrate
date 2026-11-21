import SubstrateUI
import SwiftUI
import TodayCore

/// The assistant, about one thing: what it can be asked here as buttons,
/// the exchanges so far, and a field with a send button. The box is in the
/// assistant's hue and nothing else on the card is. It prepares and waits:
/// nothing leaves until the owner presses Send.
struct MossBoxView: View {
    var box: MossBox

    @Environment(\.pane) private var pane
    @State private var text = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: TodayLook.gap) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Image(systemName: Glyph.moss.systemName).font(TodayLook.font(.subheadline, weight: .semibold))
                    .accessibilityHidden(true)
                Text(box.title).font(TodayLook.Text.button).multilineTextAlignment(.leading)
            }
            .foregroundStyle(.mossWords)
            if !box.asks.isEmpty {
                WrapLayout(spacing: TodayLook.betweenButtons) {
                    ForEach(box.asks, id: \.self) { PaneButton($0, onTint: true) }
                }
            }
            ForEach(box.exchanges) { AnswerCardView(answer: $0, inBox: true) }
            if let unreachable = box.unreachable {
                NoteText(unreachable)
            } else {
                HStack(spacing: 6) {
                    TextField(box.placeholder, text: $text, axis: .vertical)
                        .font(TodayLook.Text.body)
                        .lineLimit(1 ... 4)
                        .textFieldStyle(.plain)
                        .focused($focused)
                        .onSubmit(send)
                        .submitLabel(.send)
                        .padding(.leading, 14)
                        .frame(minHeight: TodayLook.target)
                    IconButton(Symbol.send, label: PaneWords.send, look: .filled, action: send)
                }
                .background(.card, in: Capsule())
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            let shape = RoundedRectangle(cornerRadius: 20, style: .continuous)
            shape.fill(.mossFill).overlay(shape.strokeBorder(TodayLook.moss.color.opacity(0.35), lineWidth: 1))
        }
    }

    private func send() {
        let asked = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !asked.isEmpty else {
            focused = true
            return
        }
        pane.ask(box.subject, asked)
        text = ""
        focused = false
    }
}

/// One exchange with the assistant: what was asked, and its answer as it
/// arrives, with what it was answered from.
struct AnswerCardView: View {
    var answer: AnswerCard
    /// Inside the assistant's box: on its fill, with no surface of its own.
    var inBox = false

    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            (Text(answer.heading + ": ").foregroundStyle(.inkSecond) + Text(answer.question).foregroundStyle(.ink))
                .font(TodayLook.Text.footnote.weight(.semibold))
                .fixedSize(horizontal: false, vertical: true)
            switch answer.state {
            case .waiting:
                ProgressView().controlSize(.small).frame(minHeight: 22)
            case .writing, .done:
                RecordMarkdownText(answer.text).font(TodayLook.Text.body).foregroundStyle(.ink)
            case .failed:
                Text(answer.text).font(TodayLook.Text.body).foregroundStyle(.urgentWords)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if !answer.sources.isEmpty {
                WrapLayout(spacing: TodayLook.betweenChips) {
                    ForEach(answer.sources, id: \.self) { PillView(Pill(.neutral, $0)) }
                }
            }
            if let button = answer.button { PaneButton(button, onTint: inBox, small: true) }
        }
        .padding(.top, inBox ? 10 : 0)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .top) {
            if inBox { Rectangle().fill(TodayLook.line(scheme)).frame(height: 0.5) }
        }
    }
}
