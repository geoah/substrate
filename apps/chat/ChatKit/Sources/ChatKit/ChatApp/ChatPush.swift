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
    public static let budget: Duration = .seconds(20)
    /// How much of a reply a notification shows: the server's preview
    /// length, cut before any Markdown is read.
    public static let previewLimit = 240
    /// The `userInfo` key a push's response carries the session it was
    /// accepted for under (`Session.identity`): its action is held to it.
    static let sessionKey = "push.session"

    /// The content a push shows.
    ///
    /// - Parameters:
    ///   - verdict: whether the push's generation is the install's
    ///   (`PushRegistrar.verdict`). One meant for an earlier registration
    ///   fetches nothing, offers nothing and drops its badge; one the
    ///   Keychain could not be read for (before the first unlock) fetches
    ///   nothing and keeps its badge.
    ///   - client: the app's own client; nil where the install holds no
    ///     session.
    ///   - deadline: the whole operation's, on the monotonic clock: what is
    ///     still out then is cancelled and the fallback delivered.
    public static func content(
        _ userInfo: [String: String], verdict: PushVerdict, client: SubstrateClient?, until deadline: ContinuousClock.Instant
    ) async -> PushContent {
        guard let push = PushPayload(userInfo), push.event != .read else { return .fallback(dropsBadge: false) }
        switch verdict {
        case .refused: return .fallback(dropsBadge: true)
        case .unknown: return .fallback(dropsBadge: false)
        case .accepted: break
        }
        guard var client, !push.eventID.isEmpty else { return .fallback(dropsBadge: false) }
        // A busy server is not waited out: there is no time for it.
        client.pause = { _ in throw CancellationError() }
        let bounded = client
        return await within(deadline, or: .fallback(dropsBadge: false)) { await resolve(push, client: bounded, until: deadline) }
    }

    /// `work`, raced against `deadline`: what is still out then is cancelled,
    /// and `fallback` answered.
    static func within<T: Sendable>(_ deadline: ContinuousClock.Instant, or fallback: T, _ work: @escaping @Sendable () async -> T) async -> T {
        await withTaskGroup(of: T?.self) { group in
            group.addTask { await work() }
            group.addTask {
                try? await Task.sleep(until: deadline, clock: .continuous)
                return nil
            }
            let first = await group.next() ?? nil
            group.cancelAll()
            return first ?? fallback
        }
    }

    /// The seconds left until `deadline`, for a request's own timeout; nil
    /// where too little is left to ask.
    static func left(_ deadline: ContinuousClock.Instant) -> TimeInterval? {
        let seconds = Double((deadline - .now).components.seconds) + Double((deadline - .now).components.attoseconds) / 1e18
        return seconds > 0.5 ? seconds : nil
    }

    private static func resolve(_ push: PushPayload, client: SubstrateClient, until deadline: ContinuousClock.Instant) async -> PushContent {
        do {
            guard let time = left(deadline), let info = try await client.pushEvent(push.eventID, timeout: time), info.thread == push.thread,
                let item = info.item
            else { return .fallback(dropsBadge: false) }
            func read(_ kind: String, _ id: String) async throws -> Record? {
                guard let time = left(deadline) else { throw CancellationError() }
                return try await client.record(kind, id, timeout: time)
            }
            switch info.event {
            case "reply":
                guard item.kind == Kinds.message, let message = try await read(item.kind, item.id), message.version == item.version,
                    let row = try await read(Kinds.thread, info.thread)
                else { return .fallback(dropsBadge: false) }
                let thread = ChatThread(record: row)
                let agent = agentName(thread.agentID ?? "")
                var request = ChatNotifications.reply(thread: thread, agent: agent, title: thread.title(opening: nil), replies: true)
                // The reply this push is about, cut to a preview before any
                // Markdown is read; never whatever the thread says last.
                request.body = preview(message["content"]?.text ?? "") ?? "\(agent) replied."
                return .shown(request)
            case "failed":
                guard item.kind == Kinds.thread, let row = try await read(Kinds.thread, info.thread) else { return .fallback(dropsBadge: false) }
                let thread = ChatThread(record: row)
                // Later writes move the thread's version: the failure stands
                // while its status does.
                guard ["error", "overbudget"].contains(thread.status) else { return .fallback(dropsBadge: false) }
                let agent = agentName(thread.agentID ?? "")
                var request = ChatNotifications.reply(thread: thread, agent: agent, title: thread.title(opening: nil), replies: true)
                request.body = failureWords(thread, agent: agent, host: client.server) ?? "\(agent) couldn't finish this reply."
                return .shown(request)
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

    /// The start of a reply as a notification shows it: at most
    /// `previewLimit` characters of the stored text, cut before its Markdown
    /// is read; nil where nothing in it reads as text.
    static func preview(_ markdown: String) -> String? {
        let cut = String(String.UnicodeScalarView(markdown.unicodeScalars.prefix(previewLimit)))
        let said = plainText(markdown: cut).trimmingCharacters(in: .whitespacesAndNewlines)
        return said.isEmpty ? nil : said
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
    /// row holds alone: an error's, and a run over its budget's.
    static func failureWords(_ thread: ChatThread, agent: String, host: URL) -> String? {
        guard ["error", "overbudget"].contains(thread.status), let failure = thread.runFailure else { return nil }
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
    /// earlier registration, or for another thread than its resolution
    /// says, removes nothing. Everything happens before `deadline` or not
    /// at all, with no request repeated: iOS gives the app seconds for it.
    /// Answers whether anything was read.
    public static func takeBack(
        _ userInfo: [String: String], accepted: Bool, client: SubstrateClient?, notifications: any Notifications,
        until deadline: ContinuousClock.Instant
    ) async -> Bool {
        guard let push = PushPayload(userInfo), push.event == .read, accepted, var client, !push.eventID.isEmpty else { return false }
        client.pause = { _ in throw CancellationError() }
        let bounded = client
        return await within(deadline, or: false) {
            guard let time = left(deadline), let info = try? await bounded.pushEvent(push.eventID, timeout: time), info.event == "read",
                info.thread == push.thread, let covers = info.covers
            else { return false }
            let alerts = Set(covers.alerts)
            for shown in await notifications.delivered() {
                guard let eventID = shown.push?.eventID, !eventID.isEmpty, alerts.contains(PushFingerprint.of(eventID: eventID)) else { continue }
                await notifications.cancel(shown.id)
            }
            if let time = left(deadline),
                let badge = try? await bounded.send("GET", "api/v1/push/badge", timeout: time, retries: 0)
                    .decode(PushBadge.self).badge
            {
                await notifications.setBadge(min(max(badge, 0), 999))
            }
            return true
        }
    }
}
