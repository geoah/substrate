import ChatKit
import SubstrateKit
import SwiftUI

/// Where you write to the agent: a floating glass bar over the transcript,
/// in one container with Jump to Latest so the two merge rather than stack.
/// The trailing button is the microphone while the field is empty, Send once
/// it is not, and Stop while a turn runs; it morphs between them. The text
/// is the conversation's: each edit goes to it, and the field takes what it
/// writes back itself (a send clears it, a refusal gives it back).
struct ComposerBar: View {
    @Environment(ChatHost.self) private var host
    let key: ChildKey
    let screen: ConversationScreen
    let dictation: Dictation
    let atBottom: Bool
    let onJump: () -> Void
    let onKey: (String) -> Void
    @State private var text = ""
    @State private var revision = -1
    @FocusState private var focused: Bool
    @State private var selection: TextSelection?
    @Namespace private var glass
    @State private var sent = 0

    private var composer: ComposerPresentation { screen.composer }
    private var empty: Bool { text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    var body: some View {
        VStack(spacing: 8) {
            if let keyless = composer.keyless {
                KeylessCalloutView(callout: keyless) { onKey(keyless.provider) }
            }
            if let reason = composer.blocked, !empty || composer.blockedWhenEmpty {
                Text(reason)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 18)
                    .transition(.opacity)
            }
            if let problem = dictation.problem {
                Text(problem)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 18)
            }
            GlassEffectContainer(spacing: 14) {
                VStack(alignment: .trailing, spacing: 10) {
                    if !atBottom {
                        Button(action: onJump) {
                            Image(systemName: "arrow.down")
                                .font(.body.weight(.semibold))
                                .frame(width: 40, height: 40)
                        }
                        .buttonStyle(.plain)
                        .glassEffect(.regular.interactive(), in: .circle)
                        .glassEffectID("jump", in: glass)
                        .accessibilityLabel("Jump to Latest")
                        .transition(.scale.combined(with: .opacity))
                    }
                    HStack(alignment: .bottom, spacing: 10) {
                        field
                            .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 24))
                            .glassEffectID("field", in: glass)
                        trailing
                    }
                }
            }
            .animation(.snappy, value: atBottom)
            .animation(.snappy, value: empty)
            .animation(.snappy, value: screen.streaming)
            .animation(.snappy, value: dictation.active)
        }
        .padding(.horizontal, 12)
        .padding(.bottom, 8)
        .frame(maxWidth: 820)
        .frame(maxWidth: .infinity)
        .sensoryFeedback(.impact(weight: .light), trigger: sent)
        .onChange(of: composer.draftRevision, initial: true) { _, next in
            guard next != revision else { return }
            revision = next
            text = composer.draft
        }
        .onChange(of: text) { _, text in host.send(.draft(text), to: key) }
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
        HStack(alignment: .bottom, spacing: 8) {
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
                    .padding(.vertical, 11)
                    #if os(macOS)
                .onKeyPress(.return, phases: .down) { press in
                    if press.modifiers.contains(.option) { return .ignored }
                    if press.modifiers.contains(.shift) {
                        insertNewline()
                        return .handled
                    }
                    send()
                    return .handled
                }
                    #else
                .onKeyPress(.return, phases: .down) { press in
                    guard press.modifiers.contains(.command) else { return .ignored }
                    send()
                    return .handled
                }
                    #endif
                if !empty {
                    micButton
                        .padding(.bottom, 8)
                }
            }
        }
        .padding(.leading, composer.picksAgent ? 7 : 16)
        .padding(.trailing, 8)
        .frame(minHeight: 46)
    }

    /// What is being heard: the finalized words, then the guess, dimmed.
    private var listening: some View {
        HStack(alignment: .center, spacing: 10) {
            LevelMeter(level: dictation.level)
            Group {
                switch dictation.state {
                case let .preparing(words):
                    Text(words).foregroundStyle(.secondary)
                default:
                    if dictation.heard.isEmpty {
                        Text("Listening…").foregroundStyle(.secondary)
                    } else {
                        Text(dictation.finalized) + Text(dictation.volatile).foregroundStyle(.secondary)
                    }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .lineLimit(1 ... 8)
            .padding(.vertical, 11)
            .accessibilityAddTraits(.updatesFrequently)
        }
    }

    private var micButton: some View {
        Button {
            Task { await toggleListening() }
        } label: {
            Image(systemName: "mic")
                .font(.body)
                .foregroundStyle(.secondary)
                .frame(width: 30, height: 30)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Dictate")
    }

    // MARK: The trailing button

    @ViewBuilder private var trailing: some View {
        if screen.streaming {
            round("stop.fill", tint: .red, label: screen.stopping ? "Stopping" : "Stop") { host.send(.stop, to: key) }
                .disabled(screen.stopping)
        } else if dictation.active {
            round("checkmark", tint: .accentColor, label: "Done Listening") {
                Task { await toggleListening() }
            }
        } else if empty {
            round("mic.fill", tint: nil, label: "Dictate") { Task { await toggleListening() } }
                .disabled(screen.agent == nil)
        } else {
            // Return on the Mac and ⌘Return on an iPad keyboard are the
            // field's own key handling.
            round("arrow.up", tint: .accentColor, label: "Send") { send() }
                .disabled(!composer.canSend)
        }
    }

    private func round(_ symbol: String, tint: Color?, label: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.body.weight(.semibold))
                .foregroundStyle(tint == nil ? Color.primary : Color.white)
                .frame(width: 46, height: 46)
                .contentTransition(.symbolEffect(.replace))
        }
        .buttonStyle(.plain)
        .glassEffect(tint.map { Glass.regular.tint($0).interactive() } ?? .regular.interactive(), in: .circle)
        .glassEffectID("action", in: glass)
        .accessibilityLabel(label)
    }

    // MARK: Actions

    /// Shift-Return: a new line where the cursor is, over any selection.
    private func insertNewline() {
        var text = text
        var lower = text.count
        var upper = text.count
        if case let .selection(range)? = selection?.indices,
            range.lowerBound >= text.startIndex, range.upperBound <= text.endIndex
        {
            lower = text.distance(from: text.startIndex, to: range.lowerBound)
            upper = text.distance(from: text.startIndex, to: range.upperBound)
        }
        let start = text.index(text.startIndex, offsetBy: lower)
        let end = text.index(text.startIndex, offsetBy: upper)
        text.replaceSubrange(start ..< end, with: "\n")
        self.text = text
        selection = TextSelection(insertionPoint: text.index(text.startIndex, offsetBy: lower + 1))
    }

    private func send() {
        Task {
            if dictation.active { await toggleListening() }
            guard composer.canSend, !empty else { return }
            host.send(.draft(text), to: key)
            host.send(.send, to: key)
            sent += 1
        }
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

/// The agent a new chat goes to, chosen from the offered ones.
private struct AgentChip: View {
    @Environment(ChatHost.self) private var host
    let key: ChildKey
    let current: String?
    let label: String

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
            // Centred on the field's first line: 32 points in a 46-point row.
            AgentPill(id: current, name: label, picker: true)
                .frame(maxWidth: 180, alignment: .leading)
                .padding(.vertical, 7)
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
        HStack(spacing: 12) {
            Image(systemName: "key.fill").foregroundStyle(.orange)
            VStack(alignment: .leading, spacing: 2) {
                Text(callout.title).font(.subheadline.weight(.semibold))
                Text(callout.message)
                    .font(.caption).foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            Button(callout.action, action: add)
                .buttonStyle(.borderedProminent)
                .controlSize(.small)
        }
        .card(tint: .orange)
        .padding(.horizontal, 4)
    }
}

/// The microphone's loudness as five bars.
private struct LevelMeter: View {
    let level: Float

    var body: some View {
        HStack(spacing: 2.5) {
            ForEach(0 ..< 5) { i in
                let weight: Float = [0.5, 0.8, 1, 0.8, 0.5][i]
                Capsule()
                    .fill(Color.accentColor)
                    .frame(width: 3, height: 6 + CGFloat(min(1, level * weight * 1.4)) * 16)
            }
        }
        .frame(height: 24)
        .animation(.easeOut(duration: 0.08), value: level)
        .accessibilityHidden(true)
    }
}
