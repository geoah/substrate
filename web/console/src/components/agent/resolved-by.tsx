/** Whose hand resolved a card, as a labelled fact on it: "Answered by You",
 * "Declined by Ada", "Applied by Someone else" (decision 0327). It draws
 * nothing while the card waits, and nothing for a card resolved before the
 * link was kept or decided by a policy rule: the card then says what
 * happened and not who. */

import { resolverWords, useResolver } from "@/hooks/use-resolver"
import type { ResolvedKind } from "@/lib/api/resolved-by"
import type { SubstrateRecord } from "@/lib/api/types"

export function ResolvedBy({
  label,
  kind,
  record,
}: {
  /** What the person did, ending in "by": "Answered by". */
  label: string
  kind: ResolvedKind
  record: SubstrateRecord
}) {
  const resolver = useResolver(kind, record)
  if (!resolver) return null
  return (
    <p className="text-[12.5px]" data-slot="resolved-by">
      <span className="text-muted-foreground">{label}</span>{" "}
      <span className="font-medium [overflow-wrap:anywhere]">
        {resolverWords(resolver)}
      </span>
    </p>
  )
}
