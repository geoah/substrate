#if os(Android)
import Android
import Foundation
import SubstrateJNI

/// libdispatch's main queue, run by Android's main Looper: without it no
/// `@MainActor` work and nothing on `DispatchQueue.main` ever runs
/// (docs/plans/app-kit.md §3.2). The queue signals an eventfd when it has
/// work, and the Looper drains it on the thread it belongs to.
enum MainLooper {
    /// Binds the calling thread's Looper, which must be the main thread's.
    static func bind() -> BridgeFailure? {
        guard gettid() == getpid() else {
            return BridgeFailure(.swiftFailure, "start ran off the main thread; call it from Application.onCreate")
        }
        guard let looper = ALooper_forThread() else {
            return BridgeFailure(.swiftFailure, "the main thread has no Looper")
        }
        let fd = _dispatch_get_main_queue_handle_4CF()
        guard fd >= 0 else { return BridgeFailure(.swiftFailure, "libdispatch has no main queue handle") }
        let added = ALooper_addFd(looper, fd, Int32(ALOOPER_POLL_CALLBACK), Int32(ALOOPER_EVENT_INPUT), { fd, _, _ in
            var counter: UInt64 = 0
            _ = read(fd, &counter, MemoryLayout<UInt64>.size)
            _dispatch_main_queue_callback_4CF(nil)
            return 1
        }, nil)
        return added == 1 ? nil : BridgeFailure(.swiftFailure, "the main Looper refused libdispatch's queue (ALooper_addFd \(added))")
    }
}
#endif
