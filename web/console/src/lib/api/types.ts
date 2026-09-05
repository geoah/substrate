/** Wire shapes for the substrate REST surface (`/api/v1`). Timestamps are
 * RFC 3339 strings.
 *
 * A record's identity is the pair **(kind, id)**: an id is unique only within
 * its kind, so nothing here addresses a record by bare id. A KIND is a
 * reference — `<authority>/<package>/<name>` when published, a bare `<name>`
 * when the kind is this repository's own. There is NO tenant and NO group: one
 * user owns one repository, and authorities replace the old group concept. */

/** The closed wire error set, plus `network` for a
 * transport failure that never reached the substrate. Clients switch on it. */
export type ErrorCode =
  | "bad_request"
  | "auth"
  | "forbidden"
  | "guard"
  | "not_found"
  | "conflict"
  | "compacted"
  | "validation"
  | "rate_limited"
  | "internal"
  | "function_failed"
  | "unsupported"
  | "unavailable"
  | "network"

/** One validation problem, split into the input it concerns and the message.
 * The server derives it from the `problems` string list (`props.name: required`
 * becomes `{path: "props.name", message: "required"}`), so a form maps a
 * problem to a field without parsing prose. `problems` stays beside it. */
export interface ProblemDetail {
  path: string
  message: string
}

/** The one error shape every call rejects with: the REST envelope's
 * `{code, message, problems, problemDetails}` plus the HTTP status that
 * carried it. */
export class ApiError extends Error {
  code: ErrorCode
  status: number
  problems: string[]
  problemDetails: ProblemDetail[]
  retryAfter?: number

  constructor(
    code: ErrorCode,
    message: string,
    status: number,
    problems: string[] = [],
    problemDetails: ProblemDetail[] = [],
    retryAfter?: number
  ) {
    super(message)
    this.name = "ApiError"
    this.code = code
    this.status = status
    this.problems = problems
    this.problemDetails = problemDetails
    this.retryAfter = retryAfter
  }
}

/** One record as every read serves it (`substrate.Record`). Everything authored
 * lives in `properties` (title/state included); `propertyMeta` arrives only on
 * a single-record read.
 *
 * NAME NOTE: the server calls this `Record`; TypeScript already owns that name
 * for its `Record<K, V>` utility type, and shadowing it here would poison every
 * `Record<string, unknown>` in the console. `SubstrateRecord` is the same thing
 * with the collision avoided. */
export interface SubstrateRecord {
  id: string
  /** The record's kind reference. */
  kind: string
  /** Present only when the id the read was addressed by was not the canonical
   * one (the record came back through a merge trail). */
  canonicalId?: string
  properties: Record<string, unknown>
  labels: Record<string, unknown>
  annotations?: Record<string, unknown>
  version: number
  createdAt: string
  updatedAt: string
  deletedAt?: string
  finalizers?: string[]
  /** The ids this record used to live under, left by merges and server-set. */
  formerIds?: string[]
  propertyMeta?: Record<string, PropertyMeta>
}

/** The one reserved key of a reference value. Every reference is SERVED as an
 * object holding the referent's `"<kind>/<id>"` path under `ref`, with each
 * declared link property beside it, whether or not the declaration declares any
 * (`internal/engine/validate.go`, decision 0044). The bare path string is
 * accepted on WRITE as shorthand and normalized by the server; nothing serves
 * it. */
export const REFERENCE_KEY = "ref"

/** One reference value. The object is what a read hands back; the string is the
 * write-time shorthand a document may still author. A repeated reference is an
 * array of these. */
export type ReferenceValue = string | LinkedReference

/** A reference value: the referent's path under `ref`, the declaration's own
 * link properties beside it. */
export interface LinkedReference {
  ref: string
  [property: string]: unknown
}

/** Read one reference value as its path plus whatever link data rides with it.
 * `undefined` for a value that is neither shape. A reader renders that raw
 * rather than inventing a pointer.
 *
 * It reads BOTH shapes and keeps doing so: the object is what the server
 * serves, and the string arm covers an authored document being checked before
 * it is sent, plus any row written before the one-shape rule landed. */
