import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateScreens

/// An error as a value an event carries: what it means for the chat, and
/// the words a person reads.
struct Failure: Codable, Hashable, Sendable {
    enum Kind: Codable, Hashable, Sendable {
        /// The server's own refusal of a request it then did nothing for: a
        /// 4xx with its envelope; `permanent` where retrying cannot help.
        case refused(status: Int, permanent: Bool)
        /// A 401: the token's access ended.
        case sessionExpired
        /// A 403 `insufficient_scope`: the approval does not reach.
        case needsApproval
        /// The turn's own failure, after its stream began.
        case turnFailed
        /// Anything else: an answer that may have come after the request
        /// took effect, or none.
        case other
    }

    var kind: Kind
    var words: String

    init(kind: Kind, words: String) {
        self.kind = kind
        self.words = words
    }

    init(_ error: any Error, server: URL?) {
        words = explain(error, app: ChatManifest.appName, server: server)
        switch error {
        case let error as ServerError where (400 ..< 500).contains(error.status):
            kind = .refused(status: error.status, permanent: error.isPermanent)
            words = error.text
        case is SessionExpired:
            kind = .sessionExpired
        case let error as InsufficientScope:
            kind = .needsApproval
            words = error.message ?? words
        case let error as TurnFailed:
            kind = .turnFailed
            words = error.text
        default:
            kind = .other
        }
    }

    /// The server's own refusal, in its words: nil for an answer that may
    /// have come after the request took effect.
    func refusal(permanent: Bool = false) -> (status: Int, text: String)? {
        switch kind {
        case let .refused(status, isPermanent) where !permanent || isPermanent: (status, words)
        case .needsApproval: (403, words)
        default: nil
        }
    }

    var status: Int? {
        if case let .refused(status, _) = kind { return status }
        return nil
    }

    /// What the failure tells the link: a 401 or a 403 `insufficient_scope`.
    var report: LinkReport? {
        switch kind {
        case .sessionExpired: .accessEnded
        case .needsApproval: .needsApproval
        default: nil
        }
    }
}
