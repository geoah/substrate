// swift-tools-version: 6.0
import PackageDescription

// The bridge's generator (docs/plans/app-kit.md §5.5): reads the `Bridged`
// declarations of the Swift sources apps/kit/android/bridge.json names,
// writes bridge.golden.json, the Kotlin kit-core decodes them with, and the
// protocol fingerprint both sides check at start. A package of its own, so
// swift-syntax is a dependency of this tool and of nothing an app links.
//
//   mise run android:generate         write all three
//   mise run android:generate:check   fail when any is stale
let package = Package(
    name: "BridgeGen",
    platforms: [.macOS("15.0")],
    dependencies: [
        .package(url: "https://github.com/swiftlang/swift-syntax.git", exact: "604.0.0")
    ],
    targets: [
        .target(
            name: "BridgeModel",
            dependencies: [
                .product(name: "SwiftSyntax", package: "swift-syntax"),
                .product(name: "SwiftParser", package: "swift-syntax"),
            ]),
        .executableTarget(name: "bridgegen", dependencies: ["BridgeModel"]),
        .testTarget(name: "BridgeModelTests", dependencies: ["BridgeModel"]),
    ],
    swiftLanguageModes: [.v6]
)
