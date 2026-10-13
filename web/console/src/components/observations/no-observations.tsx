/** What the observations pages say where nothing has been measured: no kind
 * binds the `observed` trait yet, or none of its records holds a value, and
 * the two places a measurement comes from, a sample that records them
 * (All data's "Add a collection", on its samples) and a provider. */

import { Link } from "@tanstack/react-router"
import { ActivityIcon, LayersIcon, PlugIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { cn } from "@/lib/utils"

/** The two ways to start measuring, as buttons. */
export function MeasurementSources({ className }: { className?: string }) {
  return (
    <div className={cn("flex flex-wrap gap-2", className)}>
      <Button
        variant="outline"
        size="sm"
        render={<Link to="/data" search={{ add: "sample" } as never} />}
        nativeButton={false}
      >
        <LayersIcon />
        Add a sample
      </Button>
      <Button
        variant="outline"
        size="sm"
        render={<Link to="/providers" />}
        nativeButton={false}
      >
        <PlugIcon />
        Providers
      </Button>
    </div>
  )
}

export function NoObservations() {
  return (
    <Empty className="mt-8">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <ActivityIcon />
        </EmptyMedia>
        <EmptyTitle>Nothing here records measurements yet</EmptyTitle>
        <EmptyDescription>
          Measurements come from a sample that records them, such as health, or
          from a provider that writes them.
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <MeasurementSources />
      </EmptyContent>
    </Empty>
  )
}
