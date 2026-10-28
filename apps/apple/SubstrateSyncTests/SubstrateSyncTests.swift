import Foundation
import HealthKit
import XCTest

@testable import SubstrateSync

/// The apple README's vectors and the calendar ones beside them, minted by
/// `ExternalID` in internal/runner/ids.go.
final class ExternalIDTests: XCTestCase {
    /// The apple README's reminder, visit and share vectors.
    func testReminderVisitAndShareVectors() {
        let ids = AppleIDs(install: "6f1c2e9a-3b4d-4e5f-8a7b-9c0d1e2f3a4b")
        XCTAssertEqual(ids.calendar("4C9E1B2A-7D3F-4E50-8A61-B2C3D4E5F607"), "apple-22767140db30b90972f627ba203de320")
        XCTAssertEqual(ids.reminder("8F2A6C1D-3E4B-4F5A-9B6C-7D8E9F0A1B2C"), "apple-a45e5e9da9e98130ec4d269d8a1aba19")
        XCTAssertEqual(ids.visitstore, "apple-4e3738eb0c9436e7b08bdd1a8b68ef08")
        XCTAssertEqual(ids.visit("arrival:2026-10-27T08:15:00Z"), "apple-847ec3b8965f9037b5a6e52ae28acb5c")
        XCTAssertEqual(ids.visit("departure:2026-10-27T18:40:00Z"), "apple-f22ab5f3787cd3f9a3076e3a0332c710")
        XCTAssertEqual(ExternalID.make("apple", "share", "https://example.com/article"), "apple-31a4b492dfd8bc6e77aea4fa0e31eb7e")
    }

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

/// A delivery the door stored is not yet a delivery the provider filed: a
/// day counts as sent only when every one of its deliveries has a receipt,
/// and a parked one sends the day again with the server's reason.
final class HealthSettleTests: XCTestCase {
    func testADayIsSentOnlyWhenEveryDeliveryIsFiled() {
        let fires = ["2026-10-20": ["hook-a", "hook-b"], "2026-10-21": ["hook-c", "hook-d"], "2026-10-22": ["hook-e"]]
        let out = SyncEngine.settle(
            fires, filed: ["hook-a", "hook-b", "hook-c"],
            parked: ["hook-e": "the delivery names account 'x' and no account record has that id"])
        XCTAssertEqual(out.sent, ["2026-10-20"])
        XCTAssertEqual(out.pending, ["2026-10-21"])
        XCTAssertEqual(out.failed, ["2026-10-22": "the delivery names account 'x' and no account record has that id"])
    }

    func testAParkedDeliveryFailsItsDayEvenWhenTheOtherFiled() {
        let out = SyncEngine.settle(["2026-10-20": ["hook-a", "hook-b"]], filed: ["hook-a"], parked: ["hook-b": "refused"])
        XCTAssertEqual(out.sent, [])
        XCTAssertEqual(out.failed["2026-10-20"], "refused")
    }

    /// The parked list carries the requests the server accepted and has not
    /// run, and the ones it is running: neither is a refusal.
    func testOnlyARefusalInTheParkedListFailsADay() {
        let items = [
            Client.Parked(fireId: "hook-a", lastError: "delivery accepted: the fire has not settled, and a restart resumes it"),
            Client.Parked(fireId: "hook-b", lastError: "run: runner: Refused: the body is not JSON", running: true),
            Client.Parked(fireId: "hook-c", lastError: "run: runner: Refused: the body is not JSON"),
            Client.Parked(fireId: "hook-z", lastError: "another sender's refusal"),
        ]
        XCTAssertEqual(SyncEngine.refusals(items, among: ["hook-a", "hook-b", "hook-c"]),
                       ["hook-c": "run: runner: Refused: the body is not JSON"])
    }

