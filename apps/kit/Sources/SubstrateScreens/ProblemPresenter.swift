import Foundation
import SubstrateCore

/// An error as a problem: `explain`'s sentence, and the fix its kind has.
public enum ProblemPresenter {
    public static func present(_ error: any Error, title: String? = nil, app: String, server: URL? = nil) -> ProblemPresentation {
        ProblemPresentation(title: title, message: explain(error, app: app, server: server), fix: fix(for: error))
    }

    /// Signing in again fixes an ended session; approving more fixes a scope
    /// the token lacks (signing in approves nothing more); trying again
    /// fixes a network that was not there. A redirect or a refusal has no
    /// fix in the app.
    static func fix(for error: any Error) -> ProblemFix? {
        switch error {
        case is SessionExpired: ProblemFix(title: "Sign In Again", action: .signInAgain)
        case is InsufficientScope: ProblemFix(title: "Review", action: .review)
        case is URLError: ProblemFix(title: "Try Again", action: .tryAgain)
        case let transport as TransportError where transport.status == nil || (transport.status ?? 0) >= 500:
            ProblemFix(title: "Try Again", action: .tryAgain)
        default: nil
        }
    }
}
