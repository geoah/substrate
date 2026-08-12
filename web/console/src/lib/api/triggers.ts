/** Trigger delivery bookkeeping. Status is computed and stored nowhere; a
 * replay is a cursor reset, a run is one synthesized delivery, a wake is an
 * immediate scan, a parked failure is retryable by hand. The trigger ROWS and
 * the run ledger are ordinary records under `core.substrate.reamde.dev/{triggers,
 * runs}` the generic browse renders — this module is only the computed status +
 * the verbs + the run slice per trigger. */

import { queryOptions } from "@tanstack/react-query"

import { recordsQueryOptions, recordIdSegment } from "./records"
import { API_BASE, CORE_AUTHORITY, request } from "./http"

/** One trigger's delivery bookkeeping (substrate.TriggerStatus).
 *
 * NOTE: `kind` is the trigger SOURCE ARM, and the substrate deliberately KEPT
 * the word `record` for it — the trigger/function dialect (`source.record`, the
 * `when:` CEL binding, the delivery envelope's `record` key) is one coherent
 * vocabulary and did not follow the record rename. Do not "fix" it here. */
export interface TriggerStatus {
  id: string
  /** The trigger SOURCE: record | schedule | webhook. */
  kind: string
  callable: string
  enabled: boolean
  /** The trigger's cursor, on record sources. */
  cursor?: number
  /** The changelog head. */
  head: number
  /** Head − cursor, on record sources. */
  lag?: number
  /** The newest schedule occurrence delivered (or parked past). */
  lastFire?: string
  parked: number
  /** Names a trigger the dispatcher cannot run (unparseable row / dead callable). */
  error?: string
}

/** One parked delivery (substrate.TriggerFailure). */
export interface TriggerFailure {
  id: number
  trigger: string
  /** The changelog seq delivered, for record-sourced failures. */
  seq?: number
  /** Set (and seq 0) when the parked delivery was a schedule/webhook fire. */
  fireId?: string
  recordId?: string
  attempts: number
  lastError: string
  parkedAt: string
}

// Trigger operational verbs live at the core.substrate.reamde.dev resource (ruling A8).
// Core absorbed the runtime kinds, so this is `/core.substrate.reamde.dev/triggers/*` —
// the old automation.substrate.reamde.dev path is GONE, not deprecated.
const TRIGGERS = `${API_BASE}/${CORE_AUTHORITY}/triggers`

export const triggerStatusesQueryOptions = queryOptions({
  queryKey: ["triggers", "status"],
  queryFn: async ({ signal }) => {
    const res = await request<{ triggers?: TriggerStatus[] }>(
      "GET",
      `${TRIGGERS}/status`,
      undefined,
      { signal }
    )
    return res.triggers ?? []
  },
  staleTime: 15_000,
  refetchInterval: 30_000,
})

export function triggerParkedQueryOptions(id: string) {
  return queryOptions({
    queryKey: ["trigger", "parked", id],
    queryFn: async ({ signal }) => {
      const res = await request<{ parked?: TriggerFailure[] }>(
        "GET",
        `${TRIGGERS}/${recordIdSegment(id)}/parked`,
        undefined,
        { signal }
      )
      return res.parked ?? []
    },
    staleTime: 15_000,
  })
}

/** The run ledger, scoped to one trigger, newest first. Run rows live at
 * `core.substrate.reamde.dev/runs`; the trigger is a plain property, so this is the
 * generic record list with a `trigger` filter. */
export function triggerRunsQueryOptions(id: string, first = 25) {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    plural: "runs",
    first,
    filter: { properties: { trigger: { eq: id } } },
    orderBy: "startedAt:desc",
  })
}

// ── the verbs ─────────────────────────────────────────────────────────────

/** Reset an record-sourced trigger's cursor to `from`; the dispatcher does the
 * rest (retrospective runs are cursor resets). */
export function replayTrigger(id: string, from: number): Promise<{ from: number }> {
  return request<{ from: number }>(
    "POST",
    `${TRIGGERS}/${recordIdSegment(id)}/replay`,
    { from }
  )
}

/** Synthesize one delivery of a record's current state, cursor untouched. The
 * run endpoint names the delivered record by its full identity — the `kind` and
 * `id` both (ticket 001), so an ambiguous bare id can't be run. */
export function runTrigger(
  id: string,
  recordKind: string,
  recordId: string
): Promise<{ ran: number }> {
  return request<{ ran: number }>(
    "POST",
    `${TRIGGERS}/${recordIdSegment(id)}/run`,
    { kind: recordKind, id: recordId }
  )
}

/** Scan NOW: a webhook fires once, an record-sourced trigger drains, a schedule
 * checks its due occurrence. */
export function wakeTrigger(id: string): Promise<{ ran: number }> {
  return request<{ ran: number }>(
    "POST",
    `${TRIGGERS}/${recordIdSegment(id)}/wake`
  )
}

/** Re-run one parked delivery; success deletes the failure row. */
export function retryParked(id: string, fid: number): Promise<{ ran: number }> {
  return request<{ ran: number }>(
    "POST",
    `${TRIGGERS}/${recordIdSegment(id)}/parked/${fid}/retry`
  )
}
