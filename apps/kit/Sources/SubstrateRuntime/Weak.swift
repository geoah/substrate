/// A reference an effect's task carries back to the runtime without keeping
/// its target alive: a controller closed meanwhile is simply gone. Read only
/// on the runtime's executor, as the target is.
struct Weak<Target: AnyObject>: @unchecked Sendable {
    weak var target: Target?

    init(_ target: Target) {
        self.target = target
    }
}
