import Foundation
import SubstrateCore
import Testing

@testable import WorkoutsCore

/// What the Hevy import's button offers and what its finished screen says:
/// the workouts always, the routines and body measurements only through a
/// substrate.
@Suite("Hevy import's offer", .timeLimit(.minutes(10)))
struct HevyImportOfferTests {
    func measurements(_ count: Int) -> [FlatRecord] {
        (0 ..< count).map { FlatRecord(kind: WorkoutsKinds.v1.bodyMeasurement, id: "hevy-m\($0)", properties: [:]) }
    }

    func session(_ id: String) -> SessionRecords {
        SessionRecords(session: TrainingSession(id: SessionID(rawValue: id), at: Date(timeIntervalSince1970: 1_793_696_400)), sets: [])
    }

    @Test("body measurements alone are something new to import, once signed in")
    func measurementsAlone() {
        var plan = HevyImport.Plan(timeZone: .gmt)
        plan.measurements = measurements(5)
        let signedIn = plan.offer(signedIn: true)
        #expect(!signedIn.isEmpty)
        #expect(signedIn.title == "Import 5 body measurements")
        #expect(!signedIn.waitsForSignIn)
        #expect(signedIn.imported(exercises: 0, problem: nil).headline == "Imported 5 body measurements")

        // Not signed in, they wait for one, and the button says so.
        let signedOut = plan.offer(signedIn: false)
        #expect(signedOut.isEmpty)
        #expect(signedOut.title == "Nothing new to import")
        #expect(signedOut.waitsForSignIn)
    }

    @Test("the button and the finished screen count every kind it adds")
    func everyKind() {
        var plan = HevyImport.Plan(timeZone: .gmt)
        plan.sessions = [session("hevy-a"), session("hevy-b")]
        plan.routines = [Routine(id: "hevy-r-1", name: "Push Day")]
        plan.measurements = measurements(1)
        let offer = plan.offer(signedIn: true)
        #expect(offer.title == "Import 2 workouts, 1 routine and 1 body measurement")
        let imported = offer.imported(exercises: 3, problem: nil)
        #expect(imported == HevyImport.Imported(workouts: 2, exercises: 3, routines: 1, measurements: 1, notAdded: nil))
        #expect(imported.headline == "Imported 2 workouts, 1 routine and 1 body measurement")

        // Signed out, the workouts alone are added.
        #expect(plan.offer(signedIn: false).title == "Import 2 workouts")
        #expect(plan.offer(signedIn: false).waitsForSignIn)
    }

    @Test("a write the substrate refused names what it carried")
    func notAdded() {
        var plan = HevyImport.Plan(timeZone: .gmt)
        plan.sessions = [session("hevy-a")]
        plan.measurements = measurements(2)
        let measured = plan.offer(signedIn: true).imported(exercises: 0, problem: "the disk is full")
        #expect(measured.headline == "Imported 1 workout")
        #expect(measured.notAdded == "Body measurements not added: the disk is full")

        plan.routines = [Routine(id: "hevy-r-1", name: "Push Day")]
        let both = plan.offer(signedIn: true).imported(exercises: 0, problem: "the disk is full")
        #expect(both.notAdded == "Routines and body measurements not added: the disk is full")

        plan.measurements = []
        #expect(plan.offer(signedIn: true).imported(exercises: 0, problem: "no").notAdded == "Routines not added: no")

        // Nothing added at all says nothing was.
        var alone = HevyImport.Plan(timeZone: .gmt)
        alone.measurements = measurements(2)
        #expect(alone.offer(signedIn: true).imported(exercises: 0, problem: "no").headline == nil)
    }
}
