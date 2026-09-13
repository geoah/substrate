/** One view record → one `ViewSpec`, with every problem ONE ROW can answer:
 * the two-source view, a name the kind does not declare, a `to` that is not a
 * state, a write onto a property the owner may not set, a link off a
 * non-url property, a shadowed column, a facet off a property with no closed
 * set, a datetime grouping off the temporal point, and the layout
 * contracts. What only the composition knows (a token
 * no app declares) is `app-spec.ts`'s. A trait view is checked per
 * implementor at render, so its per-kind checks are skipped here. */

import {
  KIND_RECORD_PREFIX,
  TRAIT_RECORD_PREFIX,
  VIEW_RECORD_PREFIX,
} from "@/lib/api/apps"
import {
  readReference,
  type KindInfo,
  type RecordFilter,
  type SubstrateRecord,
} from "@/lib/api/types"
import {
  columnProperties,
  kindByIdentity,
  splitKind,
  temporalProperties,
} from "@/lib/definition"
import {
  humanizeName,
  ownerWritable,
  propSpecs,
  type PropSpec,
} from "@/lib/record-schema"
import { allSpecs } from "./cond"
import { stateSpecOf } from "./machine"
import {
  isLayout,
  isVerb,
  type ActionSpec,
  type Attach,
  type Layout,
  type OrderBySpec,
  type Placement,
  type Problem,
  type RelatedSpec,
  type ViewSpec,
  type WhenSpec,
} from "./spec"
import { usesRecord } from "./tokens"

/** The record columns the engine resolves before a declared property of the
 * same name (`columnFor`, engine/query.go). */
export const COLUMNS = new Set([
  "at",
  "endsAt",
  "dueAt",
  "createdAt",
  "updatedAt",
  "title",
  "body",
  "id",
  "version",
])

/** What each engine-resolved column means, in a person's words, for the
 * warning a shadowed name earns. */
const COLUMN_MEANING: Record<string, string> = {
  updatedAt: "when the substrate last wrote each record",
  createdAt: "when each record was first written",
  title: "each record's rendered title",
  body: "each record's body text",
  id: "each record's id",
  version: "each record's version",
  at: "the instant the kind's temporal trait binds",
  endsAt: "the end of the kind's temporal range",
  dueAt: "the due instant the kind's temporal trait binds",
}

/** The warning for a name that is both a column and a declared property,
 * led by what the view does with it at that path so it reads as a sentence
 * about the screen, not about the engine. */
function shadowMessage(path: string, name: string): string {
  const lead = path.startsWith("orderBy")
    ? "Ordered by"
    : path.startsWith("show")
      ? "Shows"
      : path === "groupBy"
        ? "Grouped by"
        : path.startsWith("facets")
          ? "Narrowed by"
          : undefined
  const meaning = COLUMN_MEANING[name]
  const tail = `not the \`${name}\` the kind declares.`
  return lead && meaning
    ? `${lead} ${meaning}, ${tail}`
    : `\`${name}\` here is the record's own column, ${tail}`
}

/** The datatypes a facet may narrow by: a closed set of values, or the
 * referents the rows name. */
function facetable(spec: PropSpec | undefined): boolean {
  return Boolean(
    spec &&
    (spec.kind === "state" ||
      spec.kind === "enum" ||
      spec.kind === "reference" ||
      spec.values?.length)
  )
}

const ATTACH = new Set<Attach>(["launcher", "browse", "record", "home"])
const PLACEMENT = new Set<Placement>(["primary", "header", "row"])

/** The cells a row shows when the view names no `show`: the kind's column
 * properties minus the ones its displayTemplate reads first, because the
 * heading is already the row's title and drawing it twice says nothing. */
export function defaultShow(kind: KindInfo): string[] {
  const template = (kind.definition as Record<string, unknown>).displayTemplate
  const heading = new Set<string>()
  if (typeof template === "string") {
    for (const m of template.matchAll(/\{\s*([^}]+?)\s*\}/g)) {
      for (const alt of m[1].split("|")) heading.add(alt.trim())
    }
  }
  return columnProperties(kind)
    .map((p) => p.name)
    .filter((name) => !heading.has(name))
}

/** The package identity a kind identity belongs to, the spelling the
 * launcher's "needs ..." and `requiresAtLeast` use. */
export function packageOf(identity: string): string {
  const { authority, pkg } = splitKind(identity)
  return authority ? `${authority}/${pkg}` : identity
}

/** One floor a repository does not meet: the package, the least version the
 * view renders against, and what is installed, absent when the package is
 * not. */
