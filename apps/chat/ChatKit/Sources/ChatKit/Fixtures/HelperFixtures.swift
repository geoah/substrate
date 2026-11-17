import Foundation
import SubstrateCore

extension ChatFixtures {
    /// The chat whose assistant asked other assistants for help.
    public static let helpersThread = "t-helpers"

    /// A chat whose assistant called on helpers twice: first the architect
    /// (which ran out of time), the secretary (which answered) and the
    /// librarian (whose work in the background finished); then, as the
    /// chat stands, the architect again and the designer, running side by
    /// side, and the librarian working in the background. The times are
    /// the process's first instant (`base`) less a few minutes, so the
    /// running ones read about a minute and a half when it is opened.
    public static func helpers(thread: String = helpersThread) -> String {
        func agent(_ name: String) -> JSONValue { .string("\(Kinds.agent)/substrate.reamde.dev/assistant/\(name)") }
        func call(_ id: String, _ name: String, _ arguments: JSONValue) -> JSONValue {
            ["id": .string(id), "name": .string(name), "callable": agent(name), "arguments": .string(arguments.jsonText)]
        }
        func ran(_ from: Int, _ to: Int) -> JSONValue { ["startedAt": .string(stamp(from)), "finishedAt": .string(stamp(to))] }
        var ordinal = 0
        func row(at seconds: Int, _ properties: [String: JSONValue]) -> String {
            var properties = properties
            properties["turn"] = .number(JSONNumber(ordinal))
            properties["thread"] = ["ref": .string("\(Kinds.thread)/\(thread)")]
            defer { ordinal += 1 }
            return record(
                id: "\(thread)-m\(ordinal)", kind: Kinds.message, version: 1, at: seconds,
                properties: JSONValue.object(JSONObject(properties.sorted { $0.key < $1.key }.map { ($0.key, $0.value) })).jsonText)
        }

        let architectAsked =
            "The owner asked for this; please apply it. Create kinds to track trips: a trip (name, destination, start and end dates, status planned, ongoing or done), and flights and hotel stays linked to it, each with its booking reference and times with their zones."
        let secretaryAsked =
            "Read only, send and draft nothing. Find the owner's Lisbon trip bookings in mail and the calendar: the flights out on 9 May and back on 16 May, both hotel stays, and any restaurant reservations."
        let librarianAsked =
            "Save these places from the owner's Lisbon trip into places, searching first so nothing is saved twice: Livraria Bertrand, Cervejaria Ramiro, the Gulbenkian, Time Out Market, and the Tile Museum."
        let secretaryReply = """
            I found both flights and both stays, and one dinner booking. Nothing was sent or drafted.

            **Flights**
            - **Out:** TP1351, Sat 9 May, London Heathrow 07:10 to Lisbon 09:45.
            - **Back:** TP1356, Sat 16 May, Lisbon 18:20 to London Heathrow 20:55.

            **Stays**
            - Casa do Rio, 9 to 13 May, confirmation in your mail of 2 April.
            - Hotel das Janelas, 13 to 16 May.

            **Dinner:** Cervejaria Ramiro, Tue 12 May at 20:30, for two.
            """
        let stopped =
            "Architect ran out of time: it had 5 min, its own time limit. Its last call, vocabulary, failed: substrate/engine: create index for ~/trips/flight: timeout: context deadline exceeded. That call was still running when the time ran out, so it is the call that took the time, and nothing it was doing was applied. Its daily spend was not what stopped it."

        let rows = [
            row(at: -905, ["role": "user", "content": "Plan the Lisbon trip properly: kinds for trips, my bookings from mail, and the places I went."]),
            row(
                at: -900,
                [
                    "role": "assistant",
                    "content":
                        "I'll have the architect make the trip kinds, the secretary find your bookings, and the librarian save the places, all at once.",
                    "toolCalls": [
                        call("c1", "architect", ["input": .string(architectAsked)]),
                        call("c2", "secretary", ["input": .string(secretaryAsked)]),
                        call("c3", "librarian", ["input": .string(librarianAsked)]),
                    ],
                ]),
            row(
                at: -598,
                [
                    "role": "tool", "toolCallId": "c1", "name": "architect", "callable": agent("architect"), "ok": true,
                    "content": .string(
                        JSONValue.object([
                            "reply": "", "thread": "t-architect", "status": "overbudget", "cause": "deadline", "reason": "deadline exceeded",
                            "failedCall": [
                                "tool": "vocabulary", "error": "substrate/engine: create index for ~/trips/flight: timeout: context deadline exceeded",
                            ],
                            "stopped": .string(stopped),
                        ]).jsonText),
                    "ran": ran(-899, -599), "subagentWrites": ["thread": ["ref": .string("\(Kinds.thread)/t-architect")], "records": 0],
                ]),
            row(
                at: -598,
                [
                    "role": "tool", "toolCallId": "c2", "name": "secretary", "callable": agent("secretary"), "ok": true,
                    "content": .string(JSONValue.object(["reply": .string(secretaryReply), "thread": "t-secretary", "status": "ok"]).jsonText),
                    "ran": ran(-899, -769), "subagentWrites": ["thread": ["ref": .string("\(Kinds.thread)/t-secretary")], "records": 0],
                ]),
            row(
                at: -598,
                [
                    "role": "tool", "toolCallId": "c3", "name": "librarian", "callable": agent("librarian"), "ok": true,
                    "content": #"{"job":"c3","status":"working"}"#, "ran": ran(-899, -898),
                    "job": ["state": "completed", "startedAt": .string(stamp(-598)), "finishedAt": .string(stamp(-559))],
                    "subagentWrites": [
                        "thread": ["ref": .string("\(Kinds.thread)/t-librarian")], "records": 17,
                        "kinds": ["ada.example.com/places/place", "ada.example.com/commerce/brand"],
                    ],
                ]),
            row(
                at: -559,
                [
                    "role": "system",
                    "content": .string(
                        JSONValue.object([
                            "event": "toolCompleted", "toolCallId": "c3", "name": "librarian", "callable": agent("librarian"), "ok": true,
                            "output": .string(
                                JSONValue.object([
                                    "reply": "Saved 5 places and the 12 brands they stock; none was there before. Each place is marked as visited in May.",
                                    "thread": "t-librarian", "status": "ok",
                                ]).jsonText),
                        ]).jsonText),
                ]),
            row(
                at: -84,
                [
                    "role": "assistant",
                    "content":
                        "The secretary found your flights and stays. The architect ran out of time while it built an index, so I'm asking it again with less at once, and the designer for a timeline while it works.",
                    "toolCalls": [
                        call(
                            "c4", "architect",
                            ["input": "Please carry on and apply just the trip and flight kinds; leave hotel stays for later.", "thread": "t-architect"]),
                        call("c5", "designer", ["input": "Sketch a trip timeline: one line per day, flights and stays on it, the places visited beneath."]),
                        call("c6", "librarian", ["input": "Link each saved place to the Lisbon trip once it exists."]),
                    ],
                ]),
            row(
                at: -80,
                [
                    "role": "tool", "toolCallId": "c6", "name": "librarian", "callable": agent("librarian"), "ok": true,
                    "content": #"{"job":"c6","status":"working"}"#, "ran": ran(-83, -82),
                    "job": ["state": "working", "startedAt": .string(stamp(-80))],
                ]),
        ]
        return page(rows.reversed())
    }
}
