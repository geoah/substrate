/** The browse page's filter model: a flat list of `field op value` rows the
 * toolbar renders as full-size controls, serialized two ways — into the URL
 * (shareable views are table stakes) and into the wire's filter grammar.
 *
 * Ops stay small on purpose: `eq` for scalars (comma = membership, `in`),
 * `contains` for repeated properties, `prefix` for starts-with on an
 * identifier-shaped string (an email, a URL, a phone — typed as a trailing
 * `*`, `geo*`), and `match` for free text. `match` is the wire's full-text
 * operator over ONE property's words, in the search grammar: every word must
 * appear (stemmed, any case), `lay*` is a word prefix, `"a phrase"` wants the
 * words adjacent, `-word` excludes one, `a OR b` takes either. It is what a
 * person means when they type into a text filter, so it is the DEFAULT for
 * string, text and markdown properties, and `=value` is how they ask for the
 * exact value instead. The wire's `contains` is jsonb membership on repeated
 * values, NOT substring, so it is only ever reached through `=` on a repeated
 * field. */

import type { Cond, RecordFilter } from "@/lib/api/types"
import type { DeclaredProperty } from "@/lib/definition"

export type FilterOp = "eq" | "contains" | "prefix" | "match"

export interface ActiveFilter {
  field: string
  op: FilterOp
  value: string
}

// ── URL codec ───────────────────────────────────────────────────────────────
// One URL token per filter: `field~op~value`, value URI-encoded so it can
// carry anything (including `~`). nuqs stores the token array.

export function encodeFilter(f: ActiveFilter): string {
  return `${f.field}~${f.op}~${encodeURIComponent(f.value)}`
}

export function decodeFilter(token: string): ActiveFilter | null {
  const m =
    typeof token === "string"
      ? token.match(/^([\w.]+)~(eq|contains|prefix|match)~(.*)$/)
      : null
  if (!m) return null
  try {
    return {
      field: m[1],
      op: m[2] as FilterOp,
      value: decodeURIComponent(m[3]),
    }
  } catch {
    return null
  }
}

export function decodeFilters(tokens: string[] | null): ActiveFilter[] {
  return (tokens ?? [])
    .map(decodeFilter)
    .filter((f): f is ActiveFilter => f !== null)
}

// ── wire serialization ──────────────────────────────────────────────────────

/** Coerce a text value to the declared kind, so `?filter=` compares like for
 * like (jsonb equality is typed). */
function coerceValue(raw: string, prop?: DeclaredProperty): unknown {
  const kind = prop?.kind
  if (kind === "int" || kind === "float") {
    const n = Number(raw)
    return Number.isFinite(n) ? n : raw
  }
  if (kind === "bool") {
    if (raw === "true") return true
    if (raw === "false") return false
  }
  return raw
}

/** Fold the active filters into one wire filter. Comma in an `eq` value means
 * membership (`in`); several rows on one field also fold to `in`. */
export function toRecordFilter(
  filters: ActiveFilter[],
  props: DeclaredProperty[]
): RecordFilter | undefined {
  if (!filters.length) return undefined
  const properties: Record<string, Cond> = {}
  const eqValues = new Map<string, unknown[]>()
  for (const f of filters) {
    const prop = props.find((p) => p.name === f.field)
    if (f.op === "contains") {
      properties[f.field] = {
        ...properties[f.field],
        contains: coerceValue(f.value, prop),
      }
      continue
    }
    if (f.op === "prefix") {
      properties[f.field] = { ...properties[f.field], prefix: f.value }
      continue
    }
    if (f.op === "match") {
      properties[f.field] = { ...properties[f.field], match: f.value }
      continue
    }
    const parts = f.value.includes(",")
      ? f.value
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean)
      : [f.value]
    const list = eqValues.get(f.field) ?? []
    for (const part of parts) list.push(coerceValue(part, prop))
    eqValues.set(f.field, list)
  }
  for (const [field, values] of eqValues) {
    properties[field] = {
      ...properties[field],
      ...(values.length === 1 ? { eq: values[0] } : { in: values }),
    }
  }
  return { properties }
}

/** The ids a reference filter's comma-joined value names, in order. */
export function splitReferenceIds(value: string): string[] {
  return value
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean)
}

/** The exact-value op a field filters with: repeated properties match
 * item-wise, scalars by equality. A reference is the exception, repeated or
 * not: the engine reads `eq`, `contains` and `in` on a pointer alike
 * (query.go condReference), and `in` is the only form that carries several
 * referents on one property, so a reference takes `eq` and its comma-joined
 * ids fold to `in`, "any of". */
