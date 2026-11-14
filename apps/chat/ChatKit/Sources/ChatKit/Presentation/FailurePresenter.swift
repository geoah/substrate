import Foundation

/// Why a run ended without finishing: the server's closed set, as a thread,
/// an `error` event and a `done` result carry it in `cause`. A cause this
/// build does not know reads as the sentence for its status.
enum RunCause: String, CaseIterable, Sendable {
    case providerKeyRefused = "provider_key_refused"
    case providerRateLimited = "provider_rate_limited"
    case providerBilling = "provider_billing"
    case providerOverloaded = "provider_overloaded"
    case providerUnavailable = "provider_unavailable"
    /// The provider turned the request itself down: asking again asks the
    /// same thing.
    case providerRejected = "provider_rejected"
    case modelUnavailable = "model_unavailable"
    case contextTooLong = "context_too_long"
    case allowanceSpent = "allowance_spent"
    case interrupted
    case writeFailed = "write_failed"
    case `internal`
    case turnLimit = "turn_limit"
    case toolCallLimit = "tool_call_limit"
    case deadline
    /// Somebody asked for the stop.
    case requested
    case shutdown
}

/// The envelope's `reason` where a chat request is refused before its
/// stream opens. `quota` and `key_refused` are the two a server answers
/// with today; the rest arrive with the causes. A missing key is one of
/// these and never a run's cause: it is refused before any thread opens.
enum RefusalReason: String, CaseIterable, Sendable {
    case threadBusy = "thread_busy"
    /// A continue of a thread whose reply is complete.
    case nothingToContinue = "nothing_to_continue"
    case turnCap = "turn_cap"
    case spendCap = "spend_cap"
    case providerKeyMissing = "provider_key_missing"
    case quota
    case keyRefused = "key_refused"
}

/// How a stored reply says it was cut short: the row's `cutBy`.
enum CutBy: String, CaseIterable, Sendable {
    case length, filter, stop, failure
}

/// The one place a failure becomes words: the sentence, the note, the
/// button and the details of every attempt and every reason. Each switch
/// below is exhaustive and has no default, so a new cause, reason or
/// attempt does not compile without its sentence and its button.
enum FailurePresenter {
    /// What the words need beyond the failure itself.
    struct Context: Sendable {
        /// "Moss"
        var agent = "The assistant"
        var agentID: String?
        /// The provider's display name, "Anthropic"; nil where it is not read.
        var provider: String?
        var providerID: String?
        /// The server, as a person reads its address.
        var host = "the server"
        /// The provider's and the agent's pages in the console.
        var providerPage: String?
        var agentPage: String?
        /// The app has its own sheet for this provider's key.
        var keySheet = false
        /// Why a send cannot go now, as the composer says it.
        var blocked: String?
        /// The button's work is out.
        var busy = false
        /// The failure is of the chat's newest turn, or of no turn: an
        /// older turn's says what happened and offers nothing.
        var acts = true
        /// What the reply had written when it failed is in the transcript.
        var kept = false
        var thread: String?
        /// What a rename asked for.
        var subject: String?
    }

    struct Words {
        var tone: FailurePresentation.Tone
        var sentence: String
        var note: String?
        var button: FailureButton?
        /// The sentence says all of it: the server's words add nothing.
        var whole = false
    }

    static func present(_ failure: Failure, _ context: Context = Context(), dismiss: FailureAction? = nil) -> FailurePresentation {
        let words = words(failure, context)
        return FailurePresentation(
            id: id(failure), tone: words.tone, sentence: words.sentence, note: words.note, button: context.acts ? words.button : nil,
            details: words.whole ? nil : details(failure), dismiss: dismiss)
    }

    /// A failure in one line, for the places that are a line: a card, a
    /// sheet, a row of Settings.
    static func line(_ failure: Failure, _ context: Context = Context()) -> String {
        let made = present(failure, context)
        guard let details = made.details else { return made.spoken }
        return "\(made.spoken) \(details.label): \(details.text)"
    }

