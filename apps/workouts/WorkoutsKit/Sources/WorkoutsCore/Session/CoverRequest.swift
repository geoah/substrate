/// Whether the session cover should show, asked by the session at Start,
/// Finish, Discard and the first foreground with a session open. `serial`
/// moves with each request, so the app follows the request, not the value.
public struct CoverRequest: Codable, Equatable, Sendable {
    public var shown: Bool
    public var serial: Int

    public init(shown: Bool, serial: Int) {
        self.shown = shown
        self.serial = serial
    }
}