export interface FloorShortfall {
  package: string
  floor: number
  installed?: number
}

/** The floors in `requiresAtLeast` the repository is below, held against the
 * installed versions (`installedVersions` in `lib/api/apps.ts`, off the
 * `core/package` rows). A package with no row is short the way an absent
 * kind is at the presence gate; that gate speaks first for the view's own
 * kind, so this is heard for a floor on any other package. `undefined`
 * versions are not yet read and report nothing, so the caller decides
 * whether to wait for them before drawing rows. */
export function floorShortfalls(
  spec: Pick<ViewSpec, "requiresAtLeast">,
  versions: Record<string, number> | undefined
): FloorShortfall[] {
  if (!versions) return []
  const out: FloorShortfall[] = []
  for (const [pkg, floor] of Object.entries(spec.requiresAtLeast)) {
    const installed = versions[pkg]
    if (installed !== undefined && installed >= floor) continue
    out.push({ package: pkg, floor, installed })
  }
  return out
}

/** The shortfalls as the problems the renderer shows instead of rows: an
 * error each, at `requiresAtLeast.<package>`, naming the floor and what is
 * installed. The floor guards the properties the view's actions and cells
 * were written against, so rendering below it is a wrong screen, not a
 * degraded one. */
export function floorProblems(
  spec: Pick<ViewSpec, "requiresAtLeast">,
  versions: Record<string, number> | undefined
): Problem[] {
  return floorShortfalls(spec, versions).map((s) => ({
    path: `requiresAtLeast.${s.package}`,
    message: `needs ${s.package} at version ${s.floor} (${
      s.installed === undefined ? "not installed" : `installed ${s.installed}`
    })`,
    severity: "error",
  }))
}

/** A stored reference read down to what it names: the path with the
 * referenced collection's prefix removed, so a `kind:` pin at the kind
 * collection yields the kind identity and a pin at `view` yields the view id.
 * An authored string that the server has not normalized reads as itself. */
export function refTarget(value: unknown, prefix: string): string | undefined {
  const held = readReference(value)
  if (!held) return undefined
  return held.path.startsWith(prefix)
    ? held.path.slice(prefix.length)
    : held.path
}

function str(v: unknown): string | undefined {
  return typeof v === "string" && v.trim() ? v : undefined
}

function strList(v: unknown): string[] {
  return Array.isArray(v)
    ? v.filter((x): x is string => typeof x === "string")
    : []
}

function obj(v: unknown): Record<string, unknown> {
  return v && typeof v === "object" && !Array.isArray(v)
    ? (v as Record<string, unknown>)
    : {}
}

function objList(v: unknown): Record<string, unknown>[] {
  return Array.isArray(v)
    ? v.filter(
        (x): x is Record<string, unknown> =>
          Boolean(x) && typeof x === "object" && !Array.isArray(x)
      )
    : []
}

function refList(v: unknown, prefix: string): string[] {
  return (Array.isArray(v) ? v : [])
    .map((x) => refTarget(x, prefix))
    .filter((x): x is string => Boolean(x))
}

function decodeFilter(v: unknown): RecordFilter {
  const raw = obj(v)
  const out: RecordFilter = {}
  if (raw.properties && typeof raw.properties === "object") {
    out.properties = raw.properties as RecordFilter["properties"]
  }
  if (raw.labels && typeof raw.labels === "object") {
    out.labels = raw.labels as RecordFilter["labels"]
  }
  if (Array.isArray(raw.ids)) out.ids = strList(raw.ids)
  return out
}

function decodeWhen(v: unknown): WhenSpec | undefined {
  const raw = obj(v)
  const property = str(raw.property)
  if (!property) return undefined
  return {
    property,
    in: Array.isArray(raw.in) ? raw.in.map(String) : undefined,
    exists: raw.exists === true ? true : undefined,
    not: raw.not === true ? true : undefined,
  }
}

function defaultPlacement(verb: ActionSpec["verb"]): Placement {
  return verb === "create" ? "primary" : "row"
}

interface Checker {
  kind?: KindInfo
  layout: Layout
  specs: PropSpec[]
  declared: Set<string>
  /** The names a create is born with without asking: the filter's `eq`
   * keys and `via`. */
  seeded: Set<string>
  problems: Problem[]
  /** The shadowed names already said, so one name is one line. */
  shadowed: Map<string, Problem>
}

function warn(c: Checker, path: string, message: string) {
  c.problems.push({ path, message, severity: "warning" })
}

