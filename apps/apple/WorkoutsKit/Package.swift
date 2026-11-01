// swift-tools-version: 6.4
import PackageDescription

// Everything below Substrate Workouts' views, with no UI framework in it.
// WorkoutsCore is the workout model, the session engine, the progression
// engine (next-set suggestions), the presenters and the files on the phone,
// Foundation only. WorkoutsSync is the app's side of a substrate: its own
// package's manifests and their install, the catalogue and history read from
// the server, and the outbox a finished session is written through; it
// speaks through SubstrateKit's client. macOS is listed only so that
// `swift test` runs both suites on a Mac without a simulator.
let package = Package(
    name: "WorkoutsKit",
    platforms: [.iOS(.v27), .watchOS(.v27), .macOS(.v26)],
    products: [
        .library(name: "WorkoutsCore", targets: ["WorkoutsCore"]),
        .library(name: "WorkoutsSync", targets: ["WorkoutsSync"]),
    ],
    dependencies: [
        .package(path: "../SubstrateKit"),
    ],
    targets: [
        .target(
            name: "WorkoutsCore",
            // The starter exercises and routines, and the training package's
            // manifests (apps/apple/scripts/workouts-package.py).
            resources: [.copy("Resources/Bundled"), .copy("Resources/Package")]
        ),
        .target(
            name: "WorkoutsSync",
            dependencies: ["WorkoutsCore", .product(name: "SubstrateKit", package: "SubstrateKit")]
        ),
        .testTarget(
            name: "WorkoutsCoreTests",
            dependencies: ["WorkoutsCore"],
            // Every example record, and kinds.json from the package's manifests.
            resources: [.copy("Fixtures")]
        ),
        .testTarget(
            name: "WorkoutsSyncTests",
            dependencies: ["WorkoutsSync", "WorkoutsCore", .product(name: "SubstrateKit", package: "SubstrateKit")],
            // Answers a substrate gave, recorded from a local server.
            resources: [.copy("Fixtures")]
        ),
    ],
    swiftLanguageModes: [.v6]
)
