import { useState, type MouseEvent, type PointerEvent } from "react"

import { host } from "./sdk"
import { KitIcon } from "./icons"

export interface CheckProps {
  checked?: boolean
  disabled?: boolean
  pending?: boolean
  /** The tap. A returned promise (a transition) keeps the check pending
   * until it settles, and a rejection (a `409`, a refused grant) is shown
   * here and toasted, never retried. */
  onChange(): void | Promise<unknown>
}

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

/** A 44 px toggle for a transition. It sits inside a row that navigates on
 * tap, so its events stop here. */
export function Check({ checked, disabled, pending, onChange }: CheckProps) {
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState<string>()
  const isPending = Boolean(pending) || busy

  const onClick = (e: MouseEvent) => {
    e.stopPropagation()
    if (disabled || isPending) return
    setFailed(undefined)
    const result = onChange()
    if (!result || typeof result.then !== "function") return
    setBusy(true)
    result.then(
      () => setBusy(false),
      (err: unknown) => {
        const message = messageOf(err)
        setBusy(false)
        setFailed(message)
        host.toast({
          title: "Could not update",
          description: message,
          type: "error",
        })
      }
    )
  }
  const stop = (e: PointerEvent) => e.stopPropagation()

  let cls = "kit-btn kit-check"
  if (checked) cls += " kit-check--checked"
  if (isPending) cls += " kit-check--pending"
  if (failed) cls += " kit-check--error"

  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={Boolean(checked)}
      aria-busy={isPending || undefined}
      aria-label={checked ? "Done" : "Mark done"}
      title={failed}
      className={cls}
      disabled={disabled}
      onClick={onClick}
      onPointerDown={stop}
    >
      <span className="kit-check-box">
        {checked && !isPending && <KitIcon name="check" size={14} />}
      </span>
    </button>
  )
}
