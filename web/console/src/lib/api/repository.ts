/** The repository's own description of itself: the one
 * `substrate.reamde.dev/core/repository` record, which is the control-plane
 * row projected inside the repository so a client that only speaks the record
 * API can still say where it is.
 *
 * The Registry reads it for one thing: the AUTHORITY this repository owns
 * (decision record 0046), which is where an imported sample lands (0048) and
 * so what a sample card previews before the button is pressed. */

import { queryOptions } from "@tanstack/react-query"

import { corePath, request } from "./http"
import type { Page } from "./types"

/** The repository record's own shape, the two properties the console reads. */
export interface RepositoryInfo {
  /** The username this repository belongs to. */
  name: string
  /** The DNS-style authority this repository owns, the home of every kind
   * its user declares. Empty on a repository created before the column
   * existed, which the caller has to render as "unknown" rather than guess. */
  authority: string
}

async function fetchRepository(
  signal?: AbortSignal
): Promise<RepositoryInfo | undefined> {
  const page = await request<Page>(
    "GET",
    `${corePath("repository")}?first=1`,
    undefined,
    { signal }
  )
  const record = page.records?.[0]
  if (!record) return undefined
  const properties = (record.properties ?? {}) as Record<string, unknown>
  return {
    name: String(properties.name ?? ""),
    authority: String(properties.authority ?? ""),
  }
}

export const repositoryQueryOptions = queryOptions({
  queryKey: ["repository"],
  queryFn: ({ signal }) => fetchRepository(signal),
  // One row that changes never: the authority is chosen at registration and
  // permanent.
  staleTime: 10 * 60_000,
})
