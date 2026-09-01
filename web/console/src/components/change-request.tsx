/** The change-request voice, shared by every surface that shows one: the op as
 * a badge and the target the change would land on. It lives here rather than on
 * a page because the overview's queue and the request's own detail must say the
 * same thing the same way. */

import { RecordPeek } from "@/components/record-peek"
import { Badge } from "@/components/ui/badge"
import type { KindInfo } from "@/lib/api/types"
import type { ChangeOp, ChangeTargetRef } from "@/lib/changerequests"
import { splitKind } from "@/lib/definition"

/** The op, said in one word. `delete` wears the destructive variant because it
 * is the one op that takes something away, and a reviewer must never have to
 * read the label to notice. An op the console cannot read says so: the accept
 * would refuse it too. */
export function OpBadge({ op }: { op?: ChangeOp }) {
  if (!op) {
    return (
      <Badge variant="destructive" className="font-normal">
        unknown op
      </Badge>
    )
  }
  return (
    <Badge
      variant={op === "delete" ? "destructive" : "outline"}
      className="data font-normal"
    >
      {op}
    </Badge>
  )
}

/** The record a change would land on: a peek and a link where the record
 * EXISTS (patch, delete), and the plain id where it does not yet (a create
 * names its target by targetKind/targetId, so there is nothing to open). */
export function ChangeTarget({
  target,
  types,
}: {
  target?: ChangeTargetRef
  types: KindInfo[]
}) {
  if (!target) {
    return <span className="text-muted-foreground">no target</span>
  }
  return (
    <span className="inline-flex min-w-0 items-baseline gap-1.5">
      <span className="min-w-0 truncate font-medium">
        {target.via === "reference" ? (
          <RecordPeek
            target={{ id: target.id, kind: target.kind }}
            types={types}
          />
        ) : (
          <span className="data font-normal" title={target.id}>
            {target.id}
          </span>
        )}
      </span>
      <span className="data text-xs text-muted-foreground">
        {splitKind(target.kind).name}
      </span>
    </span>
  )
}
