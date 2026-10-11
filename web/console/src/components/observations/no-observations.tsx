/** What the observations pages say where no kind binds the `observed`
 * trait: nothing here records a measurement yet, and where one comes from. */

import { Link } from "@tanstack/react-router"
import { ActivityIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"

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
          from a provider that writes them. Add one from All data or Providers.
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button
          variant="outline"
          size="sm"
          render={<Link to="/data" />}
          nativeButton={false}
        >
          All data
        </Button>
      </EmptyContent>
    </Empty>
  )
}
