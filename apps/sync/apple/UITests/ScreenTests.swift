import XCTest

/// Screens a review looks at that the end-to-end run never reaches: every
/// source failing at once, as when the server is gone. It runs only when
/// SYNC_SHOTS names a directory, after the end-to-end test left the app signed
/// in, and with that server stopped.
final class ScreenTests: XCTestCase {
    func testFailingSync() throws {
        guard let dir = ProcessInfo.processInfo.environment["SYNC_SHOTS"] else { throw XCTSkip("SYNC_SHOTS names where to keep the screenshots") }
        let app = XCUIApplication()
        app.launchEnvironment["SUBSTRATE_SYNC_E2E_NO_HEALTH"] = "1"
        app.launch()
        func shot(_ name: String) {
            try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
        }
        let syncNow = app.buttons["Sync Now"]
        XCTAssertTrue(syncNow.waitForExistence(timeout: 30), app.debugDescription)
        // The server is stopped from outside once this file says Home is up.
        let flag = URL(fileURLWithPath: dir).appendingPathComponent("home-up")
        try Data().write(to: flag)
        let down = URL(fileURLWithPath: dir).appendingPathComponent("server-down")
        let deadline = Date().addingTimeInterval(120)
        while !FileManager.default.fileExists(atPath: down.path), Date() < deadline { sleep(1) }
        syncNow.tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS \"didn't sync\"")).firstMatch.waitForExistence(timeout: 120), app.debugDescription)
        shot("failed-home")
        app.buttons["source.contacts"].tap()
        sleep(1)
        shot("failed-contacts")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Activity'")).firstMatch.tap()
        sleep(1)
        shot("failed-activity")
        // Opened with the server gone.
        app.terminate()
        app.launch()
        sleep(8)
        shot("offline-launch")
    }
}
