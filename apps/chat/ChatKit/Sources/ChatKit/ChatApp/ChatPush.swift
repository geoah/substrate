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

/// What everything a push starts is held to: the push's generation and the
/// repository of the session that accepted it, as the responses relay
/// names it (`ChatPush.sessionKey`). Each request it makes goes with the
/// client of the session that accepts the push as the request leaves
/// (`client`), and nowhere once the sign-in held is another.
struct PushBinding: Codable, Hashable, Sendable {
    var generation: String
    var identity: String

    /// The binding a response carries; nil for a local notification's, or
    /// a push's this install did not accept.
    init?(_ userInfo: [String: String]) {
        guard PushPayload(userInfo) != nil, let generation = userInfo[PushPayload.Key.generation], !generation.isEmpty,
            let identity = userInfo[ChatPush.sessionKey]
        else { return nil }
        self.init(generation: generation, identity: identity)
    }

    init(generation: String, identity: String) {
        self.generation = generation
        self.identity = identity
    }

    /// The client of the session that accepts the push now, of the same
    /// repository; nil where none does.
    func client(_ push: PushRegistrar?) async -> SubstrateClient? {
        guard let acceptance = await push?.acceptance(of: generation), acceptance.identity == identity else { return nil }
        return acceptance.client
    }

    /// Whether the push is still accepted for the same repository: asked
    /// just before anything it led to is shown.
    func holds(_ push: PushRegistrar?) async -> Bool { await client(push) != nil }
}

/// A transport that asks before every request whether it may still be sent.
struct AcceptedTransport: HTTPTransport {
    let inner: any HTTPTransport
    let holds: @Sendable () async -> Bool

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        guard await holds() else { throw CancellationError() }
        return try await inner.data(for: request)
    }

    func lines(for request: URLRequest) async throws -> LineStream {
        guard await holds() else { throw CancellationError() }
        return try await inner.lines(for: request)
    }
}

/// A value one task keeps for another to read after it.
final class Kept<T: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var kept: T?

    func keep(_ value: T) { lock.withLock { kept = value } }
    var value: T? { lock.withLock { kept } }
}

extension Optional where Wrapped: Sendable {
    /// `transform` of the value, awaited; nil for none.
    func asyncFlatMap<T>(_ transform: (Wrapped) async -> T?) async -> T? {
        guard let self else { return nil }
        return await transform(self)
    }
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
        let first = Kept<PushAcceptance>()
        let content: PushContent = await Deadline.race(until: deadline, or: PushContent.fallback(dropsBadge: false)) { (race: Deadline.Race) -> PushContent in
            let acceptance = await accepting(push.generation)
            first.keep(acceptance)
            switch acceptance.verdict {
            case .refused: return .fallback(dropsBadge: true)
            case .unknown: return .fallback(dropsBadge: false)
            case .accepted: break
            }
            guard let accepted = acceptance.client, !push.eventID.isEmpty else { return .fallback(dropsBadge: false) }
            let client = checked(accepted) {
                guard race.live else { return false }
                return await ChatPush.holds(acceptance, push, accepting)
            }
            return await resolve(push, client: client, race: race)
        }
        return await checkedLast(content, push, first.value, accepting, until: deadline)
    }

    /// How long past the deadline the last check before a push shows may
    /// take. It is a Keychain read, answered at once unless the device has
    /// not been unlocked since it restarted, and iOS gives the extension
    /// some ten seconds more than the deadline.
    static let lastCheck: Duration = .seconds(2)

    /// What a push shows, asked about last, just before it is shown: one
    /// accepted once and accepted no more (a sign-out, another sign-in)
    /// shows nothing it read and no badge, a fallback that would keep its
    /// badge included. One never accepted (an unreadable Keychain) keeps the
    /// badge it came with while nothing says it is another sign-in's. The
    /// check is answered within `lastCheck` past the deadline or not at all:
    /// unanswered, a push accepted once may since have been signed out of,
    /// and shows nothing it read and no badge.
    static func checkedLast(
        _ content: PushContent, _ push: PushPayload, _ first: PushAcceptance?, _ accepting: @escaping Accepting,
        until deadline: ContinuousClock.Instant
    ) async -> PushContent {
        guard content != .fallback(dropsBadge: true) else { return content }
        let unanswered: PushAcceptance? = nil
        guard let now = await Deadline.race(until: deadline + lastCheck, or: unanswered, { await accepting(push.generation) }) else {
            return first?.verdict == .accepted ? .fallback(dropsBadge: true) : content
        }
        switch now.verdict {
        case .refused: return .fallback(dropsBadge: true)
        case .unknown: return first?.verdict == .accepted ? .fallback(dropsBadge: true) : content
        case .accepted:
            if let first, first.verdict == .accepted, first.identity != now.identity { return .fallback(dropsBadge: true) }
            return content
        }
    }

    /// Whether the push is still accepted as it was, for the same
    /// repository.
    static func holds(_ first: PushAcceptance, _ push: PushPayload, _ accepting: Accepting) async -> Bool {
        let now = await accepting(push.generation)
        return now.verdict == .accepted && now.identity == first.identity
    }

    /// The accepting session's client, asking `holds` before every request it
    /// sends: once the push is no longer accepted, nothing more is asked of
    /// the server. A busy server is not waited out: there is no time for it.
    static func checked(_ client: SubstrateClient, holds: @escaping @Sendable () async -> Bool) -> SubstrateClient {
        var checked = client
        checked.transport = AcceptedTransport(inner: client.transport, holds: holds)
        checked.pause = { _ in throw CancellationError() }
        return checked
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
            guard acceptance.verdict == .accepted, let accepted = acceptance.client else { return false }
            // Every request, and every call to the device, only while the race
            // is undecided and the push is still accepted for the same
            // repository, asked immediately before it: a sign-out or a result
            // that comes late acts on nothing.
            @Sendable func stillHolds() async -> Bool {
                guard race.live else { return false }
                return await ChatPush.holds(acceptance, push, accepting) && race.live
            }
            let client = checked(accepted, holds: stillHolds)
            guard let time = left(race), let info = try? await client.pushEvent(push.eventID, timeout: time), info.event == "read",
                info.thread == push.thread, let covers = info.covers
            else { return false }
            let alerts = Set(covers.alerts)
            guard await stillHolds() else { return false }
            let delivered = await notifications.delivered()
            for shown in delivered {
                guard let eventID = shown.push?.eventID, !eventID.isEmpty, alerts.contains(PushFingerprint.of(eventID: eventID)) else { continue }
                guard await stillHolds() else { return false }
                await notifications.cancel(shown.id)
            }
            guard let time = left(race),
                let badge = try? await client.send("GET", "api/v1/push/badge", timeout: time, retries: 0).decode(PushBadge.self).badge
            else { return true }
            // The count is the accepting session's: a sign-out during its
            // read sets no badge.
            guard await stillHolds() else { return false }
            await notifications.setBadge(min(max(badge, 0), 999))
            return true
        }
    }
}
