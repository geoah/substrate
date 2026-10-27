import Foundation
import XCTest

@testable import SubstrateSync

/// The apple README's vectors and the calendar ones beside them, minted by
/// `ExternalID` in internal/runner/ids.go.
final class ExternalIDTests: XCTestCase {
    func testContactVectors() {
        let vectors: [(String, String, String)] = [
            ("addressbook", "6f1c2e9a-3b4d-4e5f-8a7b-9c0d1e2f3a4b", "apple-85bb44b13b0b7c81a18d0533e773a814"),
            ("apple-85bb44b13b0b7c81a18d0533e773a814", "410FE041-5C4E-48DA-B4DE-04C15EA3DBAC:ABPerson", "apple-ad8a9ccf185d20237632d37b301b1c22"),
            ("apple-85bb44b13b0b7c81a18d0533e773a814", "177C371E-701D-42F8-A03B-C61CA31627F6", "apple-a418423d223baa867f825ec3d5c95334"),
            ("addressbook", "00000000-0000-4000-8000-000000000001", "apple-daaeb117bc32a2acdd1b67b55be59b48"),
            ("apple-daaeb117bc32a2acdd1b67b55be59b48", "410FE041-5C4E-48DA-B4DE-04C15EA3DBAC:ABPerson", "apple-7c3db4e15d22226e79d94c8405e050e7"),
            ("apple-daaeb117bc32a2acdd1b67b55be59b48", "177C371E-701D-42F8-A03B-C61CA31627F6", "apple-7a08d3ea0f6f98628994fc5367e0272e"),
        ]
        for (account, external, want) in vectors {
            XCTAssertEqual(ExternalID.make("apple", account, external), want, "\(account) / \(external)")
        }
        XCTAssertEqual(AppleIDs(install: "6f1c2e9a-3b4d-4e5f-8a7b-9c0d1e2f3a4b").addressbook, "apple-85bb44b13b0b7c81a18d0533e773a814")
    }

    func testCalendarVectors() {
        let vectors: [(String, String, String)] = [
            ("eventstore", "6f1c2e9a-3b4d-4e5f-8a7b-9c0d1e2f3a4b", "apple-d54c1030b4dd449c901e59a2e6e8363d"),
            ("apple-d54c1030b4dd449c901e59a2e6e8363d", "6A1F0C7E-2B3D-4C5E-9F80-1A2B3C4D5E6F", "apple-9741c2a6831ab27d6d2a39ad8322d8ab"),
            ("apple-d54c1030b4dd449c901e59a2e6e8363d", "1E5D2A9C-7F3B-4A61-8C2D-3B9E0F4A6D17", "apple-f3b6d75ba65841c8f6ad90996fc9fa4a"),
            ("apple-d54c1030b4dd449c901e59a2e6e8363d", "1E5D2A9C-7F3B-4A61-8C2D-3B9E0F4A6D17@2026-10-27T09:00:00Z", "apple-36d872dd7a62b818e80767c319ac6323"),
            ("eventstore", "00000000-0000-4000-8000-000000000001", "apple-3529e6421577eab4ffe30ae2d6ac41d0"),
            ("apple-3529e6421577eab4ffe30ae2d6ac41d0", "6A1F0C7E-2B3D-4C5E-9F80-1A2B3C4D5E6F", "apple-d523d365336e82cafd613cd6be3c6572"),
            ("apple-3529e6421577eab4ffe30ae2d6ac41d0", "1E5D2A9C-7F3B-4A61-8C2D-3B9E0F4A6D17", "apple-731c06c8577a6fee5c2e34570677a37a"),
            ("apple-3529e6421577eab4ffe30ae2d6ac41d0", "1E5D2A9C-7F3B-4A61-8C2D-3B9E0F4A6D17@2026-10-27T09:00:00Z", "apple-0411aa3e3918115d2311cdbf83311ecb"),
        ]
        for (account, external, want) in vectors {
            XCTAssertEqual(ExternalID.make("apple", account, external), want, "\(account) / \(external)")
        }
        let ids = AppleIDs(install: "6f1c2e9a-3b4d-4e5f-8a7b-9c0d1e2f3a4b")
        XCTAssertEqual(ids.eventstore, "apple-d54c1030b4dd449c901e59a2e6e8363d")
        XCTAssertEqual(ids.calendar("6A1F0C7E-2B3D-4C5E-9F80-1A2B3C4D5E6F"), "apple-9741c2a6831ab27d6d2a39ad8322d8ab")
    }

