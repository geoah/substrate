#if canImport(UserNotifications)
import Foundation
import SubstrateDevice
import Testing

@testable import SubstrateDeviceApple

/// A notification's background action holds the app awake until the app
/// says it is done with it, and no longer than the limit.
@Suite("A background action's wait", .timeLimit(.minutes(1)))
struct ResponseWaitsTests {
    private let reply = NotificationResponse(id: "chat|t1|reply", category: "chat.reply", action: .named("reply"), text: "On my way")

    @Test func itEndsWhenTheAppSaysItIsDone() async {
        let waits = ResponseWaits(limit: .seconds(60))
        let key = ResponseWaits.key(reply)
        let waited = Task { await waits.wait(for: key) }
        while waits.count == 0 { await Task.yield() }
        waits.finish(key)
        await waited.value
        #expect(waits.count == 0)
    }

    @Test func oneFinishedBeforeItWasWaitedForDoesNotWait() async {
        let waits = ResponseWaits(limit: .seconds(60))
        let key = ResponseWaits.key(reply)
        waits.finish(key)
        await waits.wait(for: key)
        #expect(waits.count == 0)
    }

    @Test func itEndsAtTheLimitWhateverTheAppDoes() async {
        let waits = ResponseWaits(limit: .milliseconds(30))
        await waits.wait(for: ResponseWaits.key(reply))
        #expect(waits.count == 0)
    }

    @Test func anotherResponseIsAnotherWait() async {
        let waits = ResponseWaits(limit: .seconds(60))
        let approve = NotificationResponse(id: "chat|t1|card|p1", category: "chat.approval", action: .named("approve"))
        #expect(ResponseWaits.key(reply) != ResponseWaits.key(approve))
        let waited = Task { await waits.wait(for: ResponseWaits.key(approve)) }
        while waits.count == 0 { await Task.yield() }
        // The reply's end is not the approval's.
        waits.finish(ResponseWaits.key(reply))
        #expect(waits.count == 1)
        waits.finish(ResponseWaits.key(approve))
        await waited.value
    }
}
#endif
