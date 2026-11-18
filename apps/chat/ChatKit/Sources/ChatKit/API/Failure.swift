import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateScreens

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// A failure as a value an event carries and the state keeps: what was
/// attempted and why it failed. It holds no sentence. `FailurePresenter`
/// chooses the words, the button and the details when it is drawn, so
/// nothing between the request and the screen has to guess what a string
/// was about.
struct Failure: Codable, Hashable, Sendable {
    /// What was being attempted. Every request and stream the app makes for
    /// a chat is one of these.
    enum Attempt: String, Codable, Hashable, Sendable, CaseIterable {
        /// A message, before the server took it.
        case send
        /// A message sent under an id of its own that the server has not
        /// taken: it stands at its place in the chat, to send again.
        case message
        /// Writing a message sent under its id into this device's folder,
        /// where it is kept until the server has it.
        case keep
        /// Try Again or Continue: the thread run again with no new message.
        case resume
        /// The reply itself, once the server took the turn.
        case reply
        case stop
        /// Opening the stream of a reply that is being written.
        case attach
        /// Reading the chat's rows once a run ended.
        case reload
        /// Reading a chat's rows when it is opened.
        case rows
        /// Following a turn this server does not stream.
        case poll
        case rename
        /// Reading a question's, a suggestion's or an approval's card.
        case card
        /// Answering or dismissing a question.
        case answer
        /// Deciding a suggestion or an approval.
        case decide
        case addKey
        /// Reading the list: the chats, the agents, the cards.
        case refresh
        /// The change feed.
        case feed
        /// The server's search of the messages, under a search of the list.
        case search
        /// A pin or an archive the person asked for, written to their mark.
        case flag
        /// Reading the memories and the instructions.
        case guidance
        /// Switching, rewording or deleting one of them.
        case guidanceChange
    }

    /// Where a failure is drawn, by what it is about.
    enum Place: String, Codable, Hashable, Sendable {
        /// A card at the turn in the transcript.
        case turn
        /// The strip directly above the composer.
        case chat
        /// The strip at the top of the list.
        case list
        /// Inside the card, the sheet or the Settings row it is about.
        case inline
        /// Tried again on its own and never drawn.
        case nowhere
    }

    /// Why it failed.
    enum Why: Codable, Hashable, Sendable {
        /// The run ended badly: the thread's `status` (`error`,
        /// `overbudget`, `stopped`) and the server's `cause`, as it spelled
        /// it. Nil from a server older than the cause.
        case run(status: String, cause: String?)
        /// A stored reply that says how it was cut short: the row's `cutBy`.
        case cut(String)
        /// A message no reply follows in the transcript.
        case unanswered
        /// The server's own answer, with its envelope: the status, the
        /// `code`, and the `reason` where it narrows the code.
        case http(status: Int, code: String, reason: String?)
        /// This device has no connection.
        case offline
        /// The server could not be reached: the request never left.
        case unreachable
        case timeout
        /// The connection or the stream ended before the answer did.
        case streamEnded
        /// A 401: the sign-in ended.
        case sessionEnded
        /// A reply typed into a push's notification, whose sign-in is no
        /// longer the one held: nothing was sent, to this server or any.
        case sessionChanged
        /// A 403 `insufficient_scope`: the approval does not reach.
        case needsApproval
        /// An answer that was not the API's: a gateway's page, a redirect, a
        /// body the app cannot read.
        case notTheAPI(status: Int?)
        /// The server ended it in its own words and nothing narrower is
        /// known.
        case ended
        /// The app closed before the answer came.
        case closed
        /// Something else took the answer's place before it came: a stop,
        /// or the reply already being written in the chat.
        case interrupted
        /// This device could not write it to its folder.
        case disk
    }

    var attempt: Attempt
    var why: Why
    /// The server's own words, where it said any: an envelope's message, a
    /// run's error, a thread's reason. Shown under Details and nowhere else.
    var said: String?
    /// What this device saw, where the server said nothing: the system's
    /// description of a connection that failed.
    var seen: String?
    /// Seconds the provider asked to wait, on a rate limit.
    var retryAfter: Int?
    /// When the failure happened, which `retryAfter` counts from.
    var at: Date?
    /// A send whose delivery the check could not confirm either way.
    var unconfirmed = false

    init(_ attempt: Attempt, _ why: Why, said: String? = nil, seen: String? = nil, retryAfter: Int? = nil, at: Date? = nil) {
        self.attempt = attempt
        self.why = why
        self.said = said
        self.seen = seen
        self.retryAfter = retryAfter
        self.at = at
    }

