import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime

/// The server first (§5.6's Connect): one field that takes a server's
/// address or a repository's name, resolved by `ServerLocator` and checked
/// against the server's discovery document and the app's requirement when
/// the person presses Continue. A server found from a repository's name
/// carries that name to the sign-in. A server that can give the app access
/// of its own offers the browser sign-in; one the app falls back for shows
/// the app's own password door; one too old says what it lacks. Without a
/// password door, a server the app would fall back for is too old.
public struct ConnectController: Controller {
    public enum Command: Bridged, Equatable {
        case edit(String)
        /// The screen's one button: Continue to find the server, then sign
        /// in to it.
        case proceed
        case cancelSignIn
        case changeServer
        /// The too-old screen's Try Again: the same address checked again.
        case checkAgain
    }

    public enum Event: Codable, Equatable, Sendable {
        case edited(String)
        case refusedAddress
        case checkStarted(String)
        case located(ServerProbe.Result)
        case locateFailed(problem: String?, retryable: Bool)
        case signInStarted
        case signedIn
        case signInFailed(problem: String?)
        case signInCancelled
        case serverChanged
    }

    public enum Effect: Equatable, Sendable {
        case locate(String)
        case signIn(ServerProbe.Result)
        case cancelSignIn
    }

    public typealias State = ConnectState
    public typealias Presentation = ConnectPresentation

    public let manifest: AppManifest
    public let initialAddress: String
    public let hasPasswordDoor: Bool
    /// The sign-in leaves the app for the default browser, so the screen
    /// offers its own Cancel: there is no sheet to close.
    public let opensBrowser: Bool
    public let unrevoked: [SessionFacts]
    let locator: ServerLocator
    let signIn: @Sendable (ServerProbe.Result) async throws -> Void

    /// - Parameters:
    ///   - signIn: the app's sign-in to the server found: the browser, then
    ///     keeping the session. A `SignInCancelled` is the person closing it.
    public init(
        manifest: AppManifest, address: String = "", locator: ServerLocator, hasPasswordDoor: Bool = false,
        opensBrowser: Bool = false, unrevoked: [SessionFacts] = [],
        signIn: @escaping @Sendable (ServerProbe.Result) async throws -> Void
    ) {
        self.manifest = manifest
        initialAddress = address
        self.locator = locator
        self.hasPasswordDoor = hasPasswordDoor
        self.opensBrowser = opensBrowser
        self.unrevoked = unrevoked
        self.signIn = signIn
    }

    public var initialState: ConnectState { ConnectState(address: initialAddress) }

    /// The door a server takes here: the manifest's, with the password door
    /// read as too old where the app gave none.
    public func door(for found: ServerProbe.Result) -> SignInDoor {
        let door = manifest.door(for: found)
        return door == .password && !hasPasswordDoor ? .tooOld : door
    }

    // MARK: The controller

    public func events(for command: Command, in context: Enrichment<ConnectController>) -> [Event] {
        let state = context.state
        switch command {
        case let .edit(text):
            return [.edited(text)]
        case .proceed:
            guard !state.busy else { return [] }
            return state.found == nil ? check(state.address) : [.signInStarted]
        case .checkAgain:
            return check(state.address)
        case .cancelSignIn:
            return [.signInCancelled]
        case .changeServer:
            return [.serverChanged]
        }
    }

    private func check(_ address: String) -> [Event] {
        ServerAddress.origin(of: address) == nil ? [.refusedAddress] : [.checkStarted(address)]
    }

    // MARK: The engine

    public func apply(_ event: Event, to state: ConnectState) throws -> ConnectState {
        var next = state
        switch event {
        case let .edited(text):
            next.address = text
            // Writing the checked origin back is not a new address.
            if let found = state.found, text == ConnectState.shown(found) { return next }
            if state.found != nil { next.phase = .idle }
            next.problem = nil
            next.retryable = false
        case .refusedAddress:
            next.problem = "That is not a server address or a repository name."
        case .checkStarted:
            next.phase = .checking
            next.problem = nil
        case let .located(result):
            next.address = ConnectState.shown(result)
            next.addressRevision += 1
            next.retryable = false
            next.phase = .found(result)
        case let .locateFailed(problem, retryable):
            next.phase = .idle
            next.retryable = retryable
            if let problem { next.problem = problem }
        case .signInStarted:
            guard let found = state.found else { return state }
            next.phase = .signingIn(found)
            next.problem = nil
        case .signedIn, .signInCancelled:
            if case let .signingIn(found) = state.phase { next.phase = .found(found) }
        case let .signInFailed(problem):
            if case let .signingIn(found) = state.phase { next.phase = .found(found) }
            if let problem { next.problem = problem }
        case .serverChanged:
            next = ConnectState(addressRevision: state.addressRevision + 1)
        }
        return next
    }

