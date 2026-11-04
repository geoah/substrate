import Foundation

/// A value that crosses to Android's Kotlin over the bridge
/// (`SubstrateBridge`, docs/plans/app-kit.md §5.5). The Kotlin is generated
/// from the declarations that conform, so a conforming value keeps to the
/// model the generator reads:
///
/// - a struct of stored properties that are themselves in the model,
///   generic or not: a type parameter is a field's type as Kotlin's generic
///   data class takes it (`ActionButton<Action>`);
/// - an enum with a `String` raw value, or one whose cases carry payloads
///   or none: a labelled value is keyed by its label, an unlabelled one by
///   its place (`_0`), as the synthesized coding writes them;
/// - `String`, `Bool`, `Int`, `Double`, `JSONValue`, optionals, arrays and
///   `[String: T]` dictionaries of those, and SubstrateRuntime's `KeyedList`
///   of a bridged row;
/// - `Date`, `URL` and `UUID`, which `ValueCoding` writes as strings (an
///   instant as RFC 3339); a decimal or an integer past `Int` as a `String`
///   (`DecimalText`): `Decimal` encodes as a number no other language reads
///   back exactly;
/// - a type `bridge.json` names `opaque`, carried as the JSON its own
///   coding writes, which Kotlin holds and never reads.
///
/// It conforms in the declaration itself, never in an extension, and keeps
/// the synthesized `Codable`: no `CodingKeys`, no hand-written coding. The
/// generator (`apps/kit/Tools/BridgeGen`) refuses anything else by name.
/// What crosses is written by `ValueCoding`, never Foundation's coders.
public protocol Bridged: Codable, Sendable {}
