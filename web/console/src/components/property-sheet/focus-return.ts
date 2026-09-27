/** Focus back on the thing that opened an editor once the editor closes. An
 * in-place editor unmounts on save or cancel, and the element that held
 * focus goes with it, which drops focus to the page's start; the reader who
 * pressed Enter would Tab from the top again. Focus that already moved
 * somewhere real (a click on another control) is left where it is. */

import { useEffect, useRef, type RefObject } from "react"

/** Whether focus has nowhere to be: the element holding it was removed. */
export function focusLost(doc: Document = document): boolean {
  const active = doc.activeElement
  return !active || active === doc.body || !active.isConnected
}

/** Refocuses `target` when `editing` turns false and focus was lost. */
export function useFocusReturn(
  editing: boolean,
  target: RefObject<HTMLElement | null>
) {
  const was = useRef(editing)
  useEffect(() => {
    if (was.current && !editing && focusLost()) {
      target.current?.focus({ preventScroll: true })
    }
    was.current = editing
  }, [editing, target])
}