    static func id(_ failure: Failure) -> String {
        "\(failure.attempt.rawValue)|\(failure.why)|\(failure.unconfirmed)|\(failure.at.map { String(Int($0.timeIntervalSince1970)) } ?? "")"
    }

    // MARK: The switch

    static func words(_ failure: Failure, _ c: Context) -> Words {
        var words = worded(failure, c)
        // A message that stands unsent is sent again under its id,
        // whatever refused it: that cannot send it twice, and a new
        // message could.
        if failure.attempt == .message, words.button?.action != .edit { words.button = retry(failure, c) }
        return words
    }

    private static func worded(_ failure: Failure, _ c: Context) -> Words {
        switch failure.why {
        case let .run(status, cause):
            // A cause the server named is taken at its word, known or not;
            // only where it named none is its text read for one.
            let known = cause.map { RunCause(rawValue: $0) } ?? FailureFallback.cause(status: status, reason: failure.said)
            return run(known, status: status, failure, c)
        case let .cut(by):
            switch CutBy(rawValue: by) {
            case .length?: return Words(tone: .warn, sentence: "The reply stopped at the model's limit.", button: resume("Continue", c))
            case .filter?: return Words(tone: .warn, sentence: "\(provider(c)) withheld the rest of this reply.", button: resume("Continue", c))
            case .stop?: return Words(tone: .quiet, sentence: "\(c.agent) was stopped before it finished.", button: resume("Continue", c))
            case .failure?, nil: return Words(tone: .bad, sentence: "\(c.agent) couldn't finish this reply.", button: resume("Try Again", c))
            }
        case .unanswered:
            return Words(tone: .quiet, sentence: "\(c.agent) didn't answer this message.", whole: true)
        case let .http(status, _, reason):
            switch reason.flatMap(RefusalReason.init(rawValue:)) {
            case .threadBusy?:
                return Words(tone: .warn, sentence: "\(c.agent) is still answering in this chat.", note: boxNote(failure), button: retry(failure, c))
            case .turnCap?:
                return Words(
                    tone: .warn, sentence: "Too many replies are being written at once.", note: "Try again when one finishes.",
                    button: retry(failure, c))
            case .spendCap?:
                return Words(tone: .bad, sentence: "Today's spending limit is reached.", button: page("Open in Console", c.agentPage) ?? retry(failure, c))
            case .nothingToContinue?:
                return Words(tone: .quiet, sentence: "\(c.agent) already finished this reply.", whole: true)
            case .providerKeyMissing?:
                return Words(
                    tone: .bad, sentence: "\(provider(c)) has no API key yet.", note: boxNote(failure),
                    button: key("Add Key", c) ?? retry(failure, c))
            case .quota?: return run(.allowanceSpent, status: "error", failure, c)
            case .keyRefused?: return run(.providerKeyRefused, status: "error", failure, c)
            case nil: return refused(status, failure, c)
            }
        case .offline, .unreachable, .timeout, .streamEnded, .sessionEnded, .needsApproval, .notTheAPI, .ended, .closed, .interrupted:
            return attempted(failure, c)
        }
    }

