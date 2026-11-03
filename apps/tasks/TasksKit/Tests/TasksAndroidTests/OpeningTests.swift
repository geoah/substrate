import Foundation
import SubstrateBridge
import SubstrateCore
@testable import TasksAndroid
import TasksCore
import Testing

@Suite("Opening Tasks' roots")
struct OpeningTests {
    let environment = BridgeEnvironment(locale: Locale(identifier: "en_US"), timeZone: TimeZone(identifier: "UTC")!,
                                        resources: URL(fileURLWithPath: "/nonexistent"), data: FileManager.default.temporaryDirectory,
                                        cache: FileManager.default.temporaryDirectory, sdk: 36)

    @Test func theSettingsOfThisTreesKotlinOpen() throws {
        let opening = TasksOpening(fingerprint: TasksAndroid.ProtocolFingerprint.value, version: "0.1.0 (1)", device: "this phone")
        #expect(try TasksOpening.read(ValueCoding.encode(opening)) == opening)
    }

    @Test func kotlinFromAnotherTreeOrNoSettingsAreRefused() throws {
        let other = TasksOpening(fingerprint: "another", version: "0.1.0 (1)", device: "this phone")
        #expect(throws: BridgeOpenRefused.self) { try TasksOpening.read(ValueCoding.encode(other)) }
        #expect(throws: BridgeOpenRefused.self) { try TasksOpening.read(nil) }
    }

    @Test func aLocalBuildSignsInThroughLoopbackAndAReleaseThroughItsClaim() {
        let local = TasksOpening(fingerprint: TasksAndroid.ProtocolFingerprint.value, version: "0.1.0 (1)", device: "this phone").manifest(in: environment)
        #expect(local.clientID == TasksManifest.localClientID)
        #expect(local.isLocal && local.signInRedirect == nil)
        #expect(local.softwareVersion == "0.1.0 (1)")
        let release = TasksOpening(fingerprint: TasksAndroid.ProtocolFingerprint.value, clientID: "https://apps.example.com/tasks/client.json",
                                   redirect: "https://apps.example.com/tasks/callback", version: "0.2.0 (7)", device: "this phone")
            .manifest(in: environment)
        #expect(!release.isLocal)
        #expect(release.signInRedirect?.absoluteString == "https://apps.example.com/tasks/callback")
    }

    /// Android cannot read `Bundle.module`: a build whose copied-out
    /// resources are missing asks for the package's first version.
    @Test func thePackageVersionComesFromTheCopiedOutResources() {
        #expect(TasksOpening.packageVersion(in: environment) == 1)
    }
}