    // MARK: The effects

    public func effects(of event: Event, on state: ConnectState, presented presentation: ConnectPresentation) -> [Effect] {
        switch event {
        case let .checkStarted(address): [.locate(address)]
        case .signInStarted: state.found.map { [.signIn($0)] } ?? []
        case .signInCancelled: [.cancelSignIn]
        default: []
        }
    }

    public func key(of effect: Effect) -> String? {
        switch effect {
        case .locate: "locate"
        case .signIn, .cancelSignIn: "signIn"
        }
    }

    public func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        switch effect {
        case let .locate(typed):
            let (locator, manifest) = (locator, manifest)
            return .once {
                do {
                    return .located(try await locator.locate(typed, requirement: manifest.requirement, app: manifest.appName))
                } catch {
                    let problem =
                        isCancellation(error)
                        ? nil
                        : (error as? LocalizedError)?.errorDescription
                            ?? explain(error, app: manifest.appName, server: ServerAddress.origin(of: typed))
                    return .locateFailed(problem: problem, retryable: error is ServerLocator.LookupFailed)
                }
            }
        case let .signIn(found):
            let (signIn, app) = (signIn, manifest.appName)
            return .once {
                do {
                    try await signIn(found)
                    return .signedIn
                } catch is SignInCancelled {
                    // The person closed the sheet: nothing to say.
                    return .signInFailed(problem: nil)
                } catch {
                    return .signInFailed(problem: isCancellation(error) ? nil : explain(error, app: app, server: found.origin))
                }
            }
        case .cancelSignIn:
            return .cancel
        }
    }

    // MARK: The presenter

    public func present(_ input: PresenterInput<ConnectController>) -> ConnectPresentation {
        let state = input.state
        let app = manifest.appName
        let found = state.found
        let door: ConnectPresentation.Door =
            found.map { found in
                switch self.door(for: found) {
                case .app: .browser
                case .password: .password
                case .tooOld: .tooOld
                }
            } ?? .none
        let unrevoked = UnrevokedPresenter.present(appName: app, unrevoked: unrevoked)
        let field = ConnectPresentation.Field(
            text: state.address, placeholder: "ada.example.com", label: "Repository or server",
            disabled: state.busy, revision: state.addressRevision)
        if let found, door == .tooOld, case let .tooOld(running, needed, missing) = found.verdict {
            return ConnectPresentation(
                field: field, door: door, found: found,
                tooOld: .present(
                    appName: app, host: found.host, running: running, needed: needed, missing: missing,
                    tryAgain: true, changeServer: true),
                problem: nil, button: nil, cancel: nil, footer: nil, unrevoked: unrevoked)
        }
        let problem = state.problem.map {
            ProblemPresentation(title: found == nil ? "Couldn't reach that server" : "Couldn't sign in", message: $0)
        }
        var signingIn = false
        if case .signingIn = state.phase { signingIn = true }
        let title: String =
            switch state.phase {
            case .idle: state.retryable ? "Try Again" : "Continue"
            case .checking: "Checking…"
            case .found: opensBrowser ? "Continue in Your Browser" : "Sign In with Your Substrate"
            case .signingIn: "Waiting for Your Browser…"
            }
        let button =
            door == .password
            ? nil
            : ConnectPresentation.Button(
                title: title, busy: state.busy,
                enabled: !state.busy && !(found == nil && state.address.trimmingCharacters(in: .whitespaces).isEmpty))
        let footer: ConnectPresentation.Footer
        if let found {
            let note =
                if door == .password {
                    "This server can't give \(app) access of its own yet, so your password goes to it directly."
                } else if signingIn, opensBrowser {
                    "\(found.host)'s sign-in page is open in your browser. Sign in there and choose what \(app) may do; "
                        + "this window comes back when you're done."
                } else {
                    "You sign in on \(found.host) itself and choose what \(app) may do there. "
                        + "Later, Change Access in this app asks again; to revoke, use \(found.host)'s Settings, under Apps."
                }
            footer = .init(
                found: LabeledValue("Server found", found.host, verbatim: true),
                repository: found.repository.map { LabeledValue("Repository", $0, verbatim: true) }, note: note)
        } else {
            footer = .init(
                found: nil, repository: nil,
                note: "Your repository's name, such as ada.example.com, or your Substrate server's address. A name finds its server on its own.")
        }
        return ConnectPresentation(
            field: field, door: door, found: found, tooOld: nil, problem: problem, button: button,
            cancel: signingIn && opensBrowser ? "Cancel" : nil, footer: footer, unrevoked: unrevoked)
    }
}
