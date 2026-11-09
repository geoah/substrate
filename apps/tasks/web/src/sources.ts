import {
  boundTrait,
  comparePositions,
  derivedUUID,
  kindStatus,
  movesFrom,
  parseReference,
  renames,
  standing,
  type KindStatus,
  type Standing,
} from "@substrate/client"

/** Tasks over any task-like kind, as apps/tasks/contract/README.md says:
 * the collections read from the declarations and the owner's settings
 * records, each record's standing and actions, Needs you's bands across
 * collections, the suggestion queue, the groups, the pins, and the patch a
 * command writes. Pure: the contract's cases run it as they are, and the
 * screens read what it answers. TasksKit's `TasksSources` is the same. */

export const ownTaskKind = "substrate.reamde.dev/tasks/task"
export const ownListKind = "substrate.reamde.dev/tasks/list"
export const collectionKind = "substrate.reamde.dev/tasks/collection"
export const preferenceKind = "substrate.reamde.dev/tasks/preference"
/** The kind a declaration is a record of: an app's token reads the
 * declaration of a kind it reads at `core/kind/<kind>`. */
export const kindDeclaration = "substrate.reamde.dev/core/kind"

/** A kind as the implementors route serves it, or as a case gives it. */
export interface Declaration {
  identity: string
  label?: { singular: string; plural: string }
  definition: Record<string, unknown>
}

/** A record as the server answers it; the contract's cases carry the title
 * among the properties, the wire beside them. */
export interface Envelope {
  kind: string
  id: string
  version: number
  createdAt: string
  properties: Record<string, unknown>
  deletedAt?: string
  title?: string
}

export type Reading = "due" | "dueDay" | "priority" | "order"

export interface GroupReading {
  /** The single reference read as the group. */
  property: string
  /** The kind it points at. */
  kind: string
}

/** A single reference a collection may group by, as its page lists them. */
export interface ReferenceChoice extends GroupReading {
  label: string
  /** Whether the grant reads its target, so grouping by it needs no
   * approval first. */
  readable: boolean
}

export interface Collection {
  kind: string
  declaration: Declaration
  /** Its declared label, else its name in words. */
  label: { singular: string; plural: string }
  enabled: boolean
  viewOnly: boolean
  creatable: boolean
  status: KindStatus
  /** The property the title is edited through; null where the title is
   * not editable. */
  title: string | null
  due: string | null
  dueDay: string | null
  priority: string | null
  order: string | null
  group: GroupReading | null
  /** The reference the owner chose to group by whose kind the grant does
   * not read yet: no group reading until it does, and each task's group
   * is out of reach meanwhile. */
  waitingGroup: ReferenceChoice | null
  /** What each reading guesses from the declaration alone, which the
   * collection's page shows beside the owner's choice. */
  guess: Record<Reading, string | null> & { group: GroupReading | null }
  /** The priority's values, lowest first. */
  priorities: string[]
  /** Every single reference, for Group by. */
  references: ReferenceChoice[]
  /** The owner's collection record, where there is one. */
  record?: Envelope
}

export type Band = "lateOrUrgent" | "comingUp" | "later" | "noDate"

export type ActionName = "accept" | "dismiss" | "complete" | "wontDo" | "reopen"

export const actionNames: ActionName[] = [
  "accept",
  "dismiss",
  "complete",
  "wontDo",
  "reopen",
]

/** The standing each action moves from and the one it moves to. */
const actionMoves: Record<ActionName, { from: Standing[]; to: Standing }> = {
  accept: { from: ["proposed"], to: "open" },
  dismiss: { from: ["proposed"], to: "dropped" },
  complete: { from: ["open"], to: "done" },
  wontDo: { from: ["open"], to: "dropped" },
  reopen: { from: ["done", "dropped"], to: "open" },
}

/** A group reference's value on one item: the group record's path, `none`
 * for a record holding none, `missing` for one naming a record the token
 * does not hold (a path always holds a slash, so neither is one). null
 * where its collection has no group reading. */
export type GroupValue = string | null

export interface Item {
  /** `<kind>/<id>`: two kinds may hold one id, so this is the identity. */
  path: string
  kind: string
  id: string
  version: number
  createdAt: string
  record: Envelope
  collection: Collection
  /** The server-rendered title, else the id. */
  title: string
  value: string | undefined
  standing: Standing
  actions: Record<ActionName, string[]>
  group: GroupValue
  due?: { at?: Date; day: string }
  priority?: string
  pressing: boolean
  order?: string
  band: Band | null
}

