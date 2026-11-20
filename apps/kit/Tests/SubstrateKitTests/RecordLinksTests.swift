import Foundation
import Testing

@testable import SubstrateAPI
@testable import SubstrateCore

@Suite struct RecordLinks {
    @Test func aLinkNamesARecordWithOrWithoutItsScheme() {
        let trip = RecordReference(kind: "~/trips/trip", id: "01m7qar031emxt88z4nyazhb84")
        #expect(RecordReference(link: URL(string: "substrate://~/trips/trip/01m7qar031emxt88z4nyazhb84")!) == trip)
        #expect(RecordReference(link: URL(string: "ref:~/trips/trip/01m7qar031emxt88z4nyazhb84")!) == trip)
        #expect(RecordReference(link: URL(string: "~/trips/trip/01m7qar031emxt88z4nyazhb84")!) == trip)
        #expect(
            RecordReference(link: URL(string: "substrate.reamde.dev/core/contact/01m6")!)
                == RecordReference(kind: "substrate.reamde.dev/core/contact", id: "01m6"))
        #expect(RecordReference(link: URL(string: "/applets/books")!) == nil)
        #expect(RecordReference(link: URL(string: "applets/books")!) == nil)
        #expect(RecordReference(link: URL(string: "https://example.com/a/b/c/d")!) == nil)
        #expect(RecordReference(link: URL(string: "nodot/b/c/d")!) == nil)
    }

    @Test func aBarePathInProseBecomesALinkAndCodeIsLeftAlone() {
        let text = "Booked for ~/trips/trip/01m7. See `~/trips/trip/kept` and [it](~/trips/trip/linked)."
        #expect(RecordPaths.bare(in: text).map(\.path) == ["~/trips/trip/01m7"])
        let linked = RecordPaths.linking(text) { _ in "LA [2026]" }
        #expect(
            linked
                == "Booked for [LA \\[2026\\]](substrate://~/trips/trip/01m7). See `~/trips/trip/kept` and [it](~/trips/trip/linked).")
    }

    @Test func anAddressOrAPathIsNoRecord() {
        #expect(RecordPaths.bare(in: "https://example.com/~/trips/trip/x and a/b/c/d and mail@a.b/c/d/e").isEmpty)
        #expect(RecordPaths.bare(in: "(substrate.reamde.dev/core/contact/01m6)").map(\.kind) == ["substrate.reamde.dev/core/contact"])
    }

    @Test func aReferenceWrittenAsAPathDrawsAsAPill() {
        #expect(ValueShape(.string("~/trips/trip/01m7")) == .reference(RecordReference(kind: "~/trips/trip", id: "01m7")))
        let declared = PropertyDeclaration(.object(JSONObject(["type": .string("reference"), "kind": .string("~/trips/trip")])))
        #expect(ValueShape(.string("01m7"), declared: declared) == .reference(RecordReference(kind: "~/trips/trip", id: "01m7")))
        #expect(ValueShape(.string("Venice V Hotel")) == .text("Venice V Hotel"))
    }

    @Test func datesAreListedEarliestFirst() {
        let values: [String: JSONValue] = [
            "endDate": .string("2026-11-17"), "startDate": .string("2026-11-09"), "status": .string("done"),
            "destination": .string("Los Angeles"), "summary": .string(String(repeating: "x", count: 200)),
        ]
        let keys = values.keys.sorted { propertyOrder($0, values[$0], before: $1, values[$1]) }
        #expect(keys == ["destination", "status", "startDate", "endDate", "summary"])
    }

    @Test func aRecordOpensOnItsConsolePage() {
        let reader = HTTPRecordReader(server: URL(string: "https://substrate.example.com")!, token: "t")
        #expect(
            reader.webURL(for: RecordReference(kind: "~/trips/trip", id: "01m7"))?.absoluteString
                == "https://substrate.example.com/data/~/trips/trip/01m7")
    }
}