export function readReference(
  value: unknown
): { path: string; properties: Record<string, unknown> } | undefined {
  if (typeof value === "string") {
    return value ? { path: value, properties: {} } : undefined
  }
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return undefined
  }
  const held = value as Record<string, unknown>
  const path = held[REFERENCE_KEY]
  if (typeof path !== "string" || !path) return undefined
  const properties: Record<string, unknown> = {}
  for (const [key, held_] of Object.entries(held)) {
    if (key !== REFERENCE_KEY) properties[key] = held_
  }
  return { path, properties }
}

export interface PropertyMeta {
  manager: string
  /** The manager's standing against recompute: `machine` may be replaced,
   * `owner` and `bundle` hold. Absent on rows written before the tiers. */
  tier?: "owner" | "bundle" | "machine"
  updatedAt: string
  alternatives?: PropertyAlternative[]
}

export interface PropertyAlternative {
  actor: string
  value: unknown
  updatedAt: string
}

/** A create/upsert write body (`substrate.PutInput`). Everything authored
 * rides in `properties`, state properties included — and `put` refuses to move
 * one, so a transition travels as a `patch`. `kind` is implied by the
 * collection path, so the REST body omits it; it is kept here for the editor
 * dialect that carries the whole envelope. */
export interface PutInput {
  kind?: string
  id?: string
  properties?: Record<string, unknown>
  labels?: Record<string, unknown>
  annotations?: Record<string, unknown>
  ifVersion?: number
}

/** One keyset page of a collection list. `cursor` is the OPAQUE continuation
 * token — store it and resend it VERBATIM as `after=`
 * for the next page; it is omitted once the walk is exhausted. `head` is the
 * changelog head seq at the page's snapshot (open `watch?from={head}` for a
 * gapless handoff). The server has NO offset. */
export interface Page<T = SubstrateRecord> {
  records: T[]
  cursor?: string
  head?: number
  total?: number
}

/** The envelope every OPERATIONAL list answers with — tokens, the catalog,
 * trigger and bundle status, parked deliveries, trait implementors. `items`
 * holds the whole set today; `cursor` is reserved so keyset pagination lands
 * as a filled field, not a reshaped body (decision 0036). The record, history
 * and incoming lists keep their own `Page`. */
export interface OperationalList<T> {
  items: T[]
  cursor?: string
}

/** One enabled trigger's stance on one change row; omitted entirely when the
 * trigger cannot fire on the row. `trigger` is the trigger record's id,
 * `callable` what it invokes; `error` rides along on `parked` only. */
export interface ChangeTrigger {
  trigger: string
  callable: string
  state: "pending" | "processed" | "parked"
  error?: string
}

/** One changelog entry as the server serializes it (`substrate.Change`). */
export interface Change {
  seq: number
  ts: string
  actor: string
  op: string
  recordId: string
  /** The changed record's kind reference. */
  kind: string
  payload?: Record<string, unknown>
  /** The entry's chain hash, hex: a receipt checkable against the operator's
   * `repository verify` output. Absent only on an entry written before the
   * chain existed and not yet backfilled. */
  hash?: string
}

/** One computed slot of a recurring record (`substrate.Occurrence`, decision
 * 0043): the occurrences read derives it from the stored rule. It is not a
 * record — no id of its own, nothing points at it — so an agenda merges these
 * with the
 * temporal window query's rows on (kind, id, at). */
export interface Occurrence {
  /** The recurring record whose rule names this instant. */
  kind: string
  id: string
  title?: string
  at: string
  /** The occurrencelog row answering this slot, when one exists. */
  log?: OccurrenceLog
}

/** The log record that marked an occurrence, with its state (done/skipped). */
export interface OccurrenceLog {
  kind: string
  id: string
  status?: string
}

/** One recurring record the expansion could not read (a rule too dense, an
 * unknown timezone, an anchorless rule); the rest of the answer stands. */
export interface OccurrenceProblem {
  kind: string
  id: string
  message: string
}

/** The occurrences read's envelope. No cursor: a computed occurrence has no
 * stable address to resume from, so a truncated answer means narrow the
 * window. */
