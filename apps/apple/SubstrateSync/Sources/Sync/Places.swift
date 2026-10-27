import CoreLocation
import Foundation
import MapKit
import UIKit

/// One place the phone stayed at, as CoreLocation's visit monitoring reports
/// it, and the name and address the reverse geocoder found for it.
struct Visit: Codable, Equatable, Sendable {
    /// Nil when the phone was already there when monitoring began.
    var arrival: Date?
    /// Nil while the phone is still there.
    var departure: Date?
    var latitude: Double
    var longitude: Double
    /// Meters.
    var accuracy: Double
    var name: String?
    var address: String?
    var city: String?
    var country: String?
    var category: String?
    var timeZone: String?
    /// The geocoder answered, with a place or without one: it is not asked
    /// again.
    var geocoded = false

    /// What names the visit across its two reports: CoreLocation reports an
    /// arrival, then the same visit again with its departure. A visit whose
    /// arrival is unknown is named by its departure, the one report it gets.
    var key: String {
        if let arrival { return "arrival:" + Formats.utc(arrival) }
        return "departure:" + (departure.map(Formats.utc) ?? "")
    }

    /// The end of the visit, or its start while it lasts: what ages it out.
    var latest: Date? { departure ?? arrival }

    init(arrival: Date?, departure: Date?, latitude: Double, longitude: Double, accuracy: Double) {
        self.arrival = arrival
        self.departure = departure
        self.latitude = latitude
        self.longitude = longitude
        self.accuracy = accuracy
    }

    init(_ v: CLVisit) {
        self.init(
            arrival: v.arrivalDate == .distantPast ? nil : v.arrivalDate,
            departure: v.departureDate == .distantFuture ? nil : v.departureDate,
            latitude: v.coordinate.latitude, longitude: v.coordinate.longitude, accuracy: v.horizontalAccuracy)
    }
}

/// The visits this phone recorded, newest first, the last year of them, in
/// Application Support. CoreLocation reports a visit once, whether or not the
/// app is running (it relaunches it in the background), so every report is
/// kept here first and the Places sync sends from this log: a sync that
/// fails, or a repository that is reset, loses none of them.
final class PlaceLog: @unchecked Sendable {
    static let shared = PlaceLog()
    static let keep: TimeInterval = 366 * 86400

    private let lock = NSLock()
    private var visits: [String: Visit]

    private static var url: URL {
        let dir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir.appendingPathComponent("visits.json")
    }

    private init() {
        let stored = (try? Data(contentsOf: Self.url)).flatMap { try? JSONDecoder().decode([Visit].self, from: $0) } ?? []
        visits = Dictionary(stored.map { ($0.key, $0) }, uniquingKeysWith: { a, _ in a })
    }

    var all: [Visit] {
        lock.lock()
        defer { lock.unlock() }
        return visits.values.sorted { ($0.latest ?? .distantPast) > ($1.latest ?? .distantPast) }
    }

    /// Records a report: a new visit, or the departure of one already here.
    func record(_ visit: Visit) {
        update { visits in
            var merged = visits[visit.key] ?? visit
            merged.departure = visit.departure ?? merged.departure
            merged.latitude = visit.latitude
            merged.longitude = visit.longitude
            merged.accuracy = visit.accuracy
            visits[visit.key] = merged
        }
    }

    /// Writes what the geocoder found for one visit.
    func named(_ key: String, _ found: Visit) {
        update { visits in
            guard var v = visits[key] else { return }
            v.name = found.name
            v.address = found.address
            v.city = found.city
            v.country = found.country
            v.category = found.category
            v.timeZone = found.timeZone
            v.geocoded = true
            visits[key] = v
        }
    }

    private func update(_ change: (inout [String: Visit]) -> Void) {
        lock.lock()
        change(&visits)
        let floor = Date().addingTimeInterval(-Self.keep)
        visits = visits.filter { ($0.value.latest ?? .distantFuture) >= floor }
        let snapshot = Array(visits.values)
        lock.unlock()
        if let data = try? JSONEncoder().encode(snapshot) {
            try? data.write(to: Self.url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        }
    }
}

/// CoreLocation's visit monitoring, started at every launch while Places is
/// on and Always access is granted: iOS relaunches the app in the background
/// for a visit, and the report reaches the delegate only once it is set.
@MainActor
final class PlaceMonitor: NSObject, CLLocationManagerDelegate {
    static let shared = PlaceMonitor()

