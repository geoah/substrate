/** The record's prose, read as the page's body under a divider and edited in
 * place as a Markdown document (components/markdown): a click opens the
 * editor with the same footer as the list editor (Save, Cancel, and the keys
 * that do the same), ⌘Enter or leaving the editor saves and says so, Esc
 * cancels. Reader and editor span the document column, as the property sheet
 * does, and sit on the same box so opening the editor does not move the text.
 * A click on a link in the reader follows it instead of opening the editor.
 * The same single-property PATCH as every other in-place edit. */

import { Suspense, lazy, useEffect, useRef, useState } from "react"
import { CheckIcon } from "lucide-react"

import { useFocusReturn } from "@/components/property-sheet/focus-return"
import {
  useRecordPatch,
  writeError,
} from "@/components/property-sheet/use-record-patch"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import type { SubstrateRecord } from "@/lib/api/types"
import type { PropSpec } from "@/lib/record-schema"
import { cn } from "@/lib/utils"
import { lowerFirst } from "@/lib/kind-names"

/** The editor and its Markdown parser load with the first body a page
 * shows, never with the rest of the console (the YAML editor's discipline).
 * Until then the body reads as plain paragraphs. */
const MarkdownEditor = lazy(() =>
  import("@/components/markdown/markdown-editor").then((m) => ({
    default: m.MarkdownEditor,
  }))
)

function PlainText({ text }: { text: string }) {
  return text.split(/\n{2,}/).map((p, i) => (
    <p key={i} className="mb-[0.8em] whitespace-pre-wrap last:mb-0">
      {p}
    </p>
  ))
}

/** How long "Saved" stays after a save the reader did not click for. */
const SAVED_FOR = 2000

export function RecordBody({
  record,
  spec,
  readOnly,
  moved = false,
}: {
  record: SubstrateRecord
  spec: PropSpec
  readOnly: boolean
  /** It just changed under the reader: marked briefly. */
  moved?: boolean
}) {
  const stored = record.properties[spec.name]
  const text = typeof stored === "string" ? stored : ""
  // The version the edit began from, held while it is open (useEditBase).
  const [base, setBase] = useState<number>()
  const editing = base !== undefined
  // A ref, not state: a ⌘Enter in the same tick as the last keystroke must
  // save that keystroke.
  const draft = useRef("")
  const [error, setError] = useState<string>()
  const [saved, setSaved] = useState(false)
  const patch = useRecordPatch(record, base)
  const busy = useRef(false)
  const editor = useRef<HTMLDivElement>(null)
  const reader = useRef<HTMLDivElement>(null)
  useFocusReturn(editing, reader)

  useEffect(() => {
    if (!saved) return undefined
    const timer = setTimeout(() => setSaved(false), SAVED_FOR)
    return () => clearTimeout(timer)
  }, [saved])

  if (!text && readOnly) return null

  async function save() {
    if (busy.current) return
    busy.current = true
    try {
      const next = draft.current
      const changed = next !== text
      if (changed) {
        await patch.mutateAsync({ [spec.name]: next.trim() ? next : null })
      }
      setError(undefined)
      setBase(undefined)
      setSaved(changed)
    } catch (e) {
      setError(writeError(e))
    } finally {
      busy.current = false
    }
  }

  function cancel() {
    busy.current = true
    setBase(undefined)
    setError(undefined)
    setTimeout(() => (busy.current = false))
  }

  return (
    <section data-slot="record-body" aria-label={spec.label}>
      <div className="my-[22px] h-px bg-border" />
      {editing ? (
        <div
          ref={editor}
          className="flex flex-col gap-2"
          onBlur={(e) => {
            // Moving to the footer's buttons is not leaving the editor.
            if (editor.current?.contains(e.relatedTarget as Node | null)) return
            void save()
          }}
        >
          <div
            // ⌘Enter saves before the editor reads it as a line break, and
            // an Esc the editor spent closing a menu cancels nothing.
            onKeyDownCapture={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                e.preventDefault()
                e.stopPropagation()
                void save()
              }
            }}
            onKeyDown={(e) => {
              if (e.key === "Escape" && !e.nativeEvent.defaultPrevented) {
                e.preventDefault()
                cancel()
              }
            }}
            className="-mx-2 block min-h-24 w-[calc(100%+1rem)] rounded-md border border-primary bg-background px-2 py-1 ring-3 ring-primary-soft"
          >
            <Suspense fallback={<PlainText text={text} />}>
              <MarkdownEditor
                value={text}
                label={spec.label}
                editable
                autoFocus
                disabled={patch.isPending}
                onChange={(markdown) => (draft.current = markdown)}
                className="min-h-[5.5rem] [&_.ProseMirror]:min-h-[5.5rem]"
              />
            </Suspense>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              disabled={patch.isPending}
              onClick={() => void save()}
            >
              {patch.isPending && <Spinner className="size-3.5" />}
              Save
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={patch.isPending}
              onMouseDown={(e) => e.preventDefault()}
              onClick={cancel}
            >
              Cancel
            </Button>
            <span className="ml-auto text-xs text-faint max-sm:hidden">
              / for blocks · @ or ⌘K links a record · ⌘Enter saves · Esc cancels
            </span>
          </div>
        </div>
      ) : (
        <div
          ref={reader}
          role={readOnly ? undefined : "button"}
          tabIndex={readOnly ? undefined : 0}
          aria-label={readOnly ? undefined : `Edit ${spec.label}`}
          onClick={
            readOnly
              ? undefined
              : (e) => {
                  if ((e.target as Element).closest("a, input")) return
                  draft.current = text
                  setBase(record.version)
                }
          }
          onKeyDown={(e) => {
            if (
              !readOnly &&
              e.key === "Enter" &&
              e.target === e.currentTarget
            ) {
              e.preventDefault()
              draft.current = text
              setBase(record.version)
            }
          }}
          className={cn(
            "-mx-2 rounded-md px-2 py-1 leading-[1.65] transition-colors duration-700 outline-none",
            moved && "bg-primary-soft",
            !readOnly &&
              "cursor-text hover:bg-hover focus-visible:bg-hover focus-visible:ring-2 focus-visible:ring-ring"
          )}
        >
          {text ? (
            <Suspense fallback={<PlainText text={text} />}>
              <MarkdownEditor
                value={text}
                label={spec.label}
                editable={false}
              />
            </Suspense>
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
      <p
        role="status"
        className={cn(
          "flex items-center gap-1 text-xs text-faint",
          saved && "mt-1"
        )}
      >
        {saved && (
          <>
            <CheckIcon aria-hidden className="size-3 text-ok" />
            Saved
          </>
        )}
      </p>
    </section>
  )
}
