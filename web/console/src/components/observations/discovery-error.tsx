/** What the observations pages say where discovery failed: the streams did
 * not load, the server's reason, each problem it named (a refused read's
 * `problems`, such as `aggregate.budget.maxGroups`), and a retry. Never
 * "nothing has been measured", which would be a different fact. */

import { SearchXIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import type { Discovery } from "@/hooks/use-observations"
import { ApiError } from "@/lib/api/types"

export function DiscoveryError({ discovery }: { discovery: Discovery }) {
  const error = discovery.error
  const problems = error instanceof ApiError ? error.problems : []
  return (
    <Empty className="mt-6">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchXIcon />
        </EmptyMedia>
        <EmptyTitle>The streams didn’t load</EmptyTitle>
        <EmptyDescription>
          {error instanceof ApiError && error.status === 422
            ? `The server refused the read: ${error.message}`
            : (error?.message ?? "The read failed.")}
        </EmptyDescription>
        {problems.length > 0 && (
          <ul
            aria-label="Problems"
            className="mt-2 text-left text-[12.5px] text-muted-foreground"
          >
            {problems.map((p) => (
              <li key={p} className="font-mono break-words">
                {p}
              </li>
            ))}
          </ul>
        )}
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" size="sm" onClick={() => discovery.refetch()}>
          Try again
        </Button>
      </EmptyContent>
    </Empty>
  )
}