export interface OccurrenceList {
  occurrences: Occurrence[]
  truncated: boolean
  problems?: OccurrenceProblem[]
}

/** One row of the cross-collection change feed. The payload's `properties` key
 * names what moved without its values; the values ride with the write's
 * recorded EFFECTS, which the console reads through `changeEffects`
 * (lib/changelog.ts) — never raw, and never by the payload key's own name. */
export interface ChangeRow extends Change {
  triggers?: ChangeTrigger[]
}

/** One history page of the feed, newest first. `cursor` is the CONTINUATION —
 * the oldest seq on the page, handed back as the next `before`; absent when
 * the walk is exhausted. */
export interface ChangePage {
  changes: ChangeRow[]
  cursor?: number
}

/** One predicate of the filter grammar (`substrate.Cond`). The console writes
 * eq/in/contains/prefix; the rest of the grammar rides along for completeness. */
export interface Cond {
  eq?: unknown
  in?: unknown[]
  prefix?: string
  gt?: unknown
  gte?: unknown
  lt?: unknown
  lte?: unknown
  contains?: unknown
  exists?: boolean
}

/** The subset of `substrate.Filter` the console writes (`?filter=` —
 * URL-encoded JSON). A state property filters through `properties` like any
 * other. `kinds` is refused on a collection read (the path names the kind) and
 * is how a repository-wide GraphQL list narrows. */
export interface RecordFilter {
  kinds?: string[]
  properties?: Record<string, Cond>
  labels?: Record<string, Cond>
}

/** One reverse pointer (`substrate.IncomingReference`): some other live
 * record's reference property names this one. */
export interface IncomingReference {
  /** The declared name of the source's reference property. */
  property: string
  /** The dotted address of a NESTED reference site
   * (`tools.fields.callable`), empty for a kind's own property. */
  path?: string
  from: IncomingSource
}

/** The record end of a reverse pointer, shallow by design. */
export interface IncomingSource {
  id: string
  /** The pointing record's kind reference. */
  kind: string
  title?: string
}

export interface IncomingPage {
  incoming: IncomingReference[]
  cursor?: string
  total: number
}

/** One admitted value of an enum property, as the substrate serves it in a
 * property's `values` (declaration order). `value` is the raw wire value a
 * write submits; `label` is the authored display name — ALWAYS present, but an
 * EMPTY string means "no authored label, humanize the value". */
export interface EnumValue {
  value: string
  label: string
}

/** Parse a property's raw `values` (the enum admitted set) into `EnumValue[]`.
 * The wire shape is `[{value, label}]`; a bare string element is tolerated and
 * read as a value with no authored label. Non-string values are dropped. */
export function parseEnumValues(raw: unknown): EnumValue[] | undefined {
  if (!Array.isArray(raw)) return undefined
  const out: EnumValue[] = []
  for (const item of raw) {
    if (typeof item === "string") {
      out.push({ value: item, label: "" })
      continue
    }
    if (item && typeof item === "object") {
      const rec = item as Record<string, unknown>
      if (typeof rec.value === "string") {
        out.push({
          value: rec.value,
          label: typeof rec.label === "string" ? rec.label : "",
        })
      }
    }
  }
  return out.length ? out : undefined
}

/** KindInfo — the projection of one declared kind (iface.go). Replaces v0
 * `TypeInfo`: `authority` is what published the kind (empty for a
 * repository-local one), `package` is the package's own word beside it, and
 * `identity` is the kind REFERENCE `<authority>/<package>/<name>` (or a bare
 * `<name>`). There is no `sourceYAML` on the wire (record 61) — the parsed
 * `definition` IS the document. */
