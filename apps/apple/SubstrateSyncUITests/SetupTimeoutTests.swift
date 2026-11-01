import XCTest

/// Set Up against a server slower to install than the phone waits: the
/// step's error shows, nothing goes on spinning after it, and the server,
/// which finished the install after the phone stopped waiting, shows it done
/// at the next check. It runs only when SYNC_SLOW_SERVER names such a server
/// (apps/apple/scripts/slowserver.py, holding each install longer than the
/// 3 seconds this test gives one) and SYNC_E2E_REPOSITORY and
/// SYNC_E2E_PASSWORD a fresh repository on it.
final class SetupTimeoutTests: XCTestCase {
    func testATimedOutInstallStopsSpinningAndTheNextCheckFindsIt() throws {
        let env = ProcessInfo.processInfo.environment
        guard let server = env["SYNC_SLOW_SERVER"], let repository = env["SYNC_E2E_REPOSITORY"],
              let password = env["SYNC_E2E_PASSWORD"]
        else { throw XCTSkip("SYNC_SLOW_SERVER, SYNC_E2E_REPOSITORY and SYNC_E2E_PASSWORD name the server") }
        let holds = Double(env["SYNC_SLOW_SECONDS"] ?? "8") ?? 8

        let app = XCUIApplication()
        app.launchEnvironment["SUBSTRATE_SYNC_E2E_NO_HEALTH"] = "1"
        app.launchEnvironment["SUBSTRATE_SYNC_SERVER_WORK_SECONDS"] = "3"
        // Set Up is a password session's.
        app.launchEnvironment["SUBSTRATE_SYNC_PASSWORD_DOOR"] = "1"
        app.launch()

        let address = app.textFields["https://substrate.example.com"]
        XCTAssertTrue(address.waitForExistence(timeout: 10), "not signed out: reset the simulator's Keychain first")
        address.tap()
        address.typeText(server)
        app.buttons["Continue"].tap()
        let repo = app.textFields["you.example.com"]
        XCTAssertTrue(repo.waitForExistence(timeout: 20), "the server was not accepted")
        repo.tap()
        repo.typeText(repository)
        let secure = app.secureTextFields["Required"]
        secure.tap()
        secure.typeText(password)
        app.buttons["Sign In"].tap()
        let notNow = app.buttons["Not Now"]
        if notNow.waitForExistence(timeout: 5) { notNow.tap() }

        let setUp = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Set Up'")).firstMatch
        XCTAssertTrue(setUp.waitForExistence(timeout: 30), app.debugDescription)
        setUp.tap()

        // The first install outlasts the 3 seconds: its step says so.
        let slow = app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'is taking long'")).firstMatch
        XCTAssertTrue(slow.waitForExistence(timeout: 30), "no timeout shown: \(app.debugDescription)")

        // Once it shows, nothing is still at work: the check after a stop is
        // quick reads, and it never runs under the Set Up button's spinner.
        let working = app.buttons.matching(NSPredicate(format: "label CONTAINS ', working'"))
        let settled = Date().addingTimeInterval(5)
        while Date() < settled, working.count > 0 { usleep(250_000) }
        XCTAssertEqual(working.count, 0, "still spinning after the error: \(app.debugDescription)")
        XCTAssertTrue(setUp.isEnabled, "Set Up is not offered again")

        // The server finished the install after the phone stopped waiting.
        sleep(UInt32(holds) + 2)
        app.buttons["Check Again"].tap()
        let ready = app.staticTexts.matching(NSPredicate(format: "label == 'Ready'")).firstMatch
        XCTAssertTrue(ready.waitForExistence(timeout: 20), "the install the server finished is not shown done: \(app.debugDescription)")
        XCTAssertFalse(slow.exists, "the timeout outlived the check that found the install done")
    }
}