    /// A source is due by when it last ran, not when it last succeeded: a
    /// failing source is retried at the owner's frequency, not at every wake.
    func testAFailingSourceIsNotDueAgainBeforeTheInterval() {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        var state = LocalState()
        XCTAssertEqual(SyncEngine.due(state, only: [.health], interval: 3600, now: now), [.health])
        state.status[.health] = SourceStatus(lastSync: nil, counts: nil, lastError: "refused")
        state.record(SyncRun(source: .health, started: now - 70, finished: now - 60, summary: nil, error: "refused"))
        XCTAssertEqual(SyncEngine.due(state, only: [.health], interval: 3600, now: now), [])
        XCTAssertEqual(SyncEngine.due(state, only: [.health], interval: 3600, now: now + 3600), [.health])
    }
}

/// Every copy of the state names one install, and so one Health account,
/// address book and event store, before anything was ever saved.
final class InstallIDTests: XCTestCase {
    func testEveryCopyOfTheStateNamesOneInstall() {
        let a = LocalState(), b = LocalState.load(), c = LocalState()
        XCTAssertEqual(a.install, b.install)
        XCTAssertEqual(b.install, c.install)
        XCTAssertEqual(a.healthAccount, c.healthAccount)
        XCTAssertEqual(a.ids.addressbook, b.ids.addressbook)
        XCTAssertEqual(InstallID.current, Keychain.string(account: "install"))
        XCTAssertEqual(UUID(uuidString: InstallID.current)?.uuidString.lowercased(), InstallID.current)
    }
}

/// HealthKit answers a query over a type the device has no source of
/// ("Unable to invalidate interval: no data source available") with its
/// no-data error; a sync reads that as nothing, and any other error stays an
/// error.
final class HealthNoDataTests: XCTestCase {
    func testNoDataIsEmpty() async throws {
        let none: Int? = try await noDataIsEmpty { throw HKError(.errorNoData) }
        XCTAssertNil(none)
        let bridged: Int? = try await noDataIsEmpty {
            throw NSError(domain: HKErrorDomain, code: HKError.Code.errorNoData.rawValue,
                          userInfo: [NSLocalizedDescriptionKey: "Unable to invalidate interval: no data source available."])
        }
        XCTAssertNil(bridged)
        let some = try await noDataIsEmpty { 7 }
        XCTAssertEqual(some, 7)
        do {
            let _: Int? = try await noDataIsEmpty { throw HKError(.errorAuthorizationDenied) }
            XCTFail("an authorization error read as no data")
        } catch {}
    }
}

final class HealthScheduleTests: XCTestCase {
    func testFirstSyncSendsTheRangeThenResendsTheLastThreeDays() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "Europe/Athens")!
        let today = try XCTUnwrap(ISO8601DateFormatter().date(from: "2026-10-20T10:00:00Z"))
        let start = try XCTUnwrap(calendar.date(byAdding: .day, value: -6, to: calendar.startOfDay(for: today)))
        let names = { (days: [Date]) in days.map { Formats.day($0, in: calendar.timeZone) } }

        let first = names(SyncEngine.healthDays(today: today, from: start, sent: [], calendar: calendar))
        XCTAssertEqual(first, ["2026-10-14", "2026-10-15", "2026-10-16", "2026-10-17", "2026-10-18", "2026-10-19", "2026-10-20"])

