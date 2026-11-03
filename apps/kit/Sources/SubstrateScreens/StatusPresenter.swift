import Foundation
import SubstrateAuth
import SubstrateCore
import SubstrateLocal

/// The Status screen, in the words Workouts' Settings → Substrate says it.
public enum StatusPresenter {
    /// - Parameters:
    ///   - package: where the repository stands with the app's package, nil
    ///     while not checked; `known` the version this build needs.
    ///   - kindLabels: the singular words for the kinds a refused write may
    ///     be of, by reference.
    public static func present(_ status: LinkStatus, words: StatusWords, host: String?, package: HeldPackage?, known: Int,
                               sending: Bool, waiting: Int, parked: [ParkedWrite], kindLabels: [String: String] = [:],
                               locale: Locale, timeZone: TimeZone) -> StatusPresentation
    {
        let (symbol, tone): (ScreenSymbol, Tone) = switch status {
        case .ready(0): (.upToDate, .positive)
        case .ready, .preparing: (.sending, .informative)
        case .offline: (.offline, .caution)
        case .local: (.device, .quiet)
        default: (.warning, .caution)
        }
        var actions: [ActionButton<StatusAction>] = []
        if status == .local {
            return StatusPresentation(title: "Substrate", symbol: symbol, tone: tone,
                                      sentence: words.sentence(status, locale: locale, timeZone: timeZone), rows: [],
                                      actions: [ActionButton("Sign In to Your Substrate…", .signIn)], busy: false, footer: nil, refused: nil)
        }
        if waiting > 0 || sending { actions.append(ActionButton("Send Now", .sendNow)) }
        switch status {
        case .needsApproval: actions.append(ActionButton("Review Access…", .review))
        case .signedOut: actions.append(ActionButton("Sign In Again…", .signIn))
        case .accessEndsSoon: actions.append(ActionButton("Renew Access…", .renew))
        default: break
        }
        let packageText: String = switch package {
        case nil: sending ? "Checking" : "Not checked yet"
        case .missing?: "Not installed"
        case let .behind(held)?: "Version \(held), waiting for the update to version \(known)"
        case let .current(held)?: "Version \(held)"
        }
        let refused = parked.isEmpty ? nil : RefusedWrites(
            title: "Refused by Your Substrate",
            rows: parked.map { write in
                RefusedWrite(id: write.id, title: title(of: write, labels: kindLabels), recordID: write.recordID, words: write.words,
                             retry: ActionButton("Retry", .retry(write.id)), discard: ActionButton("Discard", .discard(write.id)))
            },
            footer: "Retry sends the change again. Discard gives up the change and the changes waiting behind it; "
                + "what \(words.device) holds stays.")
        return StatusPresentation(title: "Substrate", symbol: symbol, tone: tone,
                                  sentence: words.sentence(status, locale: locale, timeZone: timeZone),
                                  rows: [LabeledValue("\(words.package) package", packageText)], actions: actions, busy: sending,
                                  footer: "Changes are written to \(host ?? "your substrate") as you make them, and wait on \(words.device) while it cannot be reached.",
                                  refused: refused)
    }

    /// "Task, sent", "Note, changed", "List, deleted".
    static func title(of write: ParkedWrite, labels: [String: String]) -> String {
        let what = labels[write.kind] ?? kindDisplayName(write.kind)
        return switch write.method {
        case .put: "\(what), sent"
        case .patch: "\(what), changed"
        case .purge: "\(what), deleted"
        }
    }
}