export interface GroupRow {
  /** A group record's path, `none` or `missing`. */
  group: string
  open: number
}

export interface GroupSection {
  collection: string
  kind: string
  /** The reference read as the group, for "No list" and its label. */
  property: string
  rows: GroupRow[]
}

export interface NeedsYou {
  lateOrUrgent: string[]
  comingUp: string[]
  later: string[]
  noDate: number
  suggestions: number
  badge: number
}

export interface SourcesInput {
  declarations: readonly Declaration[]
  records: readonly Envelope[]
  grant: readonly string[]
  now: Date
  timeZone: string
}

export interface Sources {
  collections: Collection[]
  /** The enabled collections, the app's own first. */
  enabled: Collection[]
  items: Map<string, Item>
  /** Every record held and not deleted, by path: groups and settings
   * included. */
  held: Map<string, Envelope>
  preference?: Envelope
  needsYou: NeedsYou
  /** Open items with no due date and no pressing priority, in order. */
  noDate: string[]
  suggestions: string[]
  groups: GroupSection[]
  pins: string[]
  /** The kind new tasks go to. */
  newTasks: string
}

// ---- the grant ---------------------------------------------------------------

function traitsOf(definition: Record<string, unknown>): string[] {
  return Array.isArray(definition.traits)
    ? definition.traits.filter((t): t is string => typeof t === "string")
    : []
}

export function bindsTrait(
  definition: Record<string, unknown> | undefined,
  trait: string
): boolean {
  return (
    !!definition && traitsOf(definition).some((e) => boundTrait(e) === trait)
  )
}

/** The trait a scope pattern names, `implements(<trait>)`. */
function traitPattern(pattern: string): string | undefined {
  const m = /^implements\((.+)\)$/.exec(pattern)
  return m ? m[1] : undefined
}

/** Whether a scope's pattern covers a kind: `*`, a package's `/*`, the
 * kind itself, or `implements(<trait>)` over a kind that binds it. No glob
 * or trait reaches an internal kind. */
export function covers(
  pattern: string,
  kind: string,
  definition?: Record<string, unknown>
): boolean {
  if (pattern === kind) return true
  const internal = definition?.purpose === "internal"
  const trait = traitPattern(pattern)
  if (trait) return !internal && bindsTrait(definition, trait)
  if (internal) return false
  if (pattern === "*") return true
  if (pattern.endsWith("/*")) {
    const pkg = pattern.slice(0, -1)
    return kind.startsWith(pkg) && !kind.slice(pkg.length).includes("/")
  }
  return false
}

function scopeCovers(
  grant: readonly string[],
  verbs: string[],
  kind: string,
  definition?: Record<string, unknown>
): boolean {
  return grant.some((s) => {
    const colon = s.indexOf(":")
    if (colon < 0) return s === "full"
    return (
      verbs.includes(s.slice(0, colon)) &&
      covers(s.slice(colon + 1), kind, definition)
    )
  })
}

export function grantReads(
  grant: readonly string[],
  kind: string,
  definition?: Record<string, unknown>
): boolean {
  return scopeCovers(grant, ["read", "write"], kind, definition)
}

export function grantWrites(
  grant: readonly string[],
  kind: string,
  definition?: Record<string, unknown>
): boolean {
  return scopeCovers(grant, ["write"], kind, definition)
}

// ---- words for a kind and a property -----------------------------------------

function capitalized(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1)
}

/** A camelCase name as words: `dueDay` is "Due day". */
export function nameWords(name: string): string {
  return capitalized(name.replace(/([a-z0-9])([A-Z])/g, "$1 $2").toLowerCase())
}

function plural(word: string): string {
  if (/(s|x|ch|sh)$/.test(word)) return `${word}es`
  if (/[^aeiou]y$/.test(word)) return `${word.slice(0, -1)}ies`
  return `${word}s`
}

/** A kind's label: the declared one, else its name in words. */
export function kindLabel(
  kind: string,
  declaration?: Declaration
): { singular: string; plural: string } {
  const declared =
    declaration?.label ??
    (declaration?.definition.label as
      { singular?: string; plural?: string } | undefined)
  const name = kind.split("/")[2] ?? kind
  const singular = declared?.singular || nameWords(name)
  return { singular, plural: declared?.plural || plural(singular) }
}

type Spec = Record<string, unknown>

