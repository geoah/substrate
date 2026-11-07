import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import Testing

@testable import ChatKit

private let moss = "substrate.reamde.dev/assistant/moss"

private func memory(_ id: String, _ text: String, version: Int = 3, active: Bool = true, at: String = "2026-11-07T09:00:00Z") -> Record {
    Record(
        id: id, kind: Kinds.memory, properties: ["text": .string(text), "active": .bool(active), "source": "agent"], version: version,
        createdAt: at, updatedAt: at)
}

private func instruction(_ id: String, _ text: String, version: Int = 2) -> Record {
    Record(
        id: id, kind: Kinds.instruction,
        properties: ["text": .string(text), "active": true, "agent": ["ref": .string("\(Kinds.agent)/\(moss)")]], version: version,
        createdAt: "2026-11-07T08:00:00Z", updatedAt: "2026-11-07T08:00:00Z")
}

private let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)

private func held(_ memories: [Record], _ instructions: [Record] = []) -> ChatAppState {
    var state = signedInState()
    state.guidance.memories = memories
    state.guidance.instructions = instructions
    state.guidance.read = true
    return state
}

private func step(_ step: ChatAppStep.GuidanceStep, _ state: ChatAppState) throws -> ChatAppState {
    try app.apply(.init(.guidance(step)), to: state)
}

@Suite("Settings → Assistant", .timeLimit(.minutes(10)))
struct GuidanceSettings {
    let vegetarian = memory("m1", "I am vegetarian")
    var reference: RecordReference { RecordReference(kind: Kinds.memory, id: "m1") }

    @Test func aSwitchShowsAtOnceAndWritesOverTheVersionShown() throws {
        let next = try step(.changeAsked(reference, .active(false)), held([vegetarian]))
        #expect(next.guidance.memories.first?["active"] == false)
        #expect(next.guidance.busy == [reference.path])
        #expect(next.asks == [.writeGuidance(reference, properties: ["active": false], before: vegetarian)])
        // One write at a time: the next waits for this one to land.
        let again = try step(.changeAsked(reference, .active(true)), next)
        #expect(again.asks.isEmpty)
        var written = vegetarian
        written.properties["active"] = false
        written.version = 4
        let landed = try step(.written(written), next)
        #expect(landed.guidance.busy.isEmpty)
        #expect(landed.guidance.memories == [written])
    }

    @Test func aRewordingShowsAtOnce() throws {
        let next = try step(.changeAsked(reference, .text("I eat fish too.")), held([vegetarian]))
        #expect(next.guidance.memories.first?["text"] == "I eat fish too.")
        #expect(next.asks == [.writeGuidance(reference, properties: ["text": "I eat fish too."], before: vegetarian)])
    }

    @Test func aRecordThatMovedIsPutBackSaidAndReadAgain() throws {
        let next = try step(.changeAsked(reference, .text("Vegan")), held([vegetarian]))
        let moved = try step(
            .writeFailed(reference, Failure(kind: .refused(status: 409, permanent: false), words: "version mismatch"), before: vegetarian), next)
        #expect(moved.guidance.memories == [vegetarian])
        #expect(moved.guidance.busy.isEmpty)
        #expect(moved.guidance.failed[reference.path] == "It changed since you opened it, so nothing was saved. This is what it says now.")
        #expect(moved.asks == [.readGuidance])
        // A refusal of another kind says the server's words and reads nothing.
        let refused = try step(.writeFailed(reference, Failure(kind: .other, words: "The server is busy."), before: vegetarian), next)
        #expect(refused.guidance.failed[reference.path] == "That didn't go through: The server is busy.")
        #expect(refused.asks.isEmpty)
        // The next change of it starts clean.
        #expect(try step(.changeAsked(reference, .active(false)), moved).guidance.failed.isEmpty)
    }

