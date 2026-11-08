import { usePending, useRefused } from "@substrate/react"
import { CircleAlert, Clock } from "lucide-react"
import { Item } from "./kinds"

/** Whether an item's last change has reached the owner's substrate: a
 * line of its own under the item, never run into its name. */
export function WriteState({ id }: { id: string }) {
  const pending = usePending(Item, id)
  const refused = useRefused(Item, id)
  if (!pending && !refused) return null
  return (
    <span
      className={`mt-0.5 flex items-center gap-1 text-[13px] ${refused ? "text-warn" : "text-secondary"}`}
    >
      {refused ? (
        <CircleAlert className="size-3.5" aria-hidden />
      ) : (
        <Clock className="size-3.5" aria-hidden />
      )}
      {refused ? "Refused by your substrate" : "Waiting to be sent"}
    </span>
  )
}
