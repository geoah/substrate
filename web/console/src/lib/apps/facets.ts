/** Facets: the properties a person narrows the rows by while looking, one
 * chip row per property under the header. The selection lives in the URL
 * search as `f.<property>` (a comma list of values), so a narrowed screen is
 * a shareable address and back/forward restore it; the screen reads it
 * (`useFacetSelection`) into `ViewContext.facets`, and every read ANDs it
 * into the request filter (`applyFacets`). The declared `filter` is never
 * touched, so a create's seed, which reads that filter, cannot change under
 * a chip. A chip's values come from the declaration for a state or an enum
 * (in declared order, held to what the filter admits) and from the loaded
 * rows for a reference, since a referent set has no declaration to read.
 *
 * A selection NARROWS and never widens: it is intersected with the value
 * test the view already holds on the property, because the URL is not the
 * chip row. A shared address, an edited one, or one kept from before the
 * view changed can carry a value the view does not admit, and honoring it
 * would show rows the view was written to hide. A value outside the admitted
 * set is dropped and said (`facetProblems`), never sent. */

import { useCallback, useMemo } from "react"
import { parseAsArrayOf, parseAsString, useQueryStates } from "nuqs"

import {
  readReference,
  type Cond,
  type KindInfo,
  type RecordFilter,
  type SubstrateRecord,
} from "@/lib/api/types"
import { splitRecordPath } from "@/lib/record-path"
import { humanizeName, type PropSpec } from "@/lib/record-schema"
import { comparable, specOf } from "./cond"
import { admittedStates } from "./machine"
import type { FacetSelection, Problem, ViewSpec } from "./spec"
import { isToken } from "./tokens"

/** The URL search key a facet lives under: `f.priority=high,low`. */
export const FACET_PARAM_PREFIX = "f."

export function facetParam(property: string): string {
  return `${FACET_PARAM_PREFIX}${property}`
}

const NO_NAMES: readonly string[] = []
const listParser = parseAsArrayOf(parseAsString).withDefault([])

export interface FacetControls {
  /** The live selection: only properties with at least one value. */
  selection: FacetSelection
  /** Add or remove one value. `only` keeps one value per property, which is
   * what a repeated property needs: it matches item-wise and the wire has
   * one `contains`. */
  toggle: (property: string, value: string, mode?: "toggle" | "only") => void
  clear: () => void
  any: boolean
}

/** The facet selection of a view, in the URL. Replace, not push: a chip is a
 * way of looking, and the back arrow leaves the screen rather than
 * unpicking chips one by one. */
export function useFacetSelection(
  spec: Pick<ViewSpec, "facets"> | undefined
): FacetControls {
  const key = (spec?.facets ?? NO_NAMES).join("\n")
  const names = useMemo(() => (key ? key.split("\n") : []), [key])
  const parsers = useMemo(
    () => Object.fromEntries(names.map((name) => [name, listParser])),
    [names]
  )
  const urlKeys = useMemo(
    () => Object.fromEntries(names.map((name) => [name, facetParam(name)])),
    [names]
  )
  const [state, setState] = useQueryStates(parsers, { urlKeys })
  const selection = useMemo<FacetSelection>(() => {
    const out: FacetSelection = {}
    for (const name of names) {
      const values = state[name]
      if (values?.length) out[name] = values
    }
    return out
  }, [names, state])
  const toggle = useCallback<FacetControls["toggle"]>(
    (property, value, mode = "toggle") => {
      void setState((prev) => {
        const current = prev[property] ?? []
        const has = current.includes(value)
        const next =
          mode === "only"
            ? has
              ? []
              : [value]
            : has
              ? current.filter((v) => v !== value)
              : [...current, value]
        return { [property]: next }
      })
    },
    [setState]
  )
  const clear = useCallback(() => void setState(null), [setState])
  return {
    selection,
    toggle,
    clear,
    any: Object.keys(selection).length > 0,
  }
}

/** The values a condition's own tests admit for the property, as strings:
 * the one `eq`, the `in` list, or both intersected. `undefined` is every
 * value (no value test); `"unresolved"` is a test still written as a token,
 * which admits nothing knowable until the read substitutes it. */
