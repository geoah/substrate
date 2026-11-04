import Foundation
import SubstrateBridge
import SubstrateCore
import TasksCore
import Testing

@testable import TasksAndroid

@Suite("Opening Tasks' roots")
struct OpeningTests {
    let environment = BridgeEnvironment(
        locale: Locale(identifier: "en_US"), timeZone: TimeZone(identifier: "UTC")!,
        resources: URL(fileURLWithPath: "/nonexistent"), data: FileManager.default.temporaryDirectory,
        cache: FileManager.default.temporaryDirectory, sdk: 36)

    @Test func theSettingsOfThisTreesKotlinOpen() throws {
        let opening = TasksOpening(fingerprint: TasksAndroid.ProtocolFingerprint.value, debug: true, version: "0.1.0 (1)", device: "this phone")
        #expect(try TasksOpening.read(ValueCoding.encode(opening)) == opening)
    }

    @Test func kotlinFromAnotherTreeOrNoSettingsAreRefused() throws {
        let other = TasksOpening(fingerprint: "another", debug: true, version: "0.1.0 (1)", device: "this phone")
        #expect(throws: BridgeOpenRefused.self) { try TasksOpening.read(ValueCoding.encode(other)) }
        #expect(throws: BridgeOpenRefused.self) { try TasksOpening.read(nil) }
    }

    @Test func aDebugBuildSignsInThroughLoopbackAndAReleaseThroughItsClaim() throws {
        let local = TasksOpening(fingerprint: TasksAndroid.ProtocolFingerprint.value, debug: true, version: "0.1.0 (1)", device: "this phone")
            .manifest(in: environment)
        #expect(local.clientID == TasksManifest.localClientID)
        #expect(local.isLocal && local.signInRedirect == nil)
        #expect(local.softwareVersion == "0.1.0 (1)")
        let release = try TasksOpening.read(
            ValueCoding.encode(
                TasksOpening(
                    fingerprint: TasksAndroid.ProtocolFingerprint.value, clientID: "https://apps.example.com/tasks/client.json",
                    redirect: "https://apps.example.com/tasks/callback", version: "0.2.0 (7)", device: "this phone"))
        ).manifest(in: environment)
        #expect(!release.isLocal)
        #expect(release.signInRedirect?.absoluteString == "https://apps.example.com/tasks/callback")
    }

    /// A debug build keeps loopback whatever it was stamped with: the stamp
    /// is a release's.
    @Test func aStampedDebugBuildStillSignsInThroughLoopback() {
        let stamped = TasksOpening(
            fingerprint: TasksAndroid.ProtocolFingerprint.value, debug: true,
            clientID: "https://apps.example.com/tasks/client.json", redirect: "https://apps.example.com/tasks/callback",
            version: "0.1.0 (1)", device: "this phone"
        ).manifest(in: environment)
        #expect(stamped.clientID == TasksManifest.localClientID)
        #expect(stamped.isLocal && stamped.signInRedirect == nil)
    }

    /// A release never selects loopback: without an https client id and an
    /// https callback it may claim, its roots refuse to open.
    @Test func aReleaseWithoutItsHttpsStampIsRefused() throws {
        let stamps: [(String?, String?)] = [
            (nil, nil),
            ("", ""),
            ("https://apps.example.com/tasks/client.json", nil),
            (nil, "https://apps.example.com/tasks/callback"),
            ("http://apps.example.com/tasks/client.json", "https://apps.example.com/tasks/callback"),
            ("https://apps.example.com/tasks/client.json", "http://apps.example.com/tasks/callback"),
            ("https://apps.example.com/tasks/client.json", "https://apps.example.com/"),
            ("http://localhost", "http://127.0.0.1/oauth/callback"),
        ]
        for (clientID, redirect) in stamps {
            let release = TasksOpening(
                fingerprint: TasksAndroid.ProtocolFingerprint.value, clientID: clientID, redirect: redirect,
                version: "0.2.0 (7)", device: "this phone")
            #expect(throws: BridgeOpenRefused.self, "\(String(describing: clientID)) \(String(describing: redirect))") {
                try TasksOpening.read(ValueCoding.encode(release))
            }
        }
    }

    /// Android cannot read `Bundle.module`: a build whose copied-out
    /// resources are missing asks for the package's first version.
    @Test func thePackageVersionComesFromTheCopiedOutResources() {
        #expect(TasksOpening.packageVersion(in: environment) == 1)
    }
}
