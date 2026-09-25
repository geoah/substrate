/** The record's prose, read as the page's body under a divider and edited in
 * place: a click opens a textarea, leaving it or ⌘Enter saves, Esc cancels.
 * Reader and editor span the document column, as the property sheet does,
 * and sit on the same box so opening the editor does not move the text.
 * The same single-property PATCH as every other in-place edit. */

import { useRef, useState } from "react"

import {
  useRecordPatch,
  writeError,
} from "@/components/property-sheet/use-record-patch"
import type { SubstrateRecord } from "@/lib/api/types"
import type { PropSpec } from "@/lib/record-schema"
import { cn } from "@/lib/utils"
import { lowerFirst } from "@/lib/kind-names"

export function RecordBody({
  record,
  spec,
  readOnly,
}: {
  record: SubstrateRecord
  spec: PropSpec
  readOnly: boolean
}) {
  const stored = record.properties[spec.name]
  const text = typeof stored === "string" ? stored : ""
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState("")
  const [error, setError] = useState<string>()
  const patch = useRecordPatch(record)
  const busy = useRef(false)

  if (!text && readOnly) return null

  async function save() {
    if (busy.current) return
    busy.current = true
    try {
      if (draft !== text) {
        await patch.mutateAsync({ [spec.name]: draft.trim() ? draft : null })
      }
      setError(undefined)
      setEditing(false)
    } catch (e) {
      setError(writeError(e))
    } finally {
      busy.current = false
    }
  }

  return (
    <section data-slot="record-body" aria-label={spec.label}>
      <div className="my-[22px] h-px bg-border" />
      {editing ? (
        <textarea
          autoFocus
          aria-label={spec.label}
          value={draft}
          disabled={patch.isPending}
          rows={Math.min(18, Math.max(4, draft.split("\n").length + 1))}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={() => void save()}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.preventDefault()
              busy.current = true
              setEditing(false)
              setError(undefined)
              setTimeout(() => (busy.current = false))
            } else if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault()
              void save()
            }
          }}
          className="-mx-2 block field-sizing-content min-h-24 w-[calc(100%+1rem)] resize-y rounded-md border border-primary bg-background px-2 py-1 leading-[1.65] ring-3 ring-primary-soft outline-none"
        />
      ) : (
        <div
          role={readOnly ? undefined : "button"}
          tabIndex={readOnly ? undefined : 0}
          aria-label={readOnly ? undefined : `Edit ${spec.label}`}
          onClick={
            readOnly
              ? undefined
              : () => {
                  setDraft(text)
                  setEditing(true)
                }
          }
          onKeyDown={(e) => {
            if (
              !readOnly &&
              e.key === "Enter" &&
              e.target === e.currentTarget
            ) {
              e.preventDefault()
              setDraft(text)
              setEditing(true)
            }
          }}
          className={cn(
            "-mx-2 rounded-md px-2 py-1 leading-[1.65] outline-none",
            !readOnly && "cursor-text hover:bg-hover focus-visible:bg-hover"
          )}
        >
          {text ? (
            text.split(/\n{2,}/).map((p, i) => (
              <p key={i} className="mb-[0.8em] whitespace-pre-wrap last:mb-0">
                {p}
              </p>
            ))
          ) : (
            <p className="text-faint">Add {lowerFirst(spec.label)}…</p>
          )}
        </div>
      )}
      {error && (
        <p role="alert" className="mt-1.5 text-[12.5px] text-destructive">
          {error}
        </p>
      )}
    </section>
  )
}
