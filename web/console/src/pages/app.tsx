/** `/apps/$id`, `/apps/$id/$screen` and `/apps/$id/$screen/$record`: an app's
 * screen under the host chrome. `/apps/$id` alone lands on the first screen
 * once the record loads. ONE component serves the three routes: they are
 * siblings, and a different component per route would remount the screen on
 * every sheet open and close. The runtime is one lazy chunk. */

import { lazy, Suspense } from "react"
import { useParams } from "@tanstack/react-router"

import { Skeleton } from "@/components/ui/skeleton"

const AppScreen = lazy(() =>
  import("@/components/apps/runtime").then((m) => ({ default: m.AppScreen }))
)

const fallback = <Skeleton className="m-6 h-6 w-48" />

export function AppPage() {
  const { id = "", screen, record } = useParams({ strict: false })
  return (
    <Suspense fallback={fallback}>
      <AppScreen id={id} screen={screen} segment={record} />
    </Suspense>
  )
}

export const AppScreenPage = AppPage
export const AppScreenRecordPage = AppPage
