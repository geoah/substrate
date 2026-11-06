/** The operator's defaults (docs/plans/host-defaults.md):
 * `GET /api/v1/operator/defaults`, one item per slot this repository spends
 * a key through (an llm/provider row, search by meaning, a provider's OAuth
 * client, a provider's tool key), each saying where its key comes from now,
 * and today's allowance where the operator pays. A deployment with no
 * defaults file answers the same read with no `operator` and every source
 * `own` or `none`.
 *
 * A server older than the route answers 404 (or 405, 501); that reads as
 * `null`, so the Providers page falls back to the rows' own keys instead of
 * failing. */

import { queryOptions } from "@tanstack/react-query"

import { request, rootPath } from "./http"
import { ApiError, type OperatorDefaults } from "./types"

/** The feature `server.json` lists where the deployment loaded an operator
 * defaults file, so a client knows the read has something to say. */
export const OPERATOR_DEFAULTS_FEATURE = "operatorDefaults"

const OPERATOR_DEFAULTS_PATH = rootPath("operator", "defaults")

export async function fetchOperatorDefaults(
  signal?: AbortSignal
): Promise<OperatorDefaults | null> {
  try {
    const read = await request<OperatorDefaults>(
      "GET",
      OPERATOR_DEFAULTS_PATH,
      undefined,
      { signal }
    )
    return { ...read, items: read.items ?? [] }
  } catch (error) {
    if (
      error instanceof ApiError &&
      (error.status === 404 || error.status === 405 || error.status === 501)
    )
      return null
    throw error
  }
}

export const operatorDefaultsQueryOptions = queryOptions({
  queryKey: ["operator", "defaults"],
  queryFn: ({ signal }) => fetchOperatorDefaults(signal),
  staleTime: 30_000,
})