export interface KindInfo {
  /** The kind REFERENCE: `<authority>/<package>/<name>`, or a bare `<name>`
   * for a repository-local kind. */
  identity: string
  name: string
  /** Who publishes the kind; empty for a repository-local one. */
  authority: string
  /** The package's own word ("core", "tasks"): the middle segment of the
   * identity, carried beside the authority so the console groups by one and
   * then the other without splitting the reference itself. */
  package: string
  /** The declaration's incremental version, server-maintained: every accepted
   * change to the declaration bumps it. 0 means no version is stored. */
  version: number
  /** The collection segment. */
  plural: string
  /** `builtin` for vocabulary the substrate ships, `installed` for kinds a
   * bundle declared, `schema` for repository-declared ones. */
  source: string
  /** What the kind is for, as its declaration says it — a sentence or two,
   * read above the collection. Empty when the declaration carries none. */
  description?: string
  /** The reconciled declaration — the `data` of the `substrate.reamde.dev/core/kind`
   * manifest that declares it (`authority`, `package`, `names`,
   * `properties`, …), key order lost to jsonb. */
  definition?: Record<string, unknown>
}

/** One token record's metadata — never the hash, never the secret. A token has
 * FULL ACCESS to its repository: no scopes, no actors, no roles. Sessions ARE
 * these records. */
export interface TokenInfo {
  id: string
  label: string
  createdAt: string
  /** Absent = the token lives until it is deleted. */
  expiresAt?: string
}

/** A mint (login, registration or `POST /tokens`): the record, plus the secret
 * shown ONCE. The secret is `substrate_tok_<hex>`. */
export interface MintedToken {
  token: TokenInfo
  secret: string
}

/** The TOTP enrollment a registration or a re-enrollment hands back: the seed
 * the caller holds until it proves one code, and the URI an authenticator
 * reads. Nothing is written until the code comes back. */
export interface TOTPEnrollment {
  totpSecret: string
  otpauthUri: string
}

/** What installing a bundle lands, by kind — the detail preview before
 * installing. Every member is a RECORD: kinds, functions and agents are records
 * of the core meta-kinds, and `records` are the data rows the same install
 * writes beside them. */
export interface BundleClosure {
  kinds?: string[]
  /** Each kind's declared description, keyed by identity — what the closure's
   * kinds ARE before an install has put them in the registry. Absent for a
   * kind that declares none, and from an older server whole. */
  kindDescriptions?: Record<string, string>
  functions?: string[]
  agents?: string[]
  /** Record mappings (source-kind → subject-kind projections). Optional so the
   * detail surface renders them when a catalog grows the list. */
  mappings?: string[]
  /** The DATA records the install writes after the declarations land — an
   * extension's triggers, the llm example's keyless provider rows. Ordinary
   * records afterward, and often the ones the reader has to go and edit. */
  records?: ShippedRecord[]
}

/** One data record a bundle ships. A data record is addressed by its KIND and
 * its id together, so both travel. */
export interface ShippedRecord {
  kind: string
  id: string
}

/** One declared input as the catalog previews it, verbatim from the bundle
 * manifest: the kind whose records satisfy it, whether its record is injected
 * into function invocations, and its declared purpose. */
export interface CatalogInput {
  /** Full identity of the kind whose records satisfy the input. */
  kind: string
  /** Only value: "functions", meaning the resolved record rides function
   * invocations. Absent means facility-read only. */
  inject?: string
  description?: string
}

/** One declaration a bundle upgrade would move (substrate.BundleUpgradeChange). */
export interface BundleUpgradeChange {
  /** The declaration's manifest kind: "package", "kind", "function", ... */
  kind: string
  /** The declaration's record id. */
  id: string
  /** The stored version; absent (0 is omitted on the wire) when this
   * repository lacks the declaration. */
  from?: number
  /** The shipped version; absent when the closure stopped shipping the
   * declaration and the upgrade prunes it. */
  to?: number
}

/** What re-importing a bundle's shipped closure would do here
 * (substrate.BundleUpgrade): the version motion and, when the server would
 * refuse it, the refusal's own guard lines. The upgrade verb IS the import
 * verb; this is its preview. */
export interface BundleUpgrade {
  available: boolean
  /** Stored and shipped versions of the bundle's owned package; absent
   * (0 is omitted on the wire) where none is stored. */
  from?: number
  to?: number
  changes?: BundleUpgradeChange[]
  /** The refuse-breakage guard lines the import would refuse on, with live
   * row counts. Non-empty means the upgrade is BLOCKED: the console shows the
   * lines and offers no button, because the server refuses it anyway. */
  blockers?: string[]
}

