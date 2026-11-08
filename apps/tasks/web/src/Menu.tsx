import { cx } from "@substrate/react"
import { Check, ChevronLeft, ChevronRight } from "lucide-react"
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react"

export type MenuItem =
  | {
      kind?: "action"
      label: string
      icon?: ReactNode
      destructive?: boolean
      checked?: boolean
      onSelect: () => void
    }
  | { kind: "submenu"; label: string; icon?: ReactNode; items: MenuItem[] }
  | { kind: "divider" }

/** A menu where the owner pointed: a row's context menu, a bar's ⋯, a
 * card's Move. A submenu opens in place, with its way back at the top.
 * ↑ and ↓ move through it, → opens a submenu, ← goes back, Escape closes,
 * and the focus goes back to what opened it. */
export function Menu(props: {
  at: { x: number; y: number } | HTMLElement
  items: MenuItem[]
  label: string
  onClose: () => void
}) {
  const [path, setPath] = useState<number[]>([])
  const box = useRef<HTMLDivElement>(null)
  const opener = useRef<Element | null>(document.activeElement)
  const [place, setPlace] = useState<{ left: number; top: number }>()

  let items = props.items
  const titles: string[] = []
  for (const i of path) {
    const sub = items[i]
    if (sub?.kind !== "submenu") break
    titles.push(sub.label)
    items = sub.items
  }

  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    const { width, height } = el.getBoundingClientRect()
    const anchor =
      props.at instanceof HTMLElement
        ? (() => {
            const r = props.at.getBoundingClientRect()
            return { x: r.right - width, y: r.bottom + 6, top: r.top }
          })()
        : { x: props.at.x, y: props.at.y, top: props.at.y }
    const margin = 8
    const left = Math.min(
      Math.max(margin, anchor.x),
      window.innerWidth - width - margin
    )
    const below = anchor.y + height <= window.innerHeight - margin
    const top = below
      ? anchor.y
      : Math.max(margin, Math.min(anchor.top - 6, window.innerHeight) - height)
    setPlace({ left, top })
  }, [props.at, path])

  useEffect(() => {
    box.current
      ?.querySelector<HTMLElement>("[role=menuitem],[role=menuitemradio]")
      ?.focus()
  }, [path, place])

  useEffect(() => {
    const back = opener.current
    return () => {
      if (back instanceof HTMLElement && back.isConnected) back.focus()
    }
  }, [])

  const close = props.onClose
  // A column of marks only where some item has one.
  const marked = items.some(
    (i) =>
      i.kind !== "divider" &&
      (i.icon || ("checked" in i && i.checked !== undefined))
  )

  const back = () => setPath((p) => p.slice(0, -1))
  const keys = useRef<(e: KeyboardEvent) => void>(() => {})
  // On the window, not the menu: a finger's hold leaves the focus where the
  // browser's own mouse events put it.
  keys.current = (e: KeyboardEvent) => {
    const all = [
      ...(box.current?.querySelectorAll<HTMLElement>(
        "[role=menuitem],[role=menuitemradio]"
      ) ?? []),
    ]
    const at = all.indexOf(document.activeElement as HTMLElement)
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault()
      const n = all.length
      const next =
        at < 0
          ? e.key === "ArrowDown"
            ? 0
            : n - 1
          : e.key === "ArrowDown"
            ? (at + 1) % n
            : (at - 1 + n) % n
      all[next]?.focus()
    } else if (e.key === "Escape") {
      e.preventDefault()
      e.stopImmediatePropagation()
      close()
    } else if (e.key === "ArrowLeft" && path.length) {
      e.preventDefault()
      back()
    } else if (e.key === "Tab") {
      e.preventDefault()
    }
  }
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => keys.current(e)
    window.addEventListener("keydown", onKey, true)
    return () => window.removeEventListener("keydown", onKey, true)
  }, [])

  return (
    <div className="fixed inset-0 z-50">
      <div
        className="absolute inset-0"
        onPointerDown={(e) => {
          e.preventDefault()
          close()
        }}
        onContextMenu={(e) => {
          e.preventDefault()
          close()
        }}
      />
      <div
        ref={box}
        role="menu"
        aria-label={titles.at(-1) ?? props.label}
        style={place ?? { left: -9999, top: 0 }}
        className="absolute max-h-[min(70dvh,480px)] w-max max-w-[min(320px,calc(100vw-16px))] min-w-[220px] overflow-y-auto rounded-[14px] bg-card/95 py-1.5 shadow-[0_10px_40px_rgb(0_0_0/0.22)] ring-1 ring-separator/40 backdrop-blur-xl"
      >
        {path.length > 0 && (
          <>
            <button
              type="button"
              role="menuitem"
              onClick={back}
              className="flex min-h-11 w-full items-center gap-2 px-3 text-left text-[15px] font-semibold text-secondary outline-none focus-visible:bg-selected"
            >
              <ChevronLeft className="size-4 shrink-0" />
              {titles.at(-1)}
            </button>
            <div className="mx-3 my-1 h-px bg-separator" />
          </>
        )}
        {items.map((item, i) =>
          item.kind === "divider" ? (
            <div key={i} className="mx-3 my-1 h-px bg-separator" />
          ) : item.kind === "submenu" ? (
            <button
              key={i}
              type="button"
              role="menuitem"
              aria-haspopup="menu"
              onClick={() => setPath((p) => [...p, i])}
              onKeyDown={(e) => {
                if (e.key === "ArrowRight") {
                  e.preventDefault()
                  setPath((p) => [...p, i])
                }
              }}
              className={itemClass(false)}
            >
              {marked && (
                <span className="flex size-5 shrink-0 items-center justify-center text-secondary">
                  {item.icon}
                </span>
              )}
              <span className="min-w-0 flex-1">{item.label}</span>
              <ChevronRight className="size-4 shrink-0 text-secondary" />
            </button>
          ) : (
            <button
              key={i}
              type="button"
              role={item.checked === undefined ? "menuitem" : "menuitemradio"}
              aria-checked={item.checked}
              onClick={() => {
                close()
                item.onSelect()
              }}
              className={itemClass(!!item.destructive)}
            >
              {marked && (
                <span
                  className={cx(
                    "flex size-5 shrink-0 items-center justify-center",
                    item.destructive ? "text-danger" : "text-secondary"
                  )}
                >
                  {item.checked ? (
                    <Check className="size-4 text-label" strokeWidth={2.5} />
                  ) : (
                    item.icon
                  )}
                </span>
              )}
              <span className="min-w-0 flex-1">{item.label}</span>
            </button>
          )
        )}
      </div>
    </div>
  )
}

