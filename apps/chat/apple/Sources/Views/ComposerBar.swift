import ChatKit
import SubstrateKit
import SwiftUI

/// Where you write to the agent: a floating glass bar over the transcript,
/// in one container with Jump to Latest so the two merge rather than stack.
/// The trailing button is the microphone while the field is empty, Send once
/// it is not, and Stop while a turn runs; it morphs between them. While a
/// reply is on its way a line over the field says what is happening
/// (`ComposerStatus`), Stop is there the whole time, and the field can be
/// typed in throughout. Where the server takes a message into a running
/// turn (`ComposerPresentation.queues`), Send stands beside Stop once
/// something is typed, and a line says what Stop does to the messages that
/// wait; elsewhere only Stop is offered until the reply ends. The text
/// is the field's own while it is typed: the conversation is told after a
/// pause, before a send and when the field stops editing (`DraftReports`),
/// so a key draws nothing but the field. The field takes what the
/// conversation writes back itself (a send clears it, a refusal gives it
/// back), and keeps what was typed meanwhile after it.
struct ComposerBar: View {
    @Environment(ChatHost.self) private var host
    @Environment(\.scenePhase) private var phase
    let key: ChildKey
    let screen: ConversationScreen
    let dictation: Dictation
    let atBottom: Bool
    /// What Jump to Latest says: how many messages are below.
    let jump: JumpToLatest
    let onJump: () -> Void
    let onKey: (String) -> Void
    @Environment(\.draftField) private var draftField
    @State private var text = ""
    @State private var draft = DraftTeller()
    @FocusState private var focused: Bool
    @State private var selection: TextSelection?
    @Namespace private var glass
    @State private var sent = 0
    @State private var stopped = 0
    @Scaled(.control) private var control

