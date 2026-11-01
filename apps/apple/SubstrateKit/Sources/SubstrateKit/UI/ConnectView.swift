import SwiftUI

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

/// The server first: the address, checked against its discovery document
/// and the app's requirement when the person presses Continue. A server that
/// can give the app access of its own offers the browser sign-in; one the
/// app falls back for shows the app's own password door; one too old says
/// what it lacks. Without a password door, a server the app would fall back
/// for is too old: there is no step to show for it.
public struct ConnectView<Header: View, PasswordDoor: View>: View {
    let manifest: AppManifest
    @Binding var address: String
    let probe: ServerProbe
    let header: Header
    let onSignIn: (ServerProbe.Result) async throws -> Void
    let passwordDoor: (ServerProbe.Result) -> PasswordDoor
    let hasPasswordDoor: Bool

    enum Phase {
        case idle
        case checking
        case found(ServerProbe.Result)
        case signingIn(ServerProbe.Result)
    }

    @State var phase: Phase
    @State var problem: String?
    @FocusState private var focused: Bool

    public init(manifest: AppManifest, address: Binding<String>, probe: ServerProbe = ServerProbe(),
                @ViewBuilder header: () -> Header,
                onSignIn: @escaping (ServerProbe.Result) async throws -> Void,
                @ViewBuilder passwordDoor: @escaping (ServerProbe.Result) -> PasswordDoor)
    {
        self.init(manifest: manifest, address: address, probe: probe, header: header(), onSignIn: onSignIn,
                  passwordDoor: passwordDoor, hasPasswordDoor: true, phase: .idle)
    }

    init(manifest: AppManifest, address: Binding<String>, probe: ServerProbe, header: Header,
         onSignIn: @escaping (ServerProbe.Result) async throws -> Void,
         passwordDoor: @escaping (ServerProbe.Result) -> PasswordDoor, hasPasswordDoor: Bool, phase: Phase,
         problem: String? = nil)
    {
        self.manifest = manifest
        _address = address
        self.probe = probe
        self.header = header
        self.onSignIn = onSignIn
        self.passwordDoor = passwordDoor
        self.hasPasswordDoor = hasPasswordDoor
        _phase = State(initialValue: phase)
        _problem = State(initialValue: problem)
    }

    private var found: ServerProbe.Result? {
        switch phase {
        case .found(let result), .signingIn(let result): result
        default: nil
        }
    }

    /// The door a server takes here: the manifest's, with the password door
    /// read as too old where the app gave none.
    private func door(for found: ServerProbe.Result) -> SignInDoor {
        let door = manifest.door(for: found)
        return door == .password && !hasPasswordDoor ? .tooOld : door
    }

    private var busy: Bool {
        switch phase {
        case .checking, .signingIn: true
        default: false
        }
    }

    public var body: some View {
        ScrollView {
            VStack(spacing: 28) {
                header.padding(.top, 48)

                addressField

                if let found, door(for: found) == .tooOld, case let .tooOld(running, needed, missing) = found.verdict {
                    ServerTooOldView(appName: manifest.appName, host: found.host, running: running, needed: needed,
                                     missing: missing, onTryAgain: check, onChangeServer: changeServer)
                } else {
                    if let found, door(for: found) == .password {
                        passwordDoor(found)
                    }
                    if let problem {
                        ProblemBox(title: found == nil ? "Couldn't reach that server" : "Couldn't sign in", message: problem)
                            .modifier(KitCard(tint: .red))
                    }
                    if found.map({ door(for: $0) }) != .password {
                        primaryButton
                    }
                    footer
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }
            }
            .padding(24)
            .frame(maxWidth: 440)
            .frame(maxWidth: .infinity)
        }
        .onAppear { focused = address.isEmpty }
    }

