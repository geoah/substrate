import { useEffect, useRef, type RefObject } from "react"

const focusable =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

/** The open modals, the topmost last: only it answers the keyboard, so
 * Escape on a question over a sheet closes the question alone. */
const stack: object[] = []

function focusables(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>(focusable)].filter(
    (el) =>
      !el.hasAttribute("inert") && el.getAttribute("aria-hidden") !== "true"
  )
}

/** A modal's keyboard contract: focus moves into it when it opens (to the
 * element marked `data-autofocus`, else its first control), Tab and
 * Shift-Tab stay inside it, Escape closes it, and focus goes back to what
 * held it before once it closes. */
export function useDialog(
  open: boolean,
  onClose: () => void
): RefObject<HTMLDivElement | null> {
  const ref = useRef<HTMLDivElement>(null)
  const close = useRef(onClose)
  useEffect(() => {
    close.current = onClose
  })

  useEffect(() => {
    if (!open) return
    const token = {}
    stack.push(token)
    const before =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null
    const root = ref.current
    if (root) {
      const target =
        root.querySelector<HTMLElement>("[data-autofocus]") ??
        root.querySelector<HTMLElement>("[autofocus]") ??
        focusables(root)[0] ??
        root
      target.focus()
    }
    const onKey = (e: KeyboardEvent) => {
      if (stack.at(-1) !== token) return
      if (e.key === "Escape") {
        e.stopPropagation()
        close.current()
        return
      }
      if (e.key !== "Tab" || !ref.current) return
      const items = focusables(ref.current)
      if (!items.length) {
        e.preventDefault()
        ref.current.focus()
        return
      }
      const first = items[0]
      const last = items[items.length - 1]
      const active = document.activeElement
      if (e.shiftKey && (active === first || !ref.current.contains(active))) {
        e.preventDefault()
        last.focus()
      } else if (
        !e.shiftKey &&
        (active === last || !ref.current.contains(active))
      ) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener("keydown", onKey, true)
    return () => {
      document.removeEventListener("keydown", onKey, true)
      stack.splice(stack.indexOf(token), 1)
      if (before?.isConnected) before.focus()
    }
  }, [open])

  return ref
}
