/** Holds a move to another page while `unsaved`, and hands the held move to
 * `onBlocked` to go on or stay. */

import { useEffect } from "react"
import { useBlocker } from "@tanstack/react-router"

export interface Pending {
  go: () => void
  stay?: () => void
}

export function RouteGuard({
  unsaved,
  onBlocked,
}: {
  unsaved: boolean
  onBlocked: (pending: Pending) => void
}) {
  const { status, proceed, reset } = useBlocker({
    shouldBlockFn: () => true,
    disabled: !unsaved,
    enableBeforeUnload: false,
    withResolver: true,
  })
  useEffect(() => {
    if (status === "blocked") onBlocked({ go: proceed, stay: reset })
  }, [status, proceed, reset, onBlocked])
  return null
}
