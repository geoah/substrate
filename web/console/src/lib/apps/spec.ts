/** THE CONTRACT between a view record and the layout that draws it.
 *
 * A view is one `substrate.reamde.dev/core/view` record. `viewSpec` (in
 * `view-spec.ts`) decodes it once into a `ViewSpec`: every reference already
 * read down to the identity or id it names, every default resolved (an
 * action's placement by its verb, a label from its name), and every problem
 * one row can answer collected under `problems` with the path it sits at.
 * Nothing in a ViewSpec executes: `filter` is the wire grammar as data, still
 * carrying its `$input.<name>[.<property>]` tokens, and `tokens.ts` resolves
 * them against a `ViewContext` at request time, never at decode time, because
 * a view does not know its apps.
 *
 * A LAYOUT is one React component per `layout` value under
 * `components/apps/<layout>-view.tsx`, mounted by `view-renderer.tsx` with
 * `LayoutProps` and nothing else. It reads its rows through
 * `useViewRecords(spec, ctx, kinds)` (queries.ts), draws cells by the
 * property's `PropSpec.kind`, offers `spec.actions` through `<RowActions>` and
 * `<ActionButton>` (which dispatch to `actions.ts`), and reports a tap through
 * `onOpenRecord`; the screen around it decides whether that pushes `opens` or
 * opens the record sheet, and the screen owns the chrome and the one primary
 * action. A layout never fetches outside `queries.ts`, never writes outside
 * `actions.ts`, and never draws chrome.
 *
 * Roles derive from the declaration, not from the view: the heading is the
 * server-rendered `title`, the badge is the kind's sole `state` property (shown
 * only when the filter admits more than one state), the instant is the
 * `temporal(point: X)` binding, and a `show` name may be that hot column
 * (`dueAt`) as well as a declared property.
 *
 * Problems have two severities. An `error` blocks the render (the renderer
 * shows `problems.tsx` instead of the layout): the two-source view, an unmet
 * layout contract. A `warning` is shown above the layout and the offending
 * piece is skipped: a `show` name the kind lacks, a shadowed column, a link
 * whose `href` is not a url. */

import type { RecordFilter, SubstrateRecord, KindInfo } from "@/lib/api/types"

/** The seven renderers. The set is closed and moves with the binary; a value
 * is permanent once a live row carries it. */
export type Layout =
  "list" | "board" | "timeline" | "contacts" | "detail" | "form" | "custom"

export const LAYOUTS: readonly Layout[] = [
  "list",
  "board",
  "timeline",
  "contacts",
  "detail",
  "form",
  "custom",
]

/** The eight closed things an action does. */
export type Verb =
  | "create"
  | "transition"
  | "patch"
  | "delete"
  | "call"
  | "chat"
  | "open"
  | "link"

export const VERBS: readonly Verb[] = [
  "create",
  "transition",
  "patch",
  "delete",
  "call",
  "chat",
  "open",
  "link",
]

/** Where a verb sits: `primary` is the screen's one bottom button (a create's
 * default), `header` the view header, `row` the row's trailing button and
 * menu (every other verb's default). */
export type Placement = "primary" | "header" | "row"

export type Attach = "launcher" | "browse" | "record" | "home"

/** One thing wrong with a view or an app, at the path it sits at
 * (`data.properties.show[1]`, `actions[0].to`). */
export interface Problem {
  path: string
  message: string
  severity: "error" | "warning"
}

/** `when`: show an action only while one property matches. `in` values are
 * the declaration's strings, coerced to the property's datatype by `cond.ts`. */
export interface WhenSpec {
  property: string
  in?: string[]
  exists?: boolean
  not?: boolean
}

export interface OrderBySpec {
  property: string
  desc: boolean
}

export interface ActionSpec {
  name: string
  /** The button text: the declared `label`, else the humanized name. */
  label: string
  /** What the action does, in a sentence: the button's hover text and the
   * body of the confirmation. */
  description?: string
  /** A lucide icon name, kebab-case, for `DynamicIcon`. */
  icon?: string
  verb: Verb
  /** Resolved to its default by verb when the row does not say. */
  placement: Placement
  /** A transition's target state. */
  to?: string
  /** The state property a transition moves; absent means the kind's sole one
   * (resolved by `machine.ts`). */
  property?: string
  /** The view id an `open` pushes with the row as its record. */
  view?: string
  /** The NAME of a url-typed property a `link` opens. Never a template. */
  href?: string
  /** The properties a create or patch asks for, in order. */
  prompt: string[]
  /** Property → value written without asking; a value may be a token. */
  set: Record<string, string>
  /** The record path of the function a `call` invokes or the agent a `chat`
   * opens. */
  callable?: string
  /** A chat's opening message, a template over the row. */
  message?: string
  /** Ask before running. A delete always asks. */
  confirm: boolean
  when?: WhenSpec
}

