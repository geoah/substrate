import { Info } from "lucide-react"
import { useState, type ReactNode } from "react"
import { IconButton } from "../IconButton"
import { Popover } from "../Popover"

/** The panel's width (Popover's `22rem`) and the room kept beside it. */
const PANEL_PX = 352 + 16

/** A card's info button and its panel, which opens toward the side of the
 * window with room for it: a card in the last column opens it leftward,
 * where a panel lined up with the button's start would leave the screen. */
export function InfoPopover(props: { label: string; children: ReactNode }) {
  const [align, setAlign] = useState<"start" | "end">("start")
  const place = (el: HTMLElement) => {
    const r = el.getBoundingClientRect()
    setAlign(r.left + PANEL_PX > window.innerWidth ? "end" : "start")
  }
  return (
    <Popover
      title={props.label}
      align={align}
      trigger={({ isOpen, triggerProps }) => (
        <IconButton
          label={props.label}
          active={isOpen}
          {...triggerProps}
          onPointerDown={(e) => place(e.currentTarget)}
          onFocus={(e) => place(e.currentTarget)}
        >
          <Info className="size-[18px]" aria-hidden />
        </IconButton>
      )}
    >
      {props.children}
    </Popover>
  )
}
