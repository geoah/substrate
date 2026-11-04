import SubstrateCore
import SubstrateRuntime

/// The link's screens and the app's, as one tree: the snapshot every
/// controller under the link is told, the Repository section, the Access
/// banner or the access ended, the Status section, and the app's own
/// presentation, nil only before the app's first.
public struct LinkPresentation<App: Codable & Equatable & Sendable>: Bridged, Equatable, Sendable {
    public var snapshot: LinkSnapshot
    public var repository: RepositoryPresentation?
    public var banner: AccessBanner?
    public var ended: AccessEndedPresentation?
    public var status: StatusPresentation
    public var signOutProblem: ProblemPresentation?
    public var app: App?

    public init(
        snapshot: LinkSnapshot, repository: RepositoryPresentation?, banner: AccessBanner?, ended: AccessEndedPresentation?,
        status: StatusPresentation, signOutProblem: ProblemPresentation?, app: App?
    ) {
        self.snapshot = snapshot
        self.repository = repository
        self.banner = banner
        self.ended = ended
        self.status = status
        self.signOutProblem = signOutProblem
        self.app = app
    }
}
