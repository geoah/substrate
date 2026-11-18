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

    /// How a push's generation is checked: `PushRegistrar.acceptance(of:)`,
    /// one read of the session and of the state, whose client is the only one
    /// a push's requests go with.
    public typealias Accepting = @Sendable (String) async -> PushAcceptance

    /// The content a push shows.
    ///
    /// - Parameters:
    ///   - accepting: whether the push's generation is the install's, and
    ///     the client of the session that accepted it. One meant for an
    ///     earlier registration fetches nothing, offers nothing and drops its
    ///     badge; one the Keychain could not be read for (before the first
    ///     unlock) fetches nothing and keeps its badge.
    ///   - deadline: the whole operation's, on the monotonic clock, the
    ///     Keychain's reads included: the fallback is answered then, and what
    ///     is still out is cancelled and never waited for.
    public static func content(
        _ userInfo: [String: String], accepting: @escaping Accepting, until deadline: ContinuousClock.Instant
    ) async -> PushContent {
        guard let push = PushPayload(userInfo), push.event != .read else { return .fallback(dropsBadge: false) }
        return await Deadline.race(until: deadline, or: .fallback(dropsBadge: false)) { race in
            let acceptance = await accepting(push.generation)
            switch acceptance.verdict {
            case .refused: return .fallback(dropsBadge: true)
            case .unknown: return .fallback(dropsBadge: false)
            case .accepted: break
            }
            guard var client = acceptance.client, !push.eventID.isEmpty else { return .fallback(dropsBadge: false) }
            // A busy server is not waited out: there is no time for it.
            client.pause = { _ in throw CancellationError() }
            let content = await resolve(push, client: client, race: race)
            guard case .shown = content else { return content }
            // Asked again just before it is shown: a sign-out while it was
            // read accepts the push no more, and then nothing read for it
            // is shown, and no badge either.
            guard race.live, await accepting(push.generation).verdict == .accepted else { return .fallback(dropsBadge: true) }
            return content
        }
    }

    /// The seconds a request may take, asked immediately before it is sent:
    /// nil where the race is decided already, or too little is left.
    static func left(_ race: Deadline.Race) -> TimeInterval? {
        guard race.live else { return nil }
        return left(race.deadline)
    }

    /// The seconds left until `deadline`, for a request's own timeout; nil
    /// where too little is left to ask, or the work is cancelled.
    static func left(_ deadline: ContinuousClock.Instant) -> TimeInterval? {
        guard !Task.isCancelled else { return nil }
        let seconds = Double((deadline - .now).components.seconds) + Double((deadline - .now).components.attoseconds) / 1e18
        return seconds > 0.5 ? seconds : nil
    }

    /// What the push is about, read with `client`, each request sent only
    /// while `race` is undecided.
    private static func resolve(_ push: PushPayload, client: SubstrateClient, race: Deadline.Race) async -> PushContent {
        do {
            guard let time = left(race), let info = try await client.pushEvent(push.eventID, timeout: time), info.thread == push.thread,
                let item = info.item
            else { return .fallback(dropsBadge: false) }
            func read(_ kind: String, _ id: String) async throws -> Record? {
                guard let time = left(race) else { throw CancellationError() }
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
    /// `previewLimit` characters of the stored text, cut between characters
    /// as a person reads them (never inside an emoji or an accented letter)
    /// and before its Markdown is read, with an ellipsis where it was cut;
    /// nil where nothing in it reads as text.
    static func preview(_ markdown: String) -> String? {
        let end = markdown.index(markdown.startIndex, offsetBy: previewLimit, limitedBy: markdown.endIndex) ?? markdown.endIndex
        let said = plainText(markdown: String(markdown[..<end])).trimmingCharacters(in: .whitespacesAndNewlines)
        guard !said.isEmpty else { return nil }
        return end < markdown.endIndex ? said + "…" : said
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
        _ userInfo: [String: String], accepting: @escaping Accepting, notifications: any Notifications, until deadline: ContinuousClock.Instant
    ) async -> Bool {
        guard let push = PushPayload(userInfo), push.event == .read, !push.eventID.isEmpty else { return false }
        return await Deadline.race(until: deadline, or: false) { race in
            let acceptance = await accepting(push.generation)
            guard acceptance.verdict == .accepted, var client = acceptance.client else { return false }
            client.pause = { _ in throw CancellationError() }
            // Each request and each call to the device below is made only
            // while the race is undecided, checked immediately before it: a
            // result that comes late acts on nothing.
            guard let time = left(race), let info = try? await client.pushEvent(push.eventID, timeout: time), info.event == "read",
                info.thread == push.thread, let covers = info.covers
            else { return false }
            let alerts = Set(covers.alerts)
            guard race.live else { return false }
            let delivered = await notifications.delivered()
            for shown in delivered {
                guard let eventID = shown.push?.eventID, !eventID.isEmpty, alerts.contains(PushFingerprint.of(eventID: eventID)) else { continue }
                guard race.live else { return false }
                await notifications.cancel(shown.id)
            }
            guard let time = left(race),
                let badge = try? await client.send("GET", "api/v1/push/badge", timeout: time, retries: 0).decode(PushBadge.self).badge
            else { return true }
            guard race.live else { return false }
            await notifications.setBadge(min(max(badge, 0), 999))
            return true
        }
    }
}