    /// A run that ended without finishing, by its cause, else by its status.
    private static func run(_ cause: RunCause?, status: String, _ failure: Failure, _ c: Context) -> Words {
        // A message the server refused is sent again; a run is continued.
        let again = (failure.attempt == .send || failure.attempt == .message ? retry(failure, c) : nil) ?? resume("Try Again", c)
        switch cause {
        case .providerKeyRefused?:
            return Words(tone: .bad, sentence: "\(provider(c)) refused the API key.", button: page("Replace Key", c.providerPage) ?? again)
        case .providerRateLimited?:
            let wait = failure.retryAfter.flatMap { $0 > 0 ? " Try again in \(span($0))." : nil } ?? ""
            return Words(
                tone: .warn, sentence: "\(provider(c)) is limiting requests.\(wait)",
                button: resume("Try Again", c, enablesAt: failure.retryAt))
        case .providerBilling?:
            let account = c.provider.map { "The \($0) account" } ?? "The provider's account"
            return Words(tone: .bad, sentence: "\(account) is out of credit.", button: again)
        case .providerOverloaded?, .providerUnavailable?:
            return Words(
                tone: .warn, sentence: "\(provider(c)) is having trouble.", note: c.kept ? "What \(c.agent) wrote so far is kept." : nil,
                button: again)
        case .providerRejected?:
            // Trying again asks the same thing: a new chat is the way on,
            // and the provider's own words are under Details.
            return Words(
                tone: .bad, sentence: "\(provider(c)) turned this request down.",
                button: FailureButton(title: "New Chat", action: .newChat(agent: c.agentID)))
        case .modelUnavailable?:
            return Words(
                tone: .bad, sentence: "\(provider(c)) does not offer this model to your key.",
                button: page("Open in Console", c.agentPage) ?? again)
        case .contextTooLong?:
            return Words(
                tone: .bad, sentence: "This chat is too long for the model.",
                button: FailureButton(title: "New Chat", action: .newChat(agent: c.agentID)))
        case .allowanceSpent?:
            return Words(
                tone: .warn, sentence: "Today's allowance is used up.", note: "It resets at midnight UTC.",
                button: key("Add Your Own Key", c) ?? again)
        case .interrupted?, .shutdown?:
            return Words(tone: .warn, sentence: "The server restarted while \(c.agent) was working.", button: again)
        case .writeFailed?:
            return Words(tone: .bad, sentence: "The server couldn't save what \(c.agent) was doing.", button: again)
        case .internal?:
            return Words(tone: .bad, sentence: "\(c.agent) ran into a problem and didn't finish.", button: again)
        case .turnLimit?, .toolCallLimit?, .deadline?:
            return Words(
                tone: .warn, sentence: "\(c.agent) ran out of room before finishing.",
                note: FailureFallback.limit(status: status, reason: failure.said), button: resume("Continue", c))
        case .requested?:
            return Words(tone: .quiet, sentence: "\(c.agent) was stopped before it finished.", button: resume("Continue", c))
        case nil:
            // No cause, or one from a newer server: the sentence for the
            // status.
            switch status {
            case "overbudget":
                return Words(tone: .warn, sentence: "\(c.agent) ran out of room before finishing.", button: resume("Continue", c))
            case "stopped":
                return Words(tone: .quiet, sentence: "\(c.agent) was stopped before it finished.", button: resume("Continue", c))
            default:
                return Words(tone: .bad, sentence: "\(c.agent) couldn't finish this reply.", button: again)
            }
        }
    }

    /// The server's own refusal that names no reason the chat acts on.
    private static func refused(_ status: Int, _ failure: Failure, _ c: Context) -> Words {
        switch (failure.attempt, status) {
        case (.answer, 409):
            return Words(tone: .warn, sentence: "The questions changed since you read them. Check them again.", whole: true)
        case (.decide, 409):
            return Words(tone: .warn, sentence: "It changed since you read it. Check it again.", whole: true)
        case (.decide, 404):
            return Words(tone: .warn, sentence: "This suggestion is no longer there.", whole: true)
        case (.guidanceChange, 409):
            return Words(
                tone: .warn, sentence: "It changed since you opened it, so nothing was saved. This is what it says now.", whole: true)
        case (.guidanceChange, 404):
            return Words(tone: .warn, sentence: "It was deleted since you opened it, so nothing was saved.", whole: true)
        default:
            return attempted(failure, c)
        }
    }