function properties(definition: Record<string, unknown>): Record<string, Spec> {
  const p = definition.properties
  return p && typeof p === "object" ? (p as Record<string, Spec>) : {}
}

/** A property's label: its declared one, else its name in words. */
export function propertyLabel(
  definition: Record<string, unknown>,
  name: string
): string {
  const label = properties(definition)[name]?.label
  return typeof label === "string" && label ? label : nameWords(name)
}

// ---- readings ------------------------------------------------------------------

const groupNames = ["list", "project", "area"]

function single(spec: Spec | undefined): boolean {
  return !!spec && spec.repeated !== true
}

function guessDue(definition: Record<string, unknown>): string | null {
  const props = properties(definition)
  for (const entry of traitsOf(definition)) {
    if (boundTrait(entry) !== "substrate.reamde.dev/core/temporal") continue
    const point = renames(entry).point
    if (!point) continue
    const spec = props[point]
    if (!spec || spec.type === "datetime") return point
  }
  return props.dueAt?.type === "datetime" ? "dueAt" : null
}

function named(
  definition: Record<string, unknown>,
  name: string,
  type: string
): string | null {
  const spec = properties(definition)[name]
  return spec?.type === type && single(spec) ? name : null
}

function editableTitle(definition: Record<string, unknown>): string | null {
  const t = definition.displayTemplate
  if (typeof t !== "string") return null
  const m = /^\{([A-Za-z][A-Za-z0-9]*)\}$/.exec(t)
  if (!m) return null
  const spec = properties(definition)[m[1]]
  return spec?.type === "string" && single(spec) ? m[1] : null
}

function references(
  definition: Record<string, unknown>,
  grant: readonly string[],
  known: ReadonlyMap<string, Declaration>
): ReferenceChoice[] {
  const out: ReferenceChoice[] = []
  for (const [name, spec] of Object.entries(properties(definition))) {
    if (spec.type !== "reference" || !single(spec)) continue
    if (typeof spec.kind !== "string") continue
    out.push({
      property: name,
      kind: spec.kind,
      label: propertyLabel(definition, name),
      readable: grantReads(grant, spec.kind, known.get(spec.kind)?.definition),
    })
  }
  return out
}

/** A collection record's choice for one reading: undefined for none made,
 * null for `-`, else the property. A property the kind does not declare
 * with the reading's type is no choice. */
function chosen(
  record: Envelope | undefined,
  key: string,
  definition: Record<string, unknown>,
  type: string,
  supplied: string | null
): string | null | undefined {
  const v = record?.properties[key]
  if (typeof v !== "string" || v === "") return undefined
  if (v === "-") return null
  const spec = properties(definition)[v]
  if (spec?.type === type && single(spec)) return v
  return v === supplied ? v : undefined
}

function pick<T>(choice: T | null | undefined, guess: T | null): T | null {
  return choice === undefined ? guess : choice
}

function isLive(r: Envelope): boolean {
  return !r.deletedAt
}

/** The record of a settings kind for `matches` every device reads, where
 * two devices left two: the one under the id each derives, else the oldest,
 * then the smallest id. Every write goes to it. */
function settingsRecord(
  records: readonly Envelope[],
  kind: string,
  derived: string,
  matches: (r: Envelope) => boolean
): Envelope | undefined {
  const id = derivedUUID(derived)
  return records
    .filter((r) => r.kind === kind && isLive(r) && matches(r))
    .sort(
      (a, b) =>
        Number(b.id === id) - Number(a.id === id) ||
        compare(a.createdAt, b.createdAt) ||
        compare(a.id, b.id)
    )[0]
}

export function collectionRecordOf(
  records: readonly Envelope[],
  kind: string
): Envelope | undefined {
  return settingsRecord(
    records,
    collectionKind,
    `collection:${kind}`,
    (r) => r.properties.kind === kind
  )
}

export function preferenceRecordOf(
  records: readonly Envelope[]
): Envelope | undefined {
  return settingsRecord(records, preferenceKind, "preference", () => true)
}

/** Every candidate: a declaration the grant reads that binds `lifecycle`
 * with a status the kits read; the app's own first, then by kind. */