    func testEventKeys() throws {
        let item = "1E5D2A9C-7F3B-4A61-8C2D-3B9E0F4A6D17"
        let occurrence = try XCTUnwrap(ISO8601DateFormatter().date(from: "2026-10-27T11:00:00+02:00"))
        // A single event is its item identifier, whatever its date.
        XCTAssertEqual(AppleIDs.eventKey(item: item, occurrence: occurrence, repeats: false), item)
        // An occurrence of a series adds its original start, in UTC.
        let key = AppleIDs.eventKey(item: item, occurrence: occurrence, repeats: true)
        XCTAssertEqual(key, item + "@2026-10-27T09:00:00Z")
        let ids = AppleIDs(install: "6f1c2e9a-3b4d-4e5f-8a7b-9c0d1e2f3a4b")
        XCTAssertEqual(ids.event(key: key), "apple-36d872dd7a62b818e80767c319ac6323")
        XCTAssertEqual(ids.event(key: item), "apple-f3b6d75ba65841c8f6ad90996fc9fa4a")
    }
}

final class ContactMappingTests: XCTestCase {
    func testBuiltInLabelsAreEnglishWords() {
        XCTAssertEqual(ContactMapping.englishLabel("_$!<Mobile>!$_"), "mobile")
        XCTAssertEqual(ContactMapping.englishLabel("_$!<HomeFAX>!$_"), "home fax")
        XCTAssertEqual(ContactMapping.englishLabel("_$!<WorkFAX>!$_"), "work fax")
        XCTAssertEqual(ContactMapping.englishLabel("_$!<Home>!$_"), "home")
        XCTAssertEqual(ContactMapping.englishLabel("_$!<HomePage>!$_"), "homepage")
        XCTAssertEqual(ContactMapping.englishLabel("_$!<Anniversary>!$_"), "anniversary")
        XCTAssertEqual(ContactMapping.englishLabel("_$!<Other>!$_"), "other")
    }

    func testCustomLabelsAreKeptAsTyped() {
        XCTAssertEqual(ContactMapping.englishLabel("iPhone"), "iPhone")
        XCTAssertEqual(ContactMapping.englishLabel("Lab Bench"), "Lab Bench")
        XCTAssertEqual(ContactMapping.englishLabel("_$!<"), "_$!<")
    }

    /// Only what Go's net/mail.ParseAddress accepts is sent; an address it
    /// would refuse would refuse the whole contact.
    func testEmailRule() {
        for good in ["ada@example.com", "a.b+tag@sub.example.co", "ü@例え.jp", "x@localhost"] {
            XCTAssertTrue(ContactMapping.isMailbox(good), good)
        }
        for bad in ["not an address", "a@@b", "@example.com", "a@", ".a@b", "a.@b", "a..b@c", "a b@c", "a@b,c", "\"q\"@b", "<a@b>"] {
            XCTAssertFalse(ContactMapping.isMailbox(bad), bad)
        }
    }
}

final class FormatTests: XCTestCase {
    func testHealthDates() throws {
        let date = try XCTUnwrap(ISO8601DateFormatter().date(from: "2026-10-07T05:05:37Z"))
        XCTAssertEqual(Formats.health(date, in: TimeZone(identifier: "Europe/Athens")!), "2026-10-07 08:05:37 +0300")
        XCTAssertEqual(Formats.health(date, in: TimeZone(identifier: "UTC")!), "2026-10-07 05:05:37 +0000")
        XCTAssertEqual(Formats.health(date, in: TimeZone(identifier: "America/St_Johns")!), "2026-10-07 02:35:37 -0230")
        XCTAssertEqual(Formats.utc(date), "2026-10-07T05:05:37Z")
        XCTAssertEqual(Formats.day(date, in: TimeZone(identifier: "Pacific/Auckland")!), "2026-10-07")
    }

