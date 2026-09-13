import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
  type PointerEvent,
  type ReactNode,
} from "react"

import type { SubstrateRecord } from "@/lib/api/types"
import { instantOf, relativeDay } from "./buckets"
import { KitIcon } from "./icons"
import { useNow } from "./now"
import { host } from "./sdk"

export interface RowAction {
  icon: string
  /** `http`, `https`, `mailto` or `tel`; opened by the host after it asks. */
  href?: string
  onPress?(): void
  label?: string
}

export interface RowProps {
  title: ReactNode
  subtitle?: ReactNode
  meta?: ReactNode
  /** An ISO instant, shown relative ("in 3 days"); past reads as overdue. */
  when?: string
  leading?: ReactNode
  trailing?: ReactNode
  chevron?: boolean
  /** A tap opens the console's record page, unless `onTap` says otherwise. */
  record?: SubstrateRecord
  onTap?(): void
  /** The first is a trailing 44 px button; the rest open under a hold. A
   * falsy entry is skipped, so an action can be conditional inline. */
  actions?: (false | undefined | null | RowAction)[]
}

/** How long a finger rests before the rest of the actions open. Short
 * enough to feel like a hold, long enough that a slow tap is still a tap. */
const HOLD_MS = 450
const HOLD_SLOP = 10

/** "map-pin" → "Map pin": the label an action gets when it declares none. */
function iconLabel(name: string): string {
  const words = name.split("-").filter(Boolean)
  if (!words.length) return name
  return words
    .map((w, i) => (i === 0 ? w.charAt(0).toUpperCase() + w.slice(1) : w))
    .join(" ")
}

function labelOf(action: RowAction): string {
  return action.label ?? iconLabel(action.icon)
}

function run(action: RowAction): void {
  if (action.onPress) action.onPress()
  else if (action.href) void host.openLink(action.href)
}

/** One line of a list: leading, the body, the relative time, trailing, a
 * chevron, then the first action. No swipe carries meaning: it fights the
 * OS back gesture, so the rest of the actions are behind a hold (or a
 * context menu, which is what a keyboard and Android give). */
export function Row({
  title,
  subtitle,
  meta,
  when,
  leading,
  trailing,
  chevron,
  record,
  onTap,
  actions,
}: RowProps) {
  const live = (actions ?? []).filter((a): a is RowAction => Boolean(a))
  const [first, ...rest] = live
  const tappable = Boolean(onTap || record)

  const now = useNow()
  const [menu, setMenu] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const origin = useRef<{ x: number; y: number } | undefined>(undefined)
  // Set when a hold opened the menu, so the click that follows the finger
  // lifting is not also a tap.
  const held = useRef(false)

  const clearHold = () => {
    if (timer.current !== undefined) clearTimeout(timer.current)
    timer.current = undefined
    origin.current = undefined
  }
  useEffect(() => clearHold, [])

  const tap = () => {
    if (onTap) onTap()
    else if (record) {
      void host.navigate({ record: { kind: record.kind, id: record.id } })
    }
  }
  const onClick = () => {
    if (held.current) {
      held.current = false
      return
    }
    if (tappable) tap()
  }
  const onKeyDown = (e: KeyboardEvent) => {
    if (!tappable || (e.key !== "Enter" && e.key !== " ")) return
    e.preventDefault()
    tap()
  }
  const onPointerDown = (e: PointerEvent) => {
    if (!rest.length || e.button !== 0) return
    clearHold()
    origin.current = { x: e.clientX, y: e.clientY }
    timer.current = setTimeout(() => {
      timer.current = undefined
      held.current = true
      setMenu(true)
    }, HOLD_MS)
  }
  const onPointerMove = (e: PointerEvent) => {
    const o = origin.current
    if (!o) return
    if (
      Math.abs(e.clientX - o.x) > HOLD_SLOP ||
      Math.abs(e.clientY - o.y) > HOLD_SLOP
    ) {
      clearHold()
    }
  }
  const onContextMenu = (e: MouseEvent) => {
    if (!rest.length) return
    e.preventDefault()
    clearHold()
    setMenu(true)
  }
  const closeMenu = () => {
    held.current = false
    setMenu(false)
  }

  const at = when === undefined ? undefined : instantOf(when)
  const past = at !== undefined && at < now

  return (
    <div className="kit-row" role="listitem">
      <div
        className={
          tappable ? "kit-row-main kit-row-main--tappable" : "kit-row-main"
        }
        role={tappable ? "button" : undefined}
        tabIndex={tappable ? 0 : undefined}
        onClick={onClick}
        onKeyDown={onKeyDown}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={clearHold}
        onPointerCancel={clearHold}
        onPointerLeave={clearHold}
        onContextMenu={onContextMenu}
      >
        {leading !== undefined && (
          <span className="kit-row-leading">{leading}</span>
        )}
        <span className="kit-row-body">
          <span className="kit-row-title">{title}</span>
          {subtitle !== undefined && subtitle !== null && subtitle !== "" && (
            <span className="kit-row-subtitle">{subtitle}</span>
          )}
          {meta !== undefined && meta !== null && meta !== "" && (
            <span className="kit-row-meta">{meta}</span>
          )}
        </span>
        {when !== undefined && (
          <time
            className={
              past ? "kit-row-when kit-row-when--past" : "kit-row-when"
            }
            dateTime={when}
            title={at === undefined ? undefined : new Date(at).toLocaleString()}
          >
            {relativeDay(when, now)}
          </time>
        )}
        {trailing !== undefined && (
          <span className="kit-row-trailing">{trailing}</span>
        )}
        {chevron && (
          <span className="kit-row-chevron">
            <KitIcon name="chevron-right" size={18} />
          </span>
        )}
      </div>
      {first && (
        <div className="kit-row-actions">
          <button
            type="button"
            className="kit-btn kit-icon-btn"
            aria-label={labelOf(first)}
            title={labelOf(first)}
            onClick={() => run(first)}
          >
            <KitIcon name={first.icon} />
          </button>
        </div>
      )}
      {menu && (
        <>
          <div className="kit-menu-backdrop" onClick={closeMenu} />
          <div className="kit-menu" role="menu" aria-label="More actions">
            {typeof title === "string" && (
              <div className="kit-menu-title">{title}</div>
            )}
            {rest.map((action, i) => (
              <button
                key={i}
                type="button"
                role="menuitem"
                className="kit-btn kit-menu-item"
                onClick={() => {
                  closeMenu()
                  run(action)
                }}
              >
                <KitIcon name={action.icon} />
                {labelOf(action)}
              </button>
            ))}
            <button
              type="button"
              className="kit-btn kit-menu-item kit-menu-cancel"
              onClick={closeMenu}
            >
              Cancel
            </button>
          </div>
        </>
      )}
    </div>
  )
}
