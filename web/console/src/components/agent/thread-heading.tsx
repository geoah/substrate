/** A conversation's heading, and its rename. A rename is an ordinary patch of
 * the thread's `subject` under the console's actor; the server never writes a
 * generated title over it (decision record 0170). Enter saves, Escape
 * cancels. A new chat has no thread yet, so it has nothing to rename. */

import { useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { PencilIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { CORE_AUTHORITY, LLM_PACKAGE_NAME } from "@/lib/api/http"
import { patchRecord } from "@/lib/api/records"

export function ThreadHeading({
  title,
  subject,
  thread,
}: {
  /** The heading as displayed, which may be cut short. */
  title: string
  /** The thread's full `subject`, what a rename starts from; the title when
   * the thread has none. */
  subject?: string
  /** The thread's id; empty for a new chat. */
  thread: string
}) {
  const full = subject ?? title
  const client = useQueryClient()
  const [draft, setDraft] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function save() {
    const next = (draft ?? "").trim()
    if (!next || next === full) {
      setDraft(null)
      return
    }
    setSaving(true)
    setError(null)
    try {
      await patchRecord(CORE_AUTHORITY, LLM_PACKAGE_NAME, "thread", thread, {
        properties: { subject: next },
      })
      setDraft(null)
      void client.invalidateQueries({ queryKey: ["records"] })
      void client.invalidateQueries({ queryKey: ["record"] })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  const failure = error && (
    <p role="alert" className="text-[12.5px] text-destructive">
      The chat wasn’t renamed: {error}
    </p>
  )
  if (draft === null) {
    return (
      <div className="flex min-w-0 flex-col">
        <div className="flex min-w-0 items-center gap-1">
          <h1 className="truncate font-semibold">{title}</h1>
          {thread && (
            <Button
              size="icon-xs"
              variant="ghost"
              aria-label="Rename this chat"
              onClick={() => {
                setError(null)
                setDraft(full)
              }}
            >
              <PencilIcon />
            </Button>
          )}
        </div>
        {failure}
      </div>
    )
  }
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <Input
        autoFocus
        aria-label="Chat name"
        value={draft}
        disabled={saving}
        maxLength={200}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault()
            void save()
          } else if (e.key === "Escape") {
            e.preventDefault()
            setDraft(null)
            setError(null)
          }
        }}
        onBlur={() => {
          if (!saving) setDraft(null)
        }}
      />
      {failure}
    </div>
  )
}
