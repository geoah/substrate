import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice

/// What a push from the relay shows, as the notification service extension
/// makes it: the relay sends no words, so the extension asks the person's
/// own server what the push is about, with the app's own token, and shows
/// it as the app's local notification of the same item would.
public enum PushContent: Equatable, Sendable {
    /// The words the push came with, the app's own (`loc-key`): nothing
    /// was fetched and no action is offered. `dropsBadge` where the push
    /// was meant for another registration of this install, whose badge
    /// says another sign-in's count.
    case fallback(dropsBadge: Bool)
    /// What the item says, as a local notification of it says it: its
    /// title, words, thread, category and `userInfo` (`ChatNotifications`).
    case shown(NotificationRequest)
}

/// The extension's work and the app's handling of a `read` push, below any
/// UI framework so they are tested like the rest of the chat.
public enum ChatPush {
    /// How long the extension spends on all its reads together: iOS gives
    /// it about 30 seconds, and what is left goes to delivering.
    public static let budget: TimeInterval = 20

    /// The content a push shows.
    ///
    /// - Parameters:
    ///   - accepted: the push's generation is the install's current or
    ///     pending one (`PushRegistrar.accepts`). One meant for an earlier
    ///     registration fetches nothing, offers nothing and drops its badge.
    ///   - client: the app's own client; nil where the install holds no
    ///     session.
    ///   - deadline: every read gets what is left until then.
    public static func content(
        _ userInfo: [String: String], accepted: Bool, client: SubstrateClient?, until deadline: Date,
        now: @escaping @Sendable () -> Date = { Date() }
    ) async -> PushContent {
        guard let push = PushPayload(userInfo), push.event != .read else { return .fallback(dropsBadge: false) }
        guard accepted else { return .fallback(dropsBadge: true) }
        guard var client, !push.eventID.isEmpty else { return .fallback(dropsBadge: false) }
        // A busy server is not waited out: there is no time for it.
        client.pause = { _ in throw CancellationError() }
        func left() -> TimeInterval? {
            let seconds = deadline.timeIntervalSince(now())
            return seconds > 0.5 ? seconds : nil
        }
        do {
            guard let time = left(), let info = try await client.pushEvent(push.eventID, timeout: time), info.thread == push.thread,
                let item = info.item
            else { return .fallback(dropsBadge: false) }
            func read(_ kind: String, _ id: String) async throws -> Record? {
                guard let time = left() else { throw CancellationError() }
                return try await client.record(kind, id, timeout: time)
            }
            switch info.event {
            case "reply":
                guard item.kind == Kinds.message, let message = try await read(item.kind, item.id), message.version == item.version,
                    let row = try await read(Kinds.thread, info.thread)
                else { return .fallback(dropsBadge: false) }
                let thread = ChatThread(record: row)
                var request = ChatNotifications.reply(
                    thread: thread, agent: agentName(thread.agentID ?? ""), title: thread.title(opening: nil), replies: true)
                // The reply this push is about, not whatever the thread
                // says last.
                let said = plainText(markdown: message["content"]?.text ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                if !said.isEmpty { request.body = said }
                return .shown(request)
            case "failed":
                guard item.kind == Kinds.thread, let row = try await read(Kinds.thread, info.thread) else { return .fallback(dropsBadge: false) }
                let thread = ChatThread(record: row)
                // Later writes move the thread's version: the failure stands
                // while its status does.
                guard ["error", "overbudget"].contains(thread.status) else { return .fallback(dropsBadge: false) }
                let agent = agentName(thread.agentID ?? "")
                return .shown(
                    ChatNotifications.reply(
                        thread: thread, agent: agent, title: thread.title(opening: nil),
                        failed: failureWords(thread, agent: agent, host: client.server), replies: true))
            case "request":
                guard [Kinds.interaction, Kinds.approval, Kinds.request].contains(item.kind), let record = try await read(item.kind, item.id),
                    record.version == item.version
                else { return .fallback(dropsBadge: false) }
                let row = try? await read(Kinds.thread, info.thread)
                return .shown(card(record, thread: info.thread, row: row.map(ChatThread.init(record:)), actions: info.actions == true))
            default:
                return .fallback(dropsBadge: false)
            }
        } catch {
            return .fallback(dropsBadge: false)
        }
    }

    /// A card's notification, as the app posts one: the same words and
    /// `userInfo`, with the record's version in it, and the actions only
    /// this item allows, and only while the server says it is still
    /// pending now (`actions`): an approval Approve and Decline; a question
    /// only Open Chat, since its options are a category the app registers
    /// at run time and an extension's push may arrive before it is; a
    /// suggestion none, and is opened to decide.
    static func card(_ record: Record, thread: String, row: ChatThread?, actions: Bool) -> NotificationRequest {
        let asker = record.kind == Kinds.approval ? referent(record["agent"], pin: Kinds.agent) : nil
        let who = agentName(asker ?? row?.agentID ?? record["proposer"]?.string ?? "Your agent")
        var request = ChatNotifications.attention(record: record, thread: thread, agent: who, title: row?.title(opening: nil) ?? "A chat")
        let pending = actions && ChatApp.waits(record)
        switch record.kind {
        case Kinds.approval:
            if !pending { request.category = nil }
        case Kinds.interaction:
            request.category = pending ? ChatNotifications.openCategory : nil
            request.userInfo[ChatNotifications.questionKey] = nil
            request.userInfo = request.userInfo.filter { !$0.key.hasPrefix("option.") }
        default:
            request.category = nil
        }
        return request
    }

    /// What a failed run's card says, in one sentence, from what the thread
    /// row holds alone.
    static func failureWords(_ thread: ChatThread, agent: String, host: URL) -> String? {
        guard thread.status == "error", let failure = thread.runFailure else { return nil }
        var context = FailurePresenter.Context()
        context.agent = agent
        context.agentID = thread.agentID
        context.host = ServerAddress.display(host)
        context.thread = thread.id
        return FailurePresenter.present(failure, context).spoken
    }

    // MARK: Taking notifications back

    /// A `read` push iOS ran the app for: what it covers is read from the
    /// server, and exactly the delivered notifications whose event ids it
    /// names (by fingerprint, `PushFingerprint`) are taken back, a fallback
    /// one among them; then the badge is the server's. One meant for an
    /// earlier registration removes nothing. Answers whether anything was
    /// read.
    public static func takeBack(
        _ userInfo: [String: String], accepted: Bool, client: SubstrateClient?, notifications: any Notifications
    ) async -> Bool {
        guard let push = PushPayload(userInfo), push.event == .read, accepted, let client, !push.eventID.isEmpty,
            let info = try? await client.pushEvent(push.eventID), info.event == "read", let covers = info.covers
        else { return false }
        let alerts = Set(covers.alerts)
        for shown in await notifications.delivered() {
            guard let eventID = shown.push?.eventID, !eventID.isEmpty, alerts.contains(PushFingerprint.of(eventID: eventID)) else { continue }
            await notifications.cancel(shown.id)
        }
        if let badge = try? await client.pushBadge() { await notifications.setBadge(badge) }
        return true
    }
}
