import XCTest

/// The app against a real substrate, through its screens: sign in, set up,
/// add sample data, sync. It runs only when a server is named, so a plain
/// test run skips it:
///
///   TEST_RUNNER_SYNC_E2E_SERVER=http://localhost:8080 \
///   TEST_RUNNER_SYNC_E2E_REPOSITORY=synctest TEST_RUNNER_SYNC_E2E_PASSWORD=… \
///   xcodebuild … -only-testing:SubstrateSyncUITests test
///
/// Grant Contacts and Calendars first (`xcrun simctl privacy <udid> grant
/// contacts dev.reamde.substrate.sync`, and `calendar`). Health stays off:
/// HealthKit's sheet does not reliably answer a test, so its deliveries are
/// checked by posting the encoder's goldens to the webhook instead.
final class EndToEndTests: XCTestCase {
    func testSignInSetUpAndSync() throws {
        let env = ProcessInfo.processInfo.environment
        guard let server = env["SYNC_E2E_SERVER"], let repository = env["SYNC_E2E_REPOSITORY"],
              let password = env["SYNC_E2E_PASSWORD"]
        else { throw XCTSkip("SYNC_E2E_SERVER, SYNC_E2E_REPOSITORY and SYNC_E2E_PASSWORD name the server") }

        let app = XCUIApplication()
        // Health is checked by posting its deliveries, not through the app:
        // the simulator's HealthKit sheet does not reliably answer a test.
        app.launchEnvironment["SUBSTRATE_SYNC_E2E_NO_HEALTH"] = "1"
        app.launch()

        let address = app.textFields["https://substrate.example.com"]
        if address.waitForExistence(timeout: 10) {
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
        }

        let setUp = app.buttons["Set Up"]
        let syncNow = app.buttons["Sync Now"]
        if setUp.waitForExistence(timeout: 30), !syncNow.exists {
            setUp.tap()
        }
        XCTAssertTrue(syncNow.waitForExistence(timeout: 180), "set up did not finish: \(app.debugDescription)")
        // iOS offers to save the password the sign-in typed.
        let notNow = app.buttons["Not Now"]
        if notNow.waitForExistence(timeout: 3) { notNow.tap() }

        let add = app.buttons["Add Sample Data"]
        scrollTo(add, in: app)
        add.tap()
        let note = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Added' OR label BEGINSWITH 'Adding sample data failed'")).firstMatch
        XCTAssertTrue(note.waitForExistence(timeout: 120), "no sample data note: \(app.debugDescription)")
        print("E2E sample data: \(note.label)")
        XCTAssertTrue(note.label.hasPrefix("Added"), note.label)

        scrollTo(syncNow, in: app, up: true)
        syncNow.tap()
        // Each source writes its last run when it finishes.
        let runs = app.staticTexts.matching(NSPredicate(format: "label CONTAINS ' written' OR label CONTAINS ' deliveries'"))
        let deadline = Date().addingTimeInterval(240)
        while Date() < deadline, runs.count < 2 { sleep(2) }
        let errors = app.staticTexts.matching(NSPredicate(format: "label == 'Last error'"))
        let screen = app.debugDescription
        XCTAssertEqual(errors.count, 0, screen)
        XCTAssertGreaterThanOrEqual(runs.count, 2, screen)
        print("E2E home: \(screen)")
    }

    private func scrollTo(_ element: XCUIElement, in app: XCUIApplication, up: Bool = false, timeout: TimeInterval = 10) {
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if element.exists, element.isHittable { return }
            up ? app.swipeDown() : app.swipeUp()
        }
    }
}
