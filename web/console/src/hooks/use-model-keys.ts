/** The AI model rows (`llm/provider`) and where each one's key comes from,
 * read the same way wherever the console asks: the Providers page's cards,
 * its count in the sidebar and on Home, and an agent's "can it answer yet".
 *
 * The operator's read is asked for where `server.json` lists
 * `operatorDefaults`, or where a caller always wants it (the Providers page,
 * whose cards also show a failing key); otherwise a row's key is its own
 * `apiKey`, which is all a deployment with no defaults file can resolve. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { PROVIDERS_AUTHORITY } from "@/lib/actor-identity"
import { llmProvidersQueryOptions } from "@/lib/api/agents"
import { bundleStatusesQueryOptions } from "@/lib/api/bundles"
import { discoveryQueryOptions, hasFeature } from "@/lib/api/discovery"
import {
  OPERATOR_DEFAULTS_FEATURE,
  operatorDefaultsQueryOptions,
} from "@/lib/api/operator"
import type {
  BundleStatus,
  OperatorDefaults,
  SubstrateRecord,
} from "@/lib/api/types"
import {
  modelItem,
  modelKey,
  modelName,
  operatorWords,
  type ModelKey,
  type OperatorWords,
} from "@/lib/operator-defaults"

export interface ModelKeys {
  /** The rows, once read. */
  rows: SubstrateRecord[] | undefined
  /** The operator's read: null from a server older than it, undefined while
   * it is not asked for or not answered. */
  read: OperatorDefaults | null | undefined
  /** `server.json` lists `operatorDefaults`. */
  offered: boolean
  words: OperatorWords
  /** Where one row's key comes from and whether it works. */
  keyOf: (row: SubstrateRecord) => ModelKey
  /** Whether the row with this id has a key that resolves; undefined while
   * that cannot be told yet, or where no such row exists. */
  resolves: (rowId: string | undefined) => boolean | undefined
  pending: boolean
  error: Error | null
}

export function useModelKeys({ always = false } = {}): ModelKeys {
  const discovery = useQuery(discoveryQueryOptions)
  const offered = hasFeature(discovery.data, OPERATOR_DEFAULTS_FEATURE)
  const asked = always || offered
  const operator = useQuery({ ...operatorDefaultsQueryOptions, enabled: asked })
  const rowsRead = useQuery(llmProvidersQueryOptions())
  const rows = rowsRead.data?.records
  const read = asked ? operator.data : undefined
  // Asked and not answered, or not yet known whether to ask: a row with no
  // key of its own may still be the operator's, so nothing is said yet.
  const waiting =
    (asked && operator.isPending) || (!always && discovery.isPending)

  return useMemo(() => {
    const items = read?.items
    const keyOf = (row: SubstrateRecord) =>
      modelKey(row, modelItem(items, row.id))
    return {
      rows,
      read,
      offered,
      words: operatorWords(read?.operator),
      keyOf,
      resolves: (rowId) => {
        if (!rowId || waiting) return undefined
        const row = rows?.find((r) => r.id === rowId)
        return row ? keyOf(row).state !== "setup" : undefined
      },
      pending: rowsRead.isPending || waiting,
      error: rowsRead.error,
    }
  }, [rows, read, offered, waiting, rowsRead.isPending, rowsRead.error])
}

/** What the Providers page shows as set up, counted: the installed provider
 * packages and the AI model rows whose key resolves. Undefined until both
 * are read. */
export function useProvidersInUse(): {
  count: number | undefined
  /** The installed provider packages. */
  packages: BundleStatus[] | undefined
  /** The names of the AI model rows whose key resolves. */
  models: string[] | undefined
} {
  const statuses = useQuery(bundleStatusesQueryOptions)
  const keys = useModelKeys()
  const packages = useMemo(
    () => statuses.data?.filter((b) => b.authority === PROVIDERS_AUTHORITY),
    [statuses.data]
  )
  const models = useMemo(
    () =>
      keys.rows && !keys.pending
        ? keys.rows
            .filter((r) => keys.keyOf(r).state !== "setup")
            .map((r) => modelName(r))
        : undefined,
    [keys]
  )
  const count = packages && models ? packages.length + models.length : undefined
  return { count, packages, models }
}