function admittedValues(
  cond: Cond | undefined
): Set<string> | "unresolved" | undefined {
  if (!cond) return undefined
  const tests: unknown[][] = []
  if (cond.eq !== undefined) tests.push([cond.eq])
  if (cond.in) tests.push(cond.in)
  if (!tests.length) return undefined
  if (tests.some((t) => t.some(isToken))) return "unresolved"
  let admitted: Set<string> | undefined
  for (const test of tests) {
    const values = new Set(test.map((v) => String(comparable(v))))
    admitted = admitted
      ? new Set([...admitted].filter((v) => values.has(v)))
      : values
  }
  return admitted
}

/** What a selection did to a filter. */
export interface FacetApplication {
  /** The filter with the selection ANDed in. The input is not mutated. */
  filter: RecordFilter
  /** The selection as sent: a value the view does not admit is gone. */
  selection: FacetSelection
  /** One warning per value dropped, at `facets.<property>`, for the bar to
   * show beside the chips. */
  problems: Problem[]
}

/** The filter with the selection ANDed in, each property's pick INTERSECTED
 * with the value test the view holds there. A scalar's picks are cut to the
 * admitted set and written as `eq` (one) or `in` (several) in the old test's
 * place, which narrows because the result is inside it; a pick with nothing
 * left is dropped and the view's own test stands. A repeated property
 * matches item-wise with its latest pick as `contains`; where the view
 * already holds a `contains` of its own, that requirement stays, and the
 * pick rides beside it as `in` on a reference alone, because on a reference
 * every value test is a containment probe and one Cond carries both
 * (engine/query.go, condReference), while on any other repeated property
 * the wire has one containment slot and the pick is dropped and said. */
export function applyFacets(
  filter: RecordFilter,
  selection: FacetSelection | undefined,
  kind?: KindInfo
): FacetApplication {
  const picked = Object.entries(selection ?? {}).filter(([, v]) => v.length)
  if (!picked.length) return { filter, selection: {}, problems: [] }
  const properties = { ...(filter.properties ?? {}) }
  const sent: FacetSelection = {}
  const problems: Problem[] = []
  for (const [name, values] of picked) {
    const original = properties[name]
    const prop = specOf(kind, name)
    const label = prop?.label ?? humanizeName(name)
    const path = `facets.${name}`
    const admitted = admittedValues(original)
    // A token is resolved by the read, which applies the selection then; a
    // caller holding the declared filter (the bar) has nothing to intersect
    // with yet, and writing the pick over the token would widen the read.
    if (admitted === "unresolved") continue
    const outside = (value: string) =>
      problems.push({
        path,
        message: `${label}: "${value}" is not among the values this view shows; that chip was ignored`,
        severity: "warning",
      })
    if (prop?.repeated) {
      const pick = values[values.length - 1]
      if (admitted && !admitted.has(pick)) {
        outside(pick)
        continue
      }
      const held =
        original?.contains === undefined
          ? undefined
          : String(comparable(original.contains))
      if (held === undefined || held === pick) {
        properties[name] = { ...original, contains: pick }
      } else if (prop.kind === "reference") {
        properties[name] = { ...original, in: [pick] }
      } else {
        problems.push({
          path,
          message: `${label} is held to "${held}" by this view, and the wire asks one ${label.toLowerCase()} at a time; "${pick}" was ignored`,
          severity: "warning",
        })
        continue
      }
      sent[name] = [pick]
      continue
    }
    const kept = admitted ? values.filter((v) => admitted.has(v)) : values
    for (const value of values) if (!kept.includes(value)) outside(value)
    if (!kept.length) continue
    const rest: Cond = { ...original }
    delete rest.eq
    delete rest.in
    properties[name] =
      kept.length === 1 ? { ...rest, eq: kept[0] } : { ...rest, in: kept }
    sent[name] = kept
  }
  return { filter: { ...filter, properties }, selection: sent, problems }
}

/** `applyFacets`, for a caller that wants the filter alone. */
export function withFacets(
  filter: RecordFilter,
  selection: FacetSelection | undefined,
  kind?: KindInfo
): RecordFilter {
  return applyFacets(filter, selection, kind).filter
}

