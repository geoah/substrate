#if canImport(SwiftUI)
import SwiftUI
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDeviceApple
import SubstrateRuntime
import SubstrateScreens

/// The app's name and what it is for, above the address field.
public struct ConnectHeader: View {
    let appName: String
    let tagline: String?
    let icon: Image?

    public init(appName: String, tagline: String? = nil, icon: Image? = nil) {
        self.appName = appName
        self.tagline = tagline
        self.icon = icon
    }

    public var body: some View {
        VStack(spacing: 14) {
            if let icon {
                icon
                    .resizable()
                    .interpolation(.high)
                    .frame(width: 96, height: 96)
                    .shadow(color: .black.opacity(0.12), radius: 12, y: 6)
                    .accessibilityHidden(true)
            }
            Text(appName)
                .font(.largeTitle.weight(.bold))
            if let tagline {
                Text(tagline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }
        }
    }
}

/// The connect screen as its presentation words it: the field, the door the
/// server found takes, the problem, the button and the footer. Every change
/// is a command; the screen keeps no state but the field's focus.
public struct ConnectScreen<Header: View, PasswordDoor: View>: View {
    let presentation: ConnectPresentation
    let header: Header
    let passwordDoor: (ServerProbe.Result) -> PasswordDoor
    let send: (ConnectController.Command) -> Void
    @Binding var text: String
    @FocusState private var focused: Bool

    public init(_ presentation: ConnectPresentation, text: Binding<String>, send: @escaping (ConnectController.Command) -> Void,
                @ViewBuilder header: () -> Header, @ViewBuilder passwordDoor: @escaping (ServerProbe.Result) -> PasswordDoor)
    {
        self.presentation = presentation
        self.header = header()
        self.passwordDoor = passwordDoor
        self.send = send
        _text = text
    }

    public var body: some View {
        ScrollView {
            VStack(spacing: 28) {
                header.padding(.top, 48)

                addressField

                if let tooOld = presentation.tooOld {
                    ServerTooOldScreen(tooOld, onTryAgain: { send(.checkAgain) }, onChangeServer: {
                        send(.changeServer)
                        text = ""
                        focused = true
                    })
                } else {
                    if presentation.door == .password, let found = presentation.found {
                        passwordDoor(found)
                    }
                    if let problem = presentation.problem {
                        ProblemBox(problem).modifier(KitCard(tint: problem.tone.color))
                    }
                    if let button = presentation.button {
                        Button {
                            send(.proceed)
                        } label: {
                            HStack(spacing: 8) {
                                if button.busy { ProgressView().controlSize(.small) }
                                Text(button.title)
                            }
                            .frame(maxWidth: .infinity, minHeight: 30)
                        }
                        .buttonStyle(.borderedProminent)
                        .buttonBorderShape(.capsule)
                        .controlSize(.large)
                        .disabled(!button.enabled)
                        .keyboardShortcut(.defaultAction)
                    }
                    if let cancel = presentation.cancel {
                        Button(cancel) { send(.cancelSignIn) }
                            .buttonStyle(.borderless)
                            .keyboardShortcut(.cancelAction)
                    }
                    if let footer = presentation.footer {
                        footerView(footer)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.center)
                    }
                }
                UnrevokedNotices(presentation.unrevoked)
                    .modifier(KitCard(tint: .orange))
            }
            .padding(24)
            .frame(maxWidth: 440)
            .frame(maxWidth: .infinity)
        }
        .onAppear { focused = text.isEmpty }
    }

    private var addressField: some View {
        HStack(spacing: 12) {
            Image(systemName: ScreenSymbol.server.systemName)
                .foregroundStyle(.secondary)
                .frame(width: 22)
            TextField(presentation.field.placeholder, text: $text)
                .textFieldStyle(.plain)
                .textContentType(.URL)
                #if os(iOS)
                .keyboardType(.URL)
                .textInputAutocapitalization(.never)
                #endif
                .autocorrectionDisabled()
                .focused($focused)
                .onSubmit { send(.proceed) }
                .onChange(of: text) { _, typed in
                    if typed != presentation.field.text { send(.edit(typed)) }
                }
                .disabled(presentation.field.disabled)
        }
        .padding(.horizontal, 16)
        .frame(minHeight: 50)
        .background(.fill.tertiary, in: .rect(cornerRadius: 14))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(presentation.field.label)
    }

    @ViewBuilder private func footerView(_ footer: ConnectPresentation.Footer) -> some View {
        VStack(spacing: 6) {
            if let found = footer.found {
                Label {
                    Text(found)
                } icon: {
                    Image(systemName: ScreenSymbol.found.systemName).foregroundStyle(.green)
                }
            }
            if let repository = footer.repository {
                Text(repository)
            }
            Text(footer.note)
        }
    }
}

