/** Facets: the properties a person narrows the rows by while looking, one
 * chip row per property under the header. The selection lives in the URL
 * search as `f.<property>` (a comma list of values), so a narrowed screen is
 * a shareable address and back/forward restore it; the screen reads it
 * (`useFacetSelection`) into `ViewContext.facets`, and every read ANDs it
 * into the request filter (`withFacets`). The declared `filter` is never
 * touched, so a create's seed, which reads that filter, cannot change under
 * a chip. A chip's values come from the declaration for a state or an enum
 * (in declared order, held to what the filter admits) and from the loaded
 * rows for a reference, since a referent set has no declaration to read. */

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
import { specOf } from "./cond"
import { admittedStates } from "./machine"
import type { FacetSelection, ViewSpec } from "./spec"

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

/** A condition with its value tests removed, so the selection's own can
 * take their place; the range and presence tests stay. */
function withoutValues(cond: Cond | undefined): Cond {
  const rest: Cond = { ...cond }
  delete rest.eq
  delete rest.in
  delete rest.contains
  return rest
}

/** The filter with the selection ANDed in: one value is `eq`, several are
 * `in`, and a repeated property takes the latest pick as `contains`. Chips
 * only ever offer what the declared filter admits, so replacing its value
 * test with the selection's narrows and never widens. The input is not
 * mutated: a seed built from `spec.filter` stays what the row declared. */
export function withFacets(
  filter: RecordFilter,
  selection: FacetSelection | undefined,
  kind?: KindInfo
): RecordFilter {
  const picked = Object.entries(selection ?? {}).filter(([, v]) => v.length)
  if (!picked.length) return filter
  const properties = { ...(filter.properties ?? {}) }
  for (const [name, values] of picked) {
    const rest = withoutValues(properties[name])
    properties[name] = specOf(kind, name)?.repeated
      ? { ...rest, contains: values[values.length - 1] }
      : values.length === 1
        ? { ...rest, eq: values[0] }
        : { ...rest, in: values }
  }
  return { ...filter, properties }
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
