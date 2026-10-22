/** A record view's breakdown block: bars from the record's own properties,
 * one per key of a keyed map or one per listed property, each with its
 * share of a total. It reads nothing past the record. */

import { Bars, Part, Quiet, type BlockContext } from "./parts"
import { listWords } from "@/lib/tools"
import { breakdownItems, type BreakdownBlock } from "@/lib/record-view"
import { useUnits } from "@/hooks/use-units"

/** The colours a breakdown has before the quiet grey. */
const SERIES_COLORS = 8

export function BreakdownBlockView({
  block,
  ctx,
}: {
  block: BreakdownBlock
  ctx: BlockContext
}) {
  const units = useUnits()
  const { items, skipped } = breakdownItems(units, block, ctx.record)
  const label = block.label ?? "Breakdown"
  return (
    <Part title={block.label}>
      {items.length ? (
        <Bars
          label={label}
          items={items.map((item, i) => ({
            key: item.key,
            name: item.name,
            amount: item.amount,
            text:
              item.share === undefined
                ? item.text
                : `${item.text}, ${Math.round(item.share * 100)}%`,
            color:
              i < SERIES_COLORS ? `var(--series-${i + 1})` : "var(--faint)",
          }))}
        />
      ) : (
        <Quiet>None of these is set on this {ctx.noun}.</Quiet>
      )}
      {skipped.length > 0 && (
        <Quiet>
          Left out, as no number, duration or quantity:{" "}
          {listWords([...skipped])}.
        </Quiet>
      )}
    </Part>
  )
}
