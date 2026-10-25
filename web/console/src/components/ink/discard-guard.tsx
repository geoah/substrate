/** Unsaved ink is asked about before it goes. Cancel and Escape go through
 * `confirm`; a link, the browser's back and a swipe back go through the
 * router, which holds the move until the dialog is answered; a reload or a
 * closed tab gets the browser's own prompt. Outside a router (a test of the
 * editor alone) only the router's part is left out. */

import { useCallback, useEffect, useState, type ReactNode } from "react"
import { useRouter } from "@tanstack/react-router"

import { ConfirmDialog } from "@/components/ui/confirm-dialog"

import { RouteGuard, type Pending } from "./route-guard"

export interface DiscardWording {
  /** A question: "Discard this handwritten note?" */
  title: string
  /** What goes, and what stays. */
  consequence: string
}

export function useDiscardGuard(
  unsaved: boolean,
  wording: DiscardWording
): { confirm: (go: () => void) => void; guard: ReactNode } {
  const [pending, setPending] = useState<Pending>()
  const confirm = useCallback(
    (go: () => void) => (unsaved ? setPending({ go }) : go()),
    [unsaved]
  )
  useEffect(() => {
    if (!unsaved) return
    const warn = (event: BeforeUnloadEvent) => event.preventDefault()
    window.addEventListener("beforeunload", warn)
    return () => window.removeEventListener("beforeunload", warn)
  }, [unsaved])
  const router = useRouter({ warn: false })
  const guard = (
    <>
      {router && <RouteGuard unsaved={unsaved} onBlocked={setPending} />}
      {pending && (
        <ConfirmDialog
          title={wording.title}
          consequence={wording.consequence}
          confirm="Discard"
          destructive
          onConfirm={() => {
            setPending(undefined)
            pending.go()
          }}
          onClose={() => {
            setPending(undefined)
            pending.stay?.()
          }}
        />
      )}
    </>
  )
  return { confirm, guard }
}
