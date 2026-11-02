import Foundation

/// Every kind name the app reads or writes. Nothing else in the code spells
/// one.
///
/// The training kinds are the app's own package, which it installs under the
/// repository's own authority (decision 0203), so their names depend on the
/// repository. On the phone they are spelled under `~`, the authority
/// placeholder an app's scopes use, and `Rehome` swaps `~` for the
/// repository's authority at the server's edge, both ways. The two kinds of
/// the health and observations catalogues keep their published names.
public struct KindNames: Sendable, Hashable {
    /// The package's authority as the phone spells it.
    public static let home = "~"
    /// The package's name.
    public static let package = "training"

    public var exercise: String
    public var routine: String
    public var session: String
    public var set: String
    public var setup: String
    public var healthWorkout = "substrate.reamde.dev/health/workout"
    public var observationSource = "substrate.reamde.dev/observations/source"

    /// The names on the phone: the training kinds under `~`.
    public static let v1 = KindNames()

    public init(authority: String = KindNames.home) {
        let prefix = "\(authority)/\(Self.package)/"
        exercise = prefix + "exercise"
        routine = prefix + "routine"
        session = prefix + "session"
        set = prefix + "set"
        setup = prefix + "setup"
    }

    /// `<authority>/training`, the package's id under `authority`.
    public static func packageID(authority: String) -> String { "\(authority)/\(package)" }
}