export function readCollections(
  declarations: readonly Declaration[],
  records: readonly Envelope[],
  grant: readonly string[]
): Collection[] {
  const known = new Map(declarations.map((d) => [d.identity, d]))
  const preference = preferenceRecordOf(records)
  const newTasks =
    typeof preference?.properties.newTasks === "string" &&
    preference.properties.newTasks
      ? preference.properties.newTasks
      : ownTaskKind
  const out: Collection[] = []
  for (const declaration of declarations) {
    const { identity: kind, definition } = declaration
    const status = kindStatus(definition)
    if (!status || !grantReads(grant, kind, definition)) continue
    const record = collectionRecordOf(records, kind)
    const own = kind === ownTaskKind
    const enabled = own
      ? record?.properties.enabled !== false
      : record?.properties.enabled === true
    const refs = references(definition, grant, known)
    const groupGuesses = refs.filter(
      (r) => groupNames.includes(r.property) && r.readable
    )
    const guessGroup =
      groupGuesses.length === 1
        ? { property: groupGuesses[0].property, kind: groupGuesses[0].kind }
        : null
    const dueGuess = guessDue(definition)
    const guess = {
      due: dueGuess,
      dueDay: named(definition, "dueDay", "date"),
      priority: named(definition, "priority", "enum"),
      order: named(definition, "position", "decimal"),
      group: guessGroup,
    }
    const due = pick(
      chosen(record, "due", definition, "datetime", dueGuess),
      guess.due
    )
    const dueDay = pick(
      chosen(record, "dueDay", definition, "date", null),
      guess.dueDay
    )
    const priority = pick(
      chosen(record, "priority", definition, "enum", null),
      guess.priority
    )
    const order = pick(
      chosen(record, "order", definition, "decimal", null),
      guess.order
    )
    let group = guess.group
    let waitingGroup: ReferenceChoice | null = null
    const groupChoice = record?.properties.group
    if (groupChoice === "-") group = null
    else if (typeof groupChoice === "string" && groupChoice) {
      const ref = refs.find((r) => r.property === groupChoice)
      if (ref) {
        group = ref.readable ? { property: ref.property, kind: ref.kind } : null
        if (!ref.readable) waitingGroup = ref
      }
    }
    const title = editableTitle(definition)
    const writes = grantWrites(grant, kind, definition)
    const edited = new Set(
      [title, due, dueDay, priority, order, group?.property].filter(
        (x): x is string => !!x
      )
    )
    const required = Object.entries(properties(definition))
      .filter(([, spec]) => spec.required === true)
      .map(([name]) => name)
    const prioritySpec = priority ? properties(definition)[priority] : undefined
    out.push({
      kind,
      declaration,
      label: kindLabel(kind, declaration),
      enabled,
      viewOnly: !writes,
      creatable:
        writes && kind === newTasks && required.every((p) => edited.has(p)),
      status,
      title,
      due,
      dueDay,
      priority,
      order,
      group,
      waitingGroup,
      guess,
      priorities: valuesOf(prioritySpec),
      references: refs,
      record,
    })
  }
  return out.sort((a, b) =>
    a.kind === ownTaskKind
      ? -1
      : b.kind === ownTaskKind
        ? 1
        : compare(a.kind, b.kind)
  )
}

function valuesOf(spec: Spec | undefined): string[] {
  const v = spec?.values
  if (!Array.isArray(v)) return []
  return v.flatMap((x) =>
    typeof x === "string"
      ? [x]
      : x &&
          typeof x === "object" &&
          typeof (x as { value?: unknown }).value === "string"
        ? [(x as { value: string }).value]
        : []
  )
}

// ---- days in a zone ------------------------------------------------------------

const dayFormats = new Map<string, Intl.DateTimeFormat>()

function dayFormat(timeZone: string): Intl.DateTimeFormat {
  let f = dayFormats.get(timeZone)
  if (!f) {
    f = new Intl.DateTimeFormat("en-CA", {
      timeZone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hourCycle: "h23",
    })
    dayFormats.set(timeZone, f)
  }
  return f
}

function parts(at: Date, timeZone: string): Record<string, number> {
  const out: Record<string, number> = {}
  for (const p of dayFormat(timeZone).formatToParts(at))
    if (p.type !== "literal") out[p.type] = Number(p.value)
  return out
}

/** `YYYY-MM-DD` of an instant in a zone. */
export function dayIn(at: Date, timeZone: string): string {
  const p = parts(at, timeZone)
  return `${p.year}-${String(p.month).padStart(2, "0")}-${String(p.day).padStart(2, "0")}`
}

