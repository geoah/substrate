// The Healthspan filters, modelled on the Labs filters: a
// Status group and a Group group, OR within a group and AND across them,
// kept in the hash's query (`status`, `group`, comma-joined) with the
// defaults left out.

import {
  HEALTHSPAN_GROUPS,
  type HealthspanCardData,
  type HealthspanGroup,
  GROUP_LABELS,
} from "./healthspanCards"
import { HEALTHSPAN_STATUSES, type HealthspanStatus } from "../rules/healthspan"

export type { HealthspanGroup } from "./healthspanCards"

export const HEALTHSPAN_STATUS_OPTIONS: {
  value: HealthspanStatus
  label: string
}[] = [
  { value: "adds", label: "Adds years" },
  { value: "slipping", label: "Slipping" },
  { value: "flagged", label: "Flagged" },
  { value: "nodata", label: "No data" },
  { value: "ok", label: "On track" },
]

export const HEALTHSPAN_GROUP_OPTIONS: {
  value: HealthspanGroup
  label: string
}[] = HEALTHSPAN_GROUPS.map((g) => ({ value: g, label: GROUP_LABELS[g] }))

export interface HealthspanFilters {
  /** Selected statuses; empty is no constraint. */
  status: HealthspanStatus[]
  /** Selected groups; empty is no constraint. */
  group: HealthspanGroup[]
}

export const DEFAULT_HEALTHSPAN_FILTERS: HealthspanFilters = {
  status: [],
  group: [],
}

/** The URL keys the filters own. */
export const HEALTHSPAN_FILTER_KEYS = ["status", "group"] as const

function listOf<T extends string>(
  raw: string | null,
  order: readonly T[]
): T[] {
  const picked = (raw ?? "").split(",").map((s) => s.trim())
  return order.filter((v) => picked.includes(v))
}

/** The filters a URL holds; unknown values are dropped. */
export function parseHealthspanFilters(
  params: URLSearchParams
): HealthspanFilters {
  return {
    status: listOf(params.get("status"), HEALTHSPAN_STATUSES),
    group: listOf(params.get("group"), HEALTHSPAN_GROUPS),
  }
}

/** The filters as URL parameters, defaults omitted, each list in a fixed
 * order. */
export function healthspanFiltersToParams(
  f: HealthspanFilters
): URLSearchParams {
  const out = new URLSearchParams()
  const status = HEALTHSPAN_STATUSES.filter((s) => f.status.includes(s))
  const group = HEALTHSPAN_GROUPS.filter((g) => f.group.includes(g))
  if (status.length) out.set("status", status.join(","))
  if (group.length) out.set("group", group.join(","))
  return out
}

function passesStatus(
  c: HealthspanCardData,
  status: HealthspanStatus[]
): boolean {
  return !status.length || status.some((s) => c.statuses.has(s))
}

function passesGroup(c: HealthspanCardData, group: HealthspanGroup[]): boolean {
  return !group.length || group.includes(c.group)
}

/** The cards passing both groups of toggles. */
export function applyHealthspanFilters(
  cards: HealthspanCardData[],
  f: HealthspanFilters
): HealthspanCardData[] {
  return cards.filter(
    (c) => passesStatus(c, f.status) && passesGroup(c, f.group)
  )
}

/** Counts per toggle. A group's counts are taken among the cards passing
 * the other group, so a count says what choosing it would show. */
export function healthspanFilterCounts(
  cards: HealthspanCardData[],
  f: HealthspanFilters
): {
  status: Record<HealthspanStatus, number>
  group: Record<HealthspanGroup, number>
} {
  const status = Object.fromEntries(
    HEALTHSPAN_STATUSES.map((s) => [s, 0])
  ) as Record<HealthspanStatus, number>
  const group = Object.fromEntries(
    HEALTHSPAN_GROUPS.map((g) => [g, 0])
  ) as Record<HealthspanGroup, number>
  for (const c of cards) {
    if (passesGroup(c, f.group)) for (const s of c.statuses) status[s]++
    if (passesStatus(c, f.status)) group[c.group]++
  }
  return { status, group }
}

/** The route that opens a card from a link: `focus` names it, the status
 * filter dropped (it may hide the card) and the group kept only when the
 * card is in it. */
export function cardHref(
  card: HealthspanCardData,
  f: HealthspanFilters
): string {
  const kept = healthspanFiltersToParams({
    status: [],
    group: f.group.length && !f.group.includes(card.group) ? [] : f.group,
  })
  kept.set("focus", card.id)
  return `#/healthspan?${kept.toString()}`
}
