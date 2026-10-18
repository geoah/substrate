/** The record views of a kind's record pages (decision record 0166). */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { recordsQueryOptions } from "@/lib/api/records"
import type { KindInfo } from "@/lib/api/types"
import { recordViewsParams, viewsOf, type RecordView } from "@/lib/record-view"

const NO_VIEWS: RecordView[] = []

/** The views of a kind's record pages, by id; none while they load, where
 * the read fails, and for a page with no kind. */
export function useRecordViews(kind: KindInfo | undefined): RecordView[] {
  const views = useQuery({
    ...recordsQueryOptions(recordViewsParams(kind?.identity ?? "")),
    enabled: Boolean(kind),
  })
  return useMemo(
    () => (views.data ? viewsOf(views.data.records) : NO_VIEWS),
    [views.data]
  )
}