/** The instant a day starts in a zone. */
export function dayStart(day: string, timeZone: string): number {
  const [y, m, d] = day.split("-").map(Number)
  const local = Date.UTC(y, m - 1, d)
  let at = local
  for (let i = 0; i < 2; i++) {
    const p = parts(new Date(at), timeZone)
    const shown = Date.UTC(
      p.year,
      p.month - 1,
      p.day,
      p.hour,
      p.minute,
      p.second
    )
    at = at - (shown - local)
  }
  return at
}

export function plusDays(day: string, n: number): string {
  const [y, m, d] = day.split("-").map(Number)
  return new Date(Date.UTC(y, m - 1, d + n)).toISOString().slice(0, 10)
}

const isDay = (v: unknown): v is string =>
  typeof v === "string" && /^\d{4}-\d{2}-\d{2}$/.test(v)

function instant(v: unknown): Date | undefined {
  if (typeof v !== "string" || !v) return undefined
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? undefined : d
}

// ---- items ------------------------------------------------------------------------

/** A reference property's value as a path: `{ref: <path>}` as served, or
 * the bare path. */
export function referenceValue(v: unknown): string | undefined {
  const text =
    typeof v === "string"
      ? v
      : v &&
          typeof v === "object" &&
          typeof (v as { ref?: unknown }).ref === "string"
        ? (v as { ref: string }).ref
        : undefined
  const ref = text ? parseReference(text) : undefined
  return ref ? `${ref.kind}/${ref.id}` : undefined
}

export function recordTitle(r: Envelope): string {
  const t = r.title ?? r.properties.title
  return typeof t === "string" && t ? t : r.id
}

/** How far down a priority's values a value sits: pressing are the top two
 * of four or more, the top one of two or three, none of one. */
function pressingOf(values: string[], value: string | undefined): boolean {
  if (value === undefined) return false
  const at = values.indexOf(value)
  if (at < 0) return false
  const n = values.length
  const top = n >= 4 ? 2 : n >= 2 ? 1 : 0
  return at >= n - top
}

export function readItem(
  r: Envelope,
  collection: Collection,
  held: ReadonlyMap<string, Envelope>,
  now: Date,
  timeZone: string
): Item {
  const p = r.properties
  const { status } = collection
  const raw = p[status.property]
  const value = typeof raw === "string" && raw ? raw : undefined
  const at = standing(status, value)
  const moves = movesFrom(status, value).filter((v) =>
    status.values.includes(v)
  )
  const actions = {} as Record<ActionName, string[]>
  for (const name of actionNames) {
    const { from, to } = actionMoves[name]
    actions[name] =
      collection.viewOnly || !from.includes(at)
        ? []
        : moves.filter((v) => standing(status, v) === to)
  }
  let group: GroupValue = null
  if (collection.group) {
    const path = referenceValue(p[collection.group.property])
    if (!path) group = p[collection.group.property] == null ? "none" : "missing"
    else group = held.has(path) ? path : "missing"
  }
  const dueAt = collection.due ? instant(p[collection.due]) : undefined
  const dueDay =
    collection.dueDay && isDay(p[collection.dueDay])
      ? (p[collection.dueDay] as string)
      : undefined
  const day = dueDay ?? (dueAt ? dayIn(dueAt, timeZone) : undefined)
  const due = day ? { at: dueAt, day } : undefined
  const priorityRaw = collection.priority ? p[collection.priority] : undefined
  const priority =
    typeof priorityRaw === "string" && priorityRaw ? priorityRaw : undefined
  const pressing = pressingOf(collection.priorities, priority)
  const orderRaw = collection.order ? p[collection.order] : undefined
  const order =
    typeof orderRaw === "string" && orderRaw !== ""
      ? orderRaw
      : typeof orderRaw === "number" && Number.isFinite(orderRaw)
        ? String(orderRaw)
        : undefined
  let band: Band | null = null
  if (at === "open") {
    if (due) {
      const today = dayIn(now, timeZone)
      const late = due.at ? due.at.getTime() < now.getTime() : due.day < today
      band = late
        ? "lateOrUrgent"
        : due.day <= plusDays(today, 7)
          ? "comingUp"
          : "later"
    } else band = pressing ? "lateOrUrgent" : "noDate"
  }
  return {
    path: `${r.kind}/${r.id}`,
    kind: r.kind,
    id: r.id,
    version: r.version,
    createdAt: r.createdAt,
    record: r,
    collection,
    title: recordTitle(r),
    value,
    standing: at,
    actions,
    group,
    due,
    priority,
    pressing,
    order,
    band,
  }
}

