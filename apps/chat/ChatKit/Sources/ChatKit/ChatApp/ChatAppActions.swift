import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime

/// Answering from a notification. An action of one of the app's
/// notifications (Reply with a line of text, Approve or Decline, an option
/// of a single question) is answered in the background through the paths
/// the app uses in front: a reply is its thread's conversation sending a
/// message under an id of its own, kept in the folder before the request
/// leaves and sent again if its answer is lost; a card's is the write that
/// resolves it, over the version its notification showed.
///
/// Each action has `actionSeconds`, which is inside what the system gives a
/// background action, and ends in one of two ways. Done: its notification
/// is taken back. Not done: a notification says so in the sentence the chat
/// says it in, and opening it opens the chat, where the reply is kept and
/// the card still stands. Only then is the device told the action is over
/// (`Notifications.finished`), since it may suspend the app at that word.
/// The same response heard twice acts once.
extension ChatApp {
    /// How long an action from a notification may take.
    static let actionSeconds: Double = 20

    /// An action arrived. Before the folder is read there is no chat to
    /// act in, and before the server's document is read a reply could not
    /// be sent under an id: the action waits for both, inside its time.
    ///
    /// - Parameter waited: the action is one that waited, heard again now:
    ///   its time has been running since it first arrived.
    func acted(_ state: inout State, _ response: NotificationResponse, key: ChildKey, message: String, at: Date, waited: Bool = false) {
        let stamp = ChatNotifications.stamp(response)
        // Heard again while it is under way: the one under way answers for
        // both, and the device is told once it is over.
        if !waited, state.acting[stamp] != nil { return }
        guard state.session != nil, !state.accessEnded, let thread = response.userInfo[ChatNotifications.threadKey],
            case let .named(name) = response.action, let action = ChatNotifications.Action(name), !state.acted.contains(stamp)
        else {
            // Nothing to do, or done already: the device is told at once.
            state.asks.append(.finishAction(response, cancel: nil, notify: nil))
            return
        }
        if !waited {
            guard !state.deferred.contains(where: { ChatNotifications.stamp($0.response) == stamp }) else { return }
            state.asks.append(.actionWait(stamp, seconds: Self.actionSeconds))
        }
        // A reply is its chat's conversation sending, to the agent the
        // thread's row names, one reply at a time: a second into the same
        // chat goes when the first has, since the conversation is handed
        // one and would hold the later of two.
        let rowKnown = state.thread(thread) != nil
        let replying = state.key(of: thread).flatMap { state.conversations[$0]?.reply } != nil
        guard state.opened, action != .reply || (state.agentsRevision != nil && rowKnown && !replying) else {
            if !waited { state.deferred.append(State.Deferred(response: response, key: key, message: message, at: at)) }
            // A launch for the action may hold no row of the chat (one
            // past the list's first page, or archived on another device):
            // it is read by name, and the reply goes when it is.
            if action == .reply, state.opened, state.agentsRevision != nil, !rowKnown { state.asks.append(.fetchThread(thread)) }
            return
        }
        state.acted.insert(stamp)
        state.deferred.removeAll { ChatNotifications.stamp($0.response) == stamp }
        var act = State.Act(response: response, thread: thread)
        switch action {
        case .open:
            state.route = .thread(thread)
            state.asks += [.cancel("actWait:\(stamp)"), .finishAction(response, cancel: nil, notify: nil)]
            return
        case .reply:
            let text = (response.text ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            guard !text.isEmpty else {
                state.asks += [.cancel("actWait:\(stamp)"), .finishAction(response, cancel: nil, notify: nil)]
                return
            }
            // Rule 1: the thread's one conversation sends it, made here
            // where no window has shown the chat since the launch.
            let child = state.key(of: thread) ?? key
            if state.conversations[child] == nil { converse(&state, thread, key: child) }
            state.conversations[child]?.reply = Conversation.Reply(id: message, text: text, at: at)
            act.reply = child
        case .approve, .decline, .option:
            guard let id = response.userInfo[ChatNotifications.cardKey], let version = response.userInfo[ChatNotifications.versionKey].flatMap(Int.init)
            else {
                state.asks += [.cancel("actWait:\(stamp)"), .finishAction(response, cancel: nil, notify: nil)]
                return
            }
            var reference = CardReference.approval(id)
            var properties: [String: JSONValue] = ["state": .string(action == .approve ? "approved" : "denied")]
            var busy = action == .approve ? "approve" : "decline"
            if case let .option(index) = action {
                guard let question = response.userInfo[ChatNotifications.questionKey], let value = response.userInfo[ChatNotifications.optionKey(index)]
                else {
                    state.asks += [.cancel("actWait:\(stamp)"), .finishAction(response, cancel: nil, notify: nil)]
                    return
                }
                reference = .question(id)
                properties = ["state": "answered", "answers": [["question": .string(question), "selected": [.string(value)]]]]
                busy = "answer"
            }
            act.card = reference
            guard state.cards[id]?.busy == nil else {
                // A write of this card is already out, from the chat.
                state.acting[stamp] = act
                return finish(&state, stamp, failure: Failure(reference.isQuestion ? .answer : .decide, .interrupted))
            }
            // Over the version the notification showed, and no other: one
            // that changed since is refused by the server.
            let kind = reference.isQuestion ? Kinds.interaction : Kinds.approval
            write(&state, id, Record(id: id, kind: kind, properties: [:], version: version), properties, as: busy)
        }
        state.acting[stamp] = act
    }

    /// The folder is read, or the server's document, or a thread's row, or
    /// a reply before it in its chat has gone: the actions that waited for
    /// any of them go now.
    func resume(_ state: inout State) {
        guard state.opened else { return }
        // Each asks again what it waits for; one that still waits stays.
        for waiting in state.deferred {
            acted(&state, waiting.response, key: waiting.key, message: waiting.message, at: waiting.at, waited: true)
        }
    }

    private static func isReply(_ response: NotificationResponse) -> Bool {
        if case let .named(name) = response.action { return ChatNotifications.Action(name) == .reply }
        return false
    }

    /// A conversation for a thread no window shows: what `show` makes,
    /// with no scene.
    private func converse(_ state: inout State, _ thread: String, key: ChildKey) {
        let row = state.thread(thread)
        let unsent = state.unsent[thread]
        state.conversations[key] = State.Entry(
            seed: Conversation.Seed(thread: thread, agent: row?.agentID, draft: state.drafts[thread] ?? "", listen: false, unsent: unsent),
            thread: thread, agent: row?.agentID, unsent: unsent?.id)
        if row == nil { state.asks.append(.fetchThread(thread)) }
    }

    /// The action's time is up. A reply stops being sent and stands in its
    /// chat, to send again from there; a card's write is let go, and the
    /// card is read for what became of it. Either way the person is told.
    func actionDue(_ state: inout State, _ stamp: String) {
        if let waiting = state.deferred.first(where: { ChatNotifications.stamp($0.response) == stamp }) {
            // Never acted on: the folder or the server's document did not
            // come in time. A reply's text is kept as its chat's draft.
            // Before the folder is read there is nowhere to write it: it
            // is held for the draft, and the notification says the text
            // itself, since the app may not be running by the time the
            // folder would have been read.
            state.deferred.removeAll { ChatNotifications.stamp($0.response) == stamp }
            state.acted.insert(stamp)
            guard let thread = waiting.response.userInfo[ChatNotifications.threadKey] else {
                state.asks.append(.finishAction(waiting.response, cancel: nil, notify: nil))
                return
            }
            var unsaved: String?
            if Self.isReply(waiting.response), let text = waiting.response.text?.trimmingCharacters(in: .whitespacesAndNewlines), !text.isEmpty {
                if state.opened {
                    state.drafts[thread] = Conversation.above(text, state.drafts[thread] ?? "")
                } else {
                    state.unkept[thread] = Conversation.above(text, state.unkept[thread] ?? "")
                    unsaved = text
                }
            }
            state.acting[stamp] = State.Act(response: waiting.response, thread: thread)
            return finish(&state, stamp, failure: Failure(Self.isReply(waiting.response) ? .send : .decide, .timeout), unsaved: unsaved)
        }
        guard let act = state.acting[stamp] else { return }
        if let key = act.reply {
            // The conversation stands the message, and says so; the
            // person is told now, whatever it then reports.
            state.conversations[key]?.reply?.expired = true
            finish(&state, stamp, failure: Failure(.message, .timeout), keepsReply: true)
        } else if let card = act.card {
            state.asks.append(.cancel("write:\(card.id)"))
            state.cards[card.id]?.busy = nil
            finish(&state, stamp, failure: Failure(card.isQuestion ? .answer : .decide, .timeout))
            state.asks.append(.readCard(card, target: false))
        }
    }

    /// What a conversation says became of a reply typed into a
    /// notification.
    func replied(_ state: inout State, _ key: ChildKey, _ outcome: Conversation.ReplyOutcome) {
        if state.conversations[key]?.reply?.id == outcome.id { state.conversations[key]?.reply = nil }
        // The next reply into this chat, where one waited for this one.
        defer { if !state.deferred.isEmpty { resume(&state) } }
        if let stamp = state.acting.first(where: { $0.value.reply == key })?.key {
            finish(&state, stamp, failure: outcome.failure)
        } else if outcome.failure == nil, let thread = state.conversations[key]?.thread {
            // It arrived after the app said it had not: that is taken back.
            state.asks.append(.cancelNotification(Self.failedID(thread)))
            state.failedActions.remove(thread)
        }
    }

    /// A card's write was answered: where a notification asked for it, the
    /// action is over.
    func decided(_ state: inout State, card id: String, failure: Failure?) {
        guard let stamp = state.acting.first(where: { $0.value.card?.id == id })?.key else { return }
        finish(&state, stamp, failure: failure)
    }

    static func failedID(_ thread: String) -> String { ChatNotifications.prefix(thread) + "failed" }

    /// The action is over. Done, its notification is taken back; not done,
    /// a notification says so. Then the device is told, once the drafts
    /// are in the folder where the action did not go through: a reply's
    /// text may have gone back into them.
    ///
    /// - Parameter unsaved: a reply's text that is in no folder, which the
    ///   notification then says whole.
    private func finish(_ state: inout State, _ stamp: String, failure: Failure?, keepsReply: Bool = false, unsaved: String? = nil) {
        guard let act = state.acting.removeValue(forKey: stamp) else { return }
        state.asks.append(.cancel("actWait:\(stamp)"))
        if let key = act.reply, !keepsReply { state.conversations[key]?.reply = nil }
        var said: NotificationRequest?
        if let failure {
            let row = state.thread(act.thread)
            let words = FailurePresenter.present(failure, failureContext(state, agent: row?.agentID, thread: act.thread))
            said = ChatNotifications.failed(
                thread: act.thread, agent: agentName(row?.agentID ?? ""), title: row.map(state.title) ?? "A chat", sentence: words.sentence,
                kept: unsaved.map { "Your reply was not sent or saved. You wrote: “\($0)”" }
                    ?? (act.card == nil ? "Your reply is kept in the chat." : "Open the chat to answer it there."))
        }
        // A card's failure is said only in a chat that may be notified
        // for, as the card's own was; a reply's is the person's own words
        // not sent, which wait on them wherever they wrote them.
        if said != nil {
            if state.notifies(act.thread, waits: act.card == nil) { noteFailed(&state, act.thread) } else { said = nil }
        }
        let keep = failure == nil ? nil : state.disk.map { KeptDrafts(lease: $0, drafts: state.drafts) }
        state.asks.append(.finishAction(act.response, cancel: failure == nil ? act.response.id : nil, notify: said, keep: keep))
    }
}

extension CardReference {
    var isQuestion: Bool {
        if case .question = self { return true }
        return false
    }
}
