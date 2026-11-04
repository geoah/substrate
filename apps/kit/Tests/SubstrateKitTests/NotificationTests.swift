import SubstrateCore
import SubstrateDevice
import SubstrateTesting
import Testing

@Suite("Notification responses, categories and requests", .timeLimit(.minutes(10)))
struct NotificationTests {
    @Test func aResponseBeforeAnyReaderIsKeptForTheFirst() async {
        let notifications = RecordedNotifications()
        let launched = NotificationResponse(id: "n1", category: "rest", action: .open, userInfo: ["session": "s1"])
        notifications.respond(launched)
        var reader = notifications.responses().makeAsyncIterator()
        #expect(await reader.next() == launched)
        let later = NotificationResponse(id: "n2", category: "reply", action: .named("reply"), text: "On my way")
        notifications.respond(later)
        #expect(await reader.next() == later)
    }

    @Test func categoriesAreRegisteredThroughTheService() async {
        let notifications = RecordedNotifications()
        let reply = NotificationCategory(
            id: "reply",
            actions: [
                NotificationAction(id: "answer", title: "Reply", input: NotificationTextInput(button: "Send", placeholder: "Message"))
            ])
        await notifications.register([reply])
        #expect(notifications.categories == [reply])
        #expect(notifications.calls == [.register([reply])])
    }

    // A request crosses to Android as a bridged value, so every field it
    // carries comes back as it went: shown at once, quiet in front, its
    // category and its user info.
    @Test func aRequestCrossesTheBridgeWithEveryField() throws {
        let request = NotificationRequest(
            id: "n", title: "Rest over", body: "Bench press, set 3", fireAt: nil, thread: "session",
            category: "rest", timeSensitive: true, showsWhileActive: false, userInfo: ["session": "s1"])
        let back = try ValueCoding.decode(NotificationRequest.self, from: ValueCoding.encode(request))
        #expect(back == request)
        #expect(back.fireAt == nil)
        #expect(!back.showsWhileActive)
    }
}
