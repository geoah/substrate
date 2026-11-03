import SubstrateRuntime
import SubstrateScreens

/// Everything the app's root draws, as one value: the items, what stands in
/// their way, and the app's part of Settings. A UI reads this and sends
/// commands; it decides nothing.
public struct TemplatePresentation: Codable, Equatable, Sendable {
    public var title: String
    public var items: KeyedList<ItemRow>
    /// The field that adds one: "Add an item".
    public var add: String
    public var empty: EmptyPresentation?
    public var notices: [NoticeLine]
    public var problem: ProblemPresentation?
    /// The app's own settings sections, before the kit's.
    public var settings: [SettingsSection]
    public var about: AboutPresentation
}

/// One item as a row: its name, and what the row offers.
public struct ItemRow: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var title: String
    /// The swipe's Delete.
    public var delete: String
}
