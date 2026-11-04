import SubstrateRuntime
import SubstrateScreens

/// Everything the app's root draws, as one value: the items, what stands in
/// their way, and the app's part of Settings. A UI reads this and sends
/// commands; it decides nothing.
public struct TemplatePresentation: Codable, Equatable, Sendable {
    public var title: String
    public var items: KeyedList<ItemRow>
    /// The field that adds one.
    public var add: AddField
    public var empty: EmptyPresentation?
    public var notices: [NoticeLine]
    public var problem: ProblemPresentation?
    /// The app's own settings sections, before the kit's.
    public var settings: [SettingsSection]
    /// Each area of the repository's folder, with Clear Saved Copies.
    public var storage: SettingsSection
    public var about: AboutPresentation
}

/// One item as a row: its name, and what the row offers.
public struct ItemRow: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var title: String
    /// The swipe's Delete.
    public var delete: String
}

/// The field that adds an item: its prompt, what it holds, and why the last
/// add was not kept.
public struct AddField: Codable, Equatable, Sendable {
    /// "Add an item".
    public var prompt: String
    /// What is typed, kept until the outbox has the item it adds.
    public var text: String
    /// Why the device could not keep the last add, with Retry, which adds
    /// what the field holds again.
    public var notKept: ProblemPresentation?
}
