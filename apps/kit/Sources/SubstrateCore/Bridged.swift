import Foundation

/// A value that crosses to Android's Kotlin over the bridge
/// (`SubstrateBridge`, docs/plans/app-kit.md §5.5). The Kotlin is generated
/// from the declarations that conform, so a conforming value keeps to the
/// model the generator reads:
///
/// - a struct of stored properties that are themselves in the model;
/// - an enum with a `String` raw value, or one whose cases carry labelled
///   payloads (`case failed(reason: String)`) or none;
/// - `String`, `Bool`, `Int`, `Double`, `JSONValue`, optionals, arrays and
///   `[String: T]` dictionaries of those;
/// - a decimal or an integer past `Int` as a `String` (`DecimalText`), an
///   instant as an RFC 3339 `String` (`Timestamp`): `Decimal` and `Date`
///   encode as numbers no other language reads back exactly.
///
/// It conforms in the declaration itself, never in an extension, and keeps
/// the synthesized `Codable`: no `CodingKeys`, no hand-written coding. The
/// generator (`apps/kit/Tools/BridgeGen`) refuses anything else by name.
public protocol Bridged: Codable, Sendable {}
