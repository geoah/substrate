// swift-tools-version: 6.0
import PackageDescription

// What every Apple app that speaks to a substrate shares: the record and its
// values, a kind's name and glyph as the console draws them, and the pill a
// linked record shows as, opening a read-only view of it. No app's own API
// client: an app hands the views a RecordReader over whatever it speaks.
let package = Package(
    name: "SubstrateKit",
    platforms: [.iOS("26.0"), .macOS("26.0")],
    products: [
        .library(name: "SubstrateKit", targets: ["SubstrateKit"]),
    ],
    targets: [
        .target(name: "SubstrateKit"),
        .testTarget(name: "SubstrateKitTests", dependencies: ["SubstrateKit"]),
    ],
    swiftLanguageModes: [.v5]
)
