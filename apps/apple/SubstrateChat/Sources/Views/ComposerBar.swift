import SwiftUI

/// Where you write to the agent: a floating glass bar over the transcript,
/// in one container with Jump to Latest so the two merge rather than stack.
/// The trailing button is the microphone while the field is empty, Send once
/// it is not, and Stop while a turn runs; it morphs between them.
struct ComposerBar: View {
    @Environment(ChatStore.self) private var store
    @Bindable var conversation: Conversation
    let dictation: Dictation
    let atBottom: Bool
    let onJump: () -> Void
    let onKey: (Provider) -> Void
    @FocusState private var focused: Bool
    @Namespace private var glass
    @State private var sent = 0

    private var empty: Bool { conversation.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    var body: some View {
        VStack(spacing: 8) {
            if let provider = store.keyless(conversation.agent) {
                KeylessCallout(provider: provider) { onKey(provider) }
            }
            if let reason = conversation.blockedReason, !empty || reason == "Offline",
               store.keyless(conversation.agent) == nil
            {
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
            .animation(.snappy, value: conversation.streaming)
            .animation(.snappy, value: dictation.active)
        }
        .padding(.horizontal, 12)
        .padding(.bottom, 8)
        .frame(maxWidth: 820)
        .frame(maxWidth: .infinity)
        .sensoryFeedback(.impact(weight: .light), trigger: sent)
        .task {
            if conversation.listenOnOpen {
                conversation.listenOnOpen = false
                await dictation.start(localeID: store.prefs.voiceLocale)
            } else if conversation.threadID == nil {
                focused = true
            }
        }
    }

    // MARK: The field

    @ViewBuilder private var field: some View {
        HStack(alignment: .bottom, spacing: 8) {
            if conversation.threadID == nil {
                AgentChip(conversation: conversation)
            }
            if dictation.active {
                listening
            } else {
                TextField(placeholder, text: $conversation.draft, axis: .vertical)
                    .textFieldStyle(.plain)
                    .lineLimit(1 ... 8)
                    .focused($focused)
                    .padding(.vertical, 11)
                    #if os(macOS)
                    .onKeyPress(.return, phases: .down) { press in
                        if press.modifiers.contains(.option) { return .ignored }
                        if press.modifiers.contains(.shift) {
                            conversation.draft += "\n"
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
        .padding(.leading, conversation.threadID == nil ? 6 : 16)
        .padding(.trailing, 8)
        .frame(minHeight: 46)
    }

    private var placeholder: String {
        conversation.agent.map { "Message \($0.name)" } ?? "Message"
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
        if conversation.streaming {
            round("stop.fill", tint: .red, label: conversation.stopping ? "Stopping" : "Stop") { conversation.stop() }
                .disabled(conversation.stopping)
        } else if dictation.active {
            round("checkmark", tint: .accentColor, label: "Done Listening") {
                Task { await toggleListening() }
            }
        } else if empty {
            round("mic.fill", tint: nil, label: "Dictate") { Task { await toggleListening() } }
                .disabled(conversation.agentID == nil)
        } else {
            round("arrow.up", tint: .accentColor, label: "Send") { send() }
                .disabled(conversation.blockedReason != nil)
                .keyboardShortcut(.return, modifiers: .command)
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

    private func send() {
        Task {
            if dictation.active { await toggleListening() }
            guard conversation.blockedReason == nil, !empty else { return }
            conversation.send()
            sent += 1
            if !store.prefs.askedNotifications {
                store.prefs.askedNotifications = true
                _ = await Notifier.requestAuthorization()
            }
        }
    }

    private func toggleListening() async {
        if dictation.active {
            let heard = await dictation.stop()
            conversation.draft = joined(conversation.draft, heard)
            focused = true
        } else {
            focused = false
            await dictation.start(localeID: store.prefs.voiceLocale)
        }
    }
}

/// The agent a new chat goes to, chosen from the offered ones.
private struct AgentChip: View {
    @Environment(ChatStore.self) private var store
    @Bindable var conversation: Conversation

    var body: some View {
        Menu {
            ForEach(store.offered) { agent in
                Button {
                    conversation.agentID = agent.id
                } label: {
                    if agent.id == conversation.agentID {
                        Label(agent.name, systemImage: "checkmark")
                    } else {
                        Text(agent.name)
                    }
                }
            }
        } label: {
            HStack(spacing: 4) {
                AgentAvatar(id: conversation.agentID, size: 28)
                Image(systemName: "chevron.down")
                    .font(.caption2.weight(.bold))
                    .foregroundStyle(.secondary)
            }
            .padding(.vertical, 9)
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .fixedSize()
        .accessibilityLabel("Assistant: \(conversation.agentID.map(agentName) ?? "none")")
    }
}

/// An agent whose provider has no key cannot answer: said before anybody
/// types.
private struct KeylessCallout: View {
    let provider: Provider
    let add: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: "key.fill").foregroundStyle(.orange)
            VStack(alignment: .leading, spacing: 2) {
                Text("Add an API key for \(provider.name)").font(.subheadline.weight(.semibold))
                Text("Agents run on your own key, sealed on the provider's record.")
                    .font(.caption).foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            Button("Add Key", action: add)
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
