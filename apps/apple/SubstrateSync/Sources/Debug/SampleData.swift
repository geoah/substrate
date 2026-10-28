import Contacts
import EventKit
import Foundation
import HealthKit
import os

/// A debug build's "Add sample data": a day of Health samples, a few
/// contacts and a few events, so a simulator has something to sync. A
/// release build asks to write nothing and offers none of it.
enum SampleData {
    #if DEBUG
    static let available = true
    private static let log = Logger(subsystem: "dev.reamde.substrate.sync", category: "sample")

    /// The end-to-end test's launch flag: no Health samples, and Health off,
    /// because HealthKit's permission sheet cannot be answered reliably on a
    /// simulator. The test sends Health's deliveries over the wire instead.
    static var withoutHealth: Bool {
        ProcessInfo.processInfo.environment["SUBSTRATE_SYNC_E2E_NO_HEALTH"] == "1"
    }

    static var shareTypes: Set<HKSampleType> {
        let ids: [HKQuantityTypeIdentifier] = [
            .heartRate, .stepCount, .distanceWalkingRunning, .flightsClimbed, .activeEnergyBurned, .basalEnergyBurned,
            .restingHeartRate, .heartRateVariabilitySDNN, .respiratoryRate, .oxygenSaturation, .vo2Max, .bodyMass,
            .bodyFatPercentage, .bodyMassIndex, .height, .bloodGlucose, .bloodPressureSystolic, .bloodPressureDiastolic,
        ]
        var types: Set<HKSampleType> = Set(ids.map { HKQuantityType($0) })
        types.insert(HKCategoryType(.sleepAnalysis))
        types.insert(HKObjectType.workoutType())
        return types
    }

