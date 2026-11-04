import Foundation

/// Every kind name the app reads or writes. Nothing else in the code spells
/// one.
///
/// The training kinds are Substrate Workouts' published package,
/// `substrate.reamde.dev/training` (decision record 0214): one name in every
/// repository, on the phone and on the wire alike, so nothing is respelled
/// at the server's edge. The two kinds of the health and observations
/// catalogues keep their published names too.
public struct WorkoutsKinds: Sendable, Hashable {
    /// The authority that publishes the package.
    public static let authority = "substrate.reamde.dev"
    /// The package's name.
    public static let package = "training"
    /// `substrate.reamde.dev/training`, the package's id.
    public static let packageID = "\(authority)/\(package)"

    public var movement: String
    public var exercise: String
    public var routine: String
    public var session: String
    public var set: String
    public var setup: String
    /// What the app has seeded whole (`library.yaml`).
    public var library: String
    public var bodyMeasurement: String
    public var healthWorkout = "substrate.reamde.dev/health/workout"
    public var observationSource = "substrate.reamde.dev/observations/source"

    /// The names as published.
    public static let v1 = WorkoutsKinds()

    public init() {
        let prefix = "\(Self.packageID)/"
        movement = prefix + "movement"
        exercise = prefix + "exercise"
        routine = prefix + "routine"
        session = prefix + "session"
        set = prefix + "set"
        setup = prefix + "setup"
        library = prefix + "library"
        bodyMeasurement = prefix + "bodymeasurement"
    }
}