    private var composer: ComposerPresentation { screen.composer }
    private var empty: Bool { text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    var body: some View {
        let _ = Meter.shared.count(.body, "Composer")
        VStack(spacing: ChatSpace.items) {
            if let keyless = composer.keyless {
                KeylessCalloutView(callout: keyless) { onKey(keyless.provider) }
            }
            if let note = composer.answering {
                AnsweringNoteView(note: note) { host.send(.showOptions, to: key) }
                    .arrives()
            }
            if let status = composer.status {
                ComposerStatusLine(status: status)
                    .arrives()
                // What Stop does to the messages that wait to be read.
                if let queued = composer.queued {
                    Text(queued)
                        .font(ChatType.caption)
                        .ink(.quiet, on: .field)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, ChatSpace.edge)
                        .arrives()
                }
            } else if let reason = composer.blocked, !empty || composer.blockedWhenEmpty {
                // While a reply is on its way the line above says so: "wait
                // for the answer" would say it twice.
                Text(reason)
                    .font(ChatType.caption)
                    .ink(.quiet, on: .field)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, ChatSpace.edge)
                    .arrives()
            }
            if let problem = dictation.problem {
                Text(problem)
                    .font(ChatType.caption)
                    .ink(.tone(.critical), on: .field)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, ChatSpace.edge)
            }
            GlassEffectContainer(spacing: ChatSpace.inset) {
                VStack(alignment: .trailing, spacing: ChatSpace.apart) {
                    if !atBottom {
                        Button(action: onJump) {
                            HStack(spacing: ChatSpace.mark) {
                                Image(systemName: "arrow.down")
                                    .font(ChatType.control)
                                if let count = jump.count {
                                    Text(count)
                                        .font(ChatType.heading)
                                        .monospacedDigit()
                                        .changes(.digits)
                                }
                            }
                            .padding(.horizontal, jump.count == nil ? 0 : ChatSpace.inset)
                            .frame(minWidth: ChatSize.target, minHeight: ChatSize.target)
                            .contentShape(.capsule)
                        }
                        .buttonStyle(.plain)
                        .glassEffect(.regular.interactive(), in: .capsule)
                        .glassEffectID("jump", in: glass)
                        .accessibilityLabel(jump.label)
                        .arrives(.grow)
                    }
                    HStack(alignment: .bottom, spacing: ChatSpace.apart) {
                        field
                            .glassEffect(.regular.interactive(), in: .rect(cornerRadius: control / 2))
                            .glassEffectID("field", in: glass)
                        trailing
                    }
                }
            }
            .motion(.status, value: ComposerShape(atBottom: atBottom, jump: jump, empty: empty, streaming: screen.streaming, listening: dictation.active))
        }
        .motion(.status, value: composer.status?.kind)
        .padding(.horizontal, ChatSpace.gutter)
        .padding(.bottom, ChatSpace.items)
        // As wide as the transcript's words, however wide the window.
        .frame(maxWidth: ChatSize.measure + 2 * ChatSpace.gutter)
        .frame(maxWidth: .infinity)
        .sensoryFeedback(.impact(weight: .light), trigger: sent)
        .sensoryFeedback(.impact(weight: .light), trigger: stopped)
        .onChange(of: composer.draftRevision, initial: true) { _, _ in
            if let next = draft.reports.take(composer, holding: text) { text = next }
            say()
        }
        .onChange(of: text) { _, text in
            draft.typed(text) { [host, key] in host.send($0, to: key) }
        }
        // A question is being answered in the person's own words here.
        .onChange(of: composer.focus) { _, _ in focused = true }
        .onAppear { draftField?.flush = { host.send(draft.reports.tell(text), to: key) } }
        .onChange(of: focused) { _, focused in
            if !focused { say() }
        }
        .onChange(of: phase) { _, phase in
            if phase != .active { say() }
        }
        .onDisappear {
            // The chat left the screen: the tree carries the text for the
            // field that comes after. Its conversation may be gone before
            // it hears, so the list keeps the draft either way.
            host.send(draft.reports.leave(text), to: key)
            host.send(.keepDraft(key: key, thread: screen.thread, text: text))
        }
        .task {
            if composer.listenOnOpen {
                host.send(.listened, to: key)
                await dictation.start(localeID: host.chats?.settings.voiceLocale)
            } else if screen.thread == nil {
                focused = true
            }
        }
    }

    // MARK: The field

    @ViewBuilder private var field: some View {
        HStack(alignment: .bottom, spacing: ChatSpace.beside) {
            if composer.picksAgent {
                AgentChip(key: key, current: screen.agent, label: composer.agentLabel)
            }
            if dictation.active {
                listening
            } else {
                TextField(composer.placeholder, text: $text, selection: $selection, axis: .vertical)
                    .textFieldStyle(.plain)
                    .lineLimit(1 ... 8)
                    .focused($focused)
                    .padding(.vertical, ChatSpace.blocks)
                    // What the press does is ChatKit's rule (`ComposerKeys`):
                    // Command-Return sends, any other Return is a new line.
                    // A held key repeats the new line and never the send.
                    .onKeyPress(.return, phases: [.down, .repeat]) { press in
                        switch ComposerKeys.onReturn(.init(press.modifiers), on: .current, composing: composingMarkedText()) {
                        case .send:
                            if press.phase == .down { send() }
                            return .handled
                        case .newline:
                            insertNewline()
                            return .handled
                        case .fieldsOwn:
                            return .ignored
                        }
                    }
                if !empty {
                    micButton
                }
            }
        }
        .padding(.leading, composer.picksAgent ? ChatSpace.lines : ChatSpace.edge)
        // The microphone's target reaches the field's edge; anything else
        // stands off it.
        .padding(.trailing, !dictation.active && !empty ? 0 : ChatSpace.beside)
        .frame(minHeight: control)
    }

    /// What is being heard: the finalized words, then the guess, dimmed.
    private var listening: some View {
        HStack(alignment: .center, spacing: ChatSpace.apart) {
            LevelMeter(level: dictation.level)
            Group {
                switch dictation.state {
                case let .preparing(words):
                    Text(words).ink(.quiet, on: .field)
                default:
                    if dictation.heard.isEmpty {
                        Text("Listening…").ink(.quiet, on: .field)
                    } else {
                        Text(dictation.finalized) + Text(dictation.volatile).ink(.quiet, on: .field)
                    }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .lineLimit(1 ... 8)
            .padding(.vertical, ChatSpace.blocks)
            .accessibilityAddTraits(.updatesFrequently)
        }
    }

    private var micButton: some View {
        Button {
            Task { await toggleListening() }
        } label: {
            Image(systemName: "mic")
                .font(ChatType.body)
                .ink(.quiet, on: .field)
                .frame(width: ChatSize.target, height: control)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Dictate")
    }

    // MARK: The trailing button

    @ViewBuilder private var trailing: some View {
        if screen.streaming {
            // Where the server takes a message into a running turn, Send
            // stands beside Stop once something is typed; elsewhere only
            // Stop is offered until the reply ends.
            if composer.queues, !empty, !dictation.active {
                round("arrow.up", tint: .accentFill, label: "Send", id: "send") { send() }
                    .disabled(!composer.canSend)
                    .sendShortcut(on: .current)
                    .arrives(.grow)
            }
            round("stop.fill", tint: .destructiveFill, label: screen.stopping ? "Stopping" : "Stop") {
                stopped += 1
                host.send(.stop, to: key)
            }
            .disabled(screen.stopping)
            .accessibilityHint(composer.queued ?? "")
        } else if dictation.active {
            round("checkmark", tint: .accentFill, label: "Done Listening") {
                Task { await toggleListening() }
            }
        } else if empty {
            round("mic.fill", tint: nil, label: "Dictate") { Task { await toggleListening() } }
                .disabled(screen.agent == nil)
        } else {
            // ⌘Return is the field's own key handling; the button says so.
            round("arrow.up", tint: .accentFill, label: "Send") { send() }
                .disabled(!composer.canSend)
                .sendShortcut(on: .current)
        }
    }

    private func round(_ symbol: String, tint: Color?, label: String, id: String = "action", action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(ChatType.control)
                .ink(tint == nil ? .label : .onFill, on: tint == nil ? .field : .fill)
                .frame(width: control, height: control)
                .changes(.mark)
        }
        .buttonStyle(.plain)
        .glassEffect(tint.map { Glass.regular.tint($0).interactive() } ?? .regular.interactive(), in: .circle)
        .glassEffectID(id, in: glass)
        .accessibilityLabel(label)
    }

    // MARK: Actions

    /// Return on a Mac: a new line where the cursor is, over any selection.
    private func insertNewline() {
        var replaced: Range<Int>?
        if case let .selection(range)? = selection?.indices,
            range.lowerBound >= text.startIndex, range.upperBound <= text.endIndex
        {
            replaced = text.distance(from: text.startIndex, to: range.lowerBound) ..< text.distance(from: text.startIndex, to: range.upperBound)
        }
        // Where the cursor lands is ChatKit's to say: a new line can join
        // the character before it, and the draft is then no longer.
        let (text, cursor) = ComposerKeys.newline(in: text, replacing: replaced)
        self.text = text
        selection = TextSelection(insertionPoint: text.index(text.startIndex, offsetBy: cursor))
    }

    private func send() {
        Task {
            if dictation.active { await toggleListening() }
            guard composer.canSend, !empty else { return }
            // The conversation sends what it holds: everything typed first.
            host.send(draft.reports.tell(text), to: key)
            host.send(.send, to: key)
            sent += 1
        }
    }

    /// Tells the conversation what it has not heard of the field, now.
    @discardableResult
    private func say() -> Conversation.Command? {
        guard let command = draft.reports.say() else { return nil }
        host.send(command, to: key)
        return command
    }

    private func toggleListening() async {
        if dictation.active {
            let heard = await dictation.stop()
            text = joined(text, heard)
            focused = true
        } else {
            focused = false
            await dictation.start(localeID: host.chats?.settings.voiceLocale)
        }
    }
}

/// What a field has told its conversation (`DraftReports`), and the one
/// wait for the pause in the typing that tells it more. Not observed: a key
/// changes it, and draws nothing for it.
@MainActor
final class DraftTeller {
    var reports = DraftReports()
    private var waiting: Task<Void, Never>?

    /// The field's text changed: the conversation is told once the typing
    /// rests, by the wait already under way where there is one.
    func typed(_ text: String, tell: @escaping @MainActor (Conversation.Command) -> Void) {
        reports.typed(text, at: .now)
        guard waiting == nil, reports.due != nil else { return }
        waiting = Task { [weak self] in
            // Each key moves the instant later: wait again until it holds.
            while let due = self?.reports.due {
                let wait = due.timeIntervalSinceNow
                if wait > 0 { try? await Task.sleep(for: .seconds(wait)) }
                if Task.isCancelled { return }
                if let command = self?.reports.paused(at: .now) { tell(command) }
            }
            self?.waiting = nil
        }
    }

    isolated deinit {
        waiting?.cancel()
    }
}

/// The composer's field as the rest of a conversation's views reach it. The
/// field owns its text while it is typed, so whatever sends the
/// conversation's draft from outside the composer (a card's Send Again)
/// calls this first, then sends its command: the two reach the
/// conversation in that order, and the send carries every key.
@MainActor
final class DraftField {
    fileprivate var flush: () -> Void = {}

    /// Tells the conversation what the field holds, whatever it heard last.
    func callAsFunction() { flush() }
}

extension EnvironmentValues {
    /// The composer's field of the conversation the view is in.
    @Entry var draftField: DraftField?
}

/// Drawn again only for what it draws: of the conversation's screen, its
/// composer, its agent and thread and whether a turn streams or stops, so a
/// piece of a reply arriving above it draws nothing here. A field of the
/// screen the bar comes to read goes into this comparison with it.
/// What the glass of the bar is shaped by: a change of any of these moves
/// it as one.
private struct ComposerShape: Equatable {
    var atBottom: Bool
    var jump: JumpToLatest
    var empty: Bool
    var streaming: Bool
    var listening: Bool
}

extension ComposerBar: @MainActor Equatable {
    static func == (lhs: ComposerBar, rhs: ComposerBar) -> Bool {
        lhs.key == rhs.key && lhs.screen.composer == rhs.screen.composer && lhs.screen.streaming == rhs.screen.streaming
            && lhs.screen.stopping == rhs.screen.stopping && lhs.screen.agent == rhs.screen.agent
            && lhs.screen.thread == rhs.screen.thread && lhs.atBottom == rhs.atBottom && lhs.jump == rhs.jump
            && lhs.dictation === rhs.dictation
    }
}

/// What is happening while a reply is on its way, in one line over the
/// field: the words, with a mark that moves while something is. A screen
/// reader hears it as one thing that updates, and is not told of each
/// change.
struct ComposerStatusLine: View {
    let status: ComposerStatus
    /// Every status's mark stands in one column, so the words do not move
    /// when one status gives way to the next.
    @Scaled(.actionMark) private var mark

    var body: some View {
        HStack(spacing: ChatSpace.words) {
            Image(systemName: symbol)
                .imageScale(.small)
                .pulses(while: status.kind != .stopping)
                .frame(width: mark)
            Text(status.words)
                .lineLimit(2)
                .shimmer()
                .changes(.words)
        }
        .font(ChatType.label)
        .ink(.quiet, on: .field)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, ChatSpace.edge)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(status.words)
        .accessibilityAddTraits(.updatesFrequently)
    }

    private var symbol: String {
        switch status.kind {
        case .connecting: "antenna.radiowaves.left.and.right"
        case .reconnecting: "arrow.triangle.2.circlepath"
        case .thinking: "brain"
        case .writing: "pencil.line"
        case .tool: "wrench.and.screwdriver"
        case .stopping: "stop.circle"
        }
    }
}