    func testNumbers() {
        XCTAssertEqual(JSON.number(72), "72")
        XCTAssertEqual(JSON.number(72.5), "72.5")
        XCTAssertEqual(JSON.number(0.1), "0.1")
        XCTAssertEqual(JSON.number(-3), "-3")
        XCTAssertEqual(JSON.number(.nan), "null")
        XCTAssertEqual(JSON.object([("a", .string("Ada’s\u{00a0}\"/"))]).text, "{\"a\":\"Ada’s\u{00a0}\\\"/\"}")
    }
}

final class HealthScheduleTests: XCTestCase {
    func testFirstSyncBackfillsThenResendsTheLastTwoDays() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "Europe/Athens")!
        let today = try XCTUnwrap(ISO8601DateFormatter().date(from: "2026-10-20T10:00:00Z"))
        let names = { (days: [Date]) in days.map { Formats.day($0, in: calendar.timeZone) } }

        let first = names(SyncEngine.healthDays(today: today, backfill: 7, sent: [], calendar: calendar))
        XCTAssertEqual(first, ["2026-10-14", "2026-10-15", "2026-10-16", "2026-10-17", "2026-10-18", "2026-10-19", "2026-10-20"])

        let later = names(SyncEngine.healthDays(today: today, backfill: 7, sent: Set(first).subtracting(["2026-10-16"]), calendar: calendar))
        XCTAssertEqual(later, ["2026-10-16", "2026-10-18", "2026-10-19", "2026-10-20"])
    }

    func testQueuedDaysGoFirstAndOnlyOnce() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "Europe/Athens")!
        let today = try XCTUnwrap(ISO8601DateFormatter().date(from: "2026-10-20T10:00:00Z"))
        let sent: Set<String> = ["2026-10-18", "2026-10-17", "2026-10-16", "2026-10-15", "2026-10-14"]
        let days = SyncEngine.healthDays(
            today: today, backfill: 7, sent: sent, queued: ["2024-03-02", "2024-03-01", "2026-10-19", "2026-10-25"],
            calendar: calendar
        ).map { Formats.day($0, in: calendar.timeZone) }
        // A queued day already due is not sent twice, and one in the future never.
        XCTAssertEqual(days, ["2024-03-01", "2024-03-02", "2026-10-18", "2026-10-19", "2026-10-20"])
    }

    func testCoverageBucketsCountSyncedAgainstAvailable() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "Europe/Athens")!
        let available: Set<String> = ["2026-09-29", "2026-09-30", "2026-10-01", "2026-10-02", "2025-01-15"]
        let synced: Set<String> = ["2026-09-30", "2026-10-02", "2026-10-03"]
        let months = Coverage.days(available: available, synced: synced, by: .month, calendar: calendar)
        XCTAssertEqual(months.map(\.label), ["January 2025", "September 2026", "October 2026"])
        XCTAssertEqual(months.map(\.available), [1, 2, 2])
        XCTAssertEqual(months.map(\.synced), [0, 1, 1])
        XCTAssertEqual(months.map(\.missing), [["2025-01-15"], ["2026-09-29"], ["2026-10-01"]])
        let years = Coverage.days(available: available, synced: synced, by: .year, calendar: calendar)
        XCTAssertEqual(years.map(\.label), ["2025", "2026"])
        XCTAssertEqual(years.last?.fraction, 0.5)
    }

    func testABodyTooBigSplitsByMetricAndNeverSplitsOne() {
        let rows = HealthRows(zone: TimeZone(identifier: "UTC")!)
        let start = Date(timeIntervalSince1970: 1_791_331_200)
        func metric(_ name: String) -> HealthMetric {
            HealthMetric(name: name, units: "count", rows: (0 ..< 100).map {
                rows.quantity(at: start.addingTimeInterval(Double($0 * 60)), qty: Double($0), sources: ["Ada’s iPhone"])
            })
        }
        let delivery = HealthDelivery(grouping: .minutes, metrics: [metric("step_count"), metric("flights_climbed"), metric("height")], workouts: [])
        XCTAssertEqual(delivery.bodies().count, 1)
        let one = HealthDelivery.body(metrics: [metric("step_count")], workouts: []).count
        let split = delivery.bodies(limit: one + 10)
        XCTAssertEqual(split.count, 3)
        for body in split {
            let object = try? JSONSerialization.jsonObject(with: body) as? [String: Any]
            let metrics = (object?["data"] as? [String: Any])?["metrics"] as? [[String: Any]]
            XCTAssertEqual(metrics?.count, 1)
            XCTAssertEqual((metrics?.first?["data"] as? [Any])?.count, 100)
        }
    }

    /// A night across midnight is one night in both days' deliveries: each
    /// day sends the segments that start in it, with the same night fields.
    func testANightAcrossMidnightCarriesTheSameFieldsInBothDays() throws {
        let zone = TimeZone(identifier: "Europe/Athens")!
        let r = HealthRows(zone: zone)
        let f = ISO8601DateFormatter()
        func d(_ s: String) -> Date { f.date(from: s)! }
        let segments = [
            SleepSegment(start: d("2026-10-06T20:10:00Z"), end: d("2026-10-07T03:40:00Z"), value: "In Bed", source: "Ada’s iPhone"),
            SleepSegment(start: d("2026-10-06T20:30:00Z"), end: d("2026-10-06T21:30:00Z"), value: "Core", source: "Ada’s Apple Watch"),
            SleepSegment(start: d("2026-10-06T21:30:00Z"), end: d("2026-10-07T03:30:00Z"), value: "Deep", source: "Ada’s Apple Watch"),
        ]
        let oct6 = d("2026-10-05T21:00:00Z") ..< d("2026-10-06T21:00:00Z")
        let oct7 = d("2026-10-06T21:00:00Z") ..< d("2026-10-07T21:00:00Z")
        let first = SleepNights.rows(all: segments, day: oct6, rows: r).map(\.text)
        let second = SleepNights.rows(all: segments, day: oct7, rows: r).map(\.text)
        XCTAssertEqual(first.count, 2)
        XCTAssertEqual(second.count, 1)
        XCTAssertFalse(first[0].contains("sleepStart"), "the In Bed segment carries no night fields")
        for row in [first[1], second[0]] {
            XCTAssertTrue(row.contains("\"sleepStart\":\"2026-10-06 23:30:00 +0300\""), row)
            XCTAssertTrue(row.contains("\"sleepEnd\":\"2026-10-07 06:30:00 +0300\""), row)
            XCTAssertTrue(row.contains("\"inBedStart\":\"2026-10-06 23:10:00 +0300\""), row)
            XCTAssertTrue(row.contains("\"inBed\":7.5"), row)
        }
    }
}

