/** `/views/$id` and `/views/$id/$record`: one view under the host chrome.
 * The record segment is the record's full path in one segment (a bare id is
 * accepted when the view has one kind). ONE component serves both routes:
 * they are siblings, and a different component per route would remount the
 * layout on every sheet open and close, losing its scroll and its cache
 * warmth. The runtime is one lazy chunk. */

import { lazy, Suspense } from "react"
import { useParams } from "@tanstack/react-router"

import { Skeleton } from "@/components/ui/skeleton"

const ViewScreen = lazy(() =>
  import("@/components/apps/runtime").then((m) => ({
    default: m.ViewScreen,
  }))
)

const fallback = <Skeleton className="m-6 h-6 w-48" />

export function ViewPage() {
  const { id = "", record } = useParams({ strict: false })
  return (
    <Suspense fallback={fallback}>
      <ViewScreen id={id} segment={record} />
    </Suspense>
  )
}

export const ViewRecordPage = ViewPage
