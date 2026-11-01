import Foundation
import SubstrateKit
import Testing
import WorkoutsCore
@testable import WorkoutsSync

/// The codec against what a substrate answers: records read back from a
/// throwaway server built from this tree (Fixtures/, recorded from the
/// repository probe.example.com), with their stamps, labels and property
/// metadata, and references as `{ref}` objects.
@Suite("Codec against the server's answers")
struct ServerAnswerTests {
    let rehome = Rehome(authority: recordedAuthority)
    let codec = RecordCodec()

    func records(_ name: String) throws -> [FlatRecord] {
        let page = try JSONDecoder().decode(WorkoutsAPI.Page.self, from: Fixtures.data(name))
        return try page.records.map { try WorkoutsAPI.record($0, rehome: rehome) }
    }

    @Test("a GET of an exercise decodes, in the phone's spelling")
    func anExercise() throws {
        let value = try JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: Fixtures.data("get-exercise"))
        let record = try WorkoutsAPI.record(value, rehome: rehome)
        #expect(record.kind == KindNames.v1.exercise)
        let exercise = try codec.decodeExercise(record)
        #expect(exercise.id.rawValue == "chest-press-machine")
        #expect(exercise.loadStep == Mass(value: 5, unit: .kg))
    }

    @Test("a list of routines decodes every routine, slots and planned sets, and keeps each one's version")
    func routines() throws {
        let routines = try records("list-routines").map(codec.decodeRoutine)
        #expect(routines.count == 4)
        #expect(routines.allSatisfy { !$0.slots.isEmpty })
        #expect(routines.allSatisfy { $0.version != nil })
        let names = Set(routines.flatMap(\.slots).map(\.exercise.rawValue))
        #expect(names.contains("chest-press-machine"))
    }

    @Test("a finished session and its sets decode together, references and chains intact")
    func aSession() throws {
        let decoded = try codec.sessionRecords(from: records("list-sessions") + records("list-sets"))
        #expect(decoded.count == 1)
        let session = try #require(decoded.first)
        #expect(session.session.status == .finished)
        #expect(session.sets.count == 20)
        let ids = Set(session.sets.map(\.id))
        let members = session.sets.filter { $0.continues != nil }
        #expect(members.count == 4)
        #expect(members.allSatisfy { ids.contains($0.continues!) })
    }

    @Test("a record re-encoded and sent back names the repository's kinds and nothing of the phone's")
    func roundTrip() throws {
        for record in try records("list-sets") {
            let set = try codec.decodeSet(record)
            let outbound = rehome.outbound(codec.encode(set))
            let text = String(decoding: try JSONEncoder().encode(outbound), as: UTF8.self)
            #expect(outbound.kind == "\(recordedAuthority)/training/set")
            #expect(!text.contains("\"~/"))
        }
    }

    @Test("the package row says the version the repository holds")
    func thePackageRow() throws {
        let value = try JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: Fixtures.data("get-package"))
        guard case let .object(object) = value, case let .object(properties)? = object["properties"],
              case let .number(version)? = properties["version"]
        else { Issue.record("no version in the package row"); return }
        // The recorded repository took an additive version 2 by hand, which
        // this build (version 1) reads as newer.
        #expect(PackageState.of(held: NSDecimalNumber(decimal: version).intValue, shipped: 1) == .newer(held: 2))
    }
}

@Suite("Rehome")
struct RehomeTests {
    let rehome = Rehome(authority: "ada.example.com")

    @Test("kinds, references and package ids move; a published kind does not")
    func moves() {
        let record = FlatRecord(kind: "~/training/set", id: "s1", properties: [
            "session": .object(["ref": .string("~/training/session/a")]),
            "exercise": .string("~/training/exercise/chest-press-machine"),
            "workout": .object(["ref": .string("substrate.reamde.dev/health/workout/applehealth/x/y")]),
            "notes": .string("~/training is not a path here"),
        ])
        let out = rehome.outbound(record)
        #expect(out.kind == "ada.example.com/training/set")
        #expect(out.properties["session"] == .object(["ref": .string("ada.example.com/training/session/a")]))
        #expect(out.properties["exercise"] == .string("ada.example.com/training/exercise/chest-press-machine"))
        #expect(out.properties["workout"] == record.properties["workout"])
        #expect(rehome.inbound(out) == record)
    }

    @Test("the manifests land under the repository's authority, with no ~ left")
    func manifests() throws {
        let manifest = try PackageManifest.bundled()
        #expect(manifest.package == "~/training")
        #expect(manifest.version >= 1)
        let documents = manifest.documents(for: rehome)
        let text = String(decoding: try JSONEncoder().encode(WorkoutsCore.JSONValue.array(documents)), as: UTF8.self)
        #expect(!text.contains("\"~"))
        #expect(text.contains(#""authority":"ada.example.com""#))
        #expect(text.contains(#""id":"ada.example.com\/training\/set""#) || text.contains(#""id":"ada.example.com/training/set""#))
        // The order an apply admits: the package, its bundle, then the kinds.
        let kinds = documents.compactMap { doc -> String? in
            guard case let .object(o) = doc, case let .string(k)? = o["kind"] else { return nil }
            return k
        }
        #expect(kinds.prefix(2) == ["substrate.reamde.dev/core/package", "substrate.reamde.dev/core/bundle"])
    }
}
