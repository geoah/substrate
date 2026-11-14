/** Publishing one of the repository's own packages under an authority whose
 * DNS its owner controls: the package leaves `~` for good, takes the
 * authority's name, and another server can install it. The plan says what a
 * publish would do and changes nothing; the publish itself is refused (403)
 * until the authority's `_substrate` record names this repository's
 * document. A release writes the next version of a package the repository
 * already publishes. All are the owner's routes, listed under the
 * `publishing` feature. */

import { queryOptions } from "@tanstack/react-query"

import { request, rootPath } from "./http"
import type {
  PublishInput,
  PublishPlan,
  Release,
  ReleaseInput,
  Releases,
} from "./types"

/** The discovery feature that holds these routes. */
export const PUBLISHING_FEATURE = "publishing"

/** What publishing `pkg` (in `~`) as `authority` would do, and whether the
 * authority's record proves it yet. Changes nothing. */
export function planPublish(
  pkg: string,
  authority: string,
  signal?: AbortSignal
): Promise<PublishPlan> {
  const body: PublishInput = { package: pkg, authority }
  return request<PublishPlan>("POST", rootPath("packages", "publish"), body, {
    signal,
  })
}

/** Publish `pkg` as `authority`: the answer is the plan as it ran, with its
 * first release. */
export function publish(pkg: string, authority: string): Promise<PublishPlan> {
  const body: PublishInput = { package: pkg, authority, confirm: true }
  return request<PublishPlan>("POST", rootPath("packages", "publish"), body)
}

/** Write the next release of `pkg`, a package this repository publishes.
 * Refused (409) when nothing changed since its last release. */
export function release(pkg: string): Promise<Release> {
  const body: ReleaseInput = { package: pkg }
  return request<Release>("POST", rootPath("packages", "release"), body)
}

/** Every release of every package this repository publishes. */
export async function releases(signal?: AbortSignal): Promise<Release[]> {
  const page = await request<Releases>(
    "GET",
    rootPath("packages", "releases"),
    undefined,
    { signal }
  )
  return page.items ?? []
}

export const releasesQueryOptions = queryOptions({
  queryKey: ["packages", "releases"],
  queryFn: ({ signal }) => releases(signal),
  staleTime: 5 * 60_000,
})

/** The releases of one package, oldest first, from the list. */
export function releasesOf(
  all: readonly Release[] | undefined,
  pkg: string
): Release[] {
  return (all ?? []).filter((r) => r.package === pkg)
}