    private var addressField: some View {
        HStack(spacing: 12) {
            Image(systemName: "server.rack")
                .foregroundStyle(.secondary)
                .frame(width: 22)
            TextField("substrate.example.com", text: $address)
                .textFieldStyle(.plain)
                .textContentType(.URL)
                #if os(iOS)
                .keyboardType(.URL)
                .textInputAutocapitalization(.never)
                #endif
                .autocorrectionDisabled()
                .focused($focused)
                .onSubmit(check)
                .onChange(of: address) { _, typed in
                    // Writing the checked origin back is not a new address.
                    if let found, typed == found.origin.absoluteString { return }
                    if found != nil { phase = .idle }
                    problem = nil
                }
                .disabled(busy)
        }
        .padding(.horizontal, 16)
        .frame(minHeight: 50)
        .background(.fill.tertiary, in: .rect(cornerRadius: 14))
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Server")
    }

    private var primaryButton: some View {
        Button(action: found == nil ? check : signIn) {
            HStack(spacing: 8) {
                if busy { ProgressView().controlSize(.small) }
                Text(buttonTitle)
            }
            .frame(maxWidth: .infinity, minHeight: 30)
        }
        .buttonStyle(.borderedProminent)
        .buttonBorderShape(.capsule)
        .controlSize(.large)
        .disabled(busy || (found == nil && address.trimmingCharacters(in: .whitespaces).isEmpty))
        .keyboardShortcut(.defaultAction)
    }

    private var buttonTitle: String {
        switch phase {
        case .idle: "Continue"
        case .checking: "Checking…"
        case .found: "Sign In with Your Substrate"
        case .signingIn: "Waiting for Your Browser…"
        }
    }

    @ViewBuilder private var footer: some View {
        if let found {
            VStack(spacing: 6) {
                Label {
                    Text("Server found: \(found.host)")
                } icon: {
                    Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
                }
                if door(for: found) == .password {
                    Text("This server can't give \(manifest.appName) access of its own yet, so your password goes to it directly.")
                } else {
                    Text("You sign in on \(found.host) itself and choose what \(manifest.appName) may do there. You can change or revoke it any time in its Settings, under Apps.")
                }
            }
        } else {
            Text("Your Substrate server's address. A name alone is read as https://.")
        }
    }

    private func check() {
        guard let origin = ServerAddress.origin(of: address) else {
            problem = "That is not an http or https address."
            return
        }
        phase = .checking
        problem = nil
        Task {
            do {
                let result = try await probe.probe(origin, requirement: manifest.requirement, app: manifest.appName)
                address = origin.absoluteString
                phase = .found(result)
            } catch {
                phase = .idle
                if !isCancellation(error) { problem = (error as? LocalizedError)?.errorDescription ?? explain(error, app: manifest.appName, server: origin) }
            }
        }
    }

    private func signIn() {
        guard let found else { return }
        phase = .signingIn(found)
        problem = nil
        Task {
            defer { if case .signingIn = phase { phase = .found(found) } }
            do {
                try await onSignIn(found)
            } catch is WebAuthenticator.Cancelled {
                // The person closed the sheet: nothing to say.
            } catch {
                if !isCancellation(error) { problem = explain(error, app: manifest.appName, server: found.origin) }
            }
        }
    }

    private func changeServer() {
        phase = .idle
        problem = nil
        address = ""
        focused = true
    }
}

public extension ConnectView where PasswordDoor == EmptyView {
    /// A connect screen for an app with no password door.
    init(manifest: AppManifest, address: Binding<String>, probe: ServerProbe = ServerProbe(),
         @ViewBuilder header: () -> Header, onSignIn: @escaping (ServerProbe.Result) async throws -> Void)
    {
        self.init(manifest: manifest, address: address, probe: probe, header: header(), onSignIn: onSignIn,
                  passwordDoor: { _ in EmptyView() }, hasPasswordDoor: false, phase: .idle)
    }
}

public extension ConnectView where Header == ConnectHeader, PasswordDoor == EmptyView {
    /// A connect screen headed by the app's name and what it is for.
    init(manifest: AppManifest, tagline: String? = nil, icon: Image? = nil, address: Binding<String>,
         probe: ServerProbe = ServerProbe(), onSignIn: @escaping (ServerProbe.Result) async throws -> Void)
    {
        self.init(manifest: manifest, address: address, probe: probe,
                  header: ConnectHeader(appName: manifest.appName, tagline: tagline, icon: icon),
                  onSignIn: onSignIn, passwordDoor: { _ in EmptyView() }, hasPasswordDoor: false, phase: .idle)
    }
}
