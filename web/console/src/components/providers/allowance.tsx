/** The operator's daily allowance as the Providers page shows it: one bar on
 * the AI models header (used of the day's limit, split by slot, and when it
 * resets in the reader's own time), and one line on the Tools header. It is
 * never at the top of the page or over the cards that bring data in: it
 * belongs to what it pays for. */

import { useNow } from "@/hooks/use-now"
import type { OperatorAllowance } from "@/lib/api/types"
import {
  centsWords,
  percentOf,
  resetWords,
  spendSplit,
  type OperatorWords,
} from "@/lib/operator-defaults"

/** One series token per slot, in a fixed order (design guide, Chart
 * series): the split's legend names each one. */
const SLOT_COLOUR: Record<string, string> = {
  llm: "var(--series-1)",
  embeddings: "var(--series-2)",
  function: "var(--series-3)",
  oauth: "var(--series-4)",
}

export function AllowanceBar({
  allowance,
  words,
}: {
  allowance: OperatorAllowance
  words: OperatorWords
}) {
  const now = useNow(60_000)
  const split = spendSplit(allowance)
  // Spend the split does not name (a slot that spent and was not listed)
  // still fills the bar, in the quiet colour.
  const named = split.reduce((n, s) => n + s.cents, 0)
  const rest = Math.max(0, allowance.usedCents - named)
  return (
    <div
      data-slot="allowance"
      className="mb-3 rounded-[10px] border bg-panel px-3.5 py-3"
    >
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
        <div className="text-[13px]">
          <span className="font-medium">
            Today’s allowance from {words.name}:
          </span>{" "}
          <span className="tabular-nums">
            {centsWords(allowance.usedCents)} of{" "}
            {centsWords(allowance.centsPerDay)} used
          </span>
        </div>
        <div className="text-[12px] text-faint">
          {resetWords(allowance.resetsAt, now)}
        </div>
      </div>
      <div
        role="meter"
        aria-label={`Today’s allowance from ${words.name} used`}
        aria-valuemin={0}
        aria-valuemax={allowance.centsPerDay}
        aria-valuenow={Math.min(allowance.usedCents, allowance.centsPerDay)}
        className="mt-2 flex h-2 w-full overflow-hidden rounded-full bg-hover"
      >
        {split.map((s) => (
          <div
            key={s.slot}
            data-slot-name={s.slot}
            className="h-full"
            style={{
              width: `${percentOf(s.cents, allowance.centsPerDay)}%`,
              background: SLOT_COLOUR[s.slot] ?? "var(--faint)",
            }}
          />
        ))}
        {rest > 0 && (
          <div
            className="h-full"
            style={{
              width: `${percentOf(rest, allowance.centsPerDay)}%`,
              background: "var(--faint)",
            }}
          />
        )}
      </div>
      {split.length > 0 && (
        <ul className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[12px] text-muted-foreground">
          {split.map((s) => (
            <li key={s.slot} className="flex items-center gap-1.5">
              <span
                aria-hidden
                className="size-2 rounded-full"
                style={{ background: SLOT_COLOUR[s.slot] ?? "var(--faint)" }}
              />
              <span>
                {s.word}{" "}
                <span className="tabular-nums">{centsWords(s.cents)}</span>
              </span>
            </li>
          ))}
        </ul>
      )}
      <p className="mt-1.5 text-[12px] text-faint">
        When it runs out, anything on {words.possessive} key pauses until it
        resets. Anything on your own key keeps working and is not counted.
      </p>
    </div>
  )
}

/** The Tools header's one line, where a tool runs on the operator's key. */
export function AllowanceNote({ words }: { words: OperatorWords }) {
  return (
    <span data-slot="allowance-note">
      counts toward today’s allowance from {words.name}
    </span>
  )
}
