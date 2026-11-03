/// The shared screens, each drawn by a renderer on every system.
public enum ScreenKind: String, Codable, CaseIterable, Hashable, Sendable {
    case connect
    case serverTooOld
    case accessBanner
    case accessEnded
    case review
    case repository
    case status
    case settings
    case problem
    case about
}

/// One presentation of one screen, as a renderer is handed it.
public enum FixturePresentation: Codable, Equatable, Sendable {
    case connect(ConnectPresentation)
    case serverTooOld(ServerTooOldPresentation)
    case accessBanner(AccessBanner)
    case accessEnded(AccessEndedPresentation)
    case review(ReviewPresentation)
    case repository(RepositoryPresentation)
    case status(StatusPresentation)
    case settings(SettingsPresentation)
    case problem(ProblemPresentation)
    case about(AboutPresentation)

    public var screen: ScreenKind {
        switch self {
        case .connect: .connect
        case .serverTooOld: .serverTooOld
        case .accessBanner: .accessBanner
        case .accessEnded: .accessEnded
        case .review: .review
        case .repository: .repository
        case .status: .status
        case .settings: .settings
        case .problem: .problem
        case .about: .about
        }
    }
}

/// A screen in one of the states an app shows it in, by name: what the
/// gallery draws on every system (§7.4), and what a renderer is checked
/// against.
public struct ScreenFixture: Codable, Equatable, Sendable, Identifiable {
    public var name: String
    public var presentation: FixturePresentation

    public init(_ name: String, _ presentation: FixturePresentation) {
        self.name = name
        self.presentation = presentation
    }

    public var id: String { name }
    public var screen: ScreenKind { presentation.screen }
}