/** One installable bundle from the catalog embedded in the binary, plus
 * whether THIS repository has it. */
export interface CatalogBundle {
  /** The bundle's id — the PACKAGE identity it owns
   * (`providers.substrate.reamde.dev/google`), matching BundleStatus.id once
   * installed. */
  id: string
  /** The owned package's own word ("google"). */
  name: string
  /** The authority the closure publishes under. */
  authority: string
  /** The owned package's own word, the same word as `name`: the console groups
   * the catalog by authority, then by package. */
  package: string
  description: string
  /** The owned package's incremental declaration version. */
  version: number
  /** The declared inputs, input name keyed. A bundle with no needs omits it. */
  inputs?: Record<string, CatalogInput>
  closure: BundleClosure
  installed: boolean
  /** Catalog facet (backend-owned): this bundle connects an external
   * provider, so it earns the Integration badge/filter. */
  integration?: boolean
  /** Catalog facet: a worked example — grouped under Examples in the console.
   * Curated by the catalog, never derived from the closure's shape. */
  example?: boolean
  /** The PACKAGES this closure declares against — the vocabulary its
   * mappings, references and trigger subscriptions point at. Admission REFUSES the
   * import while one of them is absent from the repository, naming what to
   * import first, so the console shows them before the button is pressed. */
  requires?: string[]
  /** Present only on an installed bundle whose shipped closure moved past the
   * stored one: what re-importing would change, or why it is blocked. */
  upgrade?: BundleUpgrade
}

/** How an input's record was chosen: an explicit bound reference, the record named
 * `default`, or the sole live record of the kind. */
export type InputVia = "bound" | "default" | "sole"

/** One declared input's resolution (substrate.InputStatus), in declaration
 * order on the status. `record`/`via` are empty while unresolved, a
 * first-class state the status surfaces as a setup item, never tie-broken. */
export interface InputStatus {
  /** The input's declared name, also the reference property the bind verb
   * writes. */
  name: string
  /** Full identity of the kind whose records satisfy the input. */
  kind: string
  description?: string
  /** The resolved record's id; empty while unresolved. */
  record?: string
  via?: InputVia
}

/** The stable setup-item reasons: missing/ambiguous/dangling are an input's
 * own resolution problems; oauth-client is a resolved client record without
 * clientId/clientSecret; provider is an agent's llmprovider row absent or
 * keyless (kind substrate.reamde.dev/core/llmprovider). */
export type SetupCode =
  "missing" | "ambiguous" | "dangling" | "oauth-client" | "provider"

/** One thing standing between a bundle and a runtime path it ships
 * (substrate.SetupItem). Problems only: an empty setup list means ready. */
export interface SetupItem {
  code: SetupCode
  /** The unresolved input's name, when the item is an input's. */
  input?: string
  /** The kind whose record would clear the item. */
  kind?: string
  /** The existing record to fix, when one exists. */
  record?: string
  /** One sentence naming the fix. */
  message: string
}

/** One installed bundle's computed status — stored nowhere, recomputed per
 * read (substrate.BundleStatus). */
export interface BundleStatus {
  /** The bundle's id — the PACKAGE it is named for
   * ("samples.substrate.reamde.dev/web"). */
  id: string
  name: string
  authority: string
  /** The owned package's own word, the same word as `name`. */
  package: string
  /** False only for a quarantined bundle surfaced from its stored rows; an
   * uninstalled bundle has no status at all (uninstall tears its rows down). */
  installed: boolean
  /** False when disabled: execution is stopped. */
  enabled: boolean
  /** Each declared input's resolution, name order. Omitted when the bundle
   * declares none. */
  inputs?: InputStatus[]
  /** What stands between the bundle and every runtime path it ships. Omitted
   * when ready; lifecycle is separate from setup. */
  setup?: SetupItem[]
  accounts?: number
  functions?: number
  kinds?: number
  /** Live data rows across the owned package — what a purge would tombstone. */
  liveRecords?: number
  quarantined?: boolean
  /** The admission error that quarantined the bundle. */
  quarantineReason?: string
}
