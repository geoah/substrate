import Foundation
@testable import WorkoutsCore
import Testing

/// SplitMix64: a seeded generator, so minted ids are reproducible in tests.
struct SeededGenerator: RandomNumberGenerator {
    var state: UInt64

    init(seed: UInt64) {
        state = seed
    }

    mutating func next() -> UInt64 {
        state &+= 0x9E37_79B9_7F4A_7C15
        var z = state
        z = (z ^ (z >> 30)) &* 0xBF58_476D_1CE4_E5B9
        z = (z ^ (z >> 27)) &* 0x94D0_49BB_1331_11EB
        return z ^ (z >> 31)
    }
}

@Suite("UUIDv7")
struct UUIDv7Tests {
    @Test("version 7, variant 10, lowercase and hyphenated")
    func versionAndVariant() throws {
        let shape = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
        var generator = SeededGenerator(seed: 7)
        for offset in 0 ..< 500 {
            let id = UUIDv7.make(at: Date(timeIntervalSince1970: 1_793_107_800 + Double(offset)), using: &generator)
            #expect(id.wholeMatch(of: shape) != nil, "\(id)")
            #expect(id == id.lowercased())
            let uuid = try #require(UUID(uuidString: id))
            #expect(uuid.uuid.6 >> 4 == 7)
            #expect(uuid.uuid.8 >> 6 == 0b10)
        }
    }

    @Test("the first 48 bits are the instant's Unix milliseconds")
    func timestamp() throws {
        var generator = SeededGenerator(seed: 1)
        for text in ["2026-10-27T13:30:00Z", "2026-10-22T11:34:08Z", "1970-01-01T00:00:00Z"] {
            let date = try #require(DateText.parse(text))
            let id = UUIDv7.make(at: date, using: &generator)
            #expect(UUIDv7.date(of: id) == date)
            let millis = UInt64(date.timeIntervalSince1970 * 1000)
            #expect(id.replacingOccurrences(of: "-", with: "").prefix(12) == String(format: "%012llx", millis))
        }
        // An eighth of a second is exact in a Double, so its milliseconds are too.
        let precise = Date(timeIntervalSince1970: 1_793_107_800.125)
        let id = UUIDv7.make(at: precise, using: &generator)
        #expect(UUIDv7.date(of: id)?.timeIntervalSince1970 == 1_793_107_800.125)
    }

    @Test("ids of later instants sort after earlier ones")
    func ordering() {
        var generator = SeededGenerator(seed: 42)
        let ids = (0 ..< 200).map { step in
            UUIDv7.make(at: Date(timeIntervalSince1970: 1_793_107_800 + Double(step) * 0.5), using: &generator)
        }
        #expect(ids == ids.sorted())
        #expect(Set(ids).count == ids.count)
    }

    @Test("a seeded generator mints the same id twice")
    func reproducible() {
        let date = Date(timeIntervalSince1970: 1_793_107_800)
        var first = SeededGenerator(seed: 99)
        var second = SeededGenerator(seed: 99)
        #expect(UUIDv7.make(at: date, using: &first) == UUIDv7.make(at: date, using: &second))
        #expect(UUIDv7.make(at: date) != UUIDv7.make(at: date))
    }

    @Test("every example session and set id carries the record's own at")
    func examplesCarryTheirInstant() throws {
        var checked = 0
        for name in ["sessions", "coverage"] {
            for records in try ExampleFixtures.sessions(name) {
                if let date = UUIDv7.date(of: records.session.id.rawValue) {
                    #expect(date == records.session.at, "\(records.session.id)")
                    checked += 1
                }
                for set in records.sets {
                    guard let date = UUIDv7.date(of: set.id.rawValue) else { continue }
                    #expect(date == set.at, "\(set.id)")
                    checked += 1
                }
            }
        }
        #expect(checked > 100)
        // An importer's key is not a UUIDv7.
        #expect(UUIDv7.date(of: "hevy-3f1c9a2e-8b47-4d0e-9a61-2c7d5e8f4b10") == nil)
        #expect(UUIDv7.date(of: UUID().uuidString.lowercased()) == nil)
    }
}

@Suite("Exercise slugs")
struct ExerciseSlugTests {
    @Test("a new exercise's id is its name's slug", arguments: [
        ("Chest Press (Machine)", "chest-press-machine"),
        ("Farmer's Carry (Dumbbell)", "farmer-s-carry-dumbbell"),
        ("  Développé couché  ", "developpe-couche"),
        ("21s — Curl", "21s-curl"),
        ("***", "exercise"),
    ])
    func slug(name: String, id: String) {
        #expect(ExerciseID.slug(for: name) == ExerciseID(id))
    }

    @Test("a taken slug gets -2, -3")
    func taken() {
        let taken: Set<ExerciseID> = ["chest-press-machine", "chest-press-machine-2"]
        #expect(ExerciseID.slug(for: "Chest Press (Machine)", taken: taken) == "chest-press-machine-3")
    }
}
