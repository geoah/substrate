import SubstrateRuntime

/// The link's screens as one tree: the snapshot every other controller is
/// told, the Account section, the Access banner or the access ended, and
/// the Status section.
public struct LinkPresentation: Codable, Equatable, Sendable {
    public var snapshot: LinkSnapshot
    public var account: AccountPresentation?
    public var banner: AccessBanner?
    public var ended: AccessEndedPresentation?
    public var status: StatusPresentation
    public var signOutProblem: ProblemPresentation?

    public init(snapshot: LinkSnapshot, account: AccountPresentation?, banner: AccessBanner?, ended: AccessEndedPresentation?,
                status: StatusPresentation, signOutProblem: ProblemPresentation?)
    {
        self.snapshot = snapshot
        self.account = account
        self.banner = banner
        self.ended = ended
        self.status = status
        self.signOutProblem = signOutProblem
    }
}
