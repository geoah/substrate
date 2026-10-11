/** What the observations pages say where discovery failed: the streams did
 * not load, the server's reason, each problem it named (a refused read's
 * `problems`, such as `aggregate.budget.maxGroups`), and a retry. Never
 * "nothing has been measured", which would be a different fact. Where only
 * the list of every metric failed, a note beside the metric picker says so
 * (`MetricListError`) and the chosen streams stay; where only a Source or
 * About menu's own read failed, a note beside the filters does
 * (`FacetListErrors`). */

import { useId } from "react"
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
import type { Discovery, FacetValues } from "@/hooks/use-observations"
import { ApiError } from "@/lib/api/types"
import { MAX_GROUPS_CAP, type StreamFacet } from "@/lib/observations"
import { cn } from "@/lib/utils"

/** A failed read's reason in words: a refusal's message, else the error's. */
function reasonOf(error: Error | null): string {
  return error instanceof ApiError && error.status === 422
    ? `The server refused the read: ${error.message}`
    : (error?.message ?? "The read failed.")
}

function Problems({ error }: { error: Error | null }) {
  const problems = error instanceof ApiError ? error.problems : []
  if (!problems.length) return null
  return (
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
  )
}

export function DiscoveryError({ discovery }: { discovery: Discovery }) {
  const error = discovery.error
  return (
    <Empty className="mt-6">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchXIcon />
        </EmptyMedia>
        <EmptyTitle>The streams didn’t load</EmptyTitle>
        <EmptyDescription>{reasonOf(error)}</EmptyDescription>
        <Problems error={error} />
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" size="sm" onClick={() => discovery.refetch()}>
          Try again
        </Button>
      </EmptyContent>
    </Empty>
  )
}

/** What a page says beside its metric picker where the list of every metric
 * failed and the chosen metrics' streams did not: the picker offers the
 * chosen metrics alone, the server's reason and each problem it named, and
 * a retry. The chosen streams stay on the page. */
export function MetricListError({
  error,
  onRetry,
  className,
}: {
  error: Error
  onRetry: () => void
  className?: string
}) {
  const title = useId()
  return (
    <div
      role="alert"
      aria-labelledby={title}
      className={cn(
        "flex flex-col items-start gap-1 rounded-md border border-border px-3 py-2 text-[12.5px]",
        className
      )}
    >
      <p id={title} className="font-medium">
        The other metrics didn’t load
      </p>
      <p className="text-muted-foreground">
        The metric picker offers only the chosen metrics. {reasonOf(error)}
      </p>
      <Problems error={error} />
      <Button variant="outline" size="sm" className="mt-1" onClick={onRetry}>
        Try again
      </Button>
    </div>
  )
}

/** Each facet menu's words: its control's label, what it lists, and the
 * choices that shorten its list (the facet's own choice never does). */
const FACET_WORDS: Record<
  StreamFacet,
  { label: string; plural: string; narrowBy: string }
> = {
  source: {
    label: "Source",
    plural: "sources",
    narrowBy: "a metric or a thing observed",
  },
  about: {
    label: "About",
    plural: "things observed",
    narrowBy: "a metric or a source",
  },
}

/** Whether a read was refused for making more groups than one read may. */
function overGroupCap(error: Error): boolean {
  return (
    error instanceof ApiError &&
    error.status === 422 &&
    error.problems.some((p) => p.startsWith("aggregate.budget.maxGroups"))
  )
}

/** What a page says beside its Source and About filters where a menu's own
 * read failed and the streams did not: the menu offers only the values of
 * the streams shown. A read over the group cap is said in words, with how to
 * shorten the list, and has no retry, since asking again answers the same;
 * any other failure carries the server's reason and a retry. The page's
 * data stays. */
export function FacetListError({
  facet,
  error,
  onRetry,
}: {
  facet: StreamFacet
  error: Error
  onRetry: () => void
}) {
  const title = useId()
  const words = FACET_WORDS[facet]
  const capped = overGroupCap(error)
  const cap = MAX_GROUPS_CAP.toLocaleString("en-US")
  return (
    <div
      role="alert"
      aria-labelledby={title}
      className="flex flex-col items-start gap-1 rounded-md border border-border px-3 py-2 text-[12.5px]"
    >
      <p id={title} className="font-medium">
        {capped
          ? `The full ${words.label} list is unavailable`
          : `The ${words.label} list didn’t load`}
      </p>
      <p className="text-muted-foreground">
        {capped
          ? `The ${words.label} menu offers only the ${words.plural} of the streams shown: more than ${cap} ${words.plural} have a point under the other filters, and one read lists at most ${cap}. Choose ${words.narrowBy} to shorten the list.`
          : `The ${words.label} menu offers only the ${words.plural} of the streams shown. ${reasonOf(error)}`}
      </p>
      <Problems error={error} />
      {!capped && (
        <Button variant="outline" size="sm" className="mt-1" onClick={onRetry}>
          Try again
        </Button>
      )}
    </div>
  )
}

/** A `FacetListError` for each facet menu whose own read failed. A page
 * shows it only where discovery answered: where discovery failed,
 * `DiscoveryError` already says the page's data did not load. */
export function FacetListErrors({
  values,
  className,
}: {
  values: FacetValues
  className?: string
}) {
  const failed = (["source", "about"] as const).filter(
    (facet) => values.errors[facet]
  )
  if (!failed.length) return null
  return (
    <div className={cn("flex flex-col gap-2", className)}>
      {failed.map((facet) => (
        <FacetListError
          key={facet}
          facet={facet}
          error={values.errors[facet]!}
          onRetry={() => values.retry(facet)}
        />
      ))}
    </div>
  )
}