export function opFor(prop?: DeclaredProperty): FilterOp {
  if (prop?.kind === "reference") return "eq"
  return prop?.repeated ? "contains" : "eq"
}

// ── the value editor's grammar ──────────────────────────────────────────────

/** Kinds whose values are words: what a person types against them is a
 * full-text `match`, and `=` asks for the exact value. */
const TEXT_KINDS = new Set(["string", "text", "markdown"])

/** Kinds shaped like identifiers, where a word match means little (an address
 * is one token to the search dictionary) and starts-with is the useful
 * wildcard: a trailing `*` on a scalar is `prefix`. */
const IDENTIFIER_KINDS = new Set(["email", "url", "phone"])

/** Whether typed text on this field is a full-text match. An undeclared
 * field (no property to consult) is text. */
export function canMatch(prop?: DeclaredProperty): boolean {
  return !prop || TEXT_KINDS.has(prop.kind)
}

/** Whether a field's value editor honors the trailing-`*` starts-with
 * wildcard. */
export function canPrefix(prop?: DeclaredProperty): boolean {
  return Boolean(prop && !prop.repeated && IDENTIFIER_KINDS.has(prop.kind))
}

/** What the user typed → the filter it means. On a text field the words are
 * a `match` and a leading `=` means the exact value (`=George` → eq George;
 * on a repeated field, `contains`). On an identifier field a trailing `*` is
 * starts-with (`geo*` → prefix geo). Everything else keeps the field's
 * natural exact op, the character literal. */
export function parseValueInput(
  raw: string,
  prop?: DeclaredProperty
): { op: FilterOp; value: string } {
  if (canMatch(prop)) {
    if (raw.startsWith("=") && raw.length > 1) {
      return { op: opFor(prop), value: raw.slice(1) }
    }
    return { op: "match", value: raw }
  }
  if (canPrefix(prop) && raw.length > 1 && raw.endsWith("*")) {
    return { op: "prefix", value: raw.slice(0, -1) }
  }
  return { op: opFor(prop), value: raw }
}

/** The value as the control (and the editor's draft) shows it, so what reads
 * back is what parseValueInput would take again: a prefix filter wears its
 * trailing `*`, and an exact value on a text field wears its leading `=`. */
export function displayValue(f: ActiveFilter, prop?: DeclaredProperty): string {
  if (f.op === "prefix") return `${f.value}*`
  if (f.op !== "match" && canMatch(prop)) return `=${f.value}`
  return f.value
}

// ── per-type persistence (localStorage) ─────────────────────────────────────
// The last-used filters and sort survive navigation: a BARE url restores
// them; explicit url params always win (shareable views stay exact).

export interface BrowsePrefs {
  /** Encoded filter tokens (`encodeFilter` output), absent when none. */
  filter?: string[]
  /** `property:dir`, absent when the default sort was in effect. */
  sort?: string
  /** `false` when the reader turned the tree off on a kind that nests by a
   * parent reference; absent otherwise, the tree being the default. */
  nest?: boolean
}

function prefsKey(group: string, name: string): string {
  return `substrate.browse.${group}/${name}`
}

export function loadBrowsePrefs(
  group: string,
  name: string
): BrowsePrefs | null {
  try {
    const raw = localStorage.getItem(prefsKey(group, name))
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== "object" || parsed === null) return null
    const out: BrowsePrefs = {}
    const p = parsed as Record<string, unknown>
    if (
      Array.isArray(p.filter) &&
      p.filter.every((t): t is string => typeof t === "string")
    ) {
      out.filter = p.filter
    }
    if (typeof p.sort === "string" && p.sort) out.sort = p.sort
    if (p.nest === false) out.nest = false
    return out.filter?.length || out.sort || out.nest === false ? out : null
  } catch {
    return null
  }
}

/** Persist the view; an all-default view (no filters, default sort, the tree
 * on) removes the entry entirely — clearing filters clears the stored state
 * too. */
export function saveBrowsePrefs(
  group: string,
  name: string,
  prefs: BrowsePrefs
): void {
  try {
    const out: BrowsePrefs = {}
    if (prefs.filter?.length) out.filter = prefs.filter
    if (prefs.sort) out.sort = prefs.sort
    if (prefs.nest === false) out.nest = false
    if (out.filter || out.sort || out.nest === false) {
      localStorage.setItem(prefsKey(group, name), JSON.stringify(out))
    } else {
      localStorage.removeItem(prefsKey(group, name))
    }
  } catch {
    // Storage full or denied — the URL still carries the view.
  }
}
