// swift-tools-version: 6.0
import PackageDescription

// Substrate Chat below its views, with no UI framework in it, so it builds
// and tests on Linux and Android as well: the chat list as the app's root
// under the kit's link, each conversation a child of it, the transcript
// fold, the stream lines, the Markdown parser, the words, and what the
// device keeps. Named for the app, so its SwiftPM identity is unique in the
// one Xcode project. macOS is listed only so `swift test` runs on a Mac.
let kit: [Target.Dependency] = [
    "SubstrateCore", "SubstrateDevice", "SubstrateAPI", "SubstrateAuth", "SubstrateRuntime", "SubstrateLocal", "SubstrateScreens",
].map { .product(name: $0, package: "kit") }

let package = Package(
    name: "ChatKit",
    platforms: [.iOS("26.0"), .macOS("26.0")],
    products: [
        .library(name: "ChatKit", targets: ["ChatKit"])
    ],
    dependencies: [
        .package(path: "../../kit"),
        // SHA-256 where CryptoKit is not: the folder and file names every
        // build has kept the cache under are hashes.
        .package(url: "https://github.com/apple/swift-crypto.git", exact: "5.1.1"),
    ],
    targets: [
        .target(
            name: "ChatKit",
            dependencies: kit + [
                .product(name: "Crypto", package: "swift-crypto", condition: .when(platforms: [.linux, .android]))
            ]),
        // Resources: a cache folder as the build before ChatKit wrote it.
        .testTarget(
            name: "ChatKitTests", dependencies: ["ChatKit", .product(name: "SubstrateTesting", package: "kit")] + kit,
            resources: [.copy("Fixtures")]),
    ],
    swiftLanguageModes: [.v6]
)
