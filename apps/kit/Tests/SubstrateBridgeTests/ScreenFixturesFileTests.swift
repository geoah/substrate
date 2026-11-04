import Foundation
import SubstrateBridgeCheck
import SubstrateCore
import SubstrateScreens
import Testing

@testable import SubstrateBridge

/// The shared screens' fixtures as one file both platforms draw from
/// (docs/plans/app-kit.md §7.4): Swift writes `ScreenFixtures.all` there as
/// the bridge carries it, kit-ui's gallery renders each from it, and
/// kit-core's test decodes and encodes each again, which Swift reads back.
enum ScreenFixturesFile {
    static let file = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
        .appendingPathComponent("../../android/fixtures/screens.json").standardizedFileURL

    static func text() throws -> String {
        String(decoding: try ValueCoding.encode(ScreenFixtures.all).data(pretty: true), as: UTF8.self)
    }
}

@Suite("The screens' fixture file", .timeLimit(.minutes(2)))
struct ScreenFixturesFileTests {
    #if os(macOS)
    static let onAMac = true
    #else
    static let onAMac = false
    #endif

    /// SUBSTRATE_UPDATE_FIXTURES=1 writes it again after a presentation or
    /// a fixture changes. Held on a Mac alone: the presenters word dates with
    /// the platform's formatters, the file holds the Mac's words, and Linux's
    /// ICU spaces a time differently.
    @Test(.enabled(if: Self.onAMac))
    func theFileIsWhatTheScreensPresentToday() throws {
        let text = try ScreenFixturesFile.text()
        if ProcessInfo.processInfo.environment["SUBSTRATE_UPDATE_FIXTURES"] == "1" {
            try text.write(to: ScreenFixturesFile.file, atomically: true, encoding: .utf8)
        }
        let onDisk = try String(contentsOf: ScreenFixturesFile.file, encoding: .utf8)
        #expect(onDisk == text, "stale: run with SUBSTRATE_UPDATE_FIXTURES=1")
    }

    @Test func everyFixtureReadsBackAsItself() throws {
        let read = try ValueCoding.decode([ScreenFixture].self, from: JSONValue(data: Data(ScreenFixturesFile.text().utf8)))
        #expect(read == ScreenFixtures.all)
    }

    @Test func theGalleryRootPresentsEveryFixture() async throws {
        let (runtime, outlet, _) = started()
        let handle = try #require(try post(runtime, Envelope(op: .open, body: ["root": "gallery"])).handle)
        let first = try await outlet.next { $0.op == .presentation && $0.handle == handle }
        let presented = try BridgeCodec.decode(GalleryPresentation.self, from: first.body)
        #expect(presented.fixtures == ScreenFixtures.all)
    }

    /// The last leg: `android:test` sets SUBSTRATE_ROUNDTRIP_KOTLIN_SCREENS to
    /// what kit-core's test wrote of each fixture.
    @Test(.enabled(if: ProcessInfo.processInfo.environment["SUBSTRATE_ROUNDTRIP_KOTLIN_SCREENS"] != nil))
    func swiftDecodesWhatKotlinWroteOfEachScreen() throws {
        let path = try #require(ProcessInfo.processInfo.environment["SUBSTRATE_ROUNDTRIP_KOTLIN_SCREENS"])
        let written = try ValueCoding.decode([ScreenFixture].self, from: JSONValue(data: Data(contentsOf: URL(fileURLWithPath: path))))
        #expect(written.count == ScreenFixtures.all.count)
        for (expected, kotlin) in zip(ScreenFixtures.all, written) {
            #expect(kotlin == expected, "Kotlin's \(expected.name)")
        }
    }
}
