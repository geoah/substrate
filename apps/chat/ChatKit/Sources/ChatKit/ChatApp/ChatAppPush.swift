import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime

/// Pushes from the publisher's relay, as the list keeps them: where the
/// server lists `push`, the install registers (`PushRegistrar`, run by the
/// `syncPush` effect at launch, at each background refresh and when a
/// switch or the permission changes), the badge is the server's count, and
/// what a push alerted is taken back once the reads say it is seen. While a
/// registration is held the app posts no reply, failure or request
/// notification of its own: the push says it (`announce`, `post(card:)`).
extension ChatAppState {
    /// The server lists `push`: the badge is its count, and the install may
    /// register.
    var pushListed: Bool { (pushRevision ?? 0) >= 1 }

    /// A registration is held: the server may push to this install.
    var pushHeld: Bool { push?.held == true }

    /// The threads whose reply or failure a push alerted for that the
    /// person has read, here or elsewhere: their mark is known and reads
    /// through the newest message, or they archived the chat and nothing
    /// in it waits on them.
    var pushRead: [String] {
        (held + Array(others.values)).map(ChatThread.init(record:)).filter { thread in
            guard !thread.running, marked(thread.id) else { return false }
            if archived(thread.id), !waitsOnPerson(thread.id, row: thread) { return true }
            return !unread(thread) && !failedUnseen(thread)
        }.map(\.id).sorted()
    }

    /// The cards that still wait, by thread, of the threads whose cards were
    /// read with the lists (`cardThreads`): a request's alert for any other
    /// card of one of them was decided.
    var pushPending: [String: [String]] {
        var out: [String: [String]] = [:]
        for thread in cardThreads {
            out[thread] = (interactions + requests + approvals)
                .filter { $0["thread"]?.referenceID == thread && ChatApp.waits($0) }.map(\.id).sorted()
        }
        return out
    }

    /// The threads in which nothing waits on the person any more: a
    /// request a push alerted for in one of them was decided.
    var pushSettled: [String] {
        guard readsMarks else { return [] }
        return (held + Array(others.values)).map(ChatThread.init(record:)).filter { !mayWait($0.id) }.map(\.id).sorted()
    }
}

extension ChatPreferences {
    /// The events this install wants pushed, by its two switches: Replies
    /// (a reply or a failure) and Requests. None is both off.
    var pushNotifies: [String] { (notifyReplies ? ["replies"] : []) + (notifyAttention ? ["requests"] : []) }
}

extension ChatApp {
    /// What sets the badge: the server's count where it lists `push`, so
    /// the screen shows what a push sets; else the app's own. While a mark
    /// this device moved is unsent or on its way the server may not count
    /// it yet: the lower of the two is shown, and the server is asked again
    /// once the write is answered (`markWritten`).
    func badge(_ state: State) -> ChatAppAsk {
        guard state.pushListed else { return .badge(state.unreadCount) }
        let moving = !state.marking.isEmpty || !state.markSending.isEmpty
        return .serverBadge(local: state.unreadCount, ceiling: moving ? state.unreadCount : nil)
    }

    func reconcile(_ state: State) -> ChatAppAsk {
        .reconcilePush(read: state.pushRead, settled: state.pushSettled, pending: state.pushPending)
    }

    /// The events that answer the lists from the server: what a reconcile
    /// reads its marks and rows from.
    static func answeredLists(_ event: Event) -> Bool {
        switch event.step {
        case .refreshed, .backgroundRefreshed(_, .some, _): true
        default: false
        }
    }

    /// Asks to register, or to give a registration up, once the server's
    /// document said whether it lists `push`.
    func syncPush(_ state: inout State) {
        guard state.session != nil, state.pushRevision != nil else { return }
        state.asks.append(.syncPush(listed: state.pushListed, notifies: state.preferences.pushNotifies, tokenID: state.session?.tokenID))
    }

    /// The thread a tapped notification opens: a local one's, or a push's
    /// whose generation `accepted` says is this install's current or pending
    /// one. The response goes on with it at the top of its `userInfo`, and
    /// without it where it opens no chat.
    static func opened(_ response: NotificationResponse, accepted: Bool) -> NotificationResponse {
        var out = response
        out.userInfo[ChatNotifications.threadKey] = PushPayload.thread(
            of: response.userInfo, key: ChatNotifications.threadKey, accepted: { _ in accepted })
        return out
    }

    /// Takes back what a push alerted that the reads say is seen. A
    /// request's alert that names its card (the extension writes it) goes
    /// as soon as that card no longer waits, though another in the chat
    /// still does; one that names none goes once nothing in its chat waits.
    static func reconcile(_ notifications: any Notifications, read: Set<String>, settled: Set<String>, pending: [String: [String]] = [:]) async {
        for shown in await notifications.delivered() {
            guard let push = shown.push else { continue }
            switch push.event {
            case .reply, .failed: if read.contains(push.thread) { await notifications.cancel(shown.id) }
            case .request:
                if settled.contains(push.thread) {
                    await notifications.cancel(shown.id)
                } else if let card = shown.userInfo[ChatNotifications.cardKey], let waiting = pending[push.thread], !waiting.contains(card) {
                    await notifications.cancel(shown.id)
                }
            case .read: await notifications.cancel(shown.id)
            }
        }
    }

    /// The threads of the pushed notifications standing on the device whose
    /// generation `accepted` says is this sign-in's.
    static func deliveredThreads(_ notifications: any Notifications, accepted: (String) async -> Bool) async -> [String] {
        var threads: Set<String> = []
        for shown in await notifications.delivered() {
            guard let push = shown.push, !threads.contains(push.thread), await accepted(push.generation) else { continue }
            threads.insert(push.thread)
        }
        return threads.sorted()
    }
}