/** A shadowed name is said once per view, and its ordering use owns the
 * line: ordering changes what is on top, where a `show` changes one cell. */
function sayShadow(c: Checker, path: string, name: string) {
  const prior = c.shadowed.get(name)
  if (prior && !path.startsWith("orderBy")) return
  const problem = prior ?? { path, message: "", severity: "warning" as const }
  problem.path = path
  problem.message = shadowMessage(path, name)
  if (!prior) {
    c.problems.push(problem)
    c.shadowed.set(name, problem)
  }
}

function fail(c: Checker, path: string, message: string) {
  c.problems.push({ path, message, severity: "error" })
}

/** Levenshtein distance, for the did-you-mean: a typo is one or two edits. */
function distance(a: string, b: string): number {
  const prev = Array.from({ length: b.length + 1 }, (_, j) => j)
  for (let i = 1; i <= a.length; i++) {
    let diag = prev[0]
    prev[0] = i
    for (let j = 1; j <= b.length; j++) {
      const tmp = prev[j]
      prev[j] = Math.min(
        prev[j] + 1,
        prev[j - 1] + 1,
        diag + (a[i - 1] === b[j - 1] ? 0 : 1)
      )
      diag = tmp
    }
  }
  return prev[b.length]
}

function suggest(c: Checker, name: string): string {
  const lower = name.toLowerCase()
  const budget = lower.length >= 6 ? 2 : 1
  let near: string | undefined
  let best = Infinity
  for (const d of c.declared) {
    const dl = d.toLowerCase()
    const score =
      dl === lower
        ? 0
        : dl.includes(lower) || lower.includes(dl)
          ? 1
          : distance(dl, lower)
    if (score <= budget && score < best) {
      best = score
      near = d
    }
  }
  return near ? ` (did you mean ${near})` : ""
}

/** A name the view reads: declared, or a hot column the kind's traits bind.
 * Returns whether it exists; a shadowed name earns its warning here. */
function checkName(c: Checker, path: string, name: string): boolean {
  if (!c.kind) return true
  if (COLUMNS.has(name) && propSpecs(c.kind).some((s) => s.name === name)) {
    sayShadow(c, path, name)
    return true
  }
  if (c.declared.has(name)) return true
  warn(
    c,
    path,
    `${JSON.stringify(name)} is not a property of ${c.kind.identity}${suggest(c, name)}`
  )
  return false
}

function checkWritable(c: Checker, path: string, name: string): boolean {
  if (!c.kind) return true
  const spec = c.specs.find((s) => s.name === name)
  if (!spec) {
    warn(
      c,
      path,
      `${JSON.stringify(name)} is not a property of ${c.kind.identity}${suggest(c, name)}`
    )
    return false
  }
  if (spec.managed) {
    warn(c, path, `${name} is stamped by the engine and cannot be written`)
    return false
  }
  if (!ownerWritable(spec)) {
    warn(c, path, `${name} is written by \`${spec.writer}\`, not the owner`)
    return false
  }
  return true
}

