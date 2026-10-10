/** The failing badge (#879): on an agent's, a tool's or a provider's page,
 * beside its alerts, a "Failing" pill with how long it has failed and which
 * triggers, while any trigger that runs the page's callables reads
 * `failing`. Nothing renders while every one is healthy. */

import { useQuery } from "@tanstack/react-query"

import { Pill } from "@/components/identity/pill"
import { triggerStatusesQueryOptions } from "@/lib/api/sync"
import { relativeTime } from "@/lib/format"
import { failingTriggersOf } from "@/lib/health"
import { cn } from "@/lib/utils"

export function FailingBadge({
  callables,
  className,
}: {
  /** The identities of the functions and agents this page stands for. */
  callables: readonly string[]
  className?: string
}) {
  const statuses = useQuery(triggerStatusesQueryOptions)
  const failing = failingTriggersOf(statuses.data ?? [], callables)
  if (!failing) return null
  const many = failing.triggers.length > 1
  return (
    <div
      data-slot="failing-badge"
      className={cn("mt-6 flex flex-col gap-1.5 text-[13px]", className)}
    >
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
        <Pill tone="bad">Failing</Pill>
        <dl className="flex min-w-0 flex-wrap gap-x-4 gap-y-1">
          {failing.since && (
            <div className="flex gap-1">
              <dt className="text-faint">Since</dt>
              <dd title={failing.since}>{relativeTime(failing.since)}</dd>
            </div>
          )}
          <div className="flex min-w-0 gap-1">
            <dt className="text-faint">{many ? "Triggers" : "Trigger"}</dt>
            <dd className="font-mono break-all">
              {failing.triggers.join(", ")}
            </dd>
          </div>
        </dl>
      </div>
      <p className="text-muted-foreground">
        Every delivery since then has failed. The next one that succeeds clears
        this.
      </p>
    </div>
  )
}