/** The chips the read could not honor, for the bar. `filter` is what the
 * read is held to: the view's filter with its tokens resolved where the
 * caller has the context, else the declared one, whose token-valued tests
 * admit everything until resolved. */
export function facetProblems(
  filter: RecordFilter,
  selection: FacetSelection | undefined,
  kind?: KindInfo
): Problem[] {
  return applyFacets(filter, selection, kind).problems
}

export interface FacetChip {
  value: string
  label: string
}

export interface FacetGroup {
  property: string
  label: string
  /** One value at a time (a repeated property). */
  single: boolean
  chips: FacetChip[]
}

/** The values a declared closed set offers, held to the filter: the `in`
 * list, the one `eq`, else every declared value, in declared order. */
function declaredChips(filter: RecordFilter, spec: PropSpec): FacetChip[] {
  let keys: string[]
  if (spec.kind === "state") {
    keys = admittedStates(filter, spec)
  } else {
    const cond = filter.properties?.[spec.name]
    if (cond?.in?.length) keys = cond.in.map(String)
    else if (cond?.eq !== undefined) keys = [String(cond.eq)]
    else keys = spec.values?.map((v) => v.value) ?? []
  }
  return keys.map((value) => ({
    value,
    label: spec.values?.find((v) => v.value === value)?.label || value,
  }))
}

/** The referents the rows name under one property, path → label, the title
 * when one is known and the id otherwise. */
export function referentChips(
  records: SubstrateRecord[],
  property: string,
  titles: Map<string, string>
): Map<string, string> {
  const out = new Map<string, string>()
  const add = (value: unknown) => {
    const held = readReference(value)
    if (!held) return
    const parts = splitRecordPath(held.path)
    if (!parts) return
    out.set(held.path, titles.get(held.path) ?? parts.id)
  }
  for (const record of records) {
    const value = record.properties[property]
    if (Array.isArray(value)) value.forEach(add)
    else add(value)
  }
  return out
}

/** The referents remembered per reference facet, path → label. */
export type ReferentMemory = Map<string, Map<string, string>>

/** The memory with the rows' referents folded in: a new path is added, a
 * path whose title arrived replaces its id, and nothing is forgotten.
 * Answers the same Map when nothing changed, so a render keyed on it does
 * not move. */
export function mergeSeen(
  prev: ReferentMemory,
  records: SubstrateRecord[],
  names: string[],
  titles: Map<string, string>
): ReferentMemory {
  let next: ReferentMemory | undefined
  for (const name of names) {
    const known = prev.get(name)
    for (const [path, label] of referentChips(records, name, titles)) {
      if (known?.get(path) === label) continue
      next ??= new Map(prev)
      const group = new Map(next.get(name) ?? known ?? [])
      group.set(path, label)
      next.set(name, group)
    }
  }
  return next ?? prev
}

/** The chip rows of a view. `seen` is the referents remembered across
 * narrowings (path → label per property): a narrowed page names fewer
 * referents than the whole did, and a chip must not vanish under the
 * finger that pressed it. A selected value the rows no longer name is kept
 * as a chip too, so it can be unpicked. */
export function facetGroups(
  spec: ViewSpec,
  kind: KindInfo | undefined,
  seen: ReferentMemory,
  selection: FacetSelection
): FacetGroup[] {
  const groups: FacetGroup[] = []
  for (const name of spec.facets) {
    const prop = specOf(kind, name)
    const label = prop?.label ?? humanizeName(name)
    const single = Boolean(prop?.repeated)
    if (prop && (prop.kind === "state" || prop.values?.length)) {
      groups.push({
        property: name,
        label,
        single,
        chips: declaredChips(spec.filter, prop),
      })
      continue
    }
    const known = new Map(seen.get(name) ?? [])
    for (const value of selection[name] ?? []) {
      if (!known.has(value)) {
        known.set(value, splitRecordPath(value)?.id ?? value)
      }
    }
    if (!known.size) continue
    const chips = [...known]
      .map(([value, text]) => ({ value, label: text }))
      .sort((a, b) => a.label.localeCompare(b.label))
    groups.push({ property: name, label, single, chips })
  }
  return groups
}