    /// What did not happen, by what was attempted, with why as the note.
    private static func attempted(_ failure: Failure, _ c: Context) -> Words {
        let because = because(failure.why, c)
        let button = retry(failure, c)
        switch failure.attempt {
        case .send:
            if failure.unconfirmed {
                return Words(
                    tone: .warn, sentence: "Your message may not have arrived.",
                    note: "It's in the box below. Look for it in this chat before sending it again.", button: button)
            }
            if case .http = failure.why {
                return Words(tone: .warn, sentence: "Your message wasn't sent.", note: boxNote(failure), button: button)
            }
            return Words(tone: .warn, sentence: because, note: boxNote(failure), button: button)
        case .message:
            if takenID(failure) {
                // The id names another record: sending it again asks the
                // same thing. Never answered by minting an id and sending
                // on the app's own: the way on is the person's, from the
                // composer, and the server's words are under Details.
                return Words(
                    tone: .bad, sentence: "This message can't be sent again as it is.",
                    note: "Something went wrong in \(ChatManifest.appName). Edit the message to send it as a new one.",
                    button: FailureButton(title: "Edit Message", action: .edit))
            }
            // Neither sentence says whether the message arrived, which a
            // refusal of one copy and a lost answer both leave open, and
            // neither needs to: it goes again under its id, and is never
            // sent twice.
            if case .http = failure.why {
                return Words(tone: .warn, sentence: "The server turned your message down.", note: because, button: button)
            }
            return Words(tone: .warn, sentence: "No answer came back for your message.", note: because, button: button)
        case .resume:
            return Words(tone: .warn, sentence: "Couldn't ask \(c.agent) to carry on.", note: because, button: button)
        case .reply:
            return Words(tone: .bad, sentence: "\(c.agent) couldn't finish this reply.", note: because, button: button)
        case .stop:
            return Words(tone: .warn, sentence: "Couldn't stop \(c.agent). The reply is still being written.", note: because, button: button)
        case .attach:
            return Words(tone: .warn, sentence: "Couldn't open the reply \(c.agent) is writing.", note: because, button: button)
        case .reload:
            return Words(tone: .warn, sentence: "Couldn't load the end of this chat.", note: because, button: button)
        case .rows:
            return Words(tone: .warn, sentence: "Couldn't load this chat's messages.", note: because, button: button)
        case .poll:
            return Words(tone: .warn, sentence: "Couldn't check on this chat.", note: because, button: button)
        case .rename:
            return Words(tone: .warn, sentence: "Couldn't rename this chat.", note: because, button: button)
        case .card:
            return Words(tone: .warn, sentence: "Couldn't load this.", note: because, button: button)
        case .answer, .decide, .guidanceChange:
            return Words(tone: .warn, sentence: "That didn't go through.", note: because, button: button)
        case .addKey:
            return Words(tone: .warn, sentence: "The key wasn't saved.", note: because, button: button)
        case .refresh:
            return Words(tone: .warn, sentence: "Couldn't refresh your chats.", note: because, button: button)
        case .feed:
            return Words(tone: .warn, sentence: "Live updates stopped.", note: because, button: button)
        case .guidance:
            return Words(tone: .warn, sentence: "Couldn't read them.", note: because, button: button)
        }
    }

    /// The one button that does an attempt again; nil where the place it is
    /// drawn in has its own controls.
    private static func retry(_ failure: Failure, _ c: Context) -> FailureButton? {
        switch failure.attempt {
        case .send: FailureButton(title: "Send Again", action: .send, busy: c.busy, blocked: c.blocked)
        case .message: FailureButton(title: "Send Again", action: .sendAgain, busy: c.busy, blocked: c.blocked)
        case .resume, .reply: resume("Try Again", c)
        case .stop: FailureButton(title: "Try Again", action: .stop, busy: c.busy)
        case .attach: FailureButton(title: "Try Again", action: .reopen, busy: c.busy)
        case .reload: FailureButton(title: "Try Again", action: .reload, busy: c.busy)
        case .rows: c.thread.map { FailureButton(title: "Try Again", action: .reloadRows(thread: $0), busy: c.busy) }
        case .rename:
            if let thread = c.thread, let subject = c.subject {
                FailureButton(title: "Try Again", action: .rename(thread: thread, subject: subject), busy: c.busy)
            } else {
                nil
            }
        case .refresh, .feed: FailureButton(title: "Try Again", action: .refresh, busy: c.busy)
        case .poll, .card, .answer, .decide, .addKey, .guidance, .guidanceChange: nil
        }
    }

