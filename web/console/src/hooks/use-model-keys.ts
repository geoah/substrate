/** The AI model rows (`llm/provider`) and where each one's key comes from,
 * read the same way wherever the console asks: the Providers page's cards,
 * its count in the sidebar and on Home, and an agent's "can it answer yet".
 *
 * Every one of them asks the operator's read, whatever `server.json` lists:
 * a deployment with no defaults file still answers it, and its answer (a
 * row whose own key the server refuses is `not-set-up`) is what the
 * Providers page shows, so the counts read it too. Only a server older than
 * the read (404, 405, 501) falls back to the rows' own `apiKey`. While the
 * read has not answered, or where it failed, a row with no key of its own
 * may still be the operator's, so nothing says it needs setting up. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { llmProvidersQueryOptions } from "@/lib/api/agents"
import { bundleStatusesQueryOptions } from "@/lib/api/bundles"
import { listedProvider } from "@/lib/bundles"
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
  stateResolves,
  uncheckedKey,
  type ModelKey,
  type OperatorWords,
} from "@/lib/operator-defaults"

export interface ModelKeys {
  /** The rows, once read. */
  rows: SubstrateRecord[] | undefined
  /** The operator's read: null from a server older than it, undefined while
   * it has not answered or where it failed. */
  read: OperatorDefaults | null | undefined
  /** Why the operator's read failed, where it did. */
  readError: Error | null
  /** Ask the operator's read again. */
  recheck: () => void
  /** `server.json` lists `operatorDefaults`: the deployment loaded a
   * defaults file, which is what opens the Providers welcome. */
  offered: boolean
  words: OperatorWords
  /** Where one row's key comes from and whether it works. */
  keyOf: (row: SubstrateRecord) => ModelKey
  /** Whether the row with this id has a key that resolves; undefined while
   * that cannot be told, or where no such row exists. */
  resolves: (rowId: string | undefined) => boolean | undefined
  pending: boolean
  error: Error | null
}

export function useModelKeys(): ModelKeys {
  const discovery = useQuery(discoveryQueryOptions)
  const offered = hasFeature(discovery.data, OPERATOR_DEFAULTS_FEATURE)
  const operator = useQuery(operatorDefaultsQueryOptions)
  const rowsRead = useQuery(llmProvidersQueryOptions())
  const rows = rowsRead.data?.records
  const read = operator.data
  const readError = read === undefined ? operator.error : null
  const refetch = operator.refetch

  return useMemo(() => {
    const keyOf = (row: SubstrateRecord): ModelKey => {
      if (read === undefined)
        return uncheckedKey(row, readError ? "unchecked" : "checking")
      return modelKey(row, read ? modelItem(read.items, row.id) : undefined)
    }
    return {
      rows,
      read,
      readError,
      recheck: () => void refetch(),
      offered,
      words: operatorWords(read?.operator),
      keyOf,
      resolves: (rowId) => {
        const row = rowId ? rows?.find((r) => r.id === rowId) : undefined
        return row ? stateResolves(keyOf(row).state) : undefined
      },
      pending: rowsRead.isPending || (read === undefined && !readError),
      error: rowsRead.error,
    }
  }, [
    rows,
    read,
    readError,
    refetch,
    offered,
    rowsRead.isPending,
    rowsRead.error,
  ])
}

/** What the Providers page shows as set up, counted: the installed provider
 * packages it lists (each status says whether it is a provider, add-ons left
 * to their provider's page, decision record 0238) and the AI model rows
 * whose key resolves. It reads the bundle statuses and never the catalog,
 * whose previews are too dear for a count on every page. Undefined until
 * both are read, and while a read has not answered or failed (`error`): a
 * count that guessed would disagree with the page. */
export function useProvidersInUse(): {
  count: number | undefined
  /** The installed provider packages. */
  packages: BundleStatus[] | undefined
  /** The names of the AI model rows whose key resolves. */
  models: string[] | undefined
  /** Why the count cannot be told: the operator's read failed. */
  error: Error | null
} {
  const statuses = useQuery(bundleStatusesQueryOptions)
  const keys = useModelKeys()
  const packages = useMemo(
    () => statuses.data?.filter(listedProvider),
    [statuses.data]
  )
  const models = useMemo(
    () =>
      keys.rows && keys.read !== undefined
        ? keys.rows
            .filter((r) => stateResolves(keys.keyOf(r).state))
            .map((r) => modelName(r))
        : undefined,
    [keys]
  )
  const count = packages && models ? packages.length + models.length : undefined
  return {
    count,
    packages,
    models,
    error: keys.readError ?? statuses.error ?? null,
  }
}