// ---- order -------------------------------------------------------------------------

function compare(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0
}

/** Ties in any band: the order reading where both are of one kind and hold
 * one, as exact decimals (a float cannot tell 2^53 from 2^53 + 1); then
 * creation; then the path. */
export function byTies(a: Item, b: Item): number {
  if (a.kind === b.kind && a.order !== undefined && b.order !== undefined) {
    const d = comparePositions(a.order, b.order)
    if (d) return d
  }
  return compare(a.createdAt, b.createdAt) || compare(a.path, b.path)
}

function dueKey(item: Item, timeZone: string): number {
  const due = item.due!
  return due.at ? due.at.getTime() : dayStart(due.day, timeZone)
}

/** How far from its top a priority stands, so values of different kinds
 * rank alike: 0 is the most urgent. */
function fromTop(item: Item): number {
  const values = item.collection.priorities
  const at = item.priority === undefined ? -1 : values.indexOf(item.priority)
  return at < 0 ? Number.MAX_SAFE_INTEGER : values.length - 1 - at
}

export function byDue(timeZone: string) {
  return (a: Item, b: Item): number =>
    dueKey(a, timeZone) - dueKey(b, timeZone) || byTies(a, b)
}

export function byUrgency(timeZone: string) {
  const due = byDue(timeZone)
  return (a: Item, b: Item): number => {
    if (a.due && b.due) return due(a, b)
    if (a.due) return -1
    if (b.due) return 1
    return fromTop(a) - fromTop(b) || byTies(a, b)
  }
}

/** Suggestions: newest first, ties by path. */
export function bySuggested(a: Item, b: Item): number {
  return compare(b.createdAt, a.createdAt) || compare(a.path, b.path)
}

/** How a group kind's records are ordered: by its order reading, a
 * `decimal` named `position`, where it declares one (as exact decimals, a
 * group holding none after those that do), then by title, then by path. */
export function groupOrder(
  definition: Record<string, unknown> | undefined,
  held: ReadonlyMap<string, Envelope>
): (a: string, b: string) => number {
  const order = definition ? named(definition, "position", "decimal") : null
  const at = (path: string): string | undefined => {
    const v = order ? held.get(path)?.properties[order] : undefined
    return typeof v === "string" && v !== ""
      ? v
      : typeof v === "number" && Number.isFinite(v)
        ? String(v)
        : undefined
  }
  const title = (path: string) => {
    const r = held.get(path)
    return r ? recordTitle(r) : path
  }
  return (a, b) => {
    return (
      comparePositions(at(a), at(b)) ||
      title(a).localeCompare(title(b)) ||
      compare(a, b)
    )
  }
}

// ---- the whole model ---------------------------------------------------------------

export function readSources(input: SourcesInput): Sources {
  const { records, grant, now, timeZone } = input
  const collections = readCollections(input.declarations, records, grant)
  const known = new Map(input.declarations.map((d) => [d.identity, d]))
  const enabled = collections.filter((c) => c.enabled)
  const held = new Map<string, Envelope>()
  for (const r of records) if (isLive(r)) held.set(`${r.kind}/${r.id}`, r)
  const byKind = new Map(enabled.map((c) => [c.kind, c]))
  const items = new Map<string, Item>()
  for (const r of held.values()) {
    const c = byKind.get(r.kind)
    if (c) items.set(`${r.kind}/${r.id}`, readItem(r, c, held, now, timeZone))
  }
  const all = [...items.values()]
  const inBand = (band: Band) => all.filter((i) => i.band === band)
  const late = inBand("lateOrUrgent").sort(byUrgency(timeZone))
  const coming = inBand("comingUp").sort(byDue(timeZone))
  const later = inBand("later").sort(byDue(timeZone))
  const noDate = inBand("noDate").sort(byUrgency(timeZone))
  const suggestions = all
    .filter((i) => i.standing === "proposed")
    .sort(bySuggested)
  const groups: GroupSection[] = []
  for (const c of enabled) {
    if (!c.group) continue
    const counts = new Map<string, number>()
    for (const i of all)
      if (i.collection === c && i.standing === "open" && i.group)
        counts.set(i.group, (counts.get(i.group) ?? 0) + 1)
    const byGroup = groupOrder(known.get(c.group.kind)?.definition, held)
    const titled = [...counts.keys()]
      .filter((g) => g !== "none" && g !== "missing")
      .sort(byGroup)
    const rows = [...titled, "none", "missing"]
      .filter((g) => (counts.get(g) ?? 0) > 0)
      .map((g) => ({ group: g, open: counts.get(g)! }))
    groups.push({
      collection: c.kind,
      kind: c.group.kind,
      property: c.group.property,
      rows,
    })
  }
  const preference = preferenceRecordOf(records)
  const groupKinds = new Set(
    enabled.flatMap((c) => (c.group ? [c.group.kind] : []))
  )
  const pinned = Array.isArray(preference?.properties.pins)
    ? (preference.properties.pins as unknown[])
    : []
  const pins = pinned.map(referenceValue).filter((p): p is string => {
    if (!p) return false
    const r = held.get(p)
    return !!r && groupKinds.has(r.kind)
  })
  const newTasks =
    typeof preference?.properties.newTasks === "string" &&
    preference.properties.newTasks
      ? preference.properties.newTasks
      : ownTaskKind
  return {
    collections,
    enabled,
    items,
    held,
    preference,
    needsYou: {
      lateOrUrgent: late.map((i) => i.path),
      comingUp: coming.map((i) => i.path),
      later: later.map((i) => i.path),
      noDate: noDate.length,
      suggestions: suggestions.length,
      badge: late.length,
    },
    noDate: noDate.map((i) => i.path),
    suggestions: suggestions.map((i) => i.path),
    groups,
    pins: [...new Set(pins)],
    newTasks,
  }
}

