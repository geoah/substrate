// swift-tools-version: 6.0
import PackageDescription

// What every Apple app that speaks to a substrate shares: the server and
// whether it is new enough, the requests, signing in through the app
// authorization flow, the session, whether the app is still set up, the
// screens for each, and the record as the console draws it (README.md).
let package = Package(
    name: "SubstrateKit",
    platforms: [.iOS("26.0"), .macOS("26.0")],
    products: [
        .library(name: "SubstrateKit", targets: ["SubstrateKit"]),
    ],
    targets: [
        .target(name: "SubstrateKit", dependencies: ["SubstrateResolv"]),
        // The plain resolver's verdict on a TXT query, which tells a SERVFAIL
        // from a name without the record where DNS-SD cannot.
        .target(name: "SubstrateResolv", linkerSettings: [.linkedLibrary("resolv")]),
        .testTarget(name: "SubstrateKitTests", dependencies: ["SubstrateKit"]),
    ],
    swiftLanguageModes: [.v5]
)
