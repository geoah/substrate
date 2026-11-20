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
    @Environment(\.dynamicTypeSize) private var size
    /// How tall the lines over the field are, where they may scroll.
    @State private var tall: CGFloat?
    /// The listening under way ends in a send, not in the field.
    @State private var talking = false

    private var composer: ComposerPresentation { screen.composer }
    private var empty: Bool { text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    var body: some View {
        let _ = Meter.shared.count(.body, "Composer")
        VStack(spacing: ChatSpace.items) {
            if !size.isAccessibilitySize {
                notes
            } else if noted {
                // At the accessibility text sizes the lines can need more
                // room than the keyboard leaves. They are as tall as their
                // words and no taller, and scroll where they are given
                // less, after the field has given up its lines to one: the
                // field and its buttons keep their place over the
                // keyboard. By the text size and not by what fits: the bar
                // is asked its height before it is told its room.
                ScrollView {
                    VStack(spacing: ChatSpace.items) { notes }
                        .onGeometryChange(for: CGFloat.self) {
                            $0.size.height
                        } action: {
                            tall = $0
                        }
                }
                .scrollBounceBehavior(.basedOnSize)
                .scrollIndicatorsFlash(onAppear: true)
                .frame(maxHeight: tall)
                // Given their room before the field is: it has one line
                // at the least, and what is typed scrolls in it.
                .layoutPriority(1)
                .arrives()
            }
            GlassEffectContainer(spacing: ChatSpace.inset) {
                VStack(alignment: .trailing, spacing: ChatSpace.apart) {
                    if !atBottom {
                        Button(action: onJump) {
                            HStack(spacing: ChatSpace.mark) {
                                Image(systemName: "arrow.down")
                                    .font(ChatType.control)
                                    .growing(to: .control)
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
                    // Send beside Stop leaves the field a word's width at
                    // the accessibility text sizes: the two stand under it
                    // there.
                    // One field in either form: it keeps what is typed and
                    // the keyboard when the form changes under it.
                    let under = size.isAccessibilitySize && screen.streaming && composer.queues && !empty && !dictation.active
                    let form =
                        under
                        ? AnyLayout(VStackLayout(alignment: .trailing, spacing: ChatSpace.apart))
                        : AnyLayout(HStackLayout(alignment: .bottom, spacing: ChatSpace.apart))
                    form {
                        field
                            .glassEffect(.regular.interactive(), in: .rect(cornerRadius: control / 2))
                            .glassEffectID("field", in: glass)
                        HStack(spacing: ChatSpace.apart) { trailing }
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
            if screen.thread == nil, let heard = host.voice.heard {
                // Heard while the chat list's microphone was held.
                let sends = host.voice.sends
                host.voice.heard = nil
                text = joined(text, heard)
                if sends { send() } else { focused = true }
            } else if composer.listenOnOpen {
                host.send(.listened, to: key)
                // The chat list's microphone says how this listening ends.
                talking = host.voice.talks
                host.voice.talks = false
                await dictation.start(localeID: host.chats?.settings.voiceLocale)
            } else if screen.thread == nil {
                focused = true
            }
        }
    }

    // MARK: The lines over the field

    /// The reason the field is blocked, where it is said: while a reply is
    /// on its way the status line says so, and "wait for the answer" would
    /// say it twice.
    private var reason: String? {
        guard composer.status == nil, let reason = composer.blocked, !empty || composer.blockedWhenEmpty else { return nil }
        return reason
    }

    /// Whether anything stands over the field.
    private var noted: Bool {
        composer.keyless != nil || composer.answering != nil || composer.status != nil || reason != nil || dictation.problem != nil
    }

    /// What stands over the field, each line whole at any text size. The
    /// question under the answering note's title is left out at the
    /// accessibility sizes: the title, the way back and the field come
    /// first there, and the question stands in its card above.
    @ViewBuilder private var notes: some View {
        if let keyless = composer.keyless {
            KeylessCalloutView(callout: keyless) { onKey(keyless.provider) }
        }
        if let note = composer.answering {
            AnsweringNoteView(note: note, asking: !size.isAccessibilitySize) { host.send(.showOptions, to: key) }
                .arrives()
        }
        if let status = composer.status {
            ComposerStatusLine(status: status)
                .arrives()
            // What Stop does to the messages that wait to be read.
            if let queued = composer.queued {
                line(Text(queued).ink(.quiet, on: .field)).arrives()
            }
        } else if let reason {
            line(Text(reason).ink(.quiet, on: .field)).arrives()
        }
        if let problem = dictation.problem {
            line(Text(problem).ink(.tone(.critical), on: .field))
        }
    }

    private func line(_ words: Text) -> some View {
        words
            .font(ChatType.caption)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, ChatSpace.edge)
    }

    // MARK: The field

    /// What is typed or heard, the field's whole width: a new chat's
    /// assistant is picked from its title (`AgentTitle`).
    private var field: some View {
        typing
            .padding(.leading, ChatSpace.edge)
            .frame(minHeight: control)
    }

    private var typing: some View {
        HStack(alignment: .bottom, spacing: ChatSpace.beside) {
            if dictation.active {
                listening
            } else {
                TextField(composer.placeholder, text: $text, selection: $selection, axis: .vertical)
                    .textFieldStyle(.plain)
                    // Fewer lines at the accessibility text sizes, where
                    // eight would be the screen: the lines over the field
                    // say what is happening and are never the ones cut.
                    .lineLimit(1 ... (size.isAccessibilitySize ? 3 : 8))
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
        // The microphone's target reaches the field's edge; anything else
        // stands off it.
        .padding(.trailing, !dictation.active && !empty ? 0 : ChatSpace.beside)
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

    /// The microphone in a field that holds words: a tap only, since the
    /// field gives way to what is heard and the press would lose its view.
    private var micButton: some View {
        Button {
            Task { await micTapped() }
        } label: {
            Image(systemName: "mic")
                .font(ChatType.body)
                .ink(.quiet, on: .field)
                .growing(to: .control)
                .frame(width: ChatSize.target, height: control)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(micLabel)
    }

    private var micSettings: (tap: MicAction, hold: MicAction) {
        (host.chats?.settings.micTap ?? .dictate, host.chats?.settings.micHold ?? .talk)
    }

    private var micLabel: String { micSettings.tap == .talk ? "Talk" : "Dictate" }

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
        } else if dictation.active || empty {
            // One view while the field is empty or heard into, so a hold
            // that starts listening still hears its own release.
            microphone
        } else {
            // ⌘Return is the field's own key handling; the button says so.
            round("arrow.up", tint: .accentFill, label: "Send") { send() }
                .disabled(!composer.canSend)
                .sendShortcut(on: .current)
        }
    }

    private func round(_ symbol: String, tint: Color?, label: String, id: String = "action", action: @escaping () -> Void) -> some View {
        Button(action: action) {
            roundFace(symbol, tint: tint)
        }
        .buttonStyle(.plain)
        .glassEffect(tint.map { Glass.regular.tint($0).interactive() } ?? .regular.interactive(), in: .circle)
        .glassEffectID(id, in: glass)
        .accessibilityLabel(label)
    }

    private func roundFace(_ symbol: String, tint: Color?) -> some View {
        Image(systemName: symbol)
            .font(ChatType.control)
            .ink(tint == nil ? .label : .onFill, on: tint == nil ? .field : .fill)
            .growing(to: .control)
            .frame(width: control, height: control)
            .changes(.mark)
    }

    /// The microphone, and while it listens, the way to end it: Send where
    /// the listening ends in a send, Done where the words go into the field.
    /// A tap does what Settings says a tap does, a hold talks until it is
    /// let go.
    private var microphone: some View {
        let listening = dictation.active
        let symbol = listening ? (talking ? "arrow.up" : "checkmark") : "mic.fill"
        let tint: Color? = listening ? .accentFill : nil
        let label = listening ? (talking ? "Send" : "Done Listening") : micLabel
        return roundFace(symbol, tint: tint)
            .glassEffect(tint.map { Glass.regular.tint($0).interactive() } ?? .regular.interactive(), in: .circle)
            .glassEffectID("action", in: glass)
            .tapOrHold(
                holds: !listening && micSettings.hold != .off,
                tap: { Task { await micTapped() } },
                holdBegan: {
                    talking = micSettings.hold == .talk
                    Task { await startListening() }
                },
                holdEnded: { Task { await finishListening() } }
            )
            .disabled(screen.agent == nil)
            .accessibilityLabel(label)
            .accessibilityHint(listening || micSettings.hold == .off ? "" : "Hold to talk, and let go to send")
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
            if dictation.active {
                talking = false
                text = joined(text, await dictation.stop())
            }
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

    /// A tap on the microphone: listening starts, as Settings says a tap
    /// listens, or the listening under way ends.
    private func micTapped() async {
        if dictation.active {
            await finishListening()
        } else {
            talking = micSettings.tap == .talk
            await startListening()
        }
    }

    private func startListening() async {
        focused = false
        await dictation.start(localeID: host.chats?.settings.voiceLocale)
    }

    /// The listening ends: what was heard joins the field, and is sent
    /// where the listening was talk. The keyboard stays down, so the
    /// microphone can be tapped again for more.
    private func finishListening() async {
        guard dictation.active else { return }
        let heard = await dictation.stop()
        text = joined(text, heard)
        if talking {
            talking = false
            send()
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
                .growing(to: .actionMark)
                .frame(width: mark)
            Text(status.words)
                .fixedSize(horizontal: false, vertical: true)
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

/// An agent whose provider has no key cannot answer: said before anybody
/// types.
/// A provider with no key, and the button that adds one.
struct KeylessCalloutView: View {
    let callout: KeylessCallout
    let add: () -> Void

    var body: some View {
        // The button under the words where the text size leaves it no
        // room beside them.
        SplitRow {
            HStack(alignment: .firstTextBaseline, spacing: ChatSpace.beside) {
                Image(systemName: "key.fill").ink(.mark(.caution), on: .card)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: ChatSpace.pair) {
                    Text(callout.title).font(ChatType.heading)
                    Text(callout.message)
                        .font(ChatType.caption).ink(.quiet, on: .card)
                }
                .fixedSize(horizontal: false, vertical: true)
            }
        } trailing: {
            Button(callout.action, action: add)
                .buttonStyle(.borderedProminent)
                .prominentTint()
                .controlSize(.small)
                .touchTarget()
        }
        .card(.caution)
    }
}

/// The microphone's loudness as five bars.
struct LevelMeter: View {
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
