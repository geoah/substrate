/** The browse page's filter model: a flat list of `field op value` rows the
 * toolbar renders as full-size controls, serialized two ways — into the URL
 * (shareable views are table stakes) and into the wire's filter grammar.
 *
 * Ops stay small on purpose: `eq` for scalars (comma = membership, `in`),
 * `contains` for repeated properties, and `prefix` for starts-with matching —
 * typed as a trailing `*` in the value (`geo*`). That trailing star is the
 * whole wildcard grammar: the wire's only substring-ish op is `prefix`
 * (LIKE stem%); its `contains` is jsonb membership on repeated values, NOT
 * substring (verified live, 2026-08-06), so `*infix*` is not offered. */

import type { Cond, RecordFilter } from "@/lib/api/types"
import type { DeclaredProperty } from "@/lib/definition"

export type FilterOp = "eq" | "contains" | "prefix"

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
      ? token.match(/^([\w.]+)~(eq|contains|prefix)~(.*)$/)
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
export function coerceValue(raw: string, prop?: DeclaredProperty): unknown {
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

/** The op a field filters with: repeated properties match item-wise. */
export function opFor(prop?: DeclaredProperty): FilterOp {
  return prop?.repeated ? "contains" : "eq"
}

// ── the value editor's wildcard grammar ─────────────────────────────────────

/** Kinds whose values are prose-ish enough that a trailing `*` reads as a
 * wildcard, never as data. Everything else (ints, bools, states…) keeps the
 * character literal. */
const PREFIXABLE_KINDS = new Set([
  "string",
  "text",
  "markdown",
  "email",
  "url",
  "phone",
])

/** Whether a field's value editor honors the trailing-`*` wildcard. */
export function canPrefix(prop?: DeclaredProperty): boolean {
  return !prop || (!prop.repeated && PREFIXABLE_KINDS.has(prop.kind))
}

/** What the user typed → the filter it means. A trailing `*` on a scalar
 * string-ish field is the starts-with wildcard (`geo*` → prefix "geo");
 * everything else keeps the field's natural op. */
export function parseValueInput(
  raw: string,
  prop?: DeclaredProperty
): { op: FilterOp; value: string } {
  if (canPrefix(prop) && raw.length > 1 && raw.endsWith("*")) {
    return { op: "prefix", value: raw.slice(0, -1) }
  }
  return { op: opFor(prop), value: raw }
}

/** The value as the control (and the editor's draft) shows it: a prefix
 * filter wears its trailing `*` back. */
export function displayValue(f: ActiveFilter): string {
  return f.op === "prefix" ? `${f.value}*` : f.value
}

// ── per-type persistence (localStorage) ─────────────────────────────────────
// The last-used filters and sort survive navigation: a BARE url restores
// them; explicit url params always win (shareable views stay exact).

export interface BrowsePrefs {
  /** Encoded filter tokens (`encodeFilter` output), absent when none. */
  filter?: string[]
  /** `property:dir`, absent when the default sort was in effect. */
  sort?: string
}

function prefsKey(group: string, collection: string): string {
  return `substrate.browse.${group}/${collection}`
}

export function loadBrowsePrefs(
  group: string,
  collection: string
): BrowsePrefs | null {
  try {
    const raw = localStorage.getItem(prefsKey(group, collection))
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
    return out.filter?.length || out.sort ? out : null
  } catch {
    return null
  }
}

/** Persist the view; an all-default view (no filters, default sort) removes
 * the entry entirely — clearing filters clears the stored state too. */
export function saveBrowsePrefs(
  group: string,
  collection: string,
  prefs: BrowsePrefs
): void {
  try {
    const out: BrowsePrefs = {}
    if (prefs.filter?.length) out.filter = prefs.filter
    if (prefs.sort) out.sort = prefs.sort
    if (out.filter || out.sort) {
      localStorage.setItem(prefsKey(group, collection), JSON.stringify(out))
    } else {
      localStorage.removeItem(prefsKey(group, collection))
    }
  } catch {
    // Storage full or denied — the URL still carries the view.
  }
}