function decodeAction(
  raw: Record<string, unknown>,
  i: number,
  c: Checker
): ActionSpec | undefined {
  const at = `actions[${i}]`
  const name = str(raw.name)
  if (!name) {
    fail(c, `${at}.name`, "an action needs a name")
    return undefined
  }
  if (!isVerb(raw.verb)) {
    fail(c, `${at}.verb`, `${JSON.stringify(raw.verb)} is not a verb`)
    return undefined
  }
  const verb = raw.verb
  const placementRaw = str(raw.placement)
  const placement =
    placementRaw && PLACEMENT.has(placementRaw as Placement)
      ? (placementRaw as Placement)
      : defaultPlacement(verb)
  const set: Record<string, string> = {}
  for (const [key, value] of Object.entries(obj(raw.set))) {
    if (checkWritable(c, `${at}.set.${key}`, key)) set[key] = String(value)
  }
  const prompt = strList(raw.prompt).filter((p) =>
    checkWritable(c, `${at}.prompt`, p)
  )
  const when = decodeWhen(raw.when)
  if (when) checkName(c, `${at}.when.property`, when.property)
  if (usesRecord(raw.set) && placement !== "row" && c.layout !== "detail") {
    warn(
      c,
      `${at}.set`,
      "`$record` names the row, and only a row action or a detail has one"
    )
  }
  if (verb === "create" && c.kind) {
    for (const s of c.specs) {
      if (!s.required || s.managed || !ownerWritable(s)) continue
      if (s.default !== undefined) continue
      if (prompt.includes(s.name) || s.name in set || c.seeded.has(s.name)) {
        continue
      }
      warn(
        c,
        `${at}.prompt`,
        `${s.name} is required by ${c.kind.identity} and nothing here supplies it`
      )
    }
  }

  const action: ActionSpec = {
    name,
    label: str(raw.label) ?? humanizeName(name),
    description: str(raw.description),
    icon: str(raw.icon),
    verb,
    placement,
    to: str(raw.to),
    property: str(raw.property),
    view: refTarget(raw.view, VIEW_RECORD_PREFIX),
    href: str(raw.href),
    prompt,
    set,
    callable: readReference(raw.callable)?.path,
    message: str(raw.message),
    confirm: verb === "delete" || raw.confirm === true,
    when,
  }

  switch (verb) {
    case "transition": {
      if (!action.to) {
        fail(c, `${at}.to`, "a transition needs a target state")
        return undefined
      }
      if (!c.kind) break
      const state = stateSpecOf(c.kind, action.property)
      if (!state) {
        warn(
          c,
          `${at}.property`,
          action.property
            ? `${action.property} is not a state property of ${c.kind.identity}`
            : `${c.kind.identity} has no single state property; name one`
        )
        return undefined
      }
      if (!state.states?.includes(action.to)) {
        warn(
          c,
          `${at}.to`,
          `${JSON.stringify(action.to)} is not a state of ${state.name}`
        )
        return undefined
      }
      break
    }
    case "link": {
      if (!action.href) {
        fail(c, `${at}.href`, "a link needs the url property it opens")
        return undefined
      }
      if (!c.kind) break
      const spec = c.specs.find((s) => s.name === action.href)
      if (!spec || spec.kind !== "url") {
        warn(
          c,
          `${at}.href`,
          `${action.href} is not a url-typed property of ${c.kind.identity}; a link opens a property, never a template`
        )
        return undefined
      }
      break
    }
    case "open":
      if (!action.view) {
        fail(c, `${at}.view`, "an open needs the view it pushes")
        return undefined
      }
      break
    case "call":
    case "chat":
      if (!action.callable) {
        warn(c, `${at}.callable`, `a ${verb} needs its callable`)
        return undefined
      }
      break
    case "patch":
      if (!prompt.length && !Object.keys(set).length) {
        warn(c, at, "a patch writes nothing: give it prompt or set")
        return undefined
      }
      break
    default:
      break
  }
  return action
}

