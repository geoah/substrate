import {
  AccessBanner,
  BarButton,
  LargeTitle,
  LinkStatus,
  NavBar,
  RefusedBanner,
  Section,
  Spinner,
  useLinkedStatus,
  useListed,
  useRecords,
} from "@substrate/react"
import { ChevronRight, Settings } from "lucide-react"
import { useMemo } from "react"
import { AddItem } from "./AddItem"
import { Item, itemTitle } from "./kinds"
import { WriteState } from "./WriteState"

/** The items, sorted by name, and the field that adds one. */
export function ItemsScreen(props: {
  onOpen: (id: string) => void
  onSettings: () => void
}) {
  const records = useRecords(Item)
  const listed = useListed(Item)
  const link = useLinkedStatus()
  const items = useMemo(
    () =>
      [...records].sort(
        (a, b) =>
          itemTitle(a).localeCompare(itemTitle(b)) || a.id.localeCompare(b.id)
      ),
    [records]
  )

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
      <LinkStatus status={link.status} words={link.words} />
      <AccessBanner className="mx-4 mb-4" />
      <RefusedBanner className="mx-4 mb-4" onOpenSettings={props.onSettings} />
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
        ) : !listed ? (
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
      <AddItem />
    </div>
  )
}