/// The agent a new chat goes to, chosen from the offered ones.
private struct AgentChip: View {
    @Environment(ChatHost.self) private var host
    let key: ChildKey
    let current: String?
    let label: String
    @Scaled(.control) private var control

    var body: some View {
        Menu {
            ForEach(host.chats?.offered ?? []) { agent in
                Button {
                    host.send(.choose(agent: agent.id), to: key)
                } label: {
                    if agent.id == current {
                        Label(agent.name, systemImage: "checkmark")
                    } else {
                        Text(agent.name)
                    }
                }
            }
        } label: {
            // Centred on the field's first line.
            AgentPill(id: current, name: label, picker: true)
                .frame(maxWidth: ChatSize.chipMax, alignment: .leading)
                .frame(minHeight: control)
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .fixedSize()
        .accessibilityLabel("Assistant: \(label)")
    }
}

/// An agent whose provider has no key cannot answer: said before anybody
/// types.
/// A provider with no key, and the button that adds one.
struct KeylessCalloutView: View {
    let callout: KeylessCallout
    let add: () -> Void

    var body: some View {
        HStack(spacing: ChatSpace.apart) {
            Image(systemName: "key.fill").ink(.mark(.caution), on: .card)
            VStack(alignment: .leading, spacing: ChatSpace.pair) {
                Text(callout.title).font(ChatType.heading)
                Text(callout.message)
                    .font(ChatType.caption).ink(.quiet, on: .card)
            }
            Spacer(minLength: 0)
            Button(callout.action, action: add)
                .buttonStyle(.borderedProminent)
                .prominentTint()
                .controlSize(.small)
        }
        .card(.caution)
    }
}

/// The microphone's loudness as five bars.
private struct LevelMeter: View {
    let level: Float
    @Scaled(.meter) private var height

    var body: some View {
        HStack(spacing: ChatSpace.pair) {
            ForEach(0 ..< 5) { i in
                // The middle bar reaches highest; a quiet room is a
                // quarter of the meter.
                let weight: Float = [0.5, 0.8, 1, 0.8, 0.5][i]
                Capsule()
                    .ink(.accent, on: .field)
                    .frame(width: ChatLine.rail, height: height * (0.25 + 0.7 * CGFloat(min(1, level * weight * 1.4))))
            }
        }
        .frame(height: height)
        .motion(.level, value: level)
        .accessibilityHidden(true)
    }
}
