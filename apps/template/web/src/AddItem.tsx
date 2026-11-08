import { BarButton, ProblemBox, useKindWrites } from "@substrate/react"
import { useState, type FormEvent } from "react"
import { Item } from "./kinds"

/** The field that adds an item, at the bottom of the screen. What it holds
 * is cleared only once the outbox keeps the item, so nothing typed is lost
 * to a write the browser could not keep; the words for one it could not
 * keep stand beside the field, with Retry. */
export function AddItem() {
  const writes = useKindWrites(Item)
  const [draft, setDraft] = useState("")
  const [notKept, setNotKept] = useState<string | null>(null)

  const add = async (e?: FormEvent) => {
    e?.preventDefault()
    if (!draft.trim()) return
    const written = await writes.create(
      { name: draft },
      { onNotKept: setNotKept }
    )
    if (!written.kept) return
    setDraft("")
    setNotKept(null)
  }

  return (
    <form
      onSubmit={add}
      className="fixed inset-x-0 bottom-0 z-20 border-t border-separator bg-grouped/90 px-4 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] backdrop-blur-xl"
    >
      <div className="mx-auto max-w-2xl space-y-2">
        {notKept && (
          <div className="overflow-hidden rounded-[10px] bg-card">
            <ProblemBox
              role="alert"
              title="A change was not kept"
              message={notKept}
              action={{ title: "Retry", onClick: () => void add() }}
            />
          </div>
        )}
        <div className="flex gap-2">
          <label className="sr-only" htmlFor="new-item">
            Add an item
          </label>
          <input
            id="new-item"
            className="h-11 min-w-0 flex-1 rounded-[10px] bg-card px-4 text-[17px] outline-none placeholder:text-tertiary focus-visible:ring-2 focus-visible:ring-accent"
            placeholder="Add an item"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            enterKeyHint="done"
          />
          <BarButton type="submit" prominent disabled={!draft.trim()}>
            Add
          </BarButton>
        </div>
      </div>
    </form>
  )
}