    /// What an error means for `attempt`.
    init(_ attempt: Attempt, _ error: any Error, server: URL?, at: Date? = nil) {
        self.attempt = attempt
        self.at = at
        switch error {
        case let error as ChatRefusal:
            why = .http(status: error.server.status, code: error.server.code, reason: error.reason)
            said = error.server.text
            retryAfter = error.retryAfter
        case let error as ServerError:
            why = .http(status: error.status, code: error.code, reason: nil)
            said = error.text
        case let error as SessionExpired:
            why = .sessionEnded
            said = error.message
        case let error as InsufficientScope:
            why = .needsApproval
            said = error.message
        case let error as TurnFailed:
            said = error.text
            retryAfter = error.retryAfter
            // A failed turn names its cause. An error without one says by
            // its code what ended the stream instead; a code this build does
            // not know, and a server older than either, is a failed turn
            // whose text is all there is.
            switch error.cause == nil ? error.code.flatMap(StreamErrorCode.init(rawValue:)) : nil {
            case .auth?: why = .sessionEnded
            case .unavailable?: why = .streamEnded
            case .internal?: why = .run(status: "error", cause: RunCause.internal.rawValue)
            case nil: why = .run(status: "error", cause: error.cause)
            }
        case let error as StreamBroke:
            let inner = Why(transport: error.underlying)
            why = inner ?? (error.answered ? .streamEnded : .unreachable)
            seen = explain(error.underlying, app: ChatManifest.appName, server: server)
        case let error as TransportError:
            why = .notTheAPI(status: error.status)
            seen = error.text
        case let error as Redirected:
            why = .notTheAPI(status: error.status)
            seen = explain(error, app: ChatManifest.appName, server: server)
        default:
            why = Why(transport: error) ?? .notTheAPI(status: nil)
            seen = explain(error, app: ChatManifest.appName, server: server)
        }
    }

    /// Where it is drawn. Exhaustive, so a new attempt names its place.
    var place: Place {
        switch attempt {
        case .reply, .resume, .message: .turn
        case .send, .keep, .stop, .attach, .reload, .rows, .rename: .chat
        case .refresh, .feed, .search, .flag: .list
        case .card, .answer, .decide, .addKey, .guidance, .guidanceChange: .inline
        case .poll: .nowhere
        }
    }

    /// The server's own refusal of a request it then did nothing for: a 4xx
    /// with its envelope, or an approval that does not reach; `permanent`
    /// asks for one retrying cannot help. Nil for an answer that may have
    /// come after the request took effect.
    func refusal(permanent: Bool = false) -> Int? {
        switch why {
        case let .http(status, _, _) where (400 ..< 500).contains(status):
            !permanent || (status != 409 && status != 429) ? status : nil
        case .needsApproval: 403
        default: nil
        }
    }

    var status: Int? {
        if case let .http(status, _, _) = why { return status }
        return nil
    }

    /// The request provably never reached the server, so repeating it
    /// cannot run anything twice.
    var neverSent: Bool {
        switch why {
        case .offline, .unreachable, .sessionChanged: true
        default: false
        }
    }

    /// The moment a rate limit lifts: what the button counts down to.
    var retryAt: Date? {
        guard let retryAfter, retryAfter > 0, let at else { return nil }
        return at.addingTimeInterval(TimeInterval(retryAfter))
    }

    /// The server's `cause` of a run that ended badly, as it spelled it.
    var cause: String? {
        if case let .run(_, cause) = why { return cause }
        return nil
    }

    /// The same failure, of another attempt.
    func attempting(_ attempt: Attempt) -> Failure {
        var copy = self
        copy.attempt = attempt
        return copy
    }

    /// What the failure tells the link: a 401 or a 403 `insufficient_scope`.
    var report: LinkReport? {
        switch why {
        case .sessionEnded: .accessEnded
        case .needsApproval: .needsApproval
        default: nil
        }
    }
}

extension Failure.Why {
    /// The system's network error as a reason; nil for an error that is not
    /// one of the connection's.
    init?(transport error: any Error) {
        guard let error = error as? URLError else { return nil }
        switch error.code {
        case .notConnectedToInternet, .dataNotAllowed, .internationalRoamingOff: self = .offline
        case .cannotFindHost, .dnsLookupFailed, .cannotConnectToHost, .secureConnectionFailed, .serverCertificateUntrusted,
            .serverCertificateHasBadDate, .serverCertificateNotYetValid, .serverCertificateHasUnknownRoot, .clientCertificateRejected,
            .appTransportSecurityRequiresSecureConnection:
            self = .unreachable
        case .timedOut: self = .timeout
        case .networkConnectionLost: self = .streamEnded
        case .badServerResponse, .cannotParseResponse, .cannotDecodeContentData, .cannotDecodeRawData: self = .notTheAPI(status: nil)
        default: return nil
        }
    }
}

/// The server's refusal of an agent stream, with what its envelope says
/// beyond the kit's `ServerError`: the `reason` that narrows the code.
struct ChatRefusal: Error, Sendable {
    let server: ServerError
    let reason: String?
    let retryAfter: Int?

    /// `error`, with the envelope's reason where `body` holds one.
    static func reading(_ error: any Error, body: Data, retryAfter: String?) -> any Error {
        guard let server = error as? ServerError else { return error }
        let envelope = (try? JSONSerialization.jsonObject(with: body) as? [String: Any])?["error"] as? [String: Any]
        return ChatRefusal(
            server: server, reason: envelope?["reason"] as? String,
            retryAfter: retryAfter.flatMap { Int($0.trimmingCharacters(in: .whitespaces)) })
    }
}

/// A stream that failed with an error that is not the server's: before the
/// server answered, or while its answer was read.
struct StreamBroke: Error, Sendable {
    let answered: Bool
    let underlying: any Error
}
