import Foundation
import SubstrateCore
import SubstrateRuntime
import SubstrateScreens

/// The root behind Android's Gallery app: every shared screen in every
/// state, made by the screens' own presenters on the device itself and sent
/// through the bridge as any root's presentation is, so the app proves the
/// generated Kotlin reads what Swift writes (docs/plans/app-kit.md §7.4).
public struct GalleryRoot: Controller {
    public enum Command: Bridged, Equatable {
        /// Presents the fixtures again, made afresh.
        case refresh
    }

    public init() {}

    public var initialState: Int { 0 }

    public func events(for command: Command, in _: Enrichment<GalleryRoot>) -> [Int] { [1] }

    public func apply(_ event: Int, to state: Int) throws -> Int { state + event }

    public func present(_ input: PresenterInput<GalleryRoot>) -> GalleryPresentation {
        GalleryPresentation(drawn: input.state, fixtures: ScreenFixtures.all)
    }
}

/// Every fixture of `ScreenFixtures`, in its order, and how many times the
/// gallery was asked to draw them again.
public struct GalleryPresentation: Bridged, Equatable {
    public var drawn: Int
    public var fixtures: [ScreenFixture]
}
