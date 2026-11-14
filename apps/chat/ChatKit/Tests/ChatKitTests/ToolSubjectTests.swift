import Foundation
import SubstrateCore
import Testing

@testable import ChatKit

private let query = ResolvedTool(function: HostFunction.query)
private let write = ResolvedTool(function: HostFunction.write)
private let edit = ResolvedTool(function: HostFunction.edit)

private func call(_ name: String = "query", _ arguments: String = "", _ output: String = "", callable: String? = nil) -> ToolCall {
    ToolCall(id: "c1", name: name, callable: callable, arguments: arguments, output: output, ok: true)
}

/// A write of book b1 that answered with the record, titled where given.
private func wrote(_ arguments: String, _ title: String? = nil) -> ToolCall {
    let properties = title.map { #"{"title":"\#($0)"}"# } ?? "{}"
    return call("write", arguments, #"{"record":{"kind":"ada.localhost/reading/book","id":"b1","properties":\#(properties)}}"#)
}

private let book = #""kind":"ada.localhost/reading/book","id":"b1""#

/// A write's `input`, setting each named property.
private func props(_ names: String...) -> String {
    #""input":{"properties":{\#(names.map { #""\#($0)":"x""# }.joined(separator: ","))}}"#
}

private func row(_ call: ToolCall, titles: [String: String] = [:]) -> ToolRowPresentation {
    TurnPresenter.tool(call, id: "a#0", agent: nil, decided: [], titles: titles)
}

private func chip(_ kind: String, _ id: String, _ title: String? = nil) -> RecordChip {
    RecordChip(record: RecordReference(kind: kind, id: id), title: title)
}

/// The console's wording, string for string (web/console/src/lib/agent-chat.test.ts).
@Suite("A tool line in words", .timeLimit(.minutes(10)))
struct ToolLineWords {
    @Test func aReadIsSaidInWordsASearchAListOneRecordAndWhatItFound() {
        #expect(toolSummary(call("query", #"{"q":"handover"}"#, #"{"records":[{"kind":"a.b/c/d","id":"1"}]}"#), query) == "Searched for “handover”, found 1")
        #expect(
            toolSummary(call("query", #"{"filter":{"kinds":["ada.localhost/tasks/task","ada.localhost/notes/note"]}}"#), query)
                == "Looked through your tasks and notes")
        #expect(toolSummary(call("query", #"{"kind":"ada.localhost/people/person","id":"g"}"#), query) == "Looked up a person")
        #expect(toolSummary(call(), query) == "Looked things up")
        // The one record found is named.
        #expect(
            toolSummary(
                call(
                    "query", #"{"kind":"ada.localhost/people/person","id":"g"}"#,
                    #"{"record":{"kind":"ada.localhost/people/person","id":"g","properties":{"title":"Grace Hopper"}}}"#), query)
                == "Looked up “Grace Hopper”")
        #expect(
            toolSummary(call("query", #"{"q":"handover"}"#, #"{"records":[{"kind":"a.b/c/d","id":"1","properties":{"title":"The handover"}}]}"#), query)
                == "Searched for “handover”, found “The handover”")
    }

    @Test func aWriteOrAnEditNamesTheRecordItWorkedOnAndTheFieldsItChanged() {
        #expect(toolSummary(wrote(#"{"op":"patch",\#(book),\#(props("description"))}"#, "Dune"), write) == "Changed the description of “Dune”")
        #expect(toolSummary(wrote(#"{"op":"patch",\#(book),\#(props("status", "notes"))}"#, "Dune"), write) == "Changed the status and notes of “Dune”")
        #expect(toolSummary(wrote(#"{"op":"patch",\#(book),\#(props("a", "b", "c"))}"#, "Dune"), write) == "Changed 3 fields of “Dune”")
        #expect(
            toolSummary(wrote(#"{"op":"put","kind":"ada.localhost/reading/book",\#(props("name"))}"#, "Children of Dune"), write)
                == "Added “Children of Dune” to your books")
        #expect(toolSummary(wrote(#"{"op":"delete",\#(book)}"#, "Dune"), write) == "Deleted “Dune”")
        // No heading: the kind, with its article.
        #expect(toolSummary(call("write", #"{"op":"patch","kind":"ada.localhost/reading/author"}"#), write) == "Changed an author")
        // A heading read for the line, and one too long to quote whole.
        #expect(
            toolSummary(
                call(
                    "edit", #"{"op":"replace","kind":"ada.localhost/notes/note","id":"n1","property":"body","edits":[{},{}]}"#,
                    #"{"kind":"ada.localhost/notes/note","id":"n1","edits":2}"#), edit, title: "Garden")
                == "Edited the body of “Garden” in 2 places")
        let long = "A very long title that goes on and on about the garden and the weather"
        #expect(toolSummary(wrote(#"{"op":"patch",\#(book)}"#, long), write) == "Changed “A very long title that goes on and on about the garden and…”")
    }

    @Test func aPutWithAnIdSavesAndAViewReadsPartOfAField() {
        #expect(toolSummary(wrote(#"{"op":"put",\#(book),\#(props("name"))}"#, "Dune"), write) == "Saved “Dune”")
        #expect(toolSummary(call("write", #"{"op":"put","kind":"ada.localhost/reading/book"}"#), write) == "Added a book")
        let view = call("edit", #"{"op":"view","kind":"ada.localhost/notes/note","id":"n1","property":"body"}"#, #"{"text":"…"}"#)
        #expect(toolSummary(view, edit) == "Read part of the body of a note")
        #expect(toolSummary(view, edit, title: "Garden") == "Read part of the body of “Garden”")
        // A failed call worked on nothing: it names no record.
        var failed = wrote(#"{"op":"delete",\#(book)}"#, "Dune")
        failed.ok = false
        #expect(toolSummary(failed, write, title: "Dune") == "Deleted a book")
    }

    @Test func aKindTakesItsArticleByHowItSounds() {
        for noun in ["unit", "user", "URL", "utility", "eucalyptus", "one-off", "book"] {
            #expect(withArticle(noun) == "a \(noun)")
        }
        for noun in ["umbrella", "update", "upload", "author", "event", "idea", "hour"] {
            #expect(withArticle(noun) == "an \(noun)")
        }
        // The seeded unit kind, as a line says it.
        #expect(toolSummary(call("query", #"{"kind":"substrate.reamde.dev/core/unit","id":"kg"}"#), query) == "Looked up a unit")
    }
}

/// What the console's tool line shows under it and once opened
/// (web/console/src/components/agent/tool-call.test.tsx).
@Suite("A tool line's records", .timeLimit(.minutes(10)))
struct ToolLineRecords {
    private let widget = "crew.test.dev/crew/widget"

    @Test func aWriteWhoseAnswerCarriedNoHeadingSaysTheFieldAndTheKindUntilItIsRead() {
        let presented = row(
            call(
                "write", #"{"op":"patch","kind":"\#(widget)","id":"w1","input":{"properties":{"name":"w"}}}"#,
                #"{"record":{"id":"w1","kind":"\#(widget)"}}"#, callable: "substrate.reamde.dev/core/function/substrate.reamde.dev/core/write"))
        #expect(presented.summary == "Changed the name of a widget")
        #expect(presented.unnamed == UnnamedSubject(record: RecordReference(kind: widget, id: "w1"), before: "Changed the name of ", after: ""))
        #expect(presented.unnamed?.line(titled: "Sprocket") == "Changed the name of “Sprocket”")
        #expect(presented.chips.isEmpty)
        // The chat's own lookups name it where the answer did not.
        let named = row(
            call(
                "write", #"{"op":"patch","kind":"\#(widget)","id":"w1","input":{"properties":{"name":"w"}}}"#,
                #"{"record":{"id":"w1","kind":"\#(widget)"}}"#, callable: "substrate.reamde.dev/core/function/substrate.reamde.dev/core/write"),
            titles: ["\(widget)/w1": "Sprocket"])
        #expect(named.summary == "Changed the name of “Sprocket”")
        #expect(named.unnamed == nil)
    }

    @Test func aLiveWriteAddsTheRecordItWasGiven() {
        let presented = row(call("write", #"{"op":"create","kind":"\#(widget)"}"#, #"{"record":{"id":"w2","kind":"\#(widget)"}}"#))
        #expect(presented.summary == "Added a widget")
        #expect(presented.unnamed?.line(titled: "Gear") == "Added “Gear” to your widgets")
    }

    @Test func aSearchShowsWhatItFoundUnderTheLineAndSaysWhenNothingMatched() {
        let found = row(
            call("query", #"{"q":"cups"}"#, #"{"records":[{"kind":"\#(widget)","id":"w1"},{"kind":"\#(widget)","id":"w2"}]}"#),
            titles: ["\(widget)/w2": "Saucer"])
        #expect(found.summary == "Searched for “cups”, found 2")
        #expect(found.chips == [RecordChips(lead: nil, label: "Records found", records: [chip(widget, "w1"), chip(widget, "w2", "Saucer")], total: 2)])
        #expect(!found.details.contains { if case .found = $0 { true } else { false } })

        let none = row(call("query", #"{"q":"cups"}"#, #"{"records":[]}"#))
        #expect(none.chips.isEmpty)
        #expect(none.details == [.found("Found nothing.", [])])
        // One record found is named in the line, and its link stays inside.
        let one = row(call("query", #"{"q":"cups"}"#, #"{"records":[{"kind":"\#(widget)","id":"w1","properties":{"title":"Mug"}}]}"#))
        #expect(one.summary == "Searched for “cups”, found “Mug”")
        #expect(one.chips.isEmpty)
        #expect(one.details == [.found("Found 1 record.", [RecordReference(kind: widget, id: "w1")])])
    }

    @Test func aSearchShowsTheFirstThreeRecordsAndTheRestOnAsking() throws {
        let records = ["Mug", "Cup", "Bowl", "Plate", "Jug"].enumerated().map { i, title in
            #"{"kind":"\#(widget)","id":"w\#(i)","properties":{"title":"\#(title)"}}"#
        }
        let presented = row(call("query", #"{"q":"kitchen"}"#, #"{"records":[\#(records.joined(separator: ","))]}"#))
        let chips = try #require(presented.chips.first)
        #expect(chips.records.map(\.title) == ["Mug", "Cup", "Bowl", "Plate", "Jug"])
        #expect(RecordChips.first == 3)
        #expect(chips.more(shown: RecordChips.first) == "and 2 more")
        #expect(chips.more(shown: chips.records.count) == nil)
    }

    @Test func aWriteNamesTheOneRecordItChangedAndTheFieldsInItsLine() {
        let presented = row(
            call(
                "write", #"{"op":"patch","kind":"crew.test.dev/crew/author","id":"a1","input":{"properties":{"description":"…"}}}"#,
                #"{"record":{"id":"a1","kind":"crew.test.dev/crew/author","properties":{"title":"Hannu Rajaniemi"}}}"#))
        #expect(presented.summary == "Changed the description of “Hannu Rajaniemi”")
        #expect(presented.unnamed == nil)
    }

    @Test func aFunctionsOwnChangesShowUnderItsOwnWordsUnopened() {
        var scrape = call(
            "web_scrape", #"{"url":"https://example.com/"}"#, ##"{"output":{"document":"d1","url":"https://example.com/","content":"# Hi"},"effects":1}"##,
            callable: "substrate.reamde.dev/core/function/providers.example.com/web/scrape")
        scrape.changes = [ChangeStamp(seq: 9, op: "put", kind: "providers.example.com/web/page", id: "d1")]
        let presented = row(scrape)
        #expect(presented.chips == [RecordChips(lead: "Saved", label: "Saved records", records: [chip("providers.example.com/web/page", "d1")], total: 1)])
        #expect(!presented.details.contains { if case .changed = $0 { true } else { false } })
    }

    @Test func aHostCallsOneChangeStaysInsideAndSeveralShowByVerb() {
        var one = call(
            "edit", #"{"op":"replace","kind":"ada.localhost/notes/note","id":"n1","property":"body","edits":[{},{}]}"#,
            #"{"kind":"ada.localhost/notes/note","id":"n1","edits":2}"#)
        one.changes = [ChangeStamp(seq: 3, op: "patch", kind: "ada.localhost/notes/note", id: "n1")]
        let edited = row(one, titles: ["ada.localhost/notes/note/n1": "Garden"])
        // The line already names it: its link is inside.
        #expect(edited.summary == "Edited the body of “Garden” in 2 places")
        #expect(edited.chips.isEmpty)
        #expect(
            edited.details.contains(
                .changed([RecordChips(lead: "Changed", label: "Changed records", records: [chip("ada.localhost/notes/note", "n1", "Garden")], total: 1)])))

        var several = call("vocabulary", #"{"documents":[]}"#, #"{"applied":3}"#)
        several.changes = [
            ChangeStamp(seq: 4, op: "put", kind: Kinds.kind, id: "ada.localhost/garden/plant"),
            ChangeStamp(seq: 5, op: "put", kind: Kinds.kind, id: "ada.localhost/garden/bed"),
            ChangeStamp(seq: 6, op: "delete", kind: Kinds.kind, id: "ada.localhost/garden/pot"),
        ]
        let declared = row(several)
        #expect(declared.chips.map(\.lead) == ["Saved", "Deleted"])
        #expect(declared.chips.map(\.records.count) == [2, 1])
        #expect(declared.chips.first?.label == "Saved records")
    }
}