    private let manager = CLLocationManager()
    private var asking: CheckedContinuation<Void, Never>?

    override private init() {
        super.init()
        manager.delegate = self
    }

    var authorization: CLAuthorizationStatus { manager.authorizationStatus }

    /// Starts the monitoring where Places is on and allowed; stops it where not.
    func start() {
        if Settings.enabled(.places), manager.authorizationStatus == .authorizedAlways {
            manager.startMonitoringVisits()
        } else {
            manager.stopMonitoringVisits()
        }
    }

    /// Asks for Always access. iOS asks for While Using first and offers the
    /// upgrade to Always later, on its own schedule; both answers end here.
    func requestAccess() async {
        guard manager.authorizationStatus == .notDetermined || manager.authorizationStatus == .authorizedWhenInUse else { return }
        await withCheckedContinuation { continuation in
            asking = continuation
            manager.requestAlwaysAuthorization()
        }
    }

    nonisolated func locationManager(_ manager: CLLocationManager, didVisit visit: CLVisit) {
        let v = Visit(visit)
        PlaceLog.shared.record(v)
        Activity.shared.log(.places, v.departure == nil ? "Arrived somewhere: the visit is kept here and sent" : "Left a place: the visit is kept here and sent")
        // The report may have relaunched the app with seconds to spare: the
        // visit is already kept, and the sync, at the owner's frequency,
        // runs inside a background task so iOS lets it finish.
        Task { @MainActor in
            var id = UIBackgroundTaskIdentifier.invalid
            let ended = Once()
            let end = { ended.run { UIApplication.shared.endBackgroundTask(id) } }
            id = UIApplication.shared.beginBackgroundTask(withName: "Substrate Sync Places") {
                Task { await SyncEngine.shared.cancel() }
                end()
            }
            await SyncEngine.shared.syncIfDue([.places], budget: .background)
            end()
        }
    }

    nonisolated func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        Task { @MainActor in
            self.start()
            if manager.authorizationStatus != .notDetermined, let asking = self.asking {
                self.asking = nil
                asking.resume()
            }
        }
    }
}

/// Names a place from its coordinate, with MapKit's reverse geocoder. A
/// coordinate the geocoder has no place for comes back unnamed; a failed
/// request (offline, throttled) throws, and is asked again next sync.
enum PlaceNamer {
    static func name(_ visit: Visit) async throws -> Visit {
        var found = visit
        let location = CLLocation(latitude: visit.latitude, longitude: visit.longitude)
        guard let request = MKReverseGeocodingRequest(location: location),
              let item = try await request.mapItems.first
        else { return found }
        found.name = item.name
        found.address = item.address?.fullAddress
        found.city = item.addressRepresentations?.cityName
        found.country = item.addressRepresentations?.regionName
        found.category = item.pointOfInterestCategory?.rawValue
        found.timeZone = item.timeZone?.identifier
        return found
    }
}

/// A visit as providers.substrate.reamde.dev/apple declares it.
enum VisitMapping {
    static let storeKind = "providers.substrate.reamde.dev/apple/visitstore"
    static let kind = "providers.substrate.reamde.dev/apple/visit"

    static func visit(_ v: Visit, visitstore: String) -> JSON {
        var f = Fields()
        f.set("visitstore", reference(storeKind, visitstore))
        f.set("arrivalDate", v.arrival.map(Formats.utc))
        f.set("departureDate", v.departure.map(Formats.utc))
        f.set("latitude", v.latitude)
        f.set("longitude", v.longitude)
        f.set("horizontalAccuracy", v.accuracy)
        f.set("placeName", v.name)
        f.set("address", v.address)
        f.set("city", v.city)
        f.set("country", v.country)
        f.set("category", v.category)
        f.set("timeZone", v.timeZone.flatMap(EventMapping.ianaZone))
        return f.json
    }
}
