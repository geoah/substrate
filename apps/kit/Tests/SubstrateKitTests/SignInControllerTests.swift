import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateScreens
import Testing

/// The Connect screen as a root of its own: every step is Connect's, and a
/// finished sign-in moves the signal the UI opens the link again on.
@Suite("The sign-in root")
struct SignInControllerTests {
    struct NoLookups: TXTResolver {
        func txt(_ name: String) async throws -> TXTAnswer { .failed("no lookups here") }
    }

    let controller = SignInController(
        ConnectController(
            manifest: AppManifest(appName: "Test", clientID: "http://127.0.0.1/test", scopes: []),
            locator: ServerLocator(probe: ServerProbe(), resolver: NoLookups()), opensBrowser: true
        ) { _ in })

    let now = Date(timeIntervalSince1970: 1_800_000_000)

    func enrichment(_ state: SignInController.State) -> Enrichment<SignInController> {
        Enrichment(
            state: state, transient: NoTransient(), presentation: nil, now: now, link: nil,
            environment: AppEnvironment(
                locale: Locale(identifier: "en_US"), timeZone: TimeZone(identifier: "UTC")!,
                devices: DeviceServices(transport: URLSessionTransport.shared)))
    }

    func present(_ state: SignInController.State) -> SignInPresentation {
        controller.present(PresenterInput(state: state, transient: NoTransient(), now: now))
    }

    @Test func commandsAndTheScreenAreConnects() throws {
        let state = controller.initialState
        #expect(controller.events(for: .edit("ada.example.com"), in: enrichment(state)) == [.edited("ada.example.com")])
        let edited = try controller.apply(.edited("ada.example.com"), to: state)
        #expect(edited.connect.address == "ada.example.com")
        #expect(present(edited).connect.field.text == "ada.example.com")
        #expect(present(edited).signedIn == 0)
    }

    @Test func onlyAFinishedSignInMovesTheSignal() throws {
        var state = controller.initialState
        state = try controller.apply(.signInFailed(problem: "refused"), to: state)
        state = try controller.apply(.signInCancelled, to: state)
        #expect(present(state).signedIn == 0)
        state = try controller.apply(.signedIn, to: state)
        #expect(present(state).signedIn == 1)
        state = try controller.apply(.signedIn, to: state)
        #expect(present(state).signedIn == 2)
    }

    @Test func aSignInsEffectsAndTheirKeyAreConnects() {
        let state = controller.initialState
        #expect(controller.effects(of: .checkStarted("ada.example.com"), on: state, presented: present(state)) == [.locate("ada.example.com")])
        #expect(controller.key(of: .cancelSignIn) == "signIn")
    }
}
