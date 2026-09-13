/** `/apps`: the launcher. The runtime behind it is one lazy chunk. */

import { lazy, Suspense } from "react"

import { Skeleton } from "@/components/ui/skeleton"

const Launcher = lazy(() =>
  import("@/components/apps/runtime").then((m) => ({ default: m.Launcher }))
)

export function AppsPage() {
  return (
    <Suspense fallback={<Skeleton className="m-6 h-6 w-32" />}>
      <Launcher />
    </Suspense>
  )
}
