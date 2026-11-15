import ChatKit
import SubstrateKit
import SwiftUI

/// A failure, drawn the same wherever it stands: a card at its turn in the
/// transcript, the strip above the composer, the strip at the top of the
/// list. One sentence, at most one note, one button that does the next
/// thing, and the details behind a disclosure, under a label that says
/// whose words they are. Never an alert.
struct FailureView: View {
    @Environment(\.colorScheme) private var scheme
    @Scaled(.markColumn) private var symbolWidth
    let failure: FailurePresentation
    let act: (FailureAction) -> Void
    @State private var open: Bool

    /// `opened` draws it with its Details showing from the start.
    init(failure: FailurePresentation, opened: Bool = false, act: @escaping (FailureAction) -> Void) {
        self.failure = failure
        self.act = act
        _open = State(initialValue: opened)
    }

    private var palette: FailurePalette { .of(failure.tone, dark: scheme == .dark) }

    var body: some View {
        VStack(alignment: .leading, spacing: ChatSpace.parts) {
            HStack(alignment: .firstTextBaseline, spacing: ChatSpace.beside) {
                Group {
                    if failure.working {
                        ProgressView().controlSize(.small)
                    } else {
                        Image(systemName: symbol).foregroundStyle(Color(palette.accent))
                    }
                }
                .frame(width: symbolWidth, alignment: .center)
                .accessibilityHidden(true)
                // One element, its sentence the label: the note follows it.
                VStack(alignment: .leading, spacing: ChatSpace.pair) {
                    Text(failure.sentence)
                        .font(ChatType.heading)
                        .foregroundStyle(Color(palette.ink))
                    if let note = failure.note {
                        Text(note)
                            .font(ChatType.detail)
                            .foregroundStyle(Color(palette.soft))
                    }
                }
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(failure.spoken)
                .accessibilityAddTraits(.isStaticText)
                Spacer(minLength: 0)
                if let dismiss = failure.dismiss {
                    // An unsent message is not dismissed: it goes back into
                    // the composer, and the mark says so.
                    let edits = dismiss == .edit
                    Button {
                        act(dismiss)
                    } label: {
                        Image(systemName: edits ? "square.and.pencil" : "xmark")
                            .font(ChatType.noteStrong)
                            .foregroundStyle(Color(palette.soft))
                            .frame(minWidth: ChatSize.target, minHeight: ChatSize.target, alignment: .topTrailing)
                            .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    // The mark stands at the card's corner; its room to
                    // touch reaches down and in from it.
                    .frame(width: symbolWidth, height: symbolWidth, alignment: .topTrailing)
                    .accessibilityLabel(edits ? "Edit Message" : "Dismiss")
                    .help(edits ? "Edit Message" : "Dismiss")
                }
            }
            if failure.button != nil || failure.details != nil {
                // Under the words, past the symbol; one above the other
                // where the text size leaves no room side by side.
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: ChatSpace.inset) { controls }
                    VStack(alignment: .leading, spacing: ChatSpace.items) { controls }
                }
                .padding(.leading, symbolWidth + ChatSpace.beside)
            }
            if open, let details = failure.details {
                VStack(alignment: .leading, spacing: ChatSpace.pair) {
                    Text(details.label)
                        .font(ChatType.label)
                        .foregroundStyle(Color(palette.soft))
                    Text(details.text)
                        .font(ChatType.verbatim)
                        .foregroundStyle(Color(palette.soft))
                        .textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .padding(.leading, symbolWidth + ChatSpace.beside)
                .accessibilityElement(children: .combine)
            }
        }
        .padding(ChatSpace.inset)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(palette.fill), in: .rect(cornerRadius: ChatRadius.card))
        .overlay {
            RoundedRectangle(cornerRadius: ChatRadius.card).strokeBorder(Color(palette.edge), lineWidth: ChatLine.hairline)
        }
        .accessibilityElement(children: .contain)
        .onChange(of: failure.id) { _, _ in open = false }
    }

    @ViewBuilder private var controls: some View {
        if let button = failure.button {
            FailureButtonView(button: button, palette: palette) { act(button.action) }
        }
        if let details = failure.details {
            Button {
                ChatMotion.animate(.fold) { open.toggle() }
            } label: {
                HStack(spacing: ChatSpace.mark) {
                    Text(details.title)
                    Image(systemName: "chevron.right")
                        .imageScale(.small)
                        .rotationEffect(.degrees(open ? 90 : 0))
                }
                .font(ChatType.noteStrong)
                .foregroundStyle(Color(palette.accent))
                .frame(minHeight: ChatSize.target)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(details.title)
            .accessibilityValue(open ? "Shown" : "Hidden")
            .accessibilityHint(details.label)
        }
    }

    private var symbol: String {
        switch failure.tone {
        case .bad: "exclamationmark.circle.fill"
        case .warn: "exclamationmark.triangle.fill"
        case .quiet: "minus.circle.fill"
        }
    }
}