/// The server first: one field that takes a server's address or a
/// repository's name, resolved by `ServerLocator` and checked against the
/// server's discovery document and the app's requirement when the person
/// presses Continue, then the door the server takes: the browser sign-in,
/// the app's own password door, or what a server too old lacks. The logic
/// is `ConnectController`'s, run on a runtime of its own, made once for the
/// view's life however often SwiftUI makes the view again; this view draws
/// its presentation and keeps the app's address binding in step with it.
public struct ConnectView<Header: View, PasswordDoor: View>: View {
    @Binding var address: String
    let header: Header
    let passwordDoor: (ServerProbe.Result) -> PasswordDoor
    let makeController: @MainActor () -> ConnectController
    let probe: ServerProbe
    @State private var holder = ModelHolder<ConnectController>()

    public init(manifest: AppManifest, address: Binding<String>, probe: ServerProbe = ServerProbe(), unrevoked: [Session] = [],
                @ViewBuilder header: () -> Header,
                onSignIn: @escaping (ServerProbe.Result) async throws -> Void,
                @ViewBuilder passwordDoor: @escaping (ServerProbe.Result) -> PasswordDoor)
    {
        self.init(manifest: manifest, address: address, probe: probe, header: header(), onSignIn: onSignIn,
                  passwordDoor: passwordDoor, hasPasswordDoor: true, unrevoked: unrevoked)
    }

    init(manifest: AppManifest, address: Binding<String>, probe: ServerProbe, header: Header,
         onSignIn: @escaping (ServerProbe.Result) async throws -> Void,
         passwordDoor: @escaping (ServerProbe.Result) -> PasswordDoor, hasPasswordDoor: Bool, unrevoked: [Session] = [])
    {
        _address = address
        self.header = header
        self.passwordDoor = passwordDoor
        self.probe = probe
        let initial = address.wrappedValue
        let facts = unrevoked.map(SessionFacts.init)
        // Made only the first time the view draws; an initialiser SwiftUI
        // calls again makes nothing.
        makeController = {
            // The app's sign-in runs on the main actor, where it was written to.
            let signIn = MainActorSignIn(onSignIn)
            return ConnectController(manifest: manifest, address: initial,
                                     locator: ServerLocator(probe: probe, resolver: DNSServiceTXTResolver()),
                                     hasPasswordDoor: hasPasswordDoor, opensBrowser: WebAuthenticator.opensBrowser,
                                     unrevoked: facts, signIn: { try await signIn.run($0) })
        }
    }

    public var body: some View {
        let model = holder.model { PresentationModel(makeController(), environment: .apple(transport: probe.transport)) }
        ConnectScreen(model.presentation, text: $address, send: model.send, header: { header }, passwordDoor: passwordDoor)
            .onChange(of: model.presentation.field.revision) {
                // What the controller wrote itself (the checked name, or an
                // empty field for another server) is the app's address too.
                address = model.presentation.field.text
            }
    }
}

/// A view's one presentation model, made the first time the view draws and
/// kept in its state from then on. It is not observed itself: the model is.
@MainActor
final class ModelHolder<Root: Controller> {
    private var made: PresentationModel<Root>?

    func model(_ make: () -> PresentationModel<Root>) -> PresentationModel<Root> {
        if let made { return made }
        let model = make()
        made = model
        return model
    }
}

public extension ConnectView where PasswordDoor == EmptyView {
    /// A connect screen for an app with no password door.
    init(manifest: AppManifest, address: Binding<String>, probe: ServerProbe = ServerProbe(), unrevoked: [Session] = [],
         @ViewBuilder header: () -> Header, onSignIn: @escaping (ServerProbe.Result) async throws -> Void)
    {
        self.init(manifest: manifest, address: address, probe: probe, header: header(), onSignIn: onSignIn,
                  passwordDoor: { _ in EmptyView() }, hasPasswordDoor: false, unrevoked: unrevoked)
    }
}

public extension ConnectView where Header == ConnectHeader, PasswordDoor == EmptyView {
    /// A connect screen headed by the app's name and what it is for.
    init(manifest: AppManifest, tagline: String? = nil, icon: Image? = nil, address: Binding<String>,
         probe: ServerProbe = ServerProbe(), unrevoked: [Session] = [],
         onSignIn: @escaping (ServerProbe.Result) async throws -> Void)
    {
        self.init(manifest: manifest, address: address, probe: probe,
                  header: ConnectHeader(appName: manifest.appName, tagline: tagline, icon: icon),
                  onSignIn: onSignIn, passwordDoor: { _ in EmptyView() }, hasPasswordDoor: false, unrevoked: unrevoked)
    }
}

/// An app's sign-in closure, written for the main actor, called from the
/// controller's effect.
private final class MainActorSignIn: @unchecked Sendable {
    private let body: (ServerProbe.Result) async throws -> Void

    init(_ body: @escaping (ServerProbe.Result) async throws -> Void) {
        self.body = body
    }

    @MainActor func run(_ found: ServerProbe.Result) async throws {
        try await body(found)
    }
}
#endif
