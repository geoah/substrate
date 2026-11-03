import SubstrateAPI

/// The connect screen (§5.6): one field that takes a server's address or a
/// repository's name, then the door the server found takes.
public struct ConnectPresentation: Codable, Equatable, Sendable {
    public enum Door: String, Codable, Equatable, Sendable {
        /// No server found yet.
        case none
        /// The browser sign-in.
        case browser
        /// The app's own password form, for a server it falls back for.
        case password
        /// The server needs an update first.
        case tooOld
    }

    public struct Field: Codable, Equatable, Sendable {
        public var text: String
        public var placeholder: String
        public var label: String
        public var disabled: Bool
        /// Moves when the controller wrote `text` itself, which the field
        /// then shows over what it holds.
        public var revision: Int

        public init(text: String, placeholder: String, label: String, disabled: Bool, revision: Int = 0) {
            self.text = text
            self.revision = revision
            self.placeholder = placeholder
            self.label = label
            self.disabled = disabled
        }
    }

    public struct Button: Codable, Equatable, Sendable {
        public var title: String
        public var busy: Bool
        public var enabled: Bool

        public init(title: String, busy: Bool, enabled: Bool) {
            self.title = title
            self.busy = busy
            self.enabled = enabled
        }
    }

    public struct Footer: Codable, Equatable, Sendable {
        /// "Server found: ada.example.com", once one is.
        public var found: String?
        /// "Repository: ada.example.com", where it was found from a name.
        public var repository: String?
        public var note: String

        public init(found: String?, repository: String?, note: String) {
            self.found = found
            self.repository = repository
            self.note = note
        }
    }

    public var field: Field
    public var door: Door
    /// The server found, which the password door is drawn for.
    public var found: ServerProbe.Result?
    public var tooOld: ServerTooOldPresentation?
    public var problem: ProblemPresentation?
    /// Nil where the password door stands instead.
    public var button: Button?
    /// The browser's own Cancel, while a sign-in waits on a browser outside
    /// the app.
    public var cancel: String?
    public var footer: Footer?
    public var unrevoked: [ProblemPresentation]
}
