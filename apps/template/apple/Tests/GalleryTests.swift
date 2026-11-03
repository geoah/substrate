#if os(macOS)
import AppKit
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TemplateCore
import Testing
@testable import SubstrateTemplateUI

/// The app's screens drawn from TemplateKit's fixtures by the views the app
/// draws them with, in its UI framework's tests, which run on the Mac with
/// no host app: each fixture must draw, and with SUBSTRATE_GALLERY naming a
/// folder, each is written there as a PNG at an iPhone's size.
@MainActor
@Suite struct GalleryTests {
    var link: LinkPresentation<TemplatePresentation>? {
        for fixture in ScreenFixtures.all where fixture.name == "status-up-to-date" {
            if case let .status(status) = fixture.presentation {
                return LinkPresentation(snapshot: LinkSnapshot(phase: .signedIn, repository: "ada.example.com", online: true),
                                        repository: nil, banner: nil, ended: nil, status: status, signOutProblem: nil, app: nil)
            }
        }
        return nil
    }

    @Test func everyFixtureDraws() async throws {
        let link = try #require(link)
        let folder = ProcessInfo.processInfo.environment["SUBSTRATE_GALLERY"].map { URL(fileURLWithPath: $0) }
        if let folder { try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true) }
        let shell = TemplateShell(drawing: Template.configuration)
        for fixture in TemplateFixtures.all {
            let png = try await OffscreenDrawing.png(ItemsView(link: link, app: fixture.app).environment(shell))
            try folder.map { try png.write(to: $0.appendingPathComponent("\(fixture.name).png")) }
        }
    }
}
#endif
