import Foundation
import SubstrateCore
import TasksCore
import Testing

@testable import TasksAndroid

/// Tasks' screens as one file the Android gallery draws from: Swift writes
/// `TasksFixtures.all` there as the bridge carries it, and the app's
/// Robolectric test draws each from it beside the SwiftUI pictures.
enum GalleryFixtureFile {
    static let file = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
        .appendingPathComponent("../../../android/fixtures/tasks.json").standardizedFileURL

    static func text() throws -> String {
        String(decoding: try ValueCoding.encode(TasksGalleryFixture.all).data(pretty: true), as: UTF8.self)
    }
}

@Suite("The Android gallery's fixture file")
struct GalleryFixtureFileTests {
    #if os(macOS)
    static let onAMac = true
    #else
    static let onAMac = false
    #endif

    /// SUBSTRATE_UPDATE_FIXTURES=1 writes it again after a presentation or
    /// a fixture changes. Held on a Mac alone, as the kit's is: the file
    /// holds the Mac's formatters' words.
    @Test(.enabled(if: Self.onAMac))
    func theFileIsWhatTheScreensPresentToday() throws {
        let text = try GalleryFixtureFile.text()
        if ProcessInfo.processInfo.environment["SUBSTRATE_UPDATE_FIXTURES"] == "1" {
            try FileManager.default.createDirectory(at: GalleryFixtureFile.file.deletingLastPathComponent(), withIntermediateDirectories: true)
            try text.write(to: GalleryFixtureFile.file, atomically: true, encoding: .utf8)
        }
        let onDisk = try String(contentsOf: GalleryFixtureFile.file, encoding: .utf8)
        #expect(onDisk == text, "stale: run with SUBSTRATE_UPDATE_FIXTURES=1")
    }

    @Test func everyFixtureCrossesAndReadsBackAsItself() throws {
        let fixtures = TasksGalleryFixture.all
        #expect(fixtures.map(\.name) == TasksFixtures.all.map(\.name))
        let read = try ValueCoding.decode([TasksGalleryFixture].self, from: JSONValue(data: Data(GalleryFixtureFile.text().utf8)))
        #expect(read == fixtures)
        for fixture in fixtures {
            switch fixture.screen {
            case .editor: #expect(fixture.editor.map { key in fixture.app.editors.keys.contains { $0.rawValue == key } } == true)
            case .settings: #expect(fixture.settings != nil)
            default: #expect(fixture.editor == nil && fixture.settings == nil)
            }
        }
    }
}