export interface RelatedSpec {
  view: string
  heading?: string
}

export interface ViewSpec {
  /** The view record's id: the `/views/$id` segment. */
  id: string
  name: string
  description?: string
  layout: Layout
  /** The kind identity the view reads, or undefined for a trait view or a
   * view whose `kind` is malformed. Presence in the registry is the
   * renderer's check, not the decoder's: a view may precede its package. */
  kind?: string
  /** The trait identity whose implementors the view reads. */
  trait?: string
  /** Package identity → least version this view renders against. */
  requiresAtLeast: Record<string, number>
  /** The records filter as declared, tokens UNRESOLVED. */
  filter: RecordFilter
  /** The declared sort keys; empty means the kind's temporal point, else
   * `title` (resolved by `queries.ts`). */
  orderBy: OrderBySpec[]
  groupBy?: string
  /** The property names shown after the title; empty means the kind's column
   * properties. */
  show: string[]
  /** The properties a person narrows the rows by while looking, one chip row
   * each: a state, an enum or a reference. The selection lives in the URL
   * and reaches the read through `ViewContext.facets`, never through
   * `filter`, so a create's seed cannot change under a chip. */
  facets: string[]
  /** A timeline's span around now, as ISO 8601 durations. */
  window: { past?: string; future?: string }
  /** The page size; 50 when absent. */
  first: number
  empty?: string
  /** The reference property that scopes this view to an opened record. */
  via?: string
  /** The view id a row tap pushes; absent opens the record sheet. */
  opens?: string
  related: RelatedSpec[]
  attach: Attach[]
  replaces: boolean
  actions: ActionSpec[]
  /** A custom layout's grant, kinds read down to identities. */
  permissions: {
    reads: { kinds: string[] }
    writes: string[]
    call: string[]
    agents: string[]
  }
  /** A custom layout's HTML document. */
  source?: string
  problems: Problem[]
}

export interface ScreenSpec {
  name: string
  /** The tab and header text: the declared `label`, else the view's name. */
  label: string
  icon?: string
  /** The view record's id. */
  view: string
}

export interface InputSpec {
  /** The kind identity whose records satisfy the input. */
  kind?: string
  description?: string
}

export interface AppSpec {
  id: string
  name: string
  description?: string
  icon?: string
  home: boolean
  screens: ScreenSpec[]
  inputs: Record<string, InputSpec>
  /** Input name → the bound record's path. */
  bindings: Record<string, string>
  problems: Problem[]
}

/** A facet selection: property name → the values a person picked, one
 * value meaning `eq` and several meaning `in`. */
export type FacetSelection = Record<string, string[]>

/** What a view is mounted with. `inputs` holds each resolved app input (the
 * record, or undefined while it is unresolved); `parent` is the record a
 * `via` view is scoped to, or a detail view's subject; `mode` says how much
 * room the mount has; `facets` is the selection live on this mount, read
 * from the URL by the screen and ANDed into every read (`facets.ts`), absent
 * where nothing threads one (a card), so a layout draws no chips there. */
export interface ViewContext {
  inputs: Record<string, SubstrateRecord | undefined>
  parent?: { record: SubstrateRecord; kind: KindInfo }
  mode: "page" | "card" | "inline"
  facets?: FacetSelection
}

/** What a button needs to run one of the view's actions: the view, its
 * kind, the registry and the mount context. A layout builds it once from its
 * `LayoutProps` and hands it to `<RowActions>` and `<ActionButton>`. */
export interface ActionHost {
  spec: ViewSpec
  kind?: KindInfo
  kinds: KindInfo[]
  ctx: ViewContext
}

/** What every layout component receives, and all it receives. */
export interface LayoutProps {
  spec: ViewSpec
  /** The view's kind from the registry; undefined for a trait view. */
  kind?: KindInfo
  kinds: KindInfo[]
  ctx: ViewContext
  /** A row tap. The screen pushes `spec.opens` or opens the record sheet. */
  onOpenRecord: (record: SubstrateRecord) => void
}

export function isLayout(value: unknown): value is Layout {
  return typeof value === "string" && (LAYOUTS as string[]).includes(value)
}

export function isVerb(value: unknown): value is Verb {
  return typeof value === "string" && (VERBS as string[]).includes(value)
}

export function blockingProblems(problems: Problem[]): Problem[] {
  return problems.filter((p) => p.severity === "error")
}