function itemClass(destructive: boolean) {
  return cx(
    "flex min-h-11 w-full items-center gap-3 px-3 py-2 text-left text-[17px] outline-none hover:bg-selected focus-visible:bg-selected",
    destructive && "text-danger"
  )
}

/** A menu opened by a button: the button and, while open, the menu under
 * it. */
export function MenuButton(props: {
  label: string
  items: () => MenuItem[]
  className?: string
  children: ReactNode
}) {
  const [anchor, setAnchor] = useState<HTMLElement>()
  return (
    <>
      <button
        type="button"
        aria-label={props.label}
        aria-haspopup="menu"
        aria-expanded={!!anchor}
        className={props.className}
        onClick={(e) => setAnchor(e.currentTarget)}
      >
        {props.children}
      </button>
      {anchor && (
        <Menu
          at={anchor}
          label={props.label}
          items={props.items()}
          onClose={() => setAnchor(undefined)}
        />
      )}
    </>
  )
}

/** A question in the system's alert style: its title, what it means, and
 * its buttons, side by side where both fit and stacked where one is long,
 * as an iPhone stacks them. */
export function Alert(props: {
  title: string
  message?: ReactNode
  children?: ReactNode
  buttons: {
    label: string
    onClick: () => void
    role?: "cancel" | "destructive" | "default"
    disabled?: boolean
  }[]
  onCancel: () => void
}) {
  const cancel = useRef(props.onCancel)
  cancel.current = props.onCancel
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault()
        cancel.current()
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])
  const stacked =
    props.buttons.length > 2 || props.buttons.some((b) => b.label.length > 12)
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-6"
      role="alertdialog"
      aria-modal
      aria-label={props.title}
    >
      <div className="absolute inset-0 bg-black/35" onClick={props.onCancel} />
      <div className="relative w-full max-w-[300px] overflow-hidden rounded-[14px] bg-card/95 text-center shadow-2xl backdrop-blur-xl">
        <div className="px-4 pt-5 pb-4">
          <h2 className="text-[17px] font-semibold">{props.title}</h2>
          {props.message && (
            <p className="mt-1 text-[13px] leading-snug">{props.message}</p>
          )}
          {props.children}
        </div>
        <div
          className={cx(
            "border-t border-separator",
            stacked
              ? "flex flex-col [&>*+*]:border-t [&>*+*]:border-separator"
              : "grid grid-cols-2 [&>*+*]:border-l [&>*+*]:border-separator"
          )}
        >
          {(stacked
            ? [...props.buttons].sort(
                (a, b) =>
                  Number(a.role === "cancel") - Number(b.role === "cancel")
              )
            : props.buttons
          ).map((b) => (
            <button
              key={b.label}
              type="button"
              disabled={b.disabled}
              onClick={b.onClick}
              className={cx(
                "min-h-11 px-3 py-2 text-[17px] disabled:opacity-40",
                b.role === "destructive" ? "text-danger" : "text-accent-text",
                b.role !== "cancel" && "font-semibold"
              )}
            >
              {b.label}
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}
