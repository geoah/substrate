/** A record view's request block: one button that asks for something by
 * stamping one of the record's own datetime properties with now, which a
 * connector watching the record acts on (unlink this account, say). The
 * view words the button and its confirmation; the property must be a
 * datetime the owner writes, or the block says so instead. Once stamped,
 * the block says when it was asked. */

import { useState } from "react"

import { Notice, Part, Quiet, type BlockContext } from "./parts"
import {
  useRecordPatch,
  writeError,
} from "@/components/property-sheet/use-record-patch"
import { ago } from "@/components/property-sheet/dates"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import { serverNow } from "@/lib/api/http"
import { ownerWritable, propSpecs } from "@/lib/record-schema"
import type { RequestBlock } from "@/lib/record-view"

export function RequestBlockView({
  block,
  ctx,
}: {
  block: RequestBlock
  ctx: BlockContext
}) {
  const spec = ctx.kind
    ? propSpecs(ctx.kind).find((s) => s.name === block.property)
    : undefined
  if (!spec || spec.kind !== "datetime" || !ownerWritable(spec)) {
    return (
      <Notice>
        This request block stamps {block.property}, which is no date and time
        you write on this {ctx.noun}.
      </Notice>
    )
  }
  return <RequestButton block={block} ctx={ctx} />
}

function RequestButton({
  block,
  ctx,
}: {
  block: RequestBlock
  ctx: BlockContext
}) {
  const [confirming, setConfirming] = useState(false)
  const patch = useRecordPatch(ctx.record)
  const raw = ctx.record.properties?.[block.property]
  const asked = typeof raw === "string" && raw ? raw : undefined
  return (
    <Part>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <Button
          variant="outline"
          size="sm"
          disabled={patch.isPending}
          onClick={() => {
            patch.reset()
            setConfirming(true)
          }}
        >
          {block.label}
        </Button>
        {asked && (
          <Quiet>
            <span data-slot="request-asked" title={asked}>
              Asked {ago(asked)}
            </span>
          </Quiet>
        )}
      </div>
      {confirming && (
        <ConfirmDialog
          title={`${block.label}?`}
          consequence={block.confirm}
          confirm={block.label}
          pending={patch.isPending}
          error={patch.error ? writeError(patch.error) : undefined}
          onConfirm={() =>
            patch.mutate(
              { [block.property]: serverNow().toISOString() },
              { onSuccess: () => setConfirming(false) }
            )
          }
          onClose={() => setConfirming(false)}
        />
      )}
    </Part>
  )
}
