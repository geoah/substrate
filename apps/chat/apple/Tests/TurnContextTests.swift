import Foundation
import Testing
import SubstrateKit
@testable import SubstrateChat

/// The turn context (decision 0196): the chat body carries the device's zone
/// and the records, and a user row's `contextRecords` reach its turn.
@Suite struct TurnContext {
    @Test func theChatBodyCarriesTheZoneAndTheRecords() {
        let body = API.chatBody(
            thread: nil, message: "what is due?",
            context: [RecordReference(kind: "ada.example.com/tasks/task", id: "q4")],
            timezone: "Europe/London")
        #expect(body["thread"] as? String == "")
        #expect(body["message"] as? String == "what is due?")
        #expect(body["timezone"] as? String == "Europe/London")
        #expect(body["context"] as? [String] == ["ada.example.com/tasks/task/q4"])
        let bare = API.chatBody(thread: "t1", message: "hi", context: [])
        #expect(bare["context"] == nil)
        #expect(bare["timezone"] as? String == TimeZone.current.identifier)
    }

    @Test func aUserRowsContextRecordsReachItsTurn() {
        let row = Record(id: "u", kind: Kinds.message, properties: [
            "role": "user", "content": "what is due?", "turn": 0,
            "context": "<turn_context>\nnow: x\n</turn_context>",
            "contextRecords": [["kind": "ada.example.com/tasks/task", "id": "q4"]],
        ], createdAt: "2026-11-01T09:41:00Z")
        let turns = transcript(of: [row])
        #expect(turns[0].content == "what is due?")
        #expect(turns[0].contextRecords == [RecordReference(kind: "ada.example.com/tasks/task", id: "q4")])
    }
}
