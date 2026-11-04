import Foundation
import SubstrateAuth
import SubstrateCore
import SubstrateLocal

/// The Status screen, in the words Workouts' Settings says it.
public enum StatusPresenter {
    /// The section's heading: what the sentence under it is about.
    public static let title = "Your Substrate"

    /// - Parameters:
    ///   - packages: where the repository stands with each package the app
    ///     needs.
    ///   - kindLabels: the singular words for the kinds a refused write may
    ///     be of, by reference.
    public static func present(_ status: LinkStatus, words: StatusWords, host: String?, packages: [PackageStanding], sending: Bool,
                               waiting: Int, parked: [ParkedWrite], kindLabels: [String: String] = [:], locale: Locale,
                               timeZone: TimeZone) -> StatusPresentation
    {
        let (symbol, tone): (ScreenSymbol, ScreenTone) = switch status {
        case .ready(0): (.upToDate, .positive)
        case .ready, .preparing: (.sending, .informative)
        case .offline: (.offline, .caution)
        case .local: (.device, .quiet)
        default: (.warning, .caution)
        }
        let sentence = words.sentence(status, locale: locale, timeZone: timeZone)
        if status == .local {
            return StatusPresentation(title: Self.title, symbol: symbol, tone: tone, sentence: sentence, packages: [],
                                      actions: [ActionButton("Sign In to Your Substrate…", .signIn)], busy: false, footer: nil, refused: nil)
        }
        var actions: [ActionButton<StatusAction>] = []
        if waiting > 0 || sending { actions.append(ActionButton("Send Now", .sendNow)) }
        switch status {
        case .needsApproval: actions.append(ActionButton("Review Access…", .review))
        case .signedOut: actions.append(ActionButton("Sign In Again…", .signIn))
        case .accessEndsSoon: actions.append(ActionButton("Renew Access…", .renew))
        default: break
        }
        let lines = packages.map { package in
            let text: String = switch package.held {
            case nil: sending ? "Checking" : "Not checked yet"
            case .missing?: "Not installed"
            case let .behind(held)?: "Version \(held), waiting for the update to version \(package.needs)"
            case let .current(held)?: "Version \(held)"
            }
            return PackageLine(id: package.id, row: LabeledValue("\(package.name) package", text))
        }
        let refused = parked.isEmpty ? nil : RefusedWrites(
            title: "Refused by Your Substrate",
            rows: parked.map { write in
                RefusedWrite(id: write.id, label: kindLabels[write.kind] ?? kindDisplayName(write.kind), status: refusal(of: write),
                             kind: LabeledValue("Kind", write.kind, verbatim: true),
                             record: LabeledValue("Record ID", write.recordID, verbatim: true), words: write.words,
                             retry: ActionButton("Retry", .retry(write.id)), discard: ActionButton("Discard", .discard(write.id)))
            },
            footer: words.refusedFooter ?? "Retry sends the change again. Discard gives up the change and the changes waiting behind it; "
                + "what \(words.device) holds stays.")
        // The reason is a fact of its own: a refused write's words are on
        // its row, and said here only where no row carries them.
        let problem: ProblemPresentation? = switch status {
        case let .notKept(reason): ProblemPresentation(title: "Not kept on \(words.device)", message: reason)
        case let .refused(_, reason) where parked.isEmpty && !reason.isEmpty:
            ProblemPresentation(title: "Refused by your substrate", message: reason, tone: .caution)
        default: nil
        }
        let notice = parked.isEmpty ? nil
            : NoticeLine(parked.count == 1 ? "1 change needs you" : "\(parked.count) changes need you", symbol: .warning, tone: .caution)
        return StatusPresentation(title: Self.title, symbol: symbol, tone: tone, sentence: sentence, problem: problem, packages: lines,
                                  actions: actions, busy: sending,
                                  footer: words.footer(host: host), refused: refused, notice: notice)
    }

    /// What the server refused of the write, a fact apart from what the
    /// write was of.
    static func refusal(of write: ParkedWrite) -> String {
        switch write.method {
        case .put: "Write refused"
        case .patch: "Change refused"
        case .purge, .delete: "Delete refused"
        }
    }
}
