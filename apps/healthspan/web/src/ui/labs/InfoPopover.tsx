import { Info } from "lucide-react"
import type { ReactNode } from "react"
import { IconButton } from "../IconButton"
import { Popover } from "../Popover"

/** A card's info button and its panel, which opens toward the side of the
 * window with room for it (Popover's own placement). */
export function InfoPopover(props: { label: string; children: ReactNode }) {
  return (
    <Popover
      title={props.label}
      trigger={({ isOpen, triggerProps }) => (
        <IconButton label={props.label} active={isOpen} {...triggerProps}>
          <Info className="size-[18px]" aria-hidden />
        </IconButton>
      )}
    >
      {props.children}
    </Popover>
  )
}
