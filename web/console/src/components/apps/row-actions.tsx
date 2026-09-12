/** A row's verbs: the first `row`-placed action the row admits as a trailing
 * 44 px button, every further one in a menu behind a 44 px overflow button.
 * A transition the machine does not admit from the row's state, a `when`
 * that does not hold and a link with nothing to open are not offered at
 * all, so a `proposed` task shows no Done. No swipe carries meaning: it
 * fights the OS back gesture. */

import { useState } from "react"
import { EllipsisVerticalIcon } from "lucide-react"

import {
  ActionButton,
  ActionIcon,
  ActionRunner,
} from "@/components/apps/action-button"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import type { SubstrateRecord } from "@/lib/api/types"
import { rowActionsFor } from "@/lib/apps/actions"
import type { ActionHost, ActionSpec } from "@/lib/apps/spec"
import { cn } from "@/lib/utils"

export function RowActions({
  host,
  record,
  className,
}: {
  host: ActionHost
  record: SubstrateRecord
  className?: string
}) {
  const [first, ...rest] = rowActionsFor(host, record)
  const [active, setActive] = useState<ActionSpec | null>(null)
  if (!first) return null
  return (
    <div className={cn("flex shrink-0 items-center gap-1", className)}>
      <ActionButton host={host} action={first} record={record} compact />
      {rest.length > 0 && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                className="size-11"
                aria-label="More actions"
              />
            }
          >
            <EllipsisVerticalIcon className="size-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {rest.map((action) => (
              <DropdownMenuItem
                key={action.name}
                className="min-h-10"
                title={action.description}
                onClick={() => setActive(action)}
              >
                <ActionIcon action={action} />
                {action.label}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
      {active && (
        <ActionRunner
          host={host}
          action={active}
          record={record}
          onSettled={() => setActive(null)}
        />
      )}
    </div>
  )
}