/// The deliveries the app sends, held byte for byte against the goldens the
/// Go suite runs through the provider and the healthapple adapter
/// (internal/providertest/substratesync_test.go). Regenerate with
/// `TEST_RUNNER_SUBSTRATE_UPDATE_GOLDEN=1` in front of `xcodebuild test`.
final class HealthGoldenTests: XCTestCase {
    static let athens = TimeZone(identifier: "Europe/Athens")!
    static let install = "6f1c2e9a-3b4d-4e5f-8a7b-9c0d1e2f3a4b"
    static let watch = "Ada’s Apple\u{00a0}Watch"
    static let phone = "Ada’s iPhone"

    static func at(_ local: String) -> Date {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = athens
        f.dateFormat = "yyyy-MM-dd HH:mm:ss"
        return f.date(from: local)!
    }

    static var minutes: HealthDelivery {
        let r = HealthRows(zone: athens)
        func q(_ name: String, _ units: String, _ rows: [(String, Double, [String])], extra: [String: [(String, JSON)]] = [:]) -> HealthMetric {
            HealthMetric(name: name, units: units, rows: rows.map { r.quantity(at: at($0.0), qty: $0.1, sources: $0.2, extra: extra[$0.0] ?? []) })
        }
        let both = [watch, phone]
        return HealthDelivery(grouping: .minutes, metrics: [
            HealthMetric(name: "heart_rate", units: "count/min", rows: [
                r.minMaxAvg(at: at("2026-10-07 06:58:00"), min: 55, avg: 56.5, max: 58, sources: [watch]),
                r.minMaxAvg(at: at("2026-10-07 07:03:00"), min: 61, avg: 63.333333333, max: 66, sources: [watch]),
                r.minMaxAvg(at: at("2026-10-07 07:07:00"), min: 70, avg: 72, max: 75, sources: [watch]),
                r.minMaxAvg(at: at("2026-10-07 07:15:00"), min: 72, avg: 74.5, max: 77, sources: [watch]),
                r.minMaxAvg(at: at("2026-10-07 18:20:00"), min: 141, avg: 148.25, max: 155, sources: [watch]),
            ]),
            q("step_count", "count", [
                ("2026-10-07 07:02:00", 47, [phone]), ("2026-10-07 07:03:00", 52.5, both), ("2026-10-07 07:07:00", 30, [phone]),
            ]),
            q("walking_running_distance", "km", [
                ("2026-10-07 07:02:00", 0.034, [phone]), ("2026-10-07 07:03:00", 0.0381, both), ("2026-10-07 07:07:00", 0.0215, [phone]),
            ]),
            q("flights_climbed", "count", [("2026-10-07 07:03:00", 1, [phone])]),
            q("active_energy", "kcal", [
                ("2026-10-07 07:02:00", 2.1, [watch]), ("2026-10-07 07:03:00", 2.6, [watch]), ("2026-10-07 07:07:00", 1.4, [watch]),
            ]),
            q("basal_energy_burned", "kcal", (0 ..< 5).map { ("2026-10-07 07:0\($0):00", 1.15, [watch]) }),
            q("resting_heart_rate", "count/min", [("2026-10-07 00:00:00", 56, [watch])]),
            q("heart_rate_variability", "ms", [("2026-10-07 03:10:00", 48.2, [watch])]),
            q("respiratory_rate", "count/min", [("2026-10-07 03:00:00", 14.2, [watch]), ("2026-10-07 03:15:00", 14.6, [watch])]),
            q("blood_oxygen_saturation", "%", [("2026-10-07 03:02:00", 97, [watch]), ("2026-10-07 03:12:00", 96, [watch])]),
            q("vo2_max", "ml/(kg·min)", [("2026-10-07 18:43:00", 44.2, [watch])]),
            q("weight_body_mass", "kg", [("2026-10-07 07:01:00", 71.4, ["Withings"])]),
            q("body_fat_percentage", "%", [("2026-10-07 07:01:00", 18.2, ["Withings"])]),
            q("body_mass_index", "count", [("2026-10-07 07:01:00", 22.6, ["Withings"])]),
            q("height", "cm", [("2026-10-07 07:01:00", 177.5, [phone])]),
            q("apple_sleeping_wrist_temperature", "degC", [("2026-10-07 06:50:00", 35.62, [watch])]),
            q("blood_glucose", "mg/dL", [("2026-10-07 07:30:00", 92, ["Contour"]), ("2026-10-07 13:45:00", 128, ["Contour"])], extra: [
                "2026-10-07 07:30:00": [("mealTime", .string("Before Meal"))],
                "2026-10-07 13:45:00": [("mealTime", .string("After Meal"))],
            ]),
            HealthMetric(name: "blood_pressure", units: "mmHg", rows: [
                r.pressure(at: at("2026-10-07 07:15:00"), systolic: 118, diastolic: 76, sources: ["Withings"]),
            ]),
        ], workouts: [])
    }

