import { Sheet, cx, useMediaQuery } from "@substrate/react"
import {
  useEffect,
  useId,
  useRef,
  useState,
  type ReactNode,
  type Ref,
} from "react"

/** What a popover hands its trigger to spread on the button that opens
 * it. */
export interface PopoverTriggerProps {
  ref: Ref<HTMLButtonElement>
  onClick: () => void
  "aria-expanded": boolean
  "aria-haspopup": "dialog"
  "aria-controls": string | undefined
}

/**
 * A panel opened from a button: on a phone the kit's Sheet from the bottom,
 * from 768 px a panel under the button (the info of a card, the display
 * settings of a screen). The panel is a named dialog that does not trap
 * focus: focus moves into it as it opens, Escape, a press outside or
 * focus leaving it closes it, and focus goes back to the button.
 */
export function Popover(props: {
  /** The panel's name, and the Sheet's title on a phone. */
  title: string
  /** The button: spread `triggerProps` on it. */
  trigger: (p: {
    isOpen: boolean
    triggerProps: PopoverTriggerProps
  }) => ReactNode
  /** Which edge of the button the panel lines up with. Default `start`. */
  align?: "start" | "end"
  /** The panel's own classes (its width, its background). */
  panelClassName?: string
  children: ReactNode
}) {
  const wide = useMediaQuery("(min-width: 768px)")
  const [open, setOpen] = useState(false)
  const button = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLDivElement>(null)
  const root = useRef<HTMLSpanElement>(null)
  const id = useId()

  const close = (refocus: boolean) => {
    setOpen(false)
    if (refocus) button.current?.focus()
  }

  useEffect(() => {
    if (!open || !wide) return
    const el = panel.current
    const first = el?.querySelector<HTMLElement>(
      "button, a[href], input, select, textarea, [tabindex]:not([tabindex='-1'])"
    )
    ;(first ?? el)?.focus()
    const onDown = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener("pointerdown", onDown)
    return () => document.removeEventListener("pointerdown", onDown)
  }, [open, wide])

  const triggerProps: PopoverTriggerProps = {
    ref: button,
    onClick: () => setOpen((v) => !v),
    "aria-expanded": open,
    "aria-haspopup": "dialog",
    "aria-controls": open && wide ? id : undefined,
  }

  if (!wide)
    return (
      <>
        {props.trigger({ isOpen: open, triggerProps })}
        <Sheet open={open} onClose={() => setOpen(false)} title={props.title}>
          {props.children}
        </Sheet>
      </>
    )

  return (
    <span ref={root} className="relative inline-flex">
      {props.trigger({ isOpen: open, triggerProps })}
      {open && (
        <div
          ref={panel}
          id={id}
          role="dialog"
          aria-label={props.title}
          tabIndex={-1}
          onKeyDown={(e) => {
            if (e.key !== "Escape") return
            e.stopPropagation()
            close(true)
          }}
          onBlur={(e) => {
            const next = e.relatedTarget as Node | null
            if (next && !root.current?.contains(next)) setOpen(false)
          }}
          className={cx(
            "absolute top-full z-40 mt-2 max-h-[min(80dvh,640px)] w-[min(22rem,calc(100vw-2rem))] overflow-y-auto rounded-[14px] bg-card p-4 text-left text-label shadow-[0_8px_32px_rgb(0_0_0/0.18)] ring-1 ring-separator/40 outline-none",
            props.align === "end" ? "right-0" : "left-0",
            props.panelClassName
          )}
        >
          {props.children}
        </div>
      )}
    </span>
  )
}
