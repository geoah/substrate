/** The Connections page's reads and verbs, every one an existing route: the
 * synchronization read (`GET /api/v1/sync/status`, one row per record of a
 * kind binding the core `sync` trait, joined with the record triggers on its
 * kind), the trigger status and parked lists, the trigger records themselves
 * (for a trigger's `when`, which the status does not carry), the run ledger
 * of one trigger, and the writes a Connection takes: a patch of the owner's
 * two hands on the trait (`syncRequestedAt`, `syncPaused`), a trigger wake,
 * run and parked retry, and the delete that disconnects an account. */

import { queryOptions } from "@tanstack/react-query"

import {
  CORE_PACKAGE,
  collectionPath,
  corePath,
  request,
  rootPath,
  seg,
} from "./http"
import { listPath, patchRecord, recordPath } from "./records"
import { splitKind } from "./http"
import type {
  OperationalList,
  Page,
  SubstrateRecord,
  SyncStatus,
  TriggerFailure,
  TriggerRan,
  TriggerStatus,
} from "./types"

/** The core `sync` trait by its full identity, the spelling every host check
 * compares (a package-local look-alike can never answer for it). */
export const SYNC_TRAIT = `${CORE_PACKAGE}/sync`

/** The core `oauth2` trait, the kinds whose records are a provider's client
 * credentials. */
export const OAUTH2_TRAIT = `${CORE_PACKAGE}/oauth2`

const TRIGGERS = corePath("trigger")
const TRIGGER_KIND = `${CORE_PACKAGE}/trigger`
const TRIGGER_RUN_KIND = `${CORE_PACKAGE}/triggerrun`

/** Every `sync`-trait record's synchronization, joined with its triggers. */
export const syncStatusesQueryOptions = queryOptions({
  queryKey: ["sync", "status"],
  queryFn: async ({ signal }) => {
    const res = await request<OperationalList<SyncStatus>>(
      "GET",
      rootPath("sync", "status"),
      undefined,
      { signal }
    )
    return res.items ?? []
  },
  staleTime: 15_000,
})

/** Every trigger's delivery bookkeeping: `…/trigger/status`. */
export const triggerStatusesQueryOptions = queryOptions({
  queryKey: ["triggers", "status"],
  queryFn: async ({ signal }) => {
    const res = await request<OperationalList<TriggerStatus>>(
      "GET",
      `${TRIGGERS}/status`,
      undefined,
      { signal }
    )
    return res.items ?? []
  },
  staleTime: 15_000,
})

/** The trigger RECORDS: the status carries no `source`, and the Connections
 * page reads a record trigger's kinds and its `when` to tell the on-request
 * trigger from the on-connect one. Registry-shaped, so one bounded page. */
export const triggerRecordsQueryOptions = queryOptions({
  queryKey: ["records", [TRIGGER_KIND], "connections"],
  queryFn: async ({ signal }) => {
    const page = await request<Page>(
      "GET",
      listPath({ kinds: [TRIGGER_KIND], first: 500 }),
      undefined,
      { signal }
    )
    return page.records ?? []
  },
  staleTime: 60_000,
})

/** One trigger's parked deliveries: `…/trigger/{id}/parked`. */
export function triggerParkedQueryOptions(id: string) {
  return queryOptions({
    queryKey: ["triggers", "parked", id],
    queryFn: async ({ signal }) => {
      const res = await request<OperationalList<TriggerFailure>>(
        "GET",
        `${TRIGGERS}/${seg(id)}/parked`,
        undefined,
        { signal }
      )
      return res.items ?? []
    },
    staleTime: 15_000,
  })
}

/** The newest runs of a set of triggers, off the `triggerrun` ledger: the
 * kind in `filter.kinds`, the trigger as a `referencing` arm on the run's
 * `trigger` reference, newest first. One trigger per read, because the
 * reverse read takes one referent. */
export function triggerRunsQueryOptions(triggerId: string, first = 20) {
  return queryOptions({
    queryKey: ["triggers", "runs", triggerId, first],
    queryFn: async ({ signal }) => {
      const page = await request<Page>(
        "GET",
        listPath({
          kinds: [TRIGGER_RUN_KIND],
          first,
          orderBy: "updatedAt:desc",
          filter: {
            referencing: {
              ref: recordPath(TRIGGER_KIND, triggerId),
              property: "trigger",
            },
          },
        }),
        undefined,
        { signal }
      )
      return page.records ?? []
    },
    staleTime: 15_000,
  })
}

// ── verbs ────────────────────────────────────────────────────────────────────

export function wakeTrigger(id: string): Promise<TriggerRan> {
  return request<TriggerRan>("POST", `${TRIGGERS}/${seg(id)}/wake`)
}

export function runTrigger(
  id: string,
  kind: string,
  recordId: string
): Promise<TriggerRan> {
  return request<TriggerRan>("POST", `${TRIGGERS}/${seg(id)}/run`, {
    kind,
    id: recordId,
  })
}

export function retryParked(
  triggerId: string,
  failureId: number
): Promise<TriggerRan> {
  return request<TriggerRan>(
    "POST",
    `${TRIGGERS}/${seg(triggerId)}/parked/${failureId}/retry`
  )
}

/** Ask for a run now: the owner's stamp on the trait's request property. The
 * on-request triggers on the kind fire on the write; `wakeTriggers` then
 * drains them without waiting for the dispatcher's next pass. */
export function requestSync(
  record: Pick<SubstrateRecord, "kind" | "id">,
  at = new Date().toISOString()
): Promise<SubstrateRecord> {
  const { authority, pkg, name } = splitKind(record.kind)
  return patchRecord(authority, pkg, name, record.id, {
    properties: { syncRequestedAt: at },
  })
}

/** Pause or resume: the trait's `syncPaused`, the owner's other hand. */
export function setSyncPaused(
  record: Pick<SubstrateRecord, "kind" | "id">,
  paused: boolean
): Promise<SubstrateRecord> {
  const { authority, pkg, name } = splitKind(record.kind)
  return patchRecord(authority, pkg, name, record.id, {
    properties: { syncPaused: paused },
  })
}

/** Wake several triggers, answering how many deliveries ran in total. A
 * wake that refuses (a trigger disabled since) fails the whole call, so the
 * caller can say so. */
export async function wakeTriggers(ids: string[]): Promise<number> {
  let ran = 0
  for (const id of ids) {
    const res = await wakeTrigger(id)
    ran += res.ran
  }
  return ran
}

/** Disconnect: delete the account record. The OAuth facility revokes the
 * grant best-effort and drops the stored credential before the row goes. */
export function deleteRecord(
  record: Pick<SubstrateRecord, "kind" | "id">
): Promise<void> {
  const { authority, pkg, name } = splitKind(record.kind)
  return request<void>(
    "DELETE",
    `${collectionPath(authority, pkg, name)}/${seg(record.id)}`
  )
}