    static var seconds: HealthDelivery {
        let r = HealthRows(zone: athens)
        var segments = [SleepSegment(start: at("2026-10-07 00:12:00"), end: at("2026-10-07 07:02:00"), value: "In Bed", source: phone)]
        var t = at("2026-10-07 00:12:00")
        for (value, minutes) in [("Awake", 14), ("Core", 70), ("Deep", 45), ("Core", 60), ("REM", 30), ("Awake", 6),
                                 ("Core", 80), ("Deep", 25), ("REM", 40), ("Asleep", 30), ("Awake", 10)] {
            let end = t.addingTimeInterval(Double(minutes * 60))
            segments.append(SleepSegment(start: t, end: end, value: value, source: watch))
            t = end
        }
        let day = at("2026-10-07 00:00:00") ..< at("2026-10-08 00:00:00")
        var run = WorkoutSummary(
            id: UUID(uuidString: "5B1F7A2C-3D4E-4F60-8A9B-0C1D2E3F4A5B")!, name: "Outdoor Run",
            start: at("2026-10-07 18:05:00"), end: at("2026-10-07 18:41:30"), isIndoor: false)
        run.activeKcal = 412.4
        run.distanceKm = 6.1
        run.heartRate = (118, 148.25, 171)
        run.elevationUpM = 42
        run.timeZone = "Europe/Athens"
        let runStart = at("2026-10-07 18:05:00")
        run.heartRateData = (0 ..< 37).map { (m: Int) -> WorkoutSummary.Minute in
            let base = Double(118 + m)
            return WorkoutSummary.Minute(
                at: runStart.addingTimeInterval(Double(m) * 60), min: base, avg: base + 3.5, max: base + 6, sources: [watch])
        }
        var lift = WorkoutSummary(
            id: UUID(uuidString: "9E8D7C6B-5A49-4382-9170-6F5E4D3C2B1A")!, name: "Traditional Strength Training",
            start: at("2026-10-07 19:30:00"), end: at("2026-10-07 20:15:00"), isIndoor: true)
        lift.activeKcal = 210
        lift.heartRate = (88, 112, 139)
        let liftStart = at("2026-10-07 19:30:00")
        lift.heartRateData = (0 ..< 3).map { (m: Int) -> WorkoutSummary.Minute in
            WorkoutSummary.Minute(at: liftStart.addingTimeInterval(Double(m) * 60), min: 88, avg: 95, max: 101, sources: [watch])
        }
        return HealthDelivery(
            grouping: .seconds,
            metrics: [HealthMetric(name: "sleep_analysis", units: "hr", rows: SleepNights.rows(all: segments, day: day, rows: r))],
            workouts: [run.json(r), lift.json(r)])
    }