// ---- commands ------------------------------------------------------------------------

export interface Command {
  action: ActionName
  path: string
  /** The value, where the action has several. */
  to?: string
  /** A suggestion's fields set before accepting, in the same patch. */
  edits?: Record<string, unknown>
}

export interface PatchWrite {
  op: "patch"
  path: string
  kind: string
  id: string
  ifVersion: number
  properties: Record<string, unknown>
}

export type CommandResult =
  | { type: "write"; write: PatchWrite; value: string }
  | { type: "choose"; choices: string[] }
  | { type: "none" }

/** What a command writes: one patch of the status set to the action's one
 * value (or `to`, among its values) and the edits; a reopen also clears
 * what the move into the current state stamped. Several values and no
 * `to` is a choice for the owner; an action not offered writes nothing. */
export function command(item: Item | undefined, cmd: Command): CommandResult {
  if (!item) return { type: "none" }
  const values = item.actions[cmd.action]
  if (!values.length) return { type: "none" }
  let value: string
  if (cmd.to !== undefined) {
    if (!values.includes(cmd.to)) return { type: "none" }
    value = cmd.to
  } else if (values.length > 1) return { type: "choose", choices: [...values] }
  else value = values[0]
  const { status } = item.collection
  const props: Record<string, unknown> = { [status.property]: value }
  Object.assign(props, cmd.edits ?? {})
  if (cmd.action === "reopen") {
    for (const prop of stampsInto(item.collection, item.value))
      props[prop] ??= null
  }
  return {
    type: "write",
    value,
    write: {
      op: "patch",
      path: item.path,
      kind: item.kind,
      id: item.id,
      ifVersion: item.version,
      properties: props,
    },
  }
}

/** The properties a machine's moves into `value` stamp. */
function stampsInto(
  collection: Collection,
  value: string | undefined
): string[] {
  if (value === undefined) return []
  const spec = properties(collection.declaration.definition)[
    collection.status.property
  ]
  const out: string[] = []
  for (const t of Array.isArray(spec?.transitions) ? spec.transitions : []) {
    const { to, stamps } = (t ?? {}) as { to?: unknown; stamps?: unknown }
    if (to !== value || !stamps || typeof stamps !== "object") continue
    for (const prop of Object.keys(stamps))
      if (!out.includes(prop)) out.push(prop)
  }
  return out
}

/** A value by its declared label, else in words. */
export function valueLabel(collection: Collection, value: string): string {
  const spec = properties(collection.declaration.definition)[
    collection.status.property
  ]
  const entries = Array.isArray(spec?.states)
    ? spec.states
    : Array.isArray(spec?.values)
      ? spec.values
      : []
  for (const e of entries)
    if (
      e &&
      typeof e === "object" &&
      (e as { value?: unknown }).value === value
    ) {
      const label = (e as { label?: unknown }).label
      if (typeof label === "string" && label) return label
    }
  return nameWords(value)
}