    /// Why, as one short sentence a person would say.
    private static func because(_ why: Failure.Why, _ c: Context) -> String {
        switch why {
        case .offline: return "You're offline."
        case .unreachable: return "Couldn't reach \(c.host)."
        case .timeout: return "The server took too long to answer."
        case .streamEnded: return "The connection dropped."
        case .notTheAPI: return "The server answered with something \(ChatManifest.appName) can't read."
        case .sessionEnded: return "Your sign-in ended."
        case .needsApproval: return "\(ChatManifest.appName) needs your approval for that."
        case .ended: return "The server ended it."
        case .closed: return "\(ChatManifest.appName) closed while it was being sent."
        case .interrupted: return "It was stopped before the server answered."
        case let .http(status, _, _):
            if status >= 500 { return "The server ran into a problem." }
            switch status {
            case 429: return "The server is busy. Try again in a moment."
            case 409: return "It changed in the meantime."
            case 404: return "It is no longer there."
            case 403: return "You're not allowed to do that."
            default: return "The server refused it."
            }
        case .run, .cut, .unanswered: return "It didn't finish."
        }
    }

    /// A `409` for a message's own id that names no reason: the id is
    /// another record's. The server gives this refusal no reason of its
    /// own, and a server older than the `turn_cap` reason answers a full
    /// set of turns the same way, so its words are read for the field's
    /// name: the one place a message's refusal is, to delete once the
    /// server names it.
    static func takenID(_ failure: Failure) -> Bool {
        guard case let .http(409, _, reason) = failure.why, reason == nil else { return false }
        return failure.said?.contains("messageId") == true
    }

    // MARK: Parts

    private static func provider(_ c: Context) -> String { c.provider ?? "The provider" }

    /// Where a refused or lost send left the text: Continue's own words are
    /// nobody's to give back.
    private static func boxNote(_ failure: Failure) -> String? {
        failure.attempt == .send ? "Your message is in the box below." : nil
    }

    private static func resume(_ title: String, _ c: Context, enablesAt: Date? = nil) -> FailureButton {
        FailureButton(title: title, action: .resume, busy: c.busy, blocked: c.blocked, enablesAt: enablesAt)
    }

    /// The app's own key sheet where it has one for the provider, else the
    /// provider's page in the console.
    private static func key(_ title: String, _ c: Context) -> FailureButton? {
        if c.keySheet, let provider = c.providerID { return FailureButton(title: title, action: .addKey(provider: provider)) }
        return page(title, c.providerPage)
    }

    private static func page(_ title: String, _ url: String?) -> FailureButton? {
        url.map { FailureButton(title: title, action: .open($0), external: true) }
    }

    private static func details(_ failure: Failure) -> FailureDetails? {
        if let said = failure.said?.trimmingCharacters(in: .whitespacesAndNewlines), !said.isEmpty {
            return FailureDetails(title: "Details", label: "The server said", text: said)
        }
        if let seen = failure.seen?.trimmingCharacters(in: .whitespacesAndNewlines), !seen.isEmpty {
            return FailureDetails(title: "Details", label: "This device reported", text: seen)
        }
        return nil
    }

    /// "20 seconds", "1 second", "2 minutes".
    static func span(_ seconds: Int) -> String {
        if seconds >= 120 { return "\(Int((Double(seconds) / 60).rounded(.up))) minutes" }
        return seconds == 1 ? "1 second" : "\(seconds) seconds"
    }
}
