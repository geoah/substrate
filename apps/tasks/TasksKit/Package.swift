// swift-tools-version: 6.0
import PackageDescription

// Everything below Substrate Tasks' views, with no UI framework in it, so
// it builds for Linux and Android as well (docs/plans/app-kit.md §6):
// the model and its records codec, the app's root under the kit's link,
// the task editor, the presenters that write every label, and the
// reminders' plan. Each platform's UI draws its presentations and sends its
// commands, nothing else. macOS is listed so `swift test` runs on a Mac
// without a simulator. TasksAndroid is the Android app's roots over the
// bridge, and SubstrateTasks the one library its APK loads
// (apps/tasks/android).
let kit: [Target.Dependency] = [
    "SubstrateCore", "SubstrateDevice", "SubstrateAPI", "SubstrateAuth", "SubstrateRuntime", "SubstrateLocal", "SubstrateScreens",
].map { .product(name: $0, package: "kit") }

let package = Package(
    name: "TasksKit",
    platforms: [.iOS("26.0"), .macOS("26.0")],
    products: [
        .library(name: "TasksCore", targets: ["TasksCore"]),
        .library(name: "SubstrateTasks", type: .dynamic, targets: ["TasksAndroid"]),
    ],
    dependencies: [
        .package(path: "../../kit")
    ],
    targets: [
        .target(
            name: "TasksCore",
            dependencies: kit,
            // The tasks package's closure, as .mise/closures.py --json writes
            // it: the version the approval asks for, and what the codec is
            // held to.
            resources: [.copy("Resources/Package")]
        ),
        .target(
            name: "TasksAndroid",
            dependencies: ["TasksCore", .product(name: "SubstrateBridge", package: "kit")] + kit
        ),
        .testTarget(
            name: "TasksAndroidTests",
            dependencies: [
                "TasksAndroid", "TasksCore", .product(name: "SubstrateBridge", package: "kit"),
                .product(name: "SubstrateTesting", package: "kit"),
            ] + kit
        ),
        .testTarget(
            name: "TasksCoreTests",
            dependencies: ["TasksCore", .product(name: "SubstrateTesting", package: "kit")] + kit
        ),
    ],
    swiftLanguageModes: [.v6]
)