/// A failure's one button. While its work is out it shows so and cannot be
/// pressed again; while a rate limit lasts it counts down and cannot be
/// pressed before the end.
private struct FailureButtonView: View {
    let button: FailureButton
    let palette: FailurePalette
    let action: () -> Void

    var body: some View {
        if let until = button.enablesAt, until > Date() {
            TimelineView(.periodic(from: .now, by: 1)) { context in
                label(at: context.date)
            }
        } else {
            label(at: Date())
        }
    }

    private func label(at now: Date) -> some View {
        let enabled = button.enabled(at: now)
        return Button(action: action) {
            HStack(spacing: ChatSpace.words) {
                if button.busy {
                    ProgressView().controlSize(.small)
                }
                Text(button.title(at: now))
                    .monospacedDigit()
                if button.external {
                    Image(systemName: ExternalMark.symbol).imageScale(.small)
                }
            }
            .font(ChatType.heading)
            .foregroundStyle(Color(enabled ? palette.onAccent : palette.soft))
            .padding(.horizontal, ChatSpace.inset)
            .padding(.vertical, ChatSpace.items)
            .background(Color(enabled ? palette.accent : palette.idle), in: .capsule)
            // The capsule is as tall as its words; its room to touch is a
            // whole target.
            .frame(minHeight: ChatSize.target)
            .contentShape(.rect)
        }
        .buttonStyle(FailureButtonStyle())
        .disabled(!enabled)
        .help(button.blocked ?? "")
        .accessibilityLabel(button.title(at: now))
        .accessibilityValue(button.busy ? "Working" : "")
        .accessibilityHint(button.blocked ?? (button.external ? ExternalMark.words : ""))
    }
}

/// The button draws its own waiting state, in colours held to the same
/// contrast: the system's dimming of a disabled label would undo that.
private struct FailureButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.opacity(configuration.isPressed ? ChatColours.pending : 1)
    }
}

extension Color {
    init(_ shade: FailurePalette.Shade) {
        self.init(red: shade.red, green: shade.green, blue: shade.blue)
    }
}

/// What a failure's button does, from wherever it is drawn.
@MainActor
struct FailureActions {
    let host: ChatHost
    let scene: String
    /// The conversation the failure is in; nil for the list's.
    var key: ChildKey?
    var openURL: OpenURLAction
    /// The composer's field of that conversation, which owns its text
    /// while it is typed.
    var draftField: DraftField?
    /// The app's own sheet for a provider's key.
    var showKey: (String) -> Void = { _ in }

    func callAsFunction(_ action: FailureAction) {
        switch action {
        case .resume: if let key { host.send(.continueTurn, to: key) }
        case .send:
            // Send Again sends what the conversation holds: the text given
            // back, and first what the field has typed after it and not
            // said.
            if let key {
                draftField?()
                host.send(.send, to: key)
            }
        case .sendAgain: if let key { host.send(.sendAgain, to: key) }
        case .edit:
            // Edit Message puts the message ahead of what the conversation
            // holds: first what the field has typed and not said.
            if let key {
                draftField?()
                host.send(.takeBack, to: key)
            }
        case .stop: if let key { host.send(.stop, to: key) }
        case .reload: if let key { host.send(.retryReload, to: key) }
        case .reopen: if let key { host.send(.reopen, to: key) }
        case .dismiss: if let key { host.send(.dismissFailure, to: key) }
        case let .reloadRows(thread): host.send(.reloadRows(thread: thread))
        case let .rename(thread, subject): host.send(.rename(thread: thread, subject: subject))
        case .dismissRename: host.send(.dismissRename)
        case .refresh: host.send(.refresh)
        case let .addKey(provider): showKey(provider)
        case let .open(page): if let url = URL(string: page) { openURL(url) }
        case let .newChat(agent): host.send(.newChat(agent: agent, listen: false, scene: scene))
        case let .retryCard(reference): host.send(.retryCard(reference))
        }
    }
}
