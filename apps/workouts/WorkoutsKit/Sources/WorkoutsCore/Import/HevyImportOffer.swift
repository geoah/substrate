import Foundation

extension HevyImport {
    /// What importing a plan adds, and the button that starts it. The
    /// workouts are kept on the device and always added; the routines and
    /// the body measurements live on a substrate, so they are added only
    /// while the app is signed in to one.
    public struct Offer: Equatable, Sendable {
        public var workouts: Int
        public var routines: Int
        public var measurements: Int
        /// Routines or body measurements the plan holds that wait for a
        /// sign-in to a substrate, the only place that keeps them.
        public var waitsForSignIn: Bool

        public var isEmpty: Bool { workouts == 0 && routines == 0 && measurements == 0 }

        /// "Import 2 workouts, 1 routine and 5 body measurements".
        public var title: String {
            isEmpty ? "Nothing new to import" : "Import \(HevyImport.listed(workouts: workouts, routines: routines, measurements: measurements))"
        }

        /// What the import added, once it ran: the workouts, and the
        /// routines and body measurements unless writing them failed, and why.
        public func imported(exercises: Int, problem: String?) -> Imported {
            Imported(
                workouts: workouts, exercises: exercises, routines: problem == nil ? routines : 0,
                measurements: problem == nil ? measurements : 0,
                notAdded: problem.map { "\(HevyImport.notAdded(routines: routines, measurements: measurements)) not added: \($0)" })
        }
    }

    /// What an import added, and what it could not.
    public struct Imported: Equatable, Sendable {
        public var workouts: Int
        /// Exercises added to the catalogue, for the workouts and routines
        /// that name them.
        public var exercises: Int
        public var routines: Int
        public var measurements: Int
        /// The routines and body measurements the substrate did not take,
        /// and why: "Body measurements not added: …".
        public var notAdded: String?

        /// "Imported 2 workouts and 5 body measurements"; nil when nothing was.
        public var headline: String? {
            guard workouts > 0 || routines > 0 || measurements > 0 else { return nil }
            return "Imported \(HevyImport.listed(workouts: workouts, routines: routines, measurements: measurements))"
        }
    }

    /// Each count that is not zero, as words: "2 workouts, 1 routine and 5
    /// body measurements".
    static func listed(workouts: Int, routines: Int, measurements: Int) -> String {
        let parts = [
            workouts > 0 ? Format.count(workouts, "workout") : nil, routines > 0 ? Format.count(routines, "routine") : nil,
            measurements > 0 ? Format.count(measurements, "body measurement") : nil,
        ].compactMap { $0 }
        guard let last = parts.last else { return "" }
        return parts.count == 1 ? last : "\(parts.dropLast().joined(separator: ", ")) and \(last)"
    }

    /// What one write of the substrate's records carried: "Routines",
    /// "Body measurements", or both.
    static func notAdded(routines: Int, measurements: Int) -> String {
        switch (routines > 0, measurements > 0) {
        case (true, true): "Routines and body measurements"
        case (false, true): "Body measurements"
        default: "Routines"
        }
    }
}

extension HevyImport.Plan {
    /// What importing this plan adds, signed in to a substrate or not.
    public func offer(signedIn: Bool) -> HevyImport.Offer {
        HevyImport.Offer(
            workouts: sessions.count, routines: signedIn ? routines.count : 0, measurements: signedIn ? measurements.count : 0,
            waitsForSignIn: !signedIn && (!routines.isEmpty || !measurements.isEmpty))
    }
}