    /// The request as the webhook door hands it to the function: the
    /// headers the trigger forwards, lowercased, and the body as text.
    static func envelope(_ delivery: HealthDelivery, automation: String, session: String, note: String) -> [String: Any] {
        let body = delivery.bodies()
        precondition(body.count == 1)
        var headers: [String: String] = [:]
        for (name, value) in HealthDelivery.headers(
            grouping: delivery.grouping, key: "<webhook key>", account: "substrate-sync-6f1c2e9a",
            automationID: automation, sessionID: session) where name != "Authorization" {
            headers[name.lowercased()] = value
        }
        return [
            "_fixture": ["note": note],
            "request": [
                "method": "POST", "contentType": "application/json", "headers": headers, "query": [String: Any](),
                "body": ["text": String(decoding: body[0], as: UTF8.self)],
            ],
        ]
    }

    static func golden(_ name: String) -> URL {
        URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("internal/providertest/testdata/applehealth/\(name)")
    }

    func check(_ name: String, _ envelope: [String: Any]) throws {
        let url = Self.golden(name)
        if ProcessInfo.processInfo.environment["SUBSTRATE_UPDATE_GOLDEN"] == "1" {
            let data = try JSONSerialization.data(withJSONObject: envelope, options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes])
            try (data + Data("\n".utf8)).write(to: url)
            return
        }
        let stored = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any], "\(url.path) is missing")
        let want = try XCTUnwrap(stored["request"] as? [String: Any])
        let got = try XCTUnwrap(envelope["request"] as? [String: Any])
        XCTAssertEqual((got["body"] as? [String: String])?["text"], (want["body"] as? [String: String])?["text"], "\(name): the body drifted")
        XCTAssertEqual(got["headers"] as? [String: String], want["headers"] as? [String: String], "\(name): the headers drifted")
    }

    func testMinutesDelivery() throws {
        try check("app_minutes.json", Self.envelope(
            Self.minutes, automation: "0B7E4D2A-6C1F-4E8B-9A3D-5F2C7B1E8D40", session: "C3A1F5E7-2B4D-4F6A-8C0E-1D3B5F7A9C2E",
            note: "Substrate Sync's minutes delivery for one day, from its encoder (web/apple/SubstrateSyncTests). Every metric the app reads, at one row per minute."))
    }

    func testSecondsDelivery() throws {
        try check("app_seconds.json", Self.envelope(
            Self.seconds, automation: "7D2E9F41-8A3B-4C5D-B6E7-F80912A3B4C5", session: "C3A1F5E7-2B4D-4F6A-8C0E-1D3B5F7A9C2E",
            note: "Substrate Sync's seconds delivery for one day, from its encoder (web/apple/SubstrateSyncTests). Sleep segments with their night's fields, and two workouts."))
    }
}
