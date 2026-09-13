/** An app's inputs resolved the way the engine resolves a bundle's
 * (`internal/engine/inputs.go`): the bound record, else the record whose id
 * is `default`, else the sole live record of the kind, else nothing. Two
 * records with neither a binding nor a `default` are `ambiguous`, and the
 * screen offers the picker. A binding that names a record the kind no longer
 * holds, or a record of another kind, is `missing` and NEVER falls through to
 * `default` or the sole record: the owner chose whose data the app shows, and
 * a fallback would change it silently.
 *
 * An input resolves only INSIDE the grant. `inputs` and `bindings` are
 * ordinary properties an agent may write under an owner-written `source` and
 * `permissions`, so the kind an input names is held to owner provenance and
 * to the EXPANDED read grant before a single read is made: an input naming a
 * kind the grant does not read is `ungranted`, never queried, and withheld
 * from the guest (`grantedInputs`, which the frame applies again where the
 * host context is built). Without that, an input would be a read of any
 * kind at all that never passed `grantFor`.
 *
 * Every lookup is by id, never by scanning a page: the bound record and the
 * `default` record are each one `ids` filter, and none/sole/ambiguous is a
 * two-record probe, so a collection of any size resolves in at most two
 * reads and a record past the first page is never mistaken for absent. The
 * picker pages the collection on its own (`input-binder.tsx`). A read that
 * fails is `error`, not a load that never ends. This twin is v0; the engine
 * projects `status.inputs` on the app row in v1 and this file goes. */

import { useQueries } from "@tanstack/react-query"

import { recordsQueryOptions } from "@/lib/api/records"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { kindByIdentity, splitKind } from "@/lib/definition"
import { splitRecordPath } from "@/lib/record-path"
import type { InputState } from "./bridge/protocol"
import type { AppSpec, InputSpec } from "./spec"

export type { InputState }

/** The host's states: the wire's, plus the one the guest never sees. */
export type ResolvedInputState =
  InputState | { state: "ungranted"; kind: string }

/** What one read has answered so far. */
export type Lookup =
  | { status: "pending" }
  | { status: "error"; message: string }
  | { status: "ok"; records: SubstrateRecord[] }

/** The id `default`, which the engine's rule reads for before the sole
 * record. */
const DEFAULT_ID = "default"

/** Two records tell none from sole from ambiguous; a third says nothing
 * more. */
const PROBE = 2

/** One input's state from its reads. `lookup` is the bound record's page (an
 * `ids` filter on the binding's id) when there is a binding, else
 * `default`'s; `probe` is the two-record page, read only without a binding.
 * A binding that names another kind is `missing` before any read: the kind
 * the owner bound is not the kind the input takes. */
export function resolveInput(
  binding: string | undefined,
  kindIdentity: string,
  lookup: Lookup,
  probe: Lookup
): InputState {
  if (binding) {
    const parts = splitRecordPath(binding)
    if (!parts || parts.kind !== kindIdentity) {
      return { state: "missing", binding }
    }
    if (lookup.status === "error") {
      return { state: "error", message: lookup.message }
    }
    if (lookup.status === "pending") return { state: "loading" }
    const bound = lookup.records.find(
      (r) => r.id === parts.id && r.kind === kindIdentity
    )
    return bound
      ? { state: "bound", record: bound }
      : { state: "missing", binding }
  }
  if (lookup.status === "error") {
    return { state: "error", message: lookup.message }
  }
  if (lookup.status === "ok") {
    const byDefault = lookup.records.find((r) => r.id === DEFAULT_ID)
    if (byDefault) return { state: "default", record: byDefault }
  }
  if (probe.status === "error") {
    return { state: "error", message: probe.message }
  }
  if (lookup.status === "pending" || probe.status === "pending") {
    return { state: "loading" }
  }
  if (probe.records.length === 0) return { state: "none" }
  if (probe.records.length === 1) {
    return { state: "sole", record: probe.records[0] }
  }
  return { state: "ambiguous" }
}

/** What an unresolved input says on the screen; undefined once it resolves. */
export function inputStatus(
  name: string,
  state: ResolvedInputState,
  kind?: KindInfo
): string | undefined {
  const noun = kind?.name ?? "record"
  switch (state.state) {
    case "missing":
      return `\`${name}\` is bound to ${state.binding}, which no longer exists`
    case "none":
      return `no ${noun} connected yet for \`${name}\``
    case "unknown-kind":
      return `\`${name}\` needs a kind this repository does not have`
    case "ungranted":
      return `\`${name}\` reads ${state.kind}, which is outside the app's grant; permissions.reads.kinds: add ${state.kind}`
    case "ambiguous":
      return `pick which ${noun} is \`${name}\``
    case "error":
      return `\`${name}\` could not be read: ${state.message}`
    default:
      return undefined
  }
}

