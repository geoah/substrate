// swift-tools-version: 6.0
import PackageDescription

// Everything below Substrate Today's views, with no UI framework in it, so
// it builds for Linux and Android as well (docs/plans/app-kit.md §6): the
// app's root under the kit's link, its kinds, its manifest and its
// presenter. Each platform's UI draws its presentations and sends its
// commands, nothing else. macOS is listed so `swift test` runs on a Mac
// without a simulator.
let kit: [Target.Dependency] = [
    "SubstrateCore", "SubstrateDevice", "SubstrateAPI", "SubstrateAuth", "SubstrateRuntime", "SubstrateLocal", "SubstrateScreens",
].map { .product(name: $0, package: "kit") }

let package = Package(
    name: "TodayKit",
    platforms: [.iOS("26.0"), .macOS("26.0")],
    products: [
        .library(name: "TodayCore", targets: ["TodayCore"])
    ],
    dependencies: [
        .package(path: "../../kit")
    ],
    targets: [
        .target(name: "TodayCore", dependencies: kit),
        .testTarget(
            name: "TodayCoreTests",
            dependencies: ["TodayCore", .product(name: "SubstrateTesting", package: "kit")] + kit
        ),
    ],
    swiftLanguageModes: [.v6]
)
