import SubstrateAPI
import Testing

/// What a webhook trigger's parked list says of each row: a delivery the
/// server accepted and has not run, one it is running, and one waiting for
/// its own retry are no refusal; a delivery set aside is.
@Suite struct ParkedDeliveries {
    @Test func aDeliveryWaitingForTheServersRetryIsNoRefusal() {
        let waiting = ParkedDelivery(
            fireID: "hook-w", lastError: "temporary, retried automatically at 2026-11-13T09:00:15Z: the provider is down")
        #expect(waiting.isWaiting)
        #expect(!waiting.refused)
    }

    @Test func oneSetAsideWhenItsRetriesRanOutIs() {
        let setAside = ParkedDelivery(fireID: "hook-w", lastError: "the provider is down")
        #expect(!setAside.isWaiting)
        #expect(setAside.refused)
    }

    /// Compatibility cover, not the change: what was no refusal before a
    /// row could wait stays none. The test that fails with the old kit is
    /// aDeliveryWaitingForTheServersRetryIsNoRefusal.
    @Test func pendingAndRunningStayNoRefusals() {
        #expect(!ParkedDelivery(fireID: "a", lastError: "x", pending: true).refused)
        #expect(!ParkedDelivery(fireID: "p", lastError: "delivery accepted: the fire has not settled").refused)
        #expect(!ParkedDelivery(fireID: "b", lastError: "refused", running: true).refused)
    }
}
