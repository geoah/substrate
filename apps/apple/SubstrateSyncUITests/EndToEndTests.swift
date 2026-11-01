import XCTest

/// The app against a real substrate, through its screens: sign in, set up,
/// add sample data, sync. It runs only when a server is named, so a plain
/// test run skips it:
///
///   TEST_RUNNER_SYNC_E2E_SERVER=http://localhost:8080 \
///   TEST_RUNNER_SYNC_E2E_REPOSITORY=synctest TEST_RUNNER_SYNC_E2E_PASSWORD=… \
///   xcodebuild … -only-testing:SubstrateSyncUITests test
///
/// Grant Contacts, Calendars, Reminders and Always location first (`xcrun
/// simctl privacy <udid> grant contacts dev.reamde.substrate.sync`,
/// `calendar`, `reminders` and `location-always`), and reset the simulator's
/// Keychain (`xcrun simctl keychain <udid> reset`), which outlives the app,
/// so the test signs in to the server it names. Health stays off:
/// HealthKit's sheet does not reliably answer a test, so its deliveries are
/// checked by posting the encoder's goldens to the webhook instead.
final class EndToEndTests: XCTestCase {
    func testSignInSetUpAndSync() throws {
        let env = ProcessInfo.processInfo.environment
        guard let server = env["SYNC_E2E_SERVER"], let repository = env["SYNC_E2E_REPOSITORY"],
              let password = env["SYNC_E2E_PASSWORD"]
        else { throw XCTSkip("SYNC_E2E_SERVER, SYNC_E2E_REPOSITORY and SYNC_E2E_PASSWORD name the server") }

        let app = XCUIApplication()
        // SYNC_E2E_SHOTS names a directory to keep a screenshot of each page in.
        let shots = env["SYNC_E2E_SHOTS"].map { URL(fileURLWithPath: $0) }
        func shot(_ name: String) {
            guard let shots else { return }
            // A pushed page slides in; the picture is of where it settles.
            sleep(1)
            try? XCUIScreen.main.screenshot().pngRepresentation.write(to: shots.appendingPathComponent("\(name).png"))
        }
        let back = { app.navigationBars.buttons.element(boundBy: 0).tap() }
        // Health is checked by posting its deliveries, not through the app:
        // the simulator's HealthKit sheet does not reliably answer a test.
        app.launchEnvironment["SUBSTRATE_SYNC_E2E_NO_HEALTH"] = "1"
        // The password session and its Set Up, on a server that would offer
        // the browser sign-in, which a test cannot approve.
        app.launchEnvironment["SUBSTRATE_SYNC_PASSWORD_DOOR"] = "1"
        app.launch()

        let address = app.textFields["https://substrate.example.com"]
        if address.waitForExistence(timeout: 10) {
            shot("signin")
            address.tap()
            address.typeText(server)
            app.buttons["Continue"].tap()
            let repo = app.textFields["you.example.com"]
            XCTAssertTrue(repo.waitForExistence(timeout: 20), "the server was not accepted")
            shot("signin-repository")
            repo.tap()
            repo.typeText(repository)
            let secure = app.secureTextFields["Required"]
            secure.tap()
            secure.typeText(password)
            app.buttons["Sign In"].tap()
        }

        // iOS offers to save the password the sign-in typed, over whatever
        // screen comes next.
        let notNow = app.buttons["Not Now"]
        if notNow.waitForExistence(timeout: 5) { notNow.tap() }

        let setUp = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Set Up'")).firstMatch
        let syncNow = app.buttons["Sync Now"]
        if setUp.waitForExistence(timeout: 30), !syncNow.exists {
            scrollTo(setUp, in: app, up: true)
            shot("setup")
            setUp.tap()
            sleep(1)
            shot("setup-busy")
        }
        XCTAssertTrue(syncNow.waitForExistence(timeout: 180), "set up did not finish: \(app.debugDescription)")
        if notNow.waitForExistence(timeout: 3) { notNow.tap() }

        // Sample data is behind the gear, with the rest of the settings.
        app.navigationBars.buttons["Settings"].tap()
        let add = app.buttons["Add Sample Data"]
        scrollTo(add, in: app)
        add.tap()
        let note = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Added' OR label BEGINSWITH 'Adding sample data failed'")).firstMatch
        XCTAssertTrue(note.waitForExistence(timeout: 120), "no sample data note: \(app.debugDescription)")
        print("E2E sample data: \(note.label)")
        XCTAssertTrue(note.label.hasPrefix("Added"), note.label)
        app.navigationBars.buttons.element(boundBy: 0).tap()

        XCTAssertTrue(syncNow.waitForExistence(timeout: 10), app.debugDescription)
        syncNow.tap()
        sleep(1)
        shot("syncing")
        // The status card says when the run ended, and how.
        let ended = app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Everything synced' OR label CONTAINS \"didn't sync\"")).firstMatch
        XCTAssertTrue(ended.waitForExistence(timeout: 240), "the sync did not end: \(app.debugDescription)")
        XCTAssertTrue(ended.label.contains("Everything synced"), app.debugDescription)
        XCTAssertEqual(app.staticTexts.matching(NSPredicate(format: "label == 'Failed'")).count, 0, app.debugDescription)

        shot("home")

        // Each source's page says what its last run wrote.
        for source in ["contacts", "calendars", "reminders"] {
            app.buttons["source.\(source)"].tap()
            let lastRun = app.staticTexts.matching(NSPredicate(format: "label CONTAINS ' written'")).firstMatch
            XCTAssertTrue(lastRun.waitForExistence(timeout: 10), app.debugDescription)
            print("E2E \(source): \(lastRun.label)")
            shot(source)
            if source == "calendars" {
                let picker = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Calendars,'")).firstMatch
                scrollTo(picker, in: app)
                picker.tap()
                XCTAssertTrue(app.switches.firstMatch.waitForExistence(timeout: 10), "no calendar to choose: \(app.debugDescription)")
                shot("calendar-picker")
                back()
            }
            back()
        }
        // A second sync finds everything in place and writes nothing.
        syncNow.tap()
        _ = app.buttons["Stop"].waitForExistence(timeout: 5)
        XCTAssertTrue(syncNow.waitForExistence(timeout: 240), "the second sync did not end: \(app.debugDescription)")
        for source in ["contacts", "calendars", "reminders"] {
            app.buttons["source.\(source)"].tap()
            let lastRun = app.staticTexts.matching(NSPredicate(format: "label CONTAINS ' written' OR label CONTAINS 'no changes'")).firstMatch
            XCTAssertTrue(lastRun.waitForExistence(timeout: 10), app.debugDescription)
            print("E2E \(source) again: \(lastRun.label)")
            XCTAssertTrue(lastRun.label.contains("no changes"), "the second sync wrote \(source) again: \(lastRun.label)")
            back()
        }

        // Places is off until turned on; the sample data put two visits in
        // its log, and location access was granted before the launch.
        app.buttons["source.places"].tap()
        let places = app.switches["Sync Places"]
        scrollTo(places, in: app)
        // A switch row's middle is its label; the knob is at its trailing end.
        if places.value as? String == "0" { places.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap() }
        let syncPlaces = app.buttons["Sync Places Now"]
        scrollTo(syncPlaces, in: app, up: true)
        syncPlaces.tap()
        let placesRun = app.staticTexts.matching(NSPredicate(format: "label CONTAINS ' visits · '")).firstMatch
        XCTAssertTrue(placesRun.waitForExistence(timeout: 120), "the places sync did not end: \(app.debugDescription)")
        print("E2E places: \(placesRun.label)")
        XCTAssertTrue(placesRun.label.contains(" written"), placesRun.label)
        shot("places")
        back()

        if shots != nil {
            app.buttons["source.health"].tap()
            shot("health")
            back()
            app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Activity'")).firstMatch.tap()
            shot("activity")
            back()
            app.navigationBars.buttons["Settings"].tap()
            shot("settings")
            app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Server Set Up'")).firstMatch.tap()
            shot("settings-setup")
            back()
            back()
        }
        let screen = app.debugDescription
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