export function viewSpec(record: SubstrateRecord, kinds: KindInfo[]): ViewSpec {
  const p = record.properties
  const problems: Problem[] = []
  const kindId = refTarget(p.kind, KIND_RECORD_PREFIX)
  const traitId = refTarget(p.trait, TRAIT_RECORD_PREFIX)
  const kind = kindId ? kindByIdentity(kinds, kindId) : undefined
  const layoutRaw = p.layout
  const layout = isLayout(layoutRaw) ? layoutRaw : "detail"
  const filter = decodeFilter(p.filter)
  const via = str(p.via)
  const seeded = new Set<string>(
    Object.entries(filter.properties ?? {})
      .filter(([, cond]) => cond.eq !== undefined)
      .map(([name]) => name)
  )
  if (via) seeded.add(via)
  const c: Checker = {
    kind,
    layout,
    specs: kind ? allSpecs(kind) : [],
    declared: new Set(kind ? allSpecs(kind).map((s) => s.name) : []),
    seeded,
    problems,
    shadowed: new Map(),
  }

  if (kindId && traitId) {
    fail(c, "kind", "a view reads one kind or one trait, not both")
  } else if (!kindId && !traitId) {
    fail(c, "kind", "a view names the kind or the trait it reads")
  }

  if (!isLayout(layoutRaw)) {
    fail(c, "layout", `${JSON.stringify(layoutRaw)} is not a layout`)
  }

  const show = strList(p.show).filter((name, i) =>
    checkName(c, `show[${i}]`, name)
  )

  const facets = strList(p.facets).filter((name, i) => {
    const at = `facets[${i}]`
    if (!checkName(c, at, name)) return false
    if (!kind) return true
    if (facetable(c.specs.find((s) => s.name === name))) return true
    warn(
      c,
      at,
      `${name} is not a state, an enum or a reference of ${kind.identity}; a facet narrows by one of those`
    )
    return false
  })

  // A bare column orders silently; one the kind also declares goes through
  // the check so the shadow is said.
  const orderBy: OrderBySpec[] = []
  objList(p.orderBy).forEach((raw, i) => {
    const property = str(raw.property)
    if (!property) return
    const declared = kind
      ? propSpecs(kind).some((s) => s.name === property)
      : false
    if (
      (COLUMNS.has(property) && !declared) ||
      checkName(c, `orderBy[${i}].property`, property)
    ) {
      orderBy.push({ property, desc: raw.desc === true })
    }
  })

  let groupBy = str(p.groupBy)
  if (groupBy && kind) {
    if (!checkName(c, "groupBy", groupBy)) {
      groupBy = undefined
    } else {
      const spec = c.specs.find((s) => s.name === groupBy)
      const temporal = temporalProperties(kind)
      if (spec?.kind === "datetime" && !temporal.includes(groupBy)) {
        warn(
          c,
          "groupBy",
          `${groupBy} is a datetime but not the temporal point${temporal.length ? ` (${temporal.join(", ")})` : ""}; a completion time has no "Overdue"`
        )
      }
    }
  }

  if (via && kind) {
    const spec = c.specs.find((s) => s.name === via)
    if (!spec) checkName(c, "via", via)
    else if (spec.kind !== "reference") {
      warn(
        c,
        "via",
        `${via} is not a reference property; via scopes through one`
      )
    }
  }

  const actions = objList(p.actions)
    .map((raw, i) => decodeAction(raw, i, c))
    .filter((a): a is ActionSpec => a !== undefined)

  const primaries = actions.filter((a) => a.placement === "primary")
  if (primaries.length > 1) {
    warn(
      c,
      "actions",
      `${primaries.length} primary actions; a screen has one bottom button, the first wins`
    )
  }

  const permissionsRaw = obj(p.permissions)
  const permissions = {
    reads: {
      kinds: refList(obj(permissionsRaw.reads).kinds, KIND_RECORD_PREFIX),
    },
    writes: refList(permissionsRaw.writes, KIND_RECORD_PREFIX),
    call: refList(permissionsRaw.call, ""),
    agents: refList(permissionsRaw.agents, ""),
  }
  const source = typeof p.source === "string" ? p.source : undefined

  // The layout contracts.
  switch (layout) {
    case "board": {
      const spec = groupBy ? c.specs.find((s) => s.name === groupBy) : undefined
      if (kind && (!spec || (spec.kind !== "state" && spec.kind !== "enum"))) {
        fail(c, "groupBy", "a board groups by a state or enum property")
      }
      break
    }
    case "timeline":
      if (!traitId && kind && !temporalProperties(kind).length) {
        fail(c, "kind", "a timeline needs a trait or a kind binding temporal")
      }
      break
    case "contacts":
      if (
        kind &&
        !str((kind.definition as Record<string, unknown>).displayTemplate)
      ) {
        fail(c, "kind", "contacts need a kind with a displayTemplate")
      }
      break
    case "form":
      if (kind && !c.specs.some((s) => ownerWritable(s) && !s.managed)) {
        fail(c, "kind", "a form needs one owner-writable property")
      }
      break
    case "custom":
      if (!source) fail(c, "source", "a custom layout carries its document")
      if (kindId && !permissions.reads.kinds.includes(kindId)) {
        fail(
          c,
          "permissions.reads.kinds",
          "a custom layout's grant names the view's own kind"
        )
      }
      break
    default:
      break
  }

  const windowRaw = obj(p.window)
  const firstRaw = typeof p.first === "number" ? p.first : undefined
  const related: RelatedSpec[] = []
  for (const raw of objList(p.related)) {
    const view = refTarget(raw.view, VIEW_RECORD_PREFIX)
    if (view) related.push({ view, heading: str(raw.heading) })
  }

  return {
    id: record.id,
    name: str(p.name) ?? record.id,
    description: str(p.description),
    layout,
    kind: kindId,
    trait: traitId,
    requiresAtLeast: Object.fromEntries(
      Object.entries(obj(p.requiresAtLeast)).filter(
        (e): e is [string, number] => typeof e[1] === "number"
      )
    ),
    filter,
    orderBy,
    groupBy,
    show,
    facets,
    window: { past: str(windowRaw.past), future: str(windowRaw.future) },
    first: firstRaw && firstRaw >= 1 ? Math.min(firstRaw, 500) : 50,
    empty: str(p.empty),
    via,
    opens: refTarget(p.opens, VIEW_RECORD_PREFIX),
    related,
    attach: strList(p.attach).filter((a): a is Attach =>
      ATTACH.has(a as Attach)
    ),
    replaces: p.replaces === true,
    actions,
    permissions,
    source,
    problems,
  }
}
