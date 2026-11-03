// swift-tools-version: 6.0
import PackageDescription

// What every app that speaks to a substrate shares, as libraries that each
// depend only on the ones listed before them (README.md, and
// `mise run lint:apps`, which holds the order). The portable ones build for
// iOS, macOS, Linux and Android; SubstrateDeviceApple and SubstrateUI
// compile to nothing off Apple systems, and SubstrateBridge's JNI half to
// nothing off Android. SubstrateKit is every library under one import, for
// the apps written against the single module it once was. SubstrateAndroid
// is the one shared library kit-core loads (apps/kit/android).
let package = Package(
    name: "SubstrateKit",
    platforms: [.iOS("26.0"), .macOS("26.0")],
    products: [
        .library(name: "SubstrateKit", targets: ["SubstrateKit"]),
        .library(name: "SubstrateCore", targets: ["SubstrateCore"]),
        .library(name: "SubstrateDevice", targets: ["SubstrateDevice"]),
        .library(name: "SubstrateAPI", targets: ["SubstrateAPI"]),
        .library(name: "SubstrateAuth", targets: ["SubstrateAuth"]),
        .library(name: "SubstrateRuntime", targets: ["SubstrateRuntime"]),
        .library(name: "SubstrateLocal", targets: ["SubstrateLocal"]),
        .library(name: "SubstrateScreens", targets: ["SubstrateScreens"]),
        .library(name: "SubstrateTesting", targets: ["SubstrateTesting"]),
        .library(name: "SubstrateDeviceApple", targets: ["SubstrateDeviceApple"]),
        .library(name: "SubstrateUI", targets: ["SubstrateUI"]),
        .library(name: "SubstrateBridge", targets: ["SubstrateBridge"]),
        .library(name: "SubstrateAndroid", type: .dynamic,
                 targets: ["SubstrateBridge", "SubstrateBridgeCheck", "SubstrateAPI", "SubstrateAuth", "SubstrateRuntime",
                           "SubstrateLocal", "SubstrateScreens"]),
    ],
    dependencies: [
        // SHA-256 for PKCE where CryptoKit is not: Linux and Android.
        .package(url: "https://github.com/apple/swift-crypto.git", exact: "5.1.1"),
    ],
    targets: [
        // The JSON value, the record and its flat wire form, kind references
        // and their words, RFC 3339 and ISO 8601, the errors and `explain`,
        // the scope words.
        .target(name: "SubstrateCore"),
        // The device services' protocols and their request and answer values.
        .target(name: "SubstrateDevice", dependencies: ["SubstrateCore"]),
        // The client, discovery, versions, the requirement and the locator.
        .target(name: "SubstrateAPI", dependencies: ["SubstrateCore", "SubstrateDevice"]),
        // The manifest, the sign-in, the session and the status check.
        .target(name: "SubstrateAuth", dependencies: [
            "SubstrateCore", "SubstrateDevice", "SubstrateAPI",
            .product(name: "Crypto", package: "swift-crypto", condition: .when(platforms: [.linux, .android])),
        ]),
        // The loop every screen runs: command, controller, engine, journal,
        // presenter, effects; keyed children, epochs, coalescing.
        .target(name: "SubstrateRuntime", dependencies: ["SubstrateCore", "SubstrateDevice", "SubstrateAPI"]),
        // The repository's folder, the journal file, the outbox, the record
        // cache, the package a repository holds and the link generation.
        .target(name: "SubstrateLocal", dependencies: ["SubstrateCore", "SubstrateDevice", "SubstrateAPI", "SubstrateRuntime"]),
        // The shared screens: the link, and each screen's engine, presenter and
        // fixtures.
        .target(name: "SubstrateScreens", dependencies: [
            "SubstrateCore", "SubstrateDevice", "SubstrateAPI", "SubstrateAuth", "SubstrateRuntime", "SubstrateLocal",
        ]),
        // The scripted server, the fake device services and the harness tests use.
        .target(name: "SubstrateTesting", dependencies: [
            "SubstrateCore", "SubstrateDevice", "SubstrateAPI", "SubstrateAuth", "SubstrateRuntime", "SubstrateLocal",
        ]),
        // The plain resolver's verdict on a TXT query, which tells a SERVFAIL
        // from a name without the record where DNS-SD cannot.
        .target(name: "SubstrateResolv", linkerSettings: [.linkedLibrary("resolv", .when(platforms: [.macOS, .iOS]))]),
        // The device services on Apple frameworks.
        .target(name: "SubstrateDeviceApple", dependencies: ["SubstrateCore", "SubstrateDevice", "SubstrateResolv"]),
        // The SwiftUI screens and record views.
        .target(name: "SubstrateUI", dependencies: [
            "SubstrateCore", "SubstrateDevice", "SubstrateAPI", "SubstrateAuth", "SubstrateRuntime", "SubstrateLocal", "SubstrateScreens",
            "SubstrateDeviceApple",
        ]),
        // jni.h, the Looper and the log from the NDK; empty off Android.
        .target(name: "SubstrateJNI", linkerSettings: [
            .linkedLibrary("android", .when(platforms: [.android])),
            .linkedLibrary("log", .when(platforms: [.android])),
        ]),
        // The Android bridge: the envelope, handles, calls and keyed lists,
        // which run anywhere, and the JNI surface, which is Android's alone.
        .target(name: "SubstrateBridge", dependencies: ["SubstrateCore", "SubstrateDevice", "SubstrateAPI", "SubstrateRuntime", "SubstrateJNI"]),
        // The root the bridge's runtime checks drive (bridge-check).
        .target(name: "SubstrateBridgeCheck", dependencies: ["SubstrateCore", "SubstrateDevice", "SubstrateRuntime", "SubstrateBridge"],
                resources: [.copy("Resources/check.txt")]),
        .target(name: "SubstrateKit", dependencies: [
            "SubstrateCore", "SubstrateDevice", "SubstrateAPI", "SubstrateAuth", "SubstrateRuntime", "SubstrateScreens",
            "SubstrateDeviceApple", "SubstrateUI",
        ]),
        // Resources: files an older build wrote, read back (Workouts' format 1
        // outbox).
        .testTarget(name: "SubstrateKitTests", dependencies: ["SubstrateKit", "SubstrateLocal", "SubstrateScreens", "SubstrateTesting"],
                    resources: [.copy("Resources")]),
        .testTarget(name: "SubstrateBridgeTests", dependencies: [
            "SubstrateCore", "SubstrateDevice", "SubstrateRuntime", "SubstrateBridge", "SubstrateBridgeCheck",
        ]),
    ],
    swiftLanguageModes: [.v6]
)
