import Foundation
import SubstrateTesting
import Testing

@testable import ChatKit

/// A new chat picks its assistant from its title: the screen carries the
/// picker's words, the composer carries none, and a chat with a thread has
/// no picker, since its assistant is the thread's.
@Suite("The assistant's picker", .timeLimit(.minutes(10)))
struct PickerTests {
    func twoAgents() -> [ChatServer.Route] {
        let agents = #"{"head":10,"generation":"g","records":[\#(agentJSON(helper)),\#(agentJSON(otherAgent))]}"#
        return [.init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, agents))] + baseRoutes()
    }

    @Test func aNewChatNamesItsAssistantInItsTitle() async throws {
        let rig = ChatRig(twoAgents())
        try await rig.signIn()
        let key = try await rig.newChat()
        let screen = try #require(await rig.conversation(key))
        #expect(screen.title == "New Chat")
        let picker = try #require(screen.picker)
        #expect(picker.current == helper)
        #expect(picker.name == "Helper")
        #expect(picker.spoken == "Assistant: Helper")
        #expect(picker.hint == "Chooses the assistant this chat goes to.")
    }

    @Test func choosingAnotherAssistantMovesThePicker() async throws {
        let rig = ChatRig(twoAgents())
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.harness.until { await $0.state.offered.count == 2 }
        try await rig.send(.choose(agent: otherAgent), to: key)
        let screen = try #require(await rig.conversation(key))
        #expect(screen.picker == AgentPicker(current: otherAgent, name: "Other", spoken: "Assistant: Other", hint: "Chooses the assistant this chat goes to."))
        #expect(screen.composer.placeholder == "Message Other")
    }

    @Test func aChatWithAThreadHasNoPicker() async throws {
        let threads = #"{"head":10,"generation":"g","records":[\#(threadJSON(version: 3))]}"#
        let rig = ChatRig(baseRoutes(threads: threads))
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        #expect(try #require(await rig.conversation(key)).picker == nil)
    }

    @Test func noAssistantOfferedIsSaid() {
        #expect(
            ChatApp.picker(nil)
                == AgentPicker(current: nil, name: "No assistant", spoken: "Assistant: No assistant", hint: "Chooses the assistant this chat goes to."))
    }
}
