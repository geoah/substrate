import SubstrateUI
import SwiftUI
import TodayCore

/// "Add or ask": the one field, fixed at the bottom of the day and of every
/// page. What is typed goes to the assistant, which adds a task where the
/// words are one and answers where they ask, in place over the field.
struct ComposerBar: View {
    var composer: ComposerPresentation
    var answer: AnswerCard?

    @Environment(\.pane) private var pane
    @Environment(\.words) private var words
    @State private var text = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let answer {
                ScrollView {
                    AnswerCardView(answer: answer)
                }
                .scrollBounceBehavior(.basedOnSize)
                .frame(maxHeight: 260)
                .fixedSize(horizontal: false, vertical: true)
                .padding(14)
                .glass(in: RoundedRectangle(cornerRadius: TodayLook.listRadius, style: .continuous), dense: true)
                .transition(.arrive)
            }
            if let notKept = composer.notKept {
                Text(notKept).font(TodayLook.Text.footnote).foregroundStyle(.urgentWords)
                    .padding(.horizontal, 14)
                    .transition(.unfold)
            }
            HStack(spacing: 2) {
                IconButton(Symbol.add, label: words.add, action: submit)
                TextField(composer.placeholder, text: $text, axis: .vertical)
                    .font(TodayLook.Text.field)
                    .lineLimit(1 ... 4)
                    .textFieldStyle(.plain)
                    .focused($focused)
                    .onSubmit(submit)
                    .submitLabel(.done)
                    .frame(minHeight: TodayLook.target)
                if trimmed.isEmpty {
                    // Dictation is the keyboard's: the button puts the
                    // cursor in the field, where the system's microphone is.
                    IconButton(Symbol.dictate, label: words.dictate, look: .filled) { focused = true }
                } else {
                    IconButton(Symbol.send, label: words.send, look: .filled, action: submit)
                }
            }
            .padding(.horizontal, 5)
            .frame(minHeight: 54)
            .glass(in: Capsule(), dense: true)
        }
        .padding(.horizontal, TodayLook.side - 2)
        .padding(.bottom, 6)
    }

    private var trimmed: String { text.trimmingCharacters(in: .whitespacesAndNewlines) }

    private func submit() {
        guard !trimmed.isEmpty else {
            focused = true
            return
        }
        pane.send(.addOrAsk(trimmed))
        text = ""
        focused = false
    }
}
