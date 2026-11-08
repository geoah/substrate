import {
  AccessBanner,
  BarButton,
  LargeTitle,
  LinkStatus,
  NavBar,
  ProblemBox,
  Section,
  Spinner,
  useKindWrites,
  useLive,
  useParkedWrites,
  useRecords,
} from "@substrate/react"
import { ChevronRight, Settings } from "lucide-react"
import { useMemo, useState, type FormEvent } from "react"
import { Item, itemTitle } from "./kinds"
import { WriteState } from "./WriteState"

/** The items, sorted by name, and the field that adds one. What the field
 * holds is cleared only once the outbox keeps the item it adds, so nothing
 * typed is lost to a write the browser could not keep. */
export function ItemsScreen(props: {
  onOpen: (id: string) => void
  onSettings: () => void
}) {
  const live = useLive()
  const records = useRecords(Item)
  const parked = useParkedWrites()
  const writes = useKindWrites(Item)
  const [draft, setDraft] = useState("")
  const [notKept, setNotKept] = useState<string | null>(null)
  const items = useMemo(
    () =>
      [...records].sort(
        (a, b) =>
          itemTitle(a).localeCompare(itemTitle(b)) || a.id.localeCompare(b.id)
      ),
    [records]
  )

  const add = async (e?: FormEvent) => {
    e?.preventDefault()
    if (!draft.trim()) return
    const id = await writes.create({ name: draft }, { onNotKept: setNotKept })
    if (!id) return
    setDraft("")
    setNotKept(null)
  }

  return (
    <div className="mx-auto w-full max-w-2xl pb-40">
      <NavBar
        actions={
          <BarButton aria-label="Settings" onClick={props.onSettings}>
            <Settings className="size-5" aria-hidden />
          </BarButton>
        }
      />
      <LargeTitle>Items</LargeTitle>
      <LinkStatus status={live.status} words={live.words} />
      <AccessBanner className="mx-4 mb-4" />
      {parked.length > 0 && (
        <div className="mx-4 mb-4 overflow-hidden rounded-[10px] bg-card">
          <ProblemBox
            tone="caution"
            title={
              parked.length === 1
                ? "1 change needs you"
                : `${parked.length} changes need you`
            }
            message="Your substrate refused it. Retry or discard it in Settings."
            action={{ title: "Open Settings", onClick: props.onSettings }}
          />
        </div>
      )}
      <div className="px-4">
        {items.length > 0 ? (
          <Section>
            {items.map((item) => (
              <button
                key={item.id}
                type="button"
                onClick={() => props.onOpen(item.id)}
                className="flex min-h-11 w-full items-center gap-3 px-4 py-2.5 text-left active:bg-selected"
              >
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="text-[17px] break-words">
                    {itemTitle(item)}
                  </span>
                  <WriteState id={item.id} />
                </span>
                <ChevronRight
                  className="size-4 shrink-0 text-tertiary"
                  aria-hidden
                />
              </button>
            ))}
          </Section>
        ) : live.status === "loading" ? (
          <div className="flex justify-center py-16 text-secondary">
            <Spinner />
          </div>
        ) : (
          <div className="py-16 text-center">
            <p className="text-[20px] font-semibold">No items</p>
            <p className="mt-1 text-[15px] text-secondary">Add one below.</p>
          </div>
        )}
      </div>

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
              className="h-11 min-w-0 flex-1 rounded-[10px] bg-card px-4 text-[17px] outline-none placeholder:text-tertiary"
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
    </div>
  )
}
