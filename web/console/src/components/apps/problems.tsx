/** What a view says about itself when the row is wrong. `ProblemList` stands
 * in for a layout the errors block; `ProblemStrip` sits above a layout the
 * warnings only dent, folded to one line until opened. Paths read in the
 * data voice because they are the record's own keys. */

import { useState } from "react"
import { ChevronDownIcon, TriangleAlertIcon } from "lucide-react"

import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import type { Problem } from "@/lib/apps/spec"
import { cn } from "@/lib/utils"

function ProblemLine({ problem }: { problem: Problem }) {
  return (
    <li className="flex flex-col gap-0.5 text-sm">
      <span className="data text-xs text-muted-foreground">{problem.path}</span>
      <span className="break-words">{problem.message}</span>
    </li>
  )
}

export function ProblemList({
  problems,
  title = "This view cannot render",
}: {
  problems: Problem[]
  title?: string
}) {
  return (
    <Empty className="py-10">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <TriangleAlertIcon />
        </EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        <EmptyDescription>
          Fix the record and it renders on the next save.
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <ul className="flex w-full max-w-md flex-col gap-3 text-left">
          {problems.map((p, i) => (
            <ProblemLine key={`${p.path}-${i}`} problem={p} />
          ))}
        </ul>
      </EmptyContent>
    </Empty>
  )
}

export function ProblemStrip({
  problems,
  className,
}: {
  problems: Problem[]
  className?: string
}) {
  const [open, setOpen] = useState(false)
  if (!problems.length) return null
  return (
    <div
      className={cn(
        "border-b border-warning/30 bg-warning/10 text-sm text-foreground",
        className
      )}
    >
      <button
        type="button"
        className="flex min-h-10 w-full items-center gap-2 px-4 text-left"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        <TriangleAlertIcon className="size-4 shrink-0 text-warning" />
        <span className="min-w-0 flex-1 truncate">
          {problems.length === 1
            ? problems[0].message
            : `${problems.length} warnings on this view`}
        </span>
        <ChevronDownIcon
          className={cn(
            "size-4 shrink-0 transition-transform",
            open && "rotate-180"
          )}
        />
      </button>
      {open && (
        <ul className="flex flex-col gap-2 px-4 pb-3">
          {problems.map((p, i) => (
            <ProblemLine key={`${p.path}-${i}`} problem={p} />
          ))}
        </ul>
      )}
    </div>
  )
}
