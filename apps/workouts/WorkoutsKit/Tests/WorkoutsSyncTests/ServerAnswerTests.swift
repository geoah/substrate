import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateDevice
import Testing
import WorkoutsCore
@testable import WorkoutsSync
import SubstrateCore

/// The codec against what a substrate answers: records read back from a
/// throwaway server built from this tree (Fixtures/), with their stamps,
/// labels and property metadata, and references as `{ref}` objects.
@Suite("Codec against the server's answers")
struct ServerAnswerTests {
    let codec = RecordCodec()

    func records(_ name: String) throws -> [FlatRecord] {
        let page = try JSONDecoder().decode(WorkoutsAPI.Page.self, from: Fixtures.data(name))
        return try page.records.map { try WorkoutsAPI.record($0) }
    }

    @Test("a GET of an exercise decodes, under the published kind name")
    func anExercise() throws {
        let value = try JSONDecoder().decode(JSONValue.self, from: Fixtures.data("get-exercise"))
        let record = try WorkoutsAPI.record(value)
        #expect(record.kind == WorkoutsKinds.v1.exercise)
        #expect(record.kind == "substrate.reamde.dev/training/exercise")
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

    @Test("a record re-encoded and sent back names the published kinds, and nothing is respelled")
    func roundTrip() throws {
        for record in try records("list-sets") {
            let set = try codec.decodeSet(record)
            let outbound = codec.encode(set)
            let text = String(decoding: try JSONEncoder().encode(outbound), as: UTF8.self)
            #expect(outbound.kind == "substrate.reamde.dev/training/set")
            #expect(!text.contains("~"))
            #expect(outbound.properties["session"] == record.properties["session"])
        }
    }

    @Test("the package row says the version the repository holds")
    func thePackageRow() throws {
        let value = try JSONDecoder().decode(JSONValue.self, from: Fixtures.data("get-package"))
        guard case let .object(object) = value, case let .object(properties)? = object["properties"],
              case let .number(version)? = properties["version"]
        else { Issue.record("no version in the package row"); return }
        // The recorded repository held version 2; a build written against
        // version 1 reads it as current, one against 3 as behind.
        #expect(PackageState.of(held: version.int ?? 0, needed: 1) == .current(held: 2))
        #expect(PackageState.of(held: version.int ?? 0, needed: 3) == .behind(held: 2))
    }
}

@Suite("The published manifests")
struct ManifestTests {
    @Test("the manifest labels every training kind as its declaration does, so a refused write says what it was")
    func trainingLabels() throws {
        let kinds = try PackageManifest.bundled().documents(ofKind: "substrate.reamde.dev/core/kind")
        #expect(Set(kinds.map(\.id)) == Set(WorkoutsManifest.trainingLabels.keys))
        for kind in kinds {
            let label = try #require(WorkoutsManifest.trainingLabels[kind.id])
            let declared = kind.data["label"]?.object
            let name = kind.data["names"]?.object?["singular"]?.string ?? ""
            #expect(label.singular == declared?["singular"]?.string ?? name.prefix(1).uppercased() + name.dropFirst())
            #expect(label.plural == declared?["plural"]?.string ?? label.singular + "s")
            let purpose = kind.data["purpose"]?.string ?? "primary"
            #expect((label.purpose ?? "primary") == purpose, "\(kind.id)")
        }
        #expect(WorkoutsManifest.current.kindLabels[WorkoutsKinds.v1.session]?.singular == "Training session")
    }

    @Test("training is published under substrate.reamde.dev, its documents in apply order, with no ~ anywhere")
    func training() throws {
        let manifest = try PackageManifest.bundled()
        #expect(manifest.package == WorkoutsKinds.packageID)
        #expect(manifest.version >= 4)
        let text = String(decoding: try JSONEncoder().encode(JSONValue.array(manifest.documents)), as: UTF8.self)
        #expect(!text.contains("~"))
        #expect(text.contains(#""authority":"substrate.reamde.dev""#))
        let kinds = manifest.documents.compactMap { doc -> String? in
            guard case let .object(o) = doc, case let .string(k)? = o["kind"] else { return nil }
            return k
        }
        #expect(kinds.prefix(2) == ["substrate.reamde.dev/core/package", "substrate.reamde.dev/core/bundle"])
        let declared = Set(manifest.documents(ofKind: "substrate.reamde.dev/core/kind").map(\.id))
        let codec = RecordCodec()
        #expect(declared.isSuperset(of: [codec.kinds.movement, codec.kinds.exercise, codec.kinds.setup, codec.kinds.routine, codec.kinds.session, codec.kinds.set]))
        let header = try #require(manifest.documents(ofKind: "substrate.reamde.dev/core/package").first)
        #expect(header.id == manifest.package && header.data["version"] == .number(Decimal(manifest.version)))
    }
}
