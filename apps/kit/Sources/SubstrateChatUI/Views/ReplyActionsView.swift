#if canImport(SwiftUI)
import SubstrateChatCore
import SubstrateRuntime
import SubstrateUI
import SwiftUI

/// What stands under the last reply of a chat: Copy, Try Again or Continue
/// where the run can go on and its card does not offer that, and Share. A
/// row of small marks, each a whole target to touch and labelled for
/// VoiceOver. Its height is the same whatever it holds, and it is there
/// from the reply's first words, so a reply that settles moves nothing.
struct ReplyActionsRow: View, Equatable {
    @Environment(ChatHost.self) private var host
    @Environment(\.conversationKey) private var conversation
    let actions: ReplyActions
    /// The reply's text as its row holds it: what Share takes.
    let text: String?
    /// Under a message of the person's, which stands on the trailing side.
    var trailing = false
    /// What Try Again and Continue do.
    var act: (FailureAction) -> Void = { _ in }
    /// A mark's width at the text's size: its target is wider, and the
    /// row is moved back by half the difference, so the first mark and not
    /// its target's edge stands under the reply's first letter.
    @Scaled(.actionMark) private var mark

    static func == (lhs: ReplyActionsRow, rhs: ReplyActionsRow) -> Bool {
        // A reply being written has other text at every flush, which only
        // Share reads, and it is not offered then.
        lhs.actions == rhs.actions && lhs.trailing == rhs.trailing && (lhs.actions.whole ? lhs.text == rhs.text : true)
    }

    var body: some View {
        let _ = Meter.shared.count(.body, "ReplyActions")
        // On one line, and on the next where the text size leaves no
        // room for them all.
        WrapLayout(spacing: 0) {
            if actions.copies {
                CopyButton(label: actions.copyLabel, copied: actions.copiedLabel) { written(host, conversation, actions.turn) ?? text ?? "" }
            }
            if let resume = actions.resume {
                ResumeButton(button: resume) { act(resume.action) }
            }
            if actions.whole, let text {
                ShareLink(item: text) {
                    Image(systemName: "square.and.arrow.up")
                        .ink(.quiet, on: .page)
                        .growing(to: .actionMark)
                        .barTarget()
                }
                .buttonStyle(.borderless)
                .accessibilityLabel(actions.shareLabel)
                .help(actions.shareLabel)
            }
        }
        .font(ChatType.detail)
        .frame(minHeight: ChatSize.target)
        .padding(.leading, actions.copies ? -max(0, (ChatSize.target - mark) / 2) : 0)
        // Drawn into the space above and below it: its marks are small
        // and its targets are not, and a row a target high would stand
        // apart from the reply it belongs to.
        .padding(.vertical, -ChatSpace.items)
        .frame(maxWidth: .infinity, alignment: trailing ? .trailing : .leading)
        .accessibilityElement(children: .contain)
    }
}

/// The reply's text as it stands now, where the conversation holds it: a
/// row may have been drawn many pieces ago.
@MainActor
func written(_ host: ChatHost, _ conversation: ChildKey?, _ turn: String) -> String? {
    guard let conversation, let turns = host.conversation(conversation)?.turns,
        case let .assistant(now)? = turns.last(where: { $0.id == turn })?.body
    else { return nil }
    return now.text
}

/// Try Again or Continue under a reply: the words, since a mark alone
/// would not say which. While its work is out it says so and cannot be
/// pressed again; while a rate limit lasts it counts down.
private struct ResumeButton: View {
    let button: FailureButton
    let action: () -> Void

    var body: some View {
        if let until = button.enablesAt, until > Date() {
            TimelineView(.periodic(from: .now, by: 1)) { context in label(at: context.date) }
        } else {
            label(at: Date())
        }
    }

    private func label(at now: Date) -> some View {
        let enabled = button.enabled(at: now)
        return Button(action: action) {
            HStack(spacing: ChatSpace.mark) {
                Group {
                    if button.busy {
                        ProgressView().controlSize(.small)
                    } else {
                        Image(systemName: "arrow.clockwise")
                    }
                }
                .growing(to: .actionMark)
                Text(button.title(at: now)).monospacedDigit()
            }
            .font(ChatType.heading)
            .ink(enabled ? .accent : .quiet, on: .page)
            .padding(.horizontal, ChatSpace.inner)
            .barTarget()
        }
        .buttonStyle(.borderless)
        .disabled(!enabled)
        .help(button.blocked ?? "")
        .accessibilityLabel(button.title(at: now))
        .accessibilityValue(button.busy ? "Working" : "")
        .accessibilityHint(button.blocked ?? "")
    }
}

/// What a long press on a reply offers: Copy, Select Text, Try Again or
/// Continue where the row under the last reply has it, and Share. A reply
/// still being written offers Copy alone, of what is there.
struct ReplyMenu: View {
    @Environment(ChatHost.self) private var host
    @Environment(\.conversationKey) private var conversation
    @Environment(\.selectReply) private var select
    /// The turn's id.
    let id: String
    let reply: AssistantTurn
    /// The row under this reply, where it is the chat's last.
    let actions: ReplyActions?
    var act: (FailureAction) -> Void = { _ in }

    var body: some View {
        Button(actions?.copyLabel ?? "Copy", systemImage: "doc.on.doc") {
            copyToPasteboard(written(host, conversation, id) ?? reply.text ?? "")
            AccessibilityNotification.Announcement(actions?.copiedLabel ?? "Copied").post()
        }
        if !reply.live, let text = reply.text {
            if let select {
                Button(actions?.selectLabel ?? "Select Text", systemImage: "selection.pin.in.out") {
                    select(SelectableReply(id: id, rows: reply.blocks.rows, missing: reply.missing))
                }
            }
            if let resume = actions?.resume {
                Button(resume.title, systemImage: "arrow.clockwise") { act(resume.action) }
                    .disabled(!resume.enabled(at: Date()))
            }
            ShareLink(item: text)
        }
    }
}
#endif
