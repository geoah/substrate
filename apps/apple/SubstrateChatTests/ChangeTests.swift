import Foundation
import Testing
@testable import SubstrateChat

/// What a change panel shows comes from the transcript alone: a token that
/// cannot read the records still sees what was written to them.
@Suite struct ChangePanels {
    private let pad = "a.example.com/llm/scratchpad"

    @Test func aWriteShowsTheValuesItWasAskedForAndTheTitleItAnsweredWith() throws {
        let call = ToolCall(id: "w1", name: "write", callable: "function:substrate.reamde.dev:core:write",
                            arguments: #"{"op":"patch","kind":"\#(pad)","id":"p1","input":{"properties":{"note":"tent, stove"}}}"#,
                            output: #"{"record":{"kind":"\#(pad)","id":"p1","properties":{"name":"Packing list","note":"tent, stove","title":"Packing list"}}}"#,
                            ok: true)
        let change = try #require(writeChange(call))
        #expect(change.op == .patch)
        #expect(change.title == "Packing list")
        #expect(change.values == ["note": .string("tent, stove")])
        // An encoded input reads the same.
        var encoded = call
        encoded.arguments = #"{"op":"put","kind":"\#(pad)","input":"{\"properties\":{\"note\":\"x\"}}"}"#
        #expect(writeChange(encoded)?.values == ["note": .string("x")])
        #expect(writeChange(encoded)?.op == .put)
        // A failed write changed nothing.
        var failed = call
        failed.ok = false
        #expect(writeChange(failed) == nil)
    }

    @Test func aProposalCarriesItsKindTargetValuesAndReason() throws {
        let call = ToolCall(id: "c1", name: "propose", callable: "function:substrate.reamde.dev:core:propose",
                            arguments: #"{"op":"patch","kind":"\#(pad)","target":"p1","diff":{"properties":{"note":"hike on Sunday"}},"rationale":"Moves it."}"#,
                            output: #"{"id":"aaaaaaaaaaaa"}"#, ok: true)
        let asked = try #require(proposedChange(call))
        #expect(asked.change.kind == pad)
        #expect(asked.change.id == "p1")
        #expect(asked.change.values == ["note": .string("hike on Sunday")])
        #expect(asked.rationale == "Moves it.")
    }

    @Test func aDecisionCarriesTheValuesTheOwnerAppliedInstead() throws {
        let content = #"{"event":"proposalDecision","request":"substrate.reamde.dev/core/recordpatchrequest/r1","decision":"accepted","op":"patch","target":"\#(pad)/p1","adjustedDiff":{"properties":{"note":"hike on Monday"}}}"#
        let turn = Turn(id: "s", role: .system, content: content)
        let notice = try #require(decisionNotice(turn))
        #expect(notice.adjusted)
        #expect(notice.adjustedValues == ["note": .string("hike on Monday")])
        #expect(notice.target == "\(pad)/p1")
    }

    @Test func titlesComeFromWhatTheChatRead() {
        let found = ToolCall(id: "q1", name: "query", arguments: "{}",
                             output: #"{"records":[{"kind":"\#(pad)","id":"p1","properties":{"title":"Groceries"}},{"kind":"\#(pad)","id":"p2","properties":{"name":"Weekend plan"}}]}"#,
                             ok: true)
        let titles = recordTitles([Turn(id: "a", role: .assistant, content: "", tools: [found])])
        #expect(titles["\(pad)/p1"] == "Groceries")
        #expect(titles["\(pad)/p2"] == "Weekend plan")
        let stamped = RecordChange(op: "patch", kind: pad, id: "p1")
        #expect(stamped.titled(titles).title == "Groceries")
    }

    @Test func valuesReadAsWords() {
        #expect(valueWords(.null) == "Empty")
        #expect(valueWords(.bool(true)) == "Yes")
        #expect(valueWords(.number(3)) == "3")
        #expect(valueWords(.array([.string("a"), .string("b")])) == "a, b")
        #expect(valueWords(.object(["ref": .string("a.example.com/people/person/ada")])) == "Person ada")
        #expect(valueWords(.object(["value": .number(70), "unit": .object(["ref": .string("substrate.reamde.dev/core/unit/kg")])])) == "70 kg")
    }
}
