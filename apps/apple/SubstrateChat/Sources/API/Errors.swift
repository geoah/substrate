import Foundation
import SubstrateKit

/// An error in a sentence a person can act on, in the kit's words for this
/// app.
func explain(_ error: Error, server: URL? = nil) -> String {
    SubstrateKit.explain(error, app: ChatManifest.appName, server: server)
}

/// The server's own refusal of a request it then did nothing for, in its
/// words: a 4xx envelope, or an approval that does not reach. Nil for an
/// answer that may have come after the request took effect.
func refusal(_ error: Error, permanent: Bool = false) -> (status: Int, text: String)? {
    if let error = error as? ServerError, (400 ..< 500).contains(error.status), !permanent || error.isPermanent {
        return (error.status, error.text)
    }
    if let error = error as? InsufficientScope {
        return (403, error.message ?? error.errorDescription ?? "")
    }
    return nil
}
