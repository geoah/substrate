/** The repository's own description of itself: the one
 * `substrate.reamde.dev/core/repository` record, which is the control-plane
 * row projected inside the repository so a client that only speaks the record
 * API can still say where it is.
 *
 * The Registry reads the AUTHORITY this repository owns (decision record
 * 0046), which is where an imported sample lands (0048) and so what a sample
 * card previews before the button is pressed; the account row and the
 * approval page read whether that name is verified (0208). */

import { queryOptions } from "@tanstack/react-query"

import { scalarText } from "@/lib/format"

import { CORE_AUTHORITY, CORE_PACKAGE_NAME, request } from "./http"
import { listPath } from "./records"
import type { Page } from "./types"

/** The repository record's own shape, the one property the console reads. */
export interface RepositoryInfo {
  /** The DNS-style authority this repository owns: its name, its id, and the
   * home of every kind its user declares. Empty on a repository created
   * before the column existed, which the caller has to render as "unknown"
   * rather than guess. */
  authority: string
  /** When the repository last proved its name (decision 0208), or "" while
   * it never has: the console marks the name unverified until then. */
  domainVerifiedAt: string
}

async function fetchRepository(
  signal?: AbortSignal
): Promise<RepositoryInfo | undefined> {
  const page = await request<Page>(
    "GET",
    listPath({
      authority: CORE_AUTHORITY,
      package: CORE_PACKAGE_NAME,
      name: "repository",
      first: 1,
    }),
    undefined,
    { signal }
  )
  const record = page.records?.[0]
  if (!record) return undefined
  const properties = record.properties ?? {}
  return {
    authority: scalarText(properties.authority),
    domainVerifiedAt: scalarText(properties.domainVerifiedAt),
  }
}

export const repositoryQueryOptions = queryOptions({
  queryKey: ["repository"],
  queryFn: ({ signal }) => fetchRepository(signal),
  // The authority is chosen at registration and permanent; the verified
  // stamp moves only on the owner's own check, which invalidates this query.
  staleTime: 10 * 60_000,
})