    /// Yesterday, whole: the day the next sync is sure to send.
    static func add() async throws -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = .current
        let today = calendar.startOfDay(for: Date())
        let day = calendar.date(byAdding: .day, value: -1, to: today)!
        // Contacts and events first: the Health sheet is the one step a test
        // on a simulator cannot always answer.
        log.notice("contacts: start")
        let contacts = try addContacts()
        log.notice("events: start")
        let events = try await addEvents(day: day, calendar: calendar)
        log.notice("reminders: start")
        let reminders = try await addReminders(day: day, calendar: calendar)
        let visits = addVisits(day: day, calendar: calendar)
        var health = 0
        if !withoutHealth {
            log.notice("health: start")
            health = try await addHealth(day: day, calendar: calendar)
        }
        log.notice("done")
        return "\(health) Health samples, \(contacts) contacts, \(events) events, \(reminders) reminders, \(visits) visits"
    }

    private static func q(_ id: HKQuantityTypeIdentifier, _ value: Double, _ unit: HKUnit, _ start: Date, _ end: Date? = nil,
                          _ meta: [String: Any]? = nil) -> HKQuantitySample {
        HKQuantitySample(type: HKQuantityType(id), quantity: HKQuantity(unit: unit, doubleValue: value),
                         start: start, end: end ?? start, metadata: meta)
    }

    private static func addHealth(day: Date, calendar: Calendar) async throws -> Int {
        let store = SyncEngine.shared.health
        try await store.requestAuthorization(toShare: shareTypes, read: HealthReader.readTypes)
        UserDefaults.standard.set(true, forKey: "health.asked")
        log.notice("health: authorized")
        let bpm = HKUnit.count().unitDivided(by: .minute())
        func at(_ h: Int, _ m: Int, _ s: Int = 0) -> Date {
            calendar.date(byAdding: DateComponents(hour: h, minute: m, second: s), to: day)!
        }
        var samples: [HKObject] = []
        var rng = SystemRandomNumberGenerator()
        for minute in stride(from: 0, to: 24 * 60, by: 5) {
            let t = at(0, minute, Int.random(in: 0 ..< 50, using: &rng))
            let resting = minute < 7 * 60 || minute > 23 * 60
            samples.append(q(.heartRate, Double(resting ? Int.random(in: 52 ... 60) : Int.random(in: 64 ... 88)), bpm, t))
            if !resting, minute % 15 == 0 {
                let steps = Double(Int.random(in: 80 ... 600))
                let end = t.addingTimeInterval(240)
                samples.append(q(.stepCount, steps, .count(), t, end))
                samples.append(q(.distanceWalkingRunning, steps * 0.75, .meter(), t, end))
                samples.append(q(.activeEnergyBurned, steps * 0.04, .kilocalorie(), t, end))
            }
            samples.append(q(.basalEnergyBurned, 6.9, .kilocalorie(), at(0, minute), at(0, minute + 5)))
        }
        samples.append(q(.flightsClimbed, 3, .count(), at(12, 10), at(12, 12)))
        samples.append(q(.restingHeartRate, 56, bpm, at(0, 0)))
        for (h, m, v) in [(2, 10, 48.0), (4, 5, 61.0), (5, 50, 55.0)] {
            samples.append(q(.heartRateVariabilitySDNN, v, .secondUnit(with: .milli), at(h, m)))
        }
        for minute in stride(from: 30, to: 7 * 60, by: 15) {
            samples.append(q(.respiratoryRate, 14 + Double(minute % 3) * 0.4, bpm, at(0, minute)))
            samples.append(q(.oxygenSaturation, 0.96 + Double(minute % 2) * 0.01, .percent(), at(0, minute + 2)))
        }
        samples.append(q(.vo2Max, 44.2, HKUnit(from: "ml/kg*min"), at(8, 10)))
        samples.append(q(.bodyMass, 71.4, .gramUnit(with: .kilo), at(7, 2)))
        samples.append(q(.bodyFatPercentage, 0.182, .percent(), at(7, 2)))
        samples.append(q(.bodyMassIndex, 22.6, .count(), at(7, 2)))
        samples.append(q(.height, 177.5, .meterUnit(with: .centi), at(7, 3)))
        samples.append(q(.bloodGlucose, 92, HKUnit(from: "mg/dL"), at(7, 30), nil,
                         [HKMetadataKeyBloodGlucoseMealTime: HKBloodGlucoseMealTime.preprandial.rawValue]))
        samples.append(q(.bloodGlucose, 128, HKUnit(from: "mg/dL"), at(13, 45), nil,
                         [HKMetadataKeyBloodGlucoseMealTime: HKBloodGlucoseMealTime.postprandial.rawValue]))
        let mmHg = HKUnit.millimeterOfMercury()
        samples.append(HKCorrelation(
            type: HKCorrelationType(.bloodPressure), start: at(7, 15), end: at(7, 15),
            objects: [q(.bloodPressureSystolic, 118, mmHg, at(7, 15)), q(.bloodPressureDiastolic, 76, mmHg, at(7, 15))]))

        // The night that ended this morning: in bed from the iPhone, stages
        // from the watch.
        let sleep = HKCategoryType(.sleepAnalysis)
        let bedtime = at(0, -50)
        func stage(_ value: HKCategoryValueSleepAnalysis, _ from: Int, _ to: Int) -> HKCategorySample {
            HKCategorySample(type: sleep, value: value.rawValue, start: bedtime.addingTimeInterval(Double(from * 60)),
                             end: bedtime.addingTimeInterval(Double(to * 60)))
        }
        samples.append(stage(.inBed, 0, 450))
        samples.append(stage(.awake, 0, 14))
        var minute = 14
        for (value, length) in [(HKCategoryValueSleepAnalysis.asleepCore, 70), (.asleepDeep, 45), (.asleepCore, 60),
                                (.asleepREM, 30), (.awake, 6), (.asleepCore, 80), (.asleepDeep, 25), (.asleepREM, 40),
                                (.asleepCore, 60), (.awake, 6)] {
            samples.append(stage(value, minute, minute + length))
            minute += length
        }
        try await store.save(samples)
        log.notice("health: saved \(samples.count) samples")

        // A run, built so HealthKit computes its statistics.
        let configuration = HKWorkoutConfiguration()
        configuration.activityType = .running
        configuration.locationType = .outdoor
        let builder = HKWorkoutBuilder(healthStore: store, configuration: configuration, device: .local())
        let start = at(18, 5), end = at(18, 41)
        try await builder.beginCollection(at: start)
        log.notice("health: workout collecting")
        var workoutSamples: [HKSample] = []
        for s in stride(from: 0, to: 36 * 60, by: 20) {
            workoutSamples.append(q(.heartRate, Double(120 + (s / 60) * 1 + Int.random(in: 0 ... 12)), bpm, start.addingTimeInterval(Double(s))))
        }
        for m in 0 ..< 36 {
            let t = start.addingTimeInterval(Double(m * 60))
            workoutSamples.append(q(.activeEnergyBurned, 11.5, .kilocalorie(), t, t.addingTimeInterval(60)))
            workoutSamples.append(q(.distanceWalkingRunning, 170, .meter(), t, t.addingTimeInterval(60)))
        }
        try await builder.addSamples(workoutSamples)
        try await builder.addMetadata([
            HKMetadataKeyIndoorWorkout: false, HKMetadataKeyTimeZone: TimeZone.current.identifier,
            HKMetadataKeyElevationAscended: HKQuantity(unit: .meter(), doubleValue: 42),
        ])
        try await builder.endCollection(at: end)
        _ = try await builder.finishWorkout()
        log.notice("health: workout finished")
        return samples.count + workoutSamples.count
    }

    private static func addContacts() throws -> Int {
        let store = CNContactStore()
        let request = CNSaveRequest()
        let ada = CNMutableContact()
        ada.givenName = "Ada"
        ada.familyName = "Lovelace"
        ada.organizationName = "Analytical Engines"
        ada.jobTitle = "Mathematician"
        ada.phoneNumbers = [
            CNLabeledValue(label: CNLabelPhoneNumberMobile, value: CNPhoneNumber(stringValue: "+44 20 7946 0018")),
            CNLabeledValue(label: CNLabelPhoneNumberHomeFax, value: CNPhoneNumber(stringValue: "+44 20 7946 0019")),
        ]
        ada.emailAddresses = [
            CNLabeledValue(label: CNLabelWork, value: "ada@example.com" as NSString),
            CNLabeledValue(label: "lab", value: "not an address" as NSString),
        ]
        ada.birthday = DateComponents(month: 12, day: 10)
        let grace = CNMutableContact()
        grace.givenName = "Grace"
        grace.familyName = "Hopper"
        grace.urlAddresses = [CNLabeledValue(label: CNLabelURLAddressHomePage, value: "https://example.com/grace" as NSString)]
        grace.postalAddresses = [CNLabeledValue(label: CNLabelHome, value: {
            let a = CNMutablePostalAddress()
            a.street = "1 Example Street"
            a.city = "Arlington"
            a.isoCountryCode = "us"
            return a
        }())]
        let org = CNMutableContact()
        org.contactType = .organization
        org.organizationName = "Example Labs"
        org.emailAddresses = [CNLabeledValue(label: CNLabelOther, value: "hello@example.org" as NSString)]
        for c in [ada, grace, org] { request.add(c, toContainerWithIdentifier: nil) }
        try store.execute(request)
        return 3
    }

    private static func addEvents(day: Date, calendar: Calendar) async throws -> Int {
        let store = EKEventStore()
        guard try await store.requestFullAccessToEvents() else { return 0 }
        // One sample calendar, however often the samples are added.
        let cal: EKCalendar
        if let existing = store.calendars(for: .event).first(where: { $0.title == "Substrate Sync samples" }) {
            cal = existing
        } else {
            cal = EKCalendar(for: .event, eventStore: store)
            cal.title = "Substrate Sync samples"
            cal.cgColor = CGColor(srgbRed: 0.18, green: 0.54, blue: 0.37, alpha: 1)
            cal.source = store.sources.first { $0.sourceType == .local } ?? store.defaultCalendarForNewEvents?.source
            try store.saveCalendar(cal, commit: true)
        }
        func at(_ dayOffset: Int, _ h: Int, _ m: Int = 0) -> Date {
            calendar.date(byAdding: DateComponents(day: dayOffset, hour: h, minute: m), to: day)!
        }
        let lunch = EKEvent(eventStore: store)
        lunch.calendar = cal
        lunch.title = "Lunch with Grace"
        lunch.location = "Example Café"
        lunch.startDate = at(1, 12, 30)
        lunch.endDate = at(1, 13, 30)
        lunch.notes = "Bring the punch cards."
        lunch.url = URL(string: "https://example.com/lunch")
        let standup = EKEvent(eventStore: store)
        standup.calendar = cal
        standup.title = "Standup"
        standup.startDate = at(0, 9, 30)
        standup.endDate = at(0, 9, 45)
        standup.addRecurrenceRule(EKRecurrenceRule(
            recurrenceWith: .weekly, interval: 1, end: EKRecurrenceEnd(occurrenceCount: 6)))
        let trip = EKEvent(eventStore: store)
        trip.calendar = cal
        trip.title = "Conference"
        trip.isAllDay = true
        trip.startDate = at(7, 0)
        trip.endDate = at(9, 0)
        for e in [lunch, standup, trip] { try store.save(e, span: .futureEvents, commit: false) }
        try store.commit()
        return 3
    }

    /// Two visits in the place log, as CoreLocation would report them: a
    /// morning's stay, over, and an evening's that has not ended. A simulator
    /// never reports a visit, so this is how Places has something to send.
    private static func addVisits(day: Date, calendar: Calendar) -> Int {
        func at(_ h: Int, _ m: Int = 0) -> Date { calendar.date(byAdding: DateComponents(hour: h, minute: m), to: day)! }
        PlaceLog.shared.record(Visit(arrival: at(9), departure: at(10, 30), latitude: 37.97553, longitude: 23.73483, accuracy: 35))
        PlaceLog.shared.record(Visit(arrival: at(18, 15), departure: nil, latitude: 37.97153, longitude: 23.72575, accuracy: 50))
        return 2
    }

    private static func addReminders(day: Date, calendar: Calendar) async throws -> Int {
        let store = EKEventStore()
        guard try await store.requestFullAccessToReminders() else { return 0 }
        let list: EKCalendar
        if let existing = store.calendars(for: .reminder).first(where: { $0.title == "Substrate Sync samples" }) {
            list = existing
        } else {
            list = EKCalendar(for: .reminder, eventStore: store)
            list.title = "Substrate Sync samples"
            list.cgColor = CGColor(srgbRed: 0.95, green: 0.55, blue: 0.1, alpha: 1)
            list.source = store.defaultCalendarForNewReminders()?.source ?? store.sources.first { $0.sourceType == .local }
            try store.saveCalendar(list, commit: true)
        }
        let groceries = EKReminder(eventStore: store)
        groceries.calendar = list
        groceries.title = "Buy oat milk"
        groceries.notes = "The barista one."
        groceries.dueDateComponents = calendar.dateComponents([.year, .month, .day], from: calendar.date(byAdding: .day, value: 1, to: day)!)
        let call = EKReminder(eventStore: store)
        call.calendar = list
        call.title = "Call the dentist"
        call.priority = 1
        call.dueDateComponents = calendar.dateComponents([.year, .month, .day, .hour, .minute],
                                                         from: calendar.date(byAdding: DateComponents(day: 1, hour: 10), to: day)!)
        call.addAlarm(EKAlarm(relativeOffset: -900))
        let done = EKReminder(eventStore: store)
        done.calendar = list
        done.title = "Water the plants"
        // EventKit refuses a repeating reminder without a due date.
        done.dueDateComponents = calendar.dateComponents([.year, .month, .day], from: day)
        done.addRecurrenceRule(EKRecurrenceRule(recurrenceWith: .weekly, interval: 1, end: nil))
        done.isCompleted = true
        for r in [groceries, call, done] { try store.save(r, commit: false) }
        try store.commit()
        return 3
    }
    #else
    static let available = false
    static var shareTypes: Set<HKSampleType> { [] }
    static func add() async throws -> String { "" }
    #endif
}
