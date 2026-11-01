import Foundation
import Testing
@testable import SubstrateKit

@Suite struct KindWords {
    @Test func aKindNameSplitsIntoTheConsolesWords() {
        #expect(kindDisplayName("substrate.reamde.dev/core/recordpatchrequest") == "Record patch request")
        #expect(kindDisplayName("substrate.reamde.dev/llm/provider") == "Provider")
        #expect(kindDisplayPlural("samples.substrate.reamde.dev/people/person") == "People")
        #expect(kindDisplayPlural("samples.substrate.reamde.dev/tasks/task") == "Tasks")
        // A name that does not split fully reads as itself.
        #expect(kindDisplayName("a.example.com/x/zqxv") == "Zqxv")
    }

    @Test func aPropertyNameReadsAsALabel() {
        #expect(propertyLabel("dueAt") == "Due")
        #expect(propertyLabel("assignee") == "Assignee")
        #expect(propertyLabel("apiKey") == "API key")
    }
}

@Suite struct Glyphs {
    @Test func theNamesWordsPickTheSymbolLastWordFirst() {
        let task = KindGlyphSpec(kind: "samples.substrate.reamde.dev/tasks/task")
        #expect(task.symbol == "checkmark.circle")
        #expect(task.hue == .blue)
        #expect(KindGlyphSpec(kind: "a.example.com/people/person").symbol == "person")
        #expect(KindGlyphSpec(kind: "a.example.com/calendar/calendarevent").symbol == "calendar")
    }

    /// The hues the console's hashHue answers for the same references.
    @Test func anUnknownNameTakesTheConsolesHashedHue() {
        let refs = ["a.example.com/commerce/category", "x.example.com/a/b", "x.example.com/a/c",
                    "mneme.rxtx.dev/health/sleep", "ada.example.com/llm/widget"]
        #expect(refs.map { KindHue.hash($0) } == [.green, .teal, .blue, .purple, .purple])
    }
}

@Suite struct References {
    @Test func aPathSplitsIntoKindAndAnIdThatMayHoldSlashes() throws {
        let agent = try #require(RecordReference(path: "substrate.reamde.dev/core/agent/x.example.com/llm/helper"))
        #expect(agent.kind == "substrate.reamde.dev/core/agent")
        #expect(agent.recordID == "x.example.com/llm/helper")
        #expect(RecordReference(.object(["ref": "a.example.com/commerce/category/kitchen-tableware"]))?.recordID == "kitchen-tableware")
        #expect(RecordReference(path: "not-a-kind/x/y/z") == nil)
        #expect(RecordReference(path: "a.example.com/x/y") == nil)
    }

    @Test func aRecordIsNamedBeforeItIsRead() {
        #expect(fallbackName(RecordReference(kind: "a.example.com/commerce/category", id: "kitchen-tableware")) == "kitchen-tableware")
        #expect(fallbackName(RecordReference(kind: "a.example.com/commerce/category", id: "abcdefgh2345")) == "Category")
    }
}

@Suite struct Shapes {
    @Test func aValueIsDrawnByItsShape() {
        let ref: JSONValue = ["ref": "a.example.com/commerce/category/x"]
        #expect(ValueShape(ref) == .reference(RecordReference(kind: "a.example.com/commerce/category", id: "x")))
        if case .references(let refs) = ValueShape([ref, ref]) { #expect(refs.count == 2) } else { Issue.record("not references") }
        if case .date(_, let time) = ValueShape("2026-10-31T10:00:00Z") { #expect(time) } else { Issue.record("not a date") }
        if case .date(_, let time) = ValueShape("2026-10-31") { #expect(!time) } else { Issue.record("not a date") }
        if case .link = ValueShape("https://example.com/x") {} else { Issue.record("not a link") }
        #expect(ValueShape(.null) == .empty)
        #expect(ValueShape([]) == .empty)
        #expect(ValueShape(["value": 70, "unit": ["ref": "substrate.reamde.dev/core/unit/kg"]]) == .words("70 kg"))
        #expect(ValueShape("utility") == .words("Utility"))
        #expect(ValueShape("inProgress") == .words("In progress"))
        #expect(ValueShape("Ada Lovelace") == .text("Ada Lovelace"))
        #expect(ValueShape("kitchen-tableware") == .text("kitchen-tableware"))
    }

    @Test func anEnumReadsAsItsDeclaredLabel() {
        let declared = PropertyDeclaration(["type": "enum", "values": ["inProgress", ["value": "done", "label": "Finished"]]])
        #expect(ValueShape("done", declared: declared) == .words("Finished"))
        #expect(ValueShape("inProgress", declared: declared) == .words("In progress"))
    }
}

@Suite @MainActor struct Directory {
    struct Fake: RecordReader {
        let records: [String: Record]
        let denied: Set<String>
        func record(_ reference: RecordReference) async throws -> Record? {
            if denied.contains(reference.path) { throw RecordReadError.notAllowed }
            return records[reference.path]
        }
    }

    @Test func aRecordIsReadOnceAndADenialIsRemembered() async throws {
        let cat = RecordReference(kind: "a.example.com/commerce/category", id: "mugs")
        let secret = RecordReference(kind: "a.example.com/x/y", id: "z")
        let directory = RecordDirectory(reader: Fake(
            records: [cat.path: Record(id: "mugs", kind: cat.kind, properties: ["title": "Mugs"])], denied: [secret.path]))
        #expect(directory.title(for: cat) == nil)
        await directory.resolve(cat)
        #expect(directory.title(for: cat) == "Mugs")
        await #expect(throws: RecordReadError.notAllowed) { try await directory.record(secret) }
        #expect(directory.denied.contains(secret.path))
    }
}
