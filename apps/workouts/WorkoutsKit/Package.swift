// swift-tools-version: 6.4
import PackageDescription

// Everything below Substrate Workouts' views, with no UI framework in it, so
// it builds for Linux and Android as well. WorkoutsCore is the workout
// model, the session engine, the progression engine (next-set suggestions),
// the presenters and the files on the phone, over Foundation and the kit's
// SubstrateCore. WorkoutsSync is the app's side of a substrate: its own
// package's manifests and their install, the catalogue and history read from
// the server, and the outbox a finished session is written through; it
// speaks through the kit's portable libraries, never its Apple ones. macOS
// is listed only so that `swift test` runs both suites on a Mac without a
// simulator.
let portableKit: [Target.Dependency] = ["SubstrateCore", "SubstrateDevice", "SubstrateAPI", "SubstrateAuth"].map {
    .product(name: $0, package: "kit")
}
// The loop the session and the app's root run on, the journal file and the
// outbox, the link and its shared screens.
let runtimeKit: [Target.Dependency] = ["SubstrateRuntime", "SubstrateLocal"].map { .product(name: $0, package: "kit") }

let package = Package(
    name: "WorkoutsKit",
    platforms: [.iOS(.v27), .watchOS(.v27), .macOS(.v26)],
    products: [
        .library(name: "WorkoutsCore", targets: ["WorkoutsCore"]),
        .library(name: "WorkoutsSync", targets: ["WorkoutsSync"]),
    ],
    dependencies: [
        .package(path: "../../kit"),
        // SHA-256 where CryptoKit is not: the ids an import or the bundled
        // library derives from a key (DerivedMintedID).
        .package(url: "https://github.com/apple/swift-crypto.git", exact: "5.1.1"),
    ],
    targets: [
        .target(
            name: "WorkoutsCore",
            dependencies: [
                .product(name: "SubstrateCore", package: "kit"), .product(name: "SubstrateDevice", package: "kit"),
                .product(name: "Crypto", package: "swift-crypto", condition: .when(platforms: [.linux, .android])),
            ] + runtimeKit,
            // The starter exercises and routines, and the training package's
            // manifests (apps/workouts/scripts/workouts-package.py).
            resources: [.copy("Resources/Bundled"), .copy("Resources/Package")]
        ),
        .target(
            name: "WorkoutsSync",
            dependencies: ["WorkoutsCore"] + portableKit + runtimeKit + [.product(name: "SubstrateScreens", package: "kit")]
        ),
        .testTarget(
            name: "WorkoutsCoreTests",
            dependencies: ["WorkoutsCore", .product(name: "SubstrateCore", package: "kit"), .product(name: "SubstrateTesting", package: "kit")]
                + runtimeKit,
            // Every example record, and kinds.json from the package's manifests.
            resources: [.copy("Fixtures")]
        ),
        .testTarget(
            name: "WorkoutsSyncTests",
            dependencies: ["WorkoutsSync", "WorkoutsCore"] + portableKit + runtimeKit
                + [.product(name: "SubstrateScreens", package: "kit"), .product(name: "SubstrateTesting", package: "kit")],
            // Answers a substrate gave, recorded from a local server.
            resources: [.copy("Fixtures")]
        ),
    ],
    swiftLanguageModes: [.v6]
)
