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
    /// the screen shows what a push sets; else the app's own.
    func badge(_ state: State) -> ChatAppAsk {
        state.pushListed ? .serverBadge(fallback: state.unreadCount) : .badge(state.unreadCount)
    }

    func reconcile(_ state: State) -> ChatAppAsk {
        .reconcilePush(read: state.pushRead, settled: state.pushSettled)
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
        state.asks.append(.syncPush(listed: state.pushListed, notifies: state.preferences.pushNotifies))
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

    /// Takes back what a push alerted that the reads say is seen.
    static func reconcile(_ notifications: any Notifications, read: Set<String>, settled: Set<String>) async {
        for shown in await notifications.delivered() {
            guard let push = shown.push else { continue }
            switch push.event {
            case .reply, .failed: if read.contains(push.thread) { await notifications.cancel(shown.id) }
            case .request: if settled.contains(push.thread) { await notifications.cancel(shown.id) }
            case .read: await notifications.cancel(shown.id)
            }
        }
    }
}
