#if canImport(SwiftUI)
import SwiftUI
import SubstrateAuth
import SubstrateScreens

/// The tokens a device stopped using that their servers have not revoked
/// yet, as `UnrevokedPresenter` words them: one `ProblemBox` each, so a form
/// section lists them as rows.
public struct UnrevokedNotices: View {
    let notices: [ProblemPresentation]

    public init(appName: String, unrevoked: [Session]) {
        notices = UnrevokedPresenter.present(appName: appName, unrevoked: unrevoked.map(SessionFacts.init))
    }

    /// Notices a presentation already words.
    public init(_ notices: [ProblemPresentation]) {
        self.notices = notices
    }

    public var body: some View {
        ForEach(notices, id: \.self) { ProblemBox($0) }
    }

    /// One sentence per server and repository, in the order they were met.
    public nonisolated static func sentences(appName: String, unrevoked: [Session]) -> [String] {
        UnrevokedPresenter.sentences(appName: appName, unrevoked: unrevoked.map(SessionFacts.init))
    }
}
#endif
