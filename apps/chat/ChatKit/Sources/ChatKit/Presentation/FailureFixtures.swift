import Foundation

/// Failures as the app draws them, made by the presenter from the values
/// the engines keep: what the app's gallery draws, in each place a failure
/// stands, with no server. The words are the presenter's own, so a
/// picture never shows a sentence the app would not say.
public enum FailureFixtures {
    public enum Place: String, Sendable {
        /// A card under the chat's newest turn.
        case turn
        /// A card under an older turn, with later turns below it.
        case olderTurn
        /// The strip directly above the composer.
        case chat
        /// The strip at the top of the list.
        case list
        /// A card under a message that stands unsent, the chat's last.
        case unsent
    }

    public struct Fixture: Sendable {
        public var name: String
        public var place: Place
        public var failure: FailurePresentation
        /// Drawn with its Details open.
        public var detailsOpen = false
    }

    static let limited =
        "substrate: validation failed: agent substrate.reamde.dev/assistant/moss: llm: error, status code: 429, "
        + "status: 429 Too Many Requests, message: Rate limit reached for requests. Please try again in 20s."

    static var unconfirmed: Failure {
        var failure = Failure(.send, .timeout)
        failure.unconfirmed = true
        return failure
    }

    static var context: FailurePresenter.Context {
        var context = FailurePresenter.Context()
        context.agent = "Moss"
        context.agentID = "substrate.reamde.dev/assistant/moss"
        context.provider = "Anthropic"
        context.providerID = "anthropic"
        context.host = "s.example.com"
        context.providerPage = "https://s.example.com/data/substrate.reamde.dev/llm/provider/anthropic"
        context.agentPage = "https://s.example.com/agents/substrate.reamde.dev%2Fassistant%2Fmoss"
        context.thread = "t1"
        context.subject = "Balcony plants"
        return context
    }

    /// Each place a failure stands, in the states the pull request shows.
    public static func places(now: Date) -> [Fixture] {
        var older = context
        older.acts = false
        var busy = context
        busy.busy = true
        return [
            Fixture(
                name: "turn-end", place: .turn,
                failure: FailurePresenter.present(Failure(.reply, .run(status: "error", cause: "provider_overloaded"), said: "llm: overloaded_error"), context)),
            Fixture(name: "turn-older", place: .olderTurn, failure: FailurePresenter.present(Failure(.reply, .unanswered), older)),
            Fixture(
                name: "rate-limit", place: .turn,
                failure: FailurePresenter.present(
                    Failure(.reply, .run(status: "error", cause: "provider_rate_limited"), said: limited, retryAfter: 20, at: now), context)),
            Fixture(
                name: "details-open", place: .turn,
                failure: FailurePresenter.present(Failure(.reply, .run(status: "error", cause: nil), said: limited), context), detailsOpen: true),
            Fixture(
                name: "turn-working", place: .turn,
                failure: FailurePresenter.present(Failure(.reply, .run(status: "error", cause: "interrupted")), busy)),
            Fixture(
                name: "stop-failed", place: .chat,
                failure: FailurePresenter.present(
                    Failure(.stop, .http(status: 500, code: "internal", reason: nil), said: "the stop could not be written"), context)),
            Fixture(name: "reload-failed", place: .chat, failure: FailurePresenter.present(Failure(.reload, .timeout), context)),
            Fixture(name: "send-offline", place: .chat, failure: FailurePresenter.present(Failure(.send, .offline), context, dismiss: .dismiss)),
            // What a lost answer reads as on a server that takes no
            // `messageId`: the app cannot send it again safely.
            Fixture(name: "send-unconfirmed", place: .chat, failure: FailurePresenter.present(unconfirmed, context, dismiss: .dismiss)),
            // And on one that does, once the app's own repeats ran out.
            Fixture(name: "unsent-no-answer", place: .unsent, failure: FailurePresenter.present(Failure(.message, .timeout), context, dismiss: .edit)),
            Fixture(name: "unsent-relaunch", place: .unsent, failure: FailurePresenter.present(Failure(.message, .closed), context, dismiss: .edit)),
            Fixture(
                name: "unsent-refused", place: .unsent,
                failure: FailurePresenter.present(
                    Failure(.message, .http(status: 409, code: "conflict", reason: "turn_cap"), said: "4 chat turns are running"), context,
                    dismiss: .edit)),
            Fixture(
                name: "unsent-id-taken", place: .unsent,
                failure: FailurePresenter.present(
                    Failure(
                        .message, .http(status: 409, code: "conflict", reason: "message_id_taken"),
                        said: "substrate: conflict: the messageId is taken: messageId \"0195c1f2-7a3e-7b10-9c44-2f6d8e1a5b03\": send the message under a new id"
                    ),
                    context)),
            // The device could not write the message it is sending.
            Fixture(
                name: "unsent-unsaved", place: .chat,
                failure: FailurePresenter.present(Failure(.keep, .disk, seen: "There isn't enough space on this device."), context)),
            Fixture(
                name: "list-refresh", place: .list,
                failure: FailurePresenter.present(Failure(.refresh, .unreachable, seen: "S.example.com isn't answering."), context)),
            Fixture(
                name: "list-feed", place: .list,
                failure: FailurePresenter.present(Failure(.feed, .ended, said: "the feed is closed to this token"), context)),
        ]
    }

    /// Every row of the table of causes: one card per cause of the closed
    /// set, then a reply cut at the model's limit and a send with no
    /// connection.
    public static func table(now: Date) -> [FailurePresentation] {
        var out = RunCause.allCases.map { cause -> FailurePresentation in
            let status =
                switch cause {
                case .turnLimit, .toolCallLimit, .deadline: "overbudget"
                case .requested, .shutdown: "stopped"
                default: "error"
                }
            return FailurePresenter.present(
                Failure(.reply, .run(status: status, cause: cause.rawValue), retryAfter: cause == .providerRateLimited ? 20 : nil, at: now), context)
        }
        out.append(FailurePresenter.present(Failure(.send, .http(status: 422, code: "validation", reason: "provider_key_missing")), context))
        out.append(FailurePresenter.present(Failure(.send, .http(status: 403, code: "guard", reason: "spend_cap")), context))
        out.append(FailurePresenter.present(Failure(.reply, .cut("length")), context))
        out.append(FailurePresenter.present(Failure(.send, .offline), context))
        return out
    }
}