        let later = names(SyncEngine.healthDays(today: today, from: start, sent: Set(first).subtracting(["2026-10-16"]), calendar: calendar))
        XCTAssertEqual(later, ["2026-10-16", "2026-10-18", "2026-10-19", "2026-10-20"])
    }

    func testQueuedDaysGoFirstAndOnlyOnce() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "Europe/Athens")!
        let today = try XCTUnwrap(ISO8601DateFormatter().date(from: "2026-10-20T10:00:00Z"))
        let start = try XCTUnwrap(calendar.date(byAdding: .day, value: -6, to: calendar.startOfDay(for: today)))
        let sent: Set<String> = ["2026-10-18", "2026-10-17", "2026-10-16", "2026-10-15", "2026-10-14"]
        let days = SyncEngine.healthDays(
            today: today, from: start, sent: sent, queued: ["2024-03-02", "2024-03-01", "2026-10-19", "2026-10-25"],
            calendar: calendar
        ).map { Formats.day($0, in: calendar.timeZone) }
        // A queued day already due is not sent twice, and one in the future never.
        XCTAssertEqual(days, ["2024-03-01", "2024-03-02", "2026-10-18", "2026-10-19", "2026-10-20"])
    }

    /// Over a long range only the days that hold data are read, and the last
    /// three days always are, data or not; everything, with no start, begins
    /// at the first day that holds data.
    func testALongRangeReadsOnlyDaysWithData() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "Europe/Athens")!
        let today = try XCTUnwrap(ISO8601DateFormatter().date(from: "2026-10-20T10:00:00Z"))
        let available: Set<String> = ["2023-03-01", "2024-06-15", "2026-10-10"]
        let names = { (days: [Date]) in days.map { Formats.day($0, in: calendar.timeZone) } }

        let year = names(SyncEngine.healthDays(
            today: today, from: Settings.History.year1.start(today: today, calendar: calendar),
            sent: [], available: available, calendar: calendar))
        XCTAssertEqual(year, ["2026-10-10", "2026-10-18", "2026-10-19", "2026-10-20"])

        let all = names(SyncEngine.healthDays(today: today, from: nil, sent: ["2024-06-15"], available: available, calendar: calendar))
        XCTAssertEqual(all, ["2023-03-01", "2026-10-10", "2026-10-18", "2026-10-19", "2026-10-20"])
    }

    func testHistoryRangesStartWhereTheyNameAndEverythingHasNoStart() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let today = try XCTUnwrap(ISO8601DateFormatter().date(from: "2026-10-20T10:00:00Z"))
        let day = { (h: Settings.History) in h.start(today: today, calendar: calendar).map { Formats.day($0, in: calendar.timeZone) } }
        XCTAssertEqual(day(.days14), "2026-10-07")
        XCTAssertEqual(day(.days30), "2026-09-21")
        XCTAssertEqual(day(.months3), "2026-07-20")
        XCTAssertEqual(day(.months6), "2026-04-20")
        XCTAssertEqual(day(.year1), "2025-10-20")
        XCTAssertNil(day(.all))
    }

    /// A stop is not a failure, and a refused token is the end of the
    /// session, however deep in a context error either arrives.
    func testStopsAndRefusedTokensAreToldApart() {
        XCTAssertTrue(isCancellation(CancellationError()))
        XCTAssertTrue(isCancellation(URLError(.cancelled)))
        XCTAssertTrue(isCancellation(ContextError(context: "Sending", underlying: URLError(.cancelled))))
        XCTAssertFalse(isCancellation(URLError(.timedOut)))
        let refused = ServerError(status: 401, data: Data(#"{"error":{"message":"invalid token"}}"#.utf8))
        XCTAssertTrue(isSignedOut(refused))
        XCTAssertTrue(isSignedOut(ContextError(context: "Sending", underlying: refused)))
        XCTAssertFalse(isSignedOut(ServerError(status: 403, data: Data())))
        XCTAssertEqual(refused.text, "invalid token")
    }

    /// A workout paused for a quarter of an hour lasts what HealthKit says,
    /// not its end minus its start.
    func testAPausedWorkoutKeepsHealthKitsDuration() throws {
        let start = Date(timeIntervalSince1970: 1_791_331_200)
        var w = WorkoutSummary(id: UUID(), name: "Outdoor Run", start: start, end: start.addingTimeInterval(3600), isIndoor: false)
        w.duration = 2700
        let json = w.json(HealthRows(zone: TimeZone(identifier: "UTC")!))
        XCTAssertEqual(json.objectValue("duration"), .double(2700))
        w.duration = nil
        XCTAssertEqual(w.json(HealthRows(zone: TimeZone(identifier: "UTC")!)).objectValue("duration"), .double(3600))
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

/// A reminder due on a day names the day alone; one due at a time names the
/// day and the moment, in the zone its components carry.
final class ReminderMappingTests: XCTestCase {
    func testADueDayAndADueMoment() {
        let day = DateComponents(year: 2026, month: 10, day: 28)
        XCTAssertEqual(ReminderMapping.moments(day, zone: nil).day, "2026-10-28")
        XCTAssertNil(ReminderMapping.moments(day, zone: nil).moment)
        // Athens is UTC+2 by then: summer time ended on 25 October.
        var timed = DateComponents(year: 2026, month: 10, day: 28, hour: 10, minute: 30)
        timed.timeZone = TimeZone(identifier: "Europe/Athens")
        XCTAssertEqual(ReminderMapping.moments(timed, zone: nil).day, "2026-10-28")
        XCTAssertEqual(ReminderMapping.moments(timed, zone: nil).moment, "2026-10-28T08:30:00Z")
        XCTAssertNil(ReminderMapping.moments(nil, zone: nil).day)
    }
}

/// A workout's track is thinned to what the provider stores, keeping its
/// ends; a short one goes whole.
final class WorkoutRouteTests: XCTestCase {
    func testALongTrackIsThinnedAndKeepsItsEnds() {
        let t0 = Date(timeIntervalSince1970: 1_800_000_000)
        let points = (0 ..< 40_000).map { RoutePoint(at: t0 + Double($0), latitude: 37 + Double($0) * 1e-6, longitude: 23) }
        let thinned = WorkoutRoute.thinned(points)
        XCTAssertLessThanOrEqual(thinned.count, WorkoutRoute.limit)
        XCTAssertGreaterThan(thinned.count, WorkoutRoute.limit / 2)
        XCTAssertEqual(thinned.first?.at, points.first?.at)
        XCTAssertEqual(thinned.last?.at, points.last?.at)
        XCTAssertEqual(WorkoutRoute.thinned(Array(points.prefix(100))).count, 100)
    }
}

/// Errors in sentences a person can act on, and the rule that stops a run
/// once a batch shows the server is out of reach.
final class ErrorTests: XCTestCase {
    let server = URL(string: "https://substrate.example.com")!

    func testTheNetworksErrorsAreSaidPlainly() {
        XCTAssertEqual(explain(URLError(.timedOut), server: server), "Substrate.example.com took too long to answer.")
        XCTAssertEqual(explain(URLError(.notConnectedToInternet), server: server), "This phone is offline.")
        XCTAssertEqual(explain(URLError(.cannotConnectToHost), server: server), "Substrate.example.com isn't answering.")
        XCTAssertEqual(explain(URLError(.cannotFindHost), server: server), "Couldn't find substrate.example.com. Check the address.")
        XCTAssertEqual(explain(URLError(.timedOut)), "The server took too long to answer.")
    }

    func testTheServersOwnWordsAreKept() {
        let refusal = ServerError(status: 409, data: Data(#"{"error":{"message":"the trigger is taken"}}"#.utf8))
        XCTAssertEqual(explain(refusal, server: server), "the trigger is taken")
        let signedOut = ServerError(status: 401, data: Data())
        XCTAssertEqual(explain(signedOut, server: server), "Your sign-in is no longer valid. Sign in again.")
        let broken = ServerError(status: 502, data: Data("Bad Gateway".utf8))
        XCTAssertTrue(explain(broken, server: server).hasPrefix("Substrate.example.com ran into a problem (502)"))
        XCTAssertTrue(timedOut(URLError(.timedOut)))
        XCTAssertFalse(timedOut(refusal))
    }

    func testABatchLostToTheNetworkStopsTheRunAndARefusalDoesNot() {
        let lost: [Error] = [URLError(.timedOut), URLError(.cannotConnectToHost)]
        XCTAssertNotNil(SyncEngine.unreachable(lost, of: 2))
        // Some rows went through: the server answers.
        XCTAssertNil(SyncEngine.unreachable(lost, of: 3))
        // The server refused: it answers, and the refusal is the row's.
        XCTAssertNil(SyncEngine.unreachable([URLError(.timedOut), ServerError(status: 400, data: Data())], of: 2))
        XCTAssertNil(SyncEngine.unreachable([], of: 0))
        // A refused token stops the run at once, however the rest fared.
        let signedOut = ServerError(status: 401, data: Data())
        XCTAssertNotNil(SyncEngine.unreachable([signedOut], of: 50))
        XCTAssertFalse(isUnreachable(URLError(.cancelled)))
        XCTAssertFalse(isUnreachable(signedOut))
    }

    func testAWebhookRefusalIsNotASignOutAndALoginRefusalIsNotAnExpiry() {
        XCTAssertFalse(isSignedOut(WebhookRefused(text: "bad key")))
        XCTAssertFalse(isSignedOut(ContextError(context: "Sending 2026-10-20", underlying: WebhookRefused(text: "bad key"))))
        XCTAssertEqual(LoginRefused(server: ServerError(status: 401, data: Data())).localizedDescription,
                       "The repository name, password or code was not accepted.")
        let worded = ServerError(status: 401, data: Data(#"{"error":{"message":"invalid credentials"}}"#.utf8))
        XCTAssertEqual(LoginRefused(server: worded).localizedDescription, "invalid credentials")
        XCTAssertEqual(explain(CancellationError()), "Stopped.")
    }

    func testAProxysHTMLPageIsNotShown() {
        let page = ServerError(status: 502, data: Data("<html><body><h1>502 Bad Gateway</h1></body></html>".utf8))
        XCTAssertEqual(page.text, "The server answered 502.")
    }

    func testAWebhookKeyIsThirtyTwoCharactersOfItsAlphabet() {
        let key = AppModel.newKey()
        XCTAssertEqual(key.count, 32)
        XCTAssertTrue(key.allSatisfy { $0.isLetter || $0.isNumber || $0 == "_" || $0 == "-" })
        XCTAssertNotEqual(key, AppModel.newKey())
    }

    func testAGiveUpComesWhenTheWorkDoesNot() async throws {
        let started = Date()
        do {
            _ = try await withTimeout(0.2) { try await Task.sleep(for: .seconds(30)); return 1 }
            XCTFail("the work outlived its timeout")
        } catch {
            XCTAssertTrue(timedOut(error), "\(error)")
        }
        XCTAssertLessThan(Date().timeIntervalSince(started), 5)
        let quick = try await withTimeout(5) { 7 }
        XCTAssertEqual(quick, 7)
    }
}

/// What the overview shows for a source, and the activity timeline.
final class OverviewTests: XCTestCase {
    private let failed = SourceStatus(lastSync: Date(), counts: nil, lastError: "Sending 2026-10-20: refused")

    func testARunningOrQueuedSourceDoesNotShowTheErrorItIsReplacing() {
        let running = SourceOverview.of(enabled: true, access: "Asked", running: true, queued: false, status: failed, waiting: 0)
        XCTAssertEqual(running.badge, "Syncing")
        XCTAssertNil(running.error)
        let queued = SourceOverview.of(enabled: true, access: "Asked", running: false, queued: true, status: failed, waiting: 0)
        XCTAssertEqual(queued.badge, "Queued")
        XCTAssertNil(queued.error)
        let idle = SourceOverview.of(enabled: true, access: "Asked", running: false, queued: false, status: failed, waiting: 0)
        XCTAssertEqual(idle.badge, "Failed")
        XCTAssertEqual(idle.error, failed.lastError)
    }

    func testStatesInOrder() {
        let ok = SourceStatus(lastSync: Date(), counts: "15 contacts", lastError: nil)
        XCTAssertEqual(SourceOverview.of(enabled: false, access: "Allowed", running: true, queued: false, status: failed, waiting: 3).badge, "Off")
        XCTAssertEqual(SourceOverview.of(enabled: true, access: "Not allowed", running: false, queued: false, status: ok, waiting: 0).badge, "No access")
        XCTAssertEqual(SourceOverview.of(enabled: true, access: "Asked", running: false, queued: false, status: ok, waiting: 7).badge, "Processing")
        XCTAssertEqual(SourceOverview.of(enabled: true, access: "Allowed", running: false, queued: false, status: nil, waiting: 0).badge, "Not synced yet")
        XCTAssertEqual(SourceOverview.of(enabled: true, access: "Allowed", running: false, queued: false, status: ok, waiting: 0).badge, "Up to date")
    }

    /// A run stopped by hand is a stop, said plainly, and no failure.
    func testAStoppedRunIsNotAFailure() {
        let stopped = SourceStatus(lastSync: Date(), counts: nil, lastError: "Stopped before it finished. Sync again to carry on.", stopped: true)
        let o = SourceOverview.of(enabled: true, access: "Allowed", running: false, queued: false, status: stopped, waiting: 0)
        XCTAssertEqual(o.badge, "Stopped")
        XCTAssertEqual(o.tone, .attention)
        XCTAssertEqual(SyncEngine.changes(0, 0), "no changes")
        XCTAssertEqual(SyncEngine.changes(3, 1), "3 written · 1 removed")
        XCTAssertEqual(SyncEngine.changes(0, 2), "2 removed")
    }

    /// Only a sync that would outlast the app on screen asks iOS to keep it
    /// running: more than a chunk of Health days, more than `bulkRows` of
    /// anything else, or a first sync whose size the phone has not counted.
    func testOnlyALargeSyncRunsOnInTheBackground() {
        XCTAssertFalse(AppModel.large(.health, behind: SyncEngine.chunk, synced: false))
        XCTAssertTrue(AppModel.large(.health, behind: SyncEngine.chunk + 1, synced: true))
        XCTAssertFalse(AppModel.large(.contacts, behind: AppModel.bulkRows, synced: true))
        XCTAssertTrue(AppModel.large(.calendars, behind: AppModel.bulkRows + 1, synced: true))
        XCTAssertTrue(AppModel.large(.contacts, behind: nil, synced: false))
        XCTAssertFalse(AppModel.large(.health, behind: nil, synced: true))
        XCTAssertEqual(SyncProgress(step: "Writing events", done: 1200, total: 4500).detail, "Writing events · 1,200 of 4,500")
        XCTAssertEqual(SyncProgress(step: "Reading reminders").detail, "Reading reminders")
    }

    func testTheTimelineFoldsEachRunsLinesIntoIt() {
        let t0 = Date(timeIntervalSince1970: 1_800_000_000)
        let contacts = SyncRun(source: .contacts, started: t0, finished: t0 + 10, summary: "15 written", error: nil)
        let health = SyncRun(source: .health, started: t0 + 20, finished: t0 + 60, summary: nil, error: "refused")
        let lines = [
            ActivityEntry(at: t0 + 60, source: .health, level: .error, message: "Sync did not finish"),
            ActivityEntry(at: t0 + 30, source: .health, level: .info, message: "Sending 3 days"),
            ActivityEntry(at: t0 + 15, source: nil, level: .warning, message: "Sync stopped by hand"),
            ActivityEntry(at: t0 + 5, source: .contacts, level: .info, message: "Read 15 contacts"),
        ]
        let items = TimelineItem.build(runs: [health, contacts], entries: lines)
        XCTAssertEqual(items, [
            .run(health, [lines[0], lines[1]]),
            .entry(lines[2]),
            .run(contacts, [lines[3]]),
        ])
        XCTAssertEqual(TimelineItem.build(runs: [health, contacts], entries: lines, only: .contacts), [.run(contacts, [lines[3]])])
    }

    func testWritesGoInBatchesThatEachCommit() {
        let batches = SyncEngine.batches(Array(0 ..< 250))
        XCTAssertEqual(batches.map(\.offset), [0, 100, 200])
        XCTAssertEqual(batches.map(\.batch.count), [100, 100, 50])
        XCTAssertEqual(batches.flatMap(\.batch), Array(0 ..< 250))
        XCTAssertTrue(SyncEngine.batches([Int]()).isEmpty)
    }

    func testAChartGroupsSoItHasAFewDozenBarsAtMost() {
        XCTAssertEqual(Granularity.fitting(days: 13), .day)
        XCTAssertEqual(Granularity.fitting(days: 90), .week)
        XCTAssertEqual(Granularity.fitting(days: 365), .month)
        XCTAssertEqual(Granularity.fitting(days: 3000), .year)
    }
}

/// The deliveries the app sends, held byte for byte against the goldens the
/// Go suite runs through the applehealth provider and its adapter
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
            q("basal_body_temperature", "degC", [("2026-10-07 06:45:00", 36.42, [phone])]),
            q("mindful_minutes", "min", [("2026-10-07 07:40:00", 10, [watch]), ("2026-10-07 22:15:00", 3.5, [phone])]),
            q("dietary_water", "mL", [("2026-10-07 08:00:00", 250, [phone]), ("2026-10-07 13:10:00", 500, [phone])]),
            q("dietary_energy", "kcal", [("2026-10-07 08:05:00", 420, ["MyFitnessPal"]), ("2026-10-07 13:20:00", 655, ["MyFitnessPal"])]),
            q("protein", "g", [("2026-10-07 08:05:00", 22.5, ["MyFitnessPal"])]),
            q("carbohydrates", "g", [("2026-10-07 08:05:00", 48, ["MyFitnessPal"])]),
            q("total_fat", "g", [("2026-10-07 08:05:00", 14.2, ["MyFitnessPal"])]),
            q("saturated_fat", "g", [("2026-10-07 08:05:00", 4.1, ["MyFitnessPal"])]),
            q("dietary_sugar", "g", [("2026-10-07 08:05:00", 12, ["MyFitnessPal"])]),
            q("fiber", "g", [("2026-10-07 08:05:00", 6.5, ["MyFitnessPal"])]),
            q("sodium", "mg", [("2026-10-07 08:05:00", 540, ["MyFitnessPal"])]),
            q("cholesterol", "mg", [("2026-10-07 08:05:00", 185, ["MyFitnessPal"])]),
            q("caffeine", "mg", [("2026-10-07 08:00:00", 95, [phone])]),
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
        run.route = (0 ..< 3).map { (i: Int) -> RoutePoint in
            var p = RoutePoint(at: runStart.addingTimeInterval(Double(i) * 15), latitude: 37.972587 + Double(i) * 0.00012,
                               longitude: 23.736912 + Double(i) * 0.00009)
            p.altitude = 87.47 + Double(i)
            p.course = 31.5
            p.courseAccuracy = 11.43
            p.horizontalAccuracy = 5.9
            p.verticalAccuracy = 2.7
            p.speed = 2.95
            p.speedAccuracy = 0.56
            return p
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
        var delivery = HealthDelivery(
            grouping: .seconds,
            metrics: [HealthMetric(name: "sleep_analysis", units: "hr", rows: SleepNights.rows(all: segments, day: day, rows: r))],
            workouts: [run.json(r), lift.json(r)])
        delivery.cycle = [
            CycleTracking.entry(name: "Menstrual Flow", value: "Medium", start: at("2026-10-07 00:00:00"), end: at("2026-10-08 00:00:00"),
                                cycleStart: true, source: phone, rows: r),
            CycleTracking.entry(name: "Ovulation Test Result", value: "Negative", start: at("2026-10-07 08:30:00"),
                                end: at("2026-10-07 08:30:00"), cycleStart: nil, source: phone, rows: r),
        ]
        return delivery
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
            note: "Substrate Sync's minutes delivery for one day, from its encoder (apps/apple/SubstrateSyncTests). Every metric the app reads, at one row per minute."))
    }

    func testSecondsDelivery() throws {
        try check("app_seconds.json", Self.envelope(
            Self.seconds, automation: "7D2E9F41-8A3B-4C5D-B6E7-F80912A3B4C5", session: "C3A1F5E7-2B4D-4F6A-8C0E-1D3B5F7A9C2E",
            note: "Substrate Sync's seconds delivery for one day, from its encoder (apps/apple/SubstrateSyncTests). Sleep segments with their night's fields, two workouts (the run with a route) and two cycle tracking entries."))
    }
}
