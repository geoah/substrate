import { ChevronRight } from "lucide-react"
import { atStake, isSlipping } from "../../engine/focus"
import { formatImpact } from "../../engine/format"
import type { Metric } from "../../engine/report"
import {
  focusCards,
  type HealthspanCardData,
} from "../../healthspan/healthspanCards"
import {
  cardHref,
  type HealthspanFilters,
} from "../../healthspan/healthspanFilters"
import { valueText } from "../../healthspan/healthspanMetrics"
import { Badge } from "../Badge"
import { EmptyState } from "../EmptyState"

// What to focus on: the contributors that add years or are slipping (the
// cards with those statuses), most years at stake first, at most three,
// each a link that opens its card. The numbers are a line and two badges,
// never a sentence.

const SHOWN = 3

export function FocusList({
  cards,
  filters,
}: {
  cards: HealthspanCardData[]
  filters: HealthspanFilters
}) {
  const ranked = focusCards(cards)
  // "Show all" filters the screen to these cards; hidden when it already
  // is.
  const filtered =
    filters.status.length === 2 &&
    filters.status.includes("adds") &&
    filters.status.includes("slipping")
  const more = ranked.length > SHOWN && !filtered
  return (
    <section className="mb-7" aria-labelledby="focus-title">
      <div className="mb-1.5 flex items-baseline justify-between gap-3 px-4">
        <h2
          id="focus-title"
          className="text-[13px] tracking-wide text-secondary uppercase"
        >
          What to focus on
        </h2>
        {more && (
          <a
            href="#/healthspan?status=adds,slipping"
            className="text-[15px] text-accent-text underline-offset-2 hover:underline"
          >{`Show all ${ranked.length}`}</a>
        )}
      </div>
      <div className="overflow-hidden rounded-[10px] bg-card">
        {ranked.length ? (
          <ul aria-label="What to focus on">
            {ranked.slice(0, SHOWN).map((c, i) => {
              const m = c.metric as Metric
              return (
                <li
                  key={c.id}
                  className={i ? "border-t border-separator/60" : ""}
                >
                  <a
                    href={cardHref(c, filters)}
                    className="flex min-h-11 items-center gap-3 px-4 py-2.5 hover:bg-fill active:bg-selected"
                  >
                    <span className="flex min-w-0 flex-1 flex-col gap-1">
                      <span className="text-[17px] break-words">{c.label}</span>
                      <span className="text-[13px] text-secondary tabular-nums">
                        {`30-day ${valueText(m.thirtyDay, m.format)}, 6-month ${valueText(m.sixMonth, m.format)}`}
                      </span>
                      <span className="flex flex-wrap gap-1.5">
                        <Badge
                          label={`${formatImpact(atStake(m))} years at stake`}
                          tone="warning"
                        />
                        {isSlipping(m) && (
                          <Badge label="Slipping" tone="warning" />
                        )}
                      </span>
                    </span>
                    <ChevronRight
                      className="size-4 shrink-0 text-tertiary"
                      aria-hidden
                    />
                  </a>
                </li>
              )
            })}
          </ul>
        ) : (
          <EmptyState
            className="px-4"
            title="Nothing adds years or is slipping."
            variant="compact"
          />
        )}
      </div>
    </section>
  )
}