    @Test func aSwitchOrARewordingOfOneDeletedSinceIsPutBackSaidAndReadAgain() throws {
        let gone = Failure(kind: .refused(status: 404, permanent: true), words: "record m1 is deleted; a put restores it")
        for change in [GuidanceChange.active(false), .text("Vegan")] {
            let next = try step(.changeAsked(reference, change), held([vegetarian]))
            let missing = try step(.writeFailed(reference, gone, before: vegetarian), next)
            #expect(missing.guidance.memories == [vegetarian])
            #expect(missing.guidance.busy.isEmpty)
            #expect(missing.guidance.failed[reference.path] == "It was deleted since you opened it, so nothing was saved.")
            #expect(missing.asks == [.readGuidance])
        }
    }

    @Test func aDeleteLeavesAtOnceAndComesBackWhereItWasWhenRefused() throws {
        let older = memory("m0", "Call me Sam", at: "2026-11-01T09:00:00Z")
        let next = try step(.changeAsked(reference, .delete), held([vegetarian, older]))
        #expect(next.guidance.memories == [older])
        #expect(next.asks == [.deleteGuidance(reference, before: vegetarian)])
        let back = try step(.writeFailed(reference, Failure(kind: .other, words: "offline"), before: vegetarian), next)
        #expect(back.guidance.memories == [vegetarian, older])
        let gone = try step(.deleted(reference), next)
        #expect(gone.guidance.memories == [older] && gone.guidance.busy.isEmpty)
    }

    @Test func eachListSaysWhatItHoldsAndAnInstructionNamesItsAgent() throws {
        var state = held([vegetarian, memory("m2", "Summaries in bullet points", active: false)], [instruction("i1", "Go by Juniper")])
        state.guidance.failed["\(Kinds.memory)/m2"] = "That didn't go through: offline"
        let section = app.settings(state).guidance
        #expect(section.title == "Assistant")
        #expect(section.loading == nil)
        #expect(section.memories.count == "1 of 2 on")
        #expect(section.memories.rows.map(\.text) == ["I am vegetarian", "Summaries in bullet points"])
        #expect(section.memories.rows[0].facts == [LabeledValue("Added by", "an assistant, accepted by you")])
        #expect(section.memories.rows[1].problem == "That didn't go through: offline")
        #expect(section.instructions.count == "1")
        #expect(section.instructions.rows[0].facts == [LabeledValue("For", "Moss"), LabeledValue("Added by", "you")])
        let empty = app.settings(signedInState()).guidance
        #expect(empty.loading == "Loading…")
        #expect(empty.memories.count == "None")
    }

    @Test func theyAreReadWhenSettingsOpensAndWrittenWithTheirVersion() async throws {
        let memories =
            #"{"head":10,"generation":"g","records":[{"id":"m1","kind":"substrate.reamde.dev/llm/memory","version":3,"createdAt":"2026-11-07T09:00:00Z","updatedAt":"2026-11-07T09:00:00Z","properties":{"text":"I am vegetarian","active":true,"source":"agent"}}]}"#
        let instructions =
            #"{"head":10,"generation":"g","records":[{"id":"i1","kind":"substrate.reamde.dev/llm/instruction","version":2,"createdAt":"2026-11-07T08:00:00Z","updatedAt":"2026-11-07T08:00:00Z","properties":{"text":"Go by Juniper","active":true,"agent":{"ref":"substrate.reamde.dev/core/agent/substrate.reamde.dev/assistant/moss"}}}]}"#
        let switched =
            #"{"id":"m1","kind":"substrate.reamde.dev/llm/memory","version":4,"createdAt":"2026-11-07T09:00:00Z","updatedAt":"2026-11-07T10:00:00Z","properties":{"text":"I am vegetarian","active":false,"source":"agent"}}"#
        let rig = ChatRig(
            [
                .init(method: "GET", path: "/api/v1/records", query: "llm/memory", answer: .json(200, memories)),
                .init(method: "GET", path: "/api/v1/records", query: "llm/instruction", answer: .json(200, instructions)),
                .init(method: "PATCH", path: "/llm/memory/m1", answer: .json(200, switched)),
            ] + baseRoutes())
        try await rig.signIn()
        await rig.harness.send(.readSettings)
        try await rig.harness.until { await $0.presentation.settings.guidance.instructions.rows.count == 1 }
        let section = await rig.harness.presentation.settings.guidance
        #expect(section.memories.rows.map(\.text) == ["I am vegetarian"])
        #expect(section.instructions.rows.first?.facts.first == LabeledValue("For", "Moss"))
        await rig.harness.send(.changeGuidance(reference, .active(false)))
        try await rig.harness.until { harness in
            let row = await harness.presentation.settings.guidance.memories.rows.first
            return row?.active == false && row?.busy == false
        }
        let body = try #require(rig.server.body("PATCH", "/llm/memory/m1"))
        #expect(JSONValue.parse(body) == ["properties": ["active": false], "ifVersion": 3])
        // The text is required: an empty one is not sent, and one with
        // space around it is sent without.
        await rig.harness.send(.changeGuidance(reference, .text("  \n ")))
        #expect(await rig.harness.state.guidance.busy.isEmpty)
        await rig.harness.send(.changeGuidance(reference, .text("  I eat fish too. ")))
        try await rig.harness.until { await $0.state.guidance.busy.isEmpty }
        let reworded = try #require(rig.server.body("PATCH", "/llm/memory/m1"))
        #expect(JSONValue.parse(reworded) == ["properties": ["text": "I eat fish too."], "ifVersion": 4])
    }

