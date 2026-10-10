/** The reads behind the alerts panel and Home's count: open `core/alert`
 * records through the one records route. Each key names the alert kind, so a
 * write to an alert refreshes them (records.ts recordWriteReaches). */

import { queryOptions } from "@tanstack/react-query"

import { ALERT_KIND } from "@/lib/alerts"

import { request } from "./http"
import { listPath } from "./records"
import type { Page } from "./types"

const OPEN = { state: { eq: "open" } }

/** The most open alerts one page lists: an alert is one per problem, so a
 * page about one record rarely holds more than a handful. */
const ALERTS_PAGE = 50

/** The open alerts about any of the records at `refs`, newest first: the
 * `referencing` arm on the alert's `about` reference. Disabled for no refs. */
export function openAlertsAboutQueryOptions(refs: readonly string[]) {
  const sorted = [...refs].sort()
  return queryOptions({
    queryKey: ["records", [ALERT_KIND], "open-about", sorted],
    queryFn: async ({ signal }) => {
      const page = await request<Page>(
        "GET",
        listPath({
          kinds: [ALERT_KIND],
          first: ALERTS_PAGE,
          orderBy: "updatedAt:desc",
          filter: {
            properties: OPEN,
            referencing: { refs: sorted, property: "about" },
          },
        }),
        undefined,
        { signal }
      )
      return page.records ?? []
    },
    enabled: sorted.length > 0,
    staleTime: 15_000,
  })
}

/** Every open alert, newest first, with the size of the whole open set: what
 * Home counts and links from. */
export const openAlertsQueryOptions = queryOptions({
  queryKey: ["records", [ALERT_KIND], "open"],
  queryFn: async ({ signal }) => {
    const page = await request<Page>(
      "GET",
      listPath({
        kinds: [ALERT_KIND],
        first: ALERTS_PAGE,
        orderBy: "updatedAt:desc",
        filter: { properties: OPEN },
        count: true,
      }),
      undefined,
      { signal }
    )
    const records = page.records ?? []
    return { records, count: page.count ?? records.length }
  },
  staleTime: 15_000,
})