/** Whether the grant admits an input's kind: owner provenance, and the kind
 * among the identities the EXPANDED read grant covers. The one check
 * `useAppInputs` makes before reading and the frame makes again before
 * telling the guest. */
export function inputGranted(
  kind: string | undefined,
  reads: string[],
  granted: boolean
): kind is string {
  return granted && Boolean(kind) && reads.includes(kind!)
}

/** The states the guest may see: each input whose kind the grant admits,
 * and none other, so a resolver that was handed a record it should not have
 * been still hands nothing on. */
export function grantedInputs(
  states: Record<string, ResolvedInputState>,
  inputs: Record<string, InputSpec>,
  reads: string[],
  granted: boolean
): Record<string, InputState> {
  const out: Record<string, InputState> = {}
  for (const [name, state] of Object.entries(states)) {
    if (state.state === "ungranted") continue
    if (!inputGranted(inputs[name]?.kind, reads, granted)) continue
    out[name] = state
  }
  return out
}

export interface ResolvedInputs {
  states: Record<string, ResolvedInputState>
  /** The record per input, undefined while unresolved. */
  records: Record<string, SubstrateRecord | undefined>
  /** The kind identities the inputs read inside the grant, for the live
   * tail. */
  kinds: string[]
}

interface Entry {
  name: string
  input: InputSpec
  kind?: KindInfo
  binding?: string
  /** The id the lookup reads: the binding's, else `default`; absent when
   * the binding names another kind, which needs no read to be `missing`. */
  lookupId?: string
  admitted: boolean
}

/** Every input of an app resolved by id inside the grant. */
export function useAppInputs(
  app: AppSpec | undefined,
  kinds: KindInfo[],
  grant: { granted: boolean; reads: string[] }
): ResolvedInputs {
  const entries: Entry[] = Object.entries(app?.inputs ?? {}).map(
    ([name, input]) => {
      const kind = input.kind ? kindByIdentity(kinds, input.kind) : undefined
      const binding = app?.bindings[name]
      const parts = binding ? splitRecordPath(binding) : undefined
      const admitted =
        Boolean(kind) &&
        inputGranted(kind?.identity, grant.reads, grant.granted)
      const lookupId = !binding
        ? DEFAULT_ID
        : parts && parts.kind === kind?.identity
          ? parts.id
          : undefined
      return { name, input, kind, binding, lookupId, admitted }
    }
  )

  // One flat list of reads: per admitted input, its lookup, and its probe
  // when nothing is bound. `plan` says which index answers which input.
  const queries: ReturnType<typeof recordsQueryOptions>[] = []
  const plan = entries.map((entry) => {
    if (!entry.admitted || !entry.kind) return {}
    const { authority, pkg, name } = splitKind(entry.kind.identity)
    const collection = { authority, package: pkg, name }
    let lookup: number | undefined
    let probe: number | undefined
    if (entry.lookupId !== undefined) {
      lookup = queries.length
      queries.push(
        recordsQueryOptions({
          ...collection,
          first: 1,
          filter: { ids: [entry.lookupId] },
        })
      )
    }
    if (!entry.binding) {
      probe = queries.length
      queries.push(recordsQueryOptions({ ...collection, first: PROBE }))
    }
    return { lookup, probe }
  })

  return useQueries({
    queries,
    combine: (results) => {
      const lookupOf = (index: number | undefined): Lookup => {
        if (index === undefined) return { status: "pending" }
        const result = results[index]
        if (result.isError) {
          return { status: "error", message: result.error.message }
        }
        // A page kept from the previous key while a new one loads is not
        // this read's answer.
        if (!result.data || result.isPlaceholderData) {
          return { status: "pending" }
        }
        return { status: "ok", records: result.data.records ?? [] }
      }
      const states: Record<string, ResolvedInputState> = {}
      const records: Record<string, SubstrateRecord | undefined> = {}
      entries.forEach((entry, i) => {
        let state: ResolvedInputState
        if (!entry.kind) state = { state: "unknown-kind" }
        else if (!entry.admitted) {
          state = { state: "ungranted", kind: entry.kind.identity }
        } else {
          state = resolveInput(
            entry.binding,
            entry.kind.identity,
            lookupOf(plan[i].lookup),
            lookupOf(plan[i].probe)
          )
        }
        states[entry.name] = state
        records[entry.name] = "record" in state ? state.record : undefined
      })
      return {
        states,
        records,
        kinds: entries.filter((e) => e.admitted).map((e) => e.kind!.identity),
      }
    },
  })
}
