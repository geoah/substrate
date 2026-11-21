import Foundation

/// How the assistant behaves, as Settings draws it: its standing
/// instructions, oldest first as it reads them, each with a switch and its
/// text to edit. They are the assistant's everywhere it works, not this
/// app's alone, and the footer says so.
public struct MossSettings: Codable, Equatable, Hashable, Sendable {
    public struct Row: Codable, Equatable, Hashable, Sendable, Identifiable {
        public var id: String
        public var text: String
        public var on: Bool
        /// Who wrote it: "Yours", "Came with a package", "Written by Moss".
        public var source: String

        public init(id: String, text: String, on: Bool, source: String) {
            self.id = id
            self.text = text
            self.on = on
            self.source = source
        }
    }

    public var title = "How Moss behaves"
    public var rows: [Row] = []
    /// Said in place of the rows where there are none.
    public var empty: String?
    public var footer: String?
    /// The words of the editor: its title, and its two buttons.
    public var editTitle = "Edit instruction"
    public var edit = "Edit"
    public var save = "Save"
    public var cancel = "Cancel"

    public init(rows: [Row] = [], empty: String? = nil, footer: String? = nil) {
        self.rows = rows
        self.empty = empty
        self.footer = footer
    }
}
