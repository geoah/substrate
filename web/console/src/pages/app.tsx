/** `/apps/$id` and `/apps/$id/$`: one app under the host chrome. The splat is
 * the app's OWN path (`/apps/projects/website` is `/website` to the app),
 * surfaced to it as its route and pushed by its `navigate`. ONE component
 * serves both routes: they are siblings, and a different component per route
 * would remount the guest on every in-app navigation. The runtime is one
 * lazy chunk. */

import { lazy, Suspense } from "react"
import { useParams } from "@tanstack/react-router"

import { Skeleton } from "@/components/ui/skeleton"

const AppScreen = lazy(() =>
  import("@/components/apps/runtime").then((m) => ({ default: m.AppScreen }))
)

const fallback = <Skeleton className="m-6 h-6 w-48" />

export function AppPage() {
  const { id = "", _splat } = useParams({ strict: false })
  return (
    <Suspense fallback={fallback}>
      <AppScreen id={id} splat={_splat ?? ""} />
    </Suspense>
  )
}