    @Test func aDeleteOfOneAlreadyGoneIsDoneAndASwitchOfOneDeletedSinceReadsTheListAgain() async throws {
        let memories =
            #"{"head":10,"generation":"g","records":[{"id":"m1","kind":"substrate.reamde.dev/llm/memory","version":3,"createdAt":"2026-11-07T09:00:00Z","updatedAt":"2026-11-07T09:00:00Z","properties":{"text":"I am vegetarian","active":true,"source":"agent"}},{"id":"m0","kind":"substrate.reamde.dev/llm/memory","version":1,"createdAt":"2026-11-01T09:00:00Z","updatedAt":"2026-11-01T09:00:00Z","properties":{"text":"Call me Sam","active":true,"source":"user"}}]}"#
        let rig = ChatRig(
            [
                .init(method: "GET", path: "/api/v1/records", query: "llm/memory", answer: .json(200, memories)),
                .init(method: "GET", path: "/api/v1/records", query: "llm/instruction", answer: .json(200, emptyPage)),
                .init(method: "DELETE", path: "/llm/memory/m1", answer: .json(404, #"{"error":{"code":"not_found","message":"record m1"}}"#)),
                .init(
                    method: "PATCH", path: "/llm/memory/m0",
                    answer: .json(404, #"{"error":{"code":"not_found","message":"record m0 is deleted; a put restores it"}}"#)),
            ] + baseRoutes())
        try await rig.signIn()
        await rig.harness.send(.readSettings)
        try await rig.harness.until { await $0.presentation.settings.guidance.memories.rows.count == 2 }
        // Gone already: the delete is done, nothing is said and nothing read again.
        await rig.harness.send(.changeGuidance(reference, .delete))
        try await rig.harness.until { await $0.state.guidance.busy.isEmpty }
        let left = await rig.harness.presentation.settings.guidance.memories.rows
        #expect(left.map(\.text) == ["Call me Sam"])
        #expect(left.first?.problem == nil)
        #expect(rig.server.count("DELETE /api/v1/substrate.reamde.dev/llm/memory/m1") == 1)
        #expect(rig.server.count("/api/v1/records", query: "llm/memory") == 1)
        // Deleted elsewhere since the list was read: the switch is refused
        // and the list read again, which no longer holds it.
        rig.server.route("GET", "/api/v1/records", query: "llm/memory", .json(200, emptyPage))
        await rig.harness.send(.changeGuidance(RecordReference(kind: Kinds.memory, id: "m0"), .active(false)))
        try await rig.harness.until { harness in
            let guidance = await harness.state.guidance
            return guidance.busy.isEmpty && !guidance.reading
        }
        #expect(await rig.harness.presentation.settings.guidance.memories.rows.isEmpty)
        #expect(rig.server.count("/api/v1/records", query: "llm/memory") == 2)
    }
}
