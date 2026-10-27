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

        let setUp = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Set Up'")).firstMatch
        let syncNow = app.buttons["Sync Now"]
        if setUp.waitForExistence(timeout: 30), !syncNow.exists {
            scrollTo(setUp, in: app, up: true)
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
        // The status line says when the run ended, and how.
        let ended = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Everything synced' OR label == 'The last sync did not finish'")).firstMatch
        XCTAssertTrue(ended.waitForExistence(timeout: 240), "the sync did not end: \(app.debugDescription)")
        XCTAssertTrue(ended.label.hasPrefix("Everything synced"), app.debugDescription)
        // Each source writes its last run when it finishes; the list builds
        // only the rows on screen, so the page is walked to find them.
        var runs = Set<String>()
        var failures = Set<String>()
        for _ in 0 ..< 8 {
            for label in app.staticTexts.allElementsBoundByIndex.map(\.label) {
                if label.contains(" written") || label.contains(" deliveries") { runs.insert(label) }
                if label.hasPrefix("Last sync of ") { failures.insert(label) }
            }
            app.swipeUp()
        }
        let screen = app.debugDescription
        XCTAssertEqual(failures, [], screen)
        XCTAssertGreaterThanOrEqual(runs.count, 2, "\(runs)")
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
