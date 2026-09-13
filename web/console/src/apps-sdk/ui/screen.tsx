import { useEffect, useRef, type ReactNode } from "react"

import { host } from "./sdk"

export interface ScreenProps {
  title: string
  /** Wire the host's back button to `host.back()`. */
  back?: boolean
  primary?: { label: string; onPress(): void; enabled?: boolean }
  loading?: boolean
  children?: ReactNode
}

/** The host's chrome, driven from the guest: the title, the one primary
 * button the host pins above the safe area, and the back button. Nothing is
 * drawn here but the children and a loading line; the header is the host's. */
export function Screen({
  title,
  back,
  primary,
  loading,
  children,
}: ScreenProps) {
  useEffect(() => {
    host.title(title)
  }, [title])

  // The latest `onPress` behind one registered listener, so a re-render with
  // a new closure does not re-register and the host sees one click handler.
  const onPress = useRef<(() => void) | undefined>(undefined)
  useEffect(() => {
    onPress.current = primary?.onPress
  })

  const hasPrimary = Boolean(primary)
  const label = primary?.label
  const enabled = primary?.enabled ?? true
  useEffect(() => {
    if (!hasPrimary || label === undefined) return
    host.primaryAction.set({ label, enabled })
  }, [hasPrimary, label, enabled])
  useEffect(() => {
    if (!hasPrimary) return
    const off = host.primaryAction.onClick(() => onPress.current?.())
    return () => {
      off()
      host.primaryAction.set(null)
    }
  }, [hasPrimary])

  useEffect(() => {
    if (!back) return
    return host.backButton.onClick(() => host.back())
  }, [back])

  return (
    <div className="kit-screen">
      {loading && (
        <div className="kit-progress" role="progressbar" aria-label="Loading" />
      )}
      {children}
    </div>
  )
}
