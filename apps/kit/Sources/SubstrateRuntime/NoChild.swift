/// The child of a controller that keeps none: it has no value, so nothing
/// makes one.
public enum NoChild: Controller, Equatable {
    public typealias Command = Never
    public typealias Event = Never
    public typealias Presentation = Never
    public typealias State = Never

    public var initialState: Never {
        switch self {}
    }

    public func events(for command: Never, in context: Enrichment<NoChild>) -> [Never] {}
    public func apply(_ event: Never, to state: Never) throws -> Never {}
    public func present(_ input: PresenterInput<NoChild>) -> Never { input.state }
}
