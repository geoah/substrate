import SubstrateAuth
import SubstrateRuntime

/// The app's push registrar, where it takes pushes from its publisher's
/// relay: set by the app on the environment, read by the link at sign-out,
/// which deletes the install's device record while the token can still
/// delete it and forgets every registration of the sign-in before the
/// token is revoked, and by the app's own effects.
public enum PushRegistrarKey: AppEnvironmentKey {
    public static var defaultValue: PushRegistrar? { nil }
}
