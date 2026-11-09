import { createContext, useContext, type ReactNode } from "react"

/** Whether the app runs on the owner's substrate or on the synthetic demo
 * member, and the day it takes as today. Decided once, at load: a hook
 * picked by the mode is the same hook for the page's life. */
export interface Mode {
  demo: boolean
  /** Today, at local midnight. The demo pins it with `?today=YYYY-MM-DD`. */
  today: Date
  /** The page's own query (`?demo&today=…&healthspan=paused`), for the
   * demo's variants. */
  params: URLSearchParams
}

function midnight(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate())
}

export function readMode(search: string, now = new Date()): Mode {
  const params = new URLSearchParams(search)
  const pinned = params.get("today")
  const day =
    pinned && /^\d{4}-\d{2}-\d{2}$/.test(pinned)
      ? new Date(`${pinned}T00:00:00`)
      : now
  return {
    demo: params.has("demo"),
    today: midnight(Number.isNaN(day.getTime()) ? now : day),
    params,
  }
}

const ModeContext = createContext<Mode | null>(null)

export function ModeProvider(props: { mode: Mode; children: ReactNode }) {
  return (
    <ModeContext.Provider value={props.mode}>
      {props.children}
    </ModeContext.Provider>
  )
}

export function useMode(): Mode {
  const mode = useContext(ModeContext)
  if (!mode) throw new Error("useMode outside <ModeProvider>")
  return mode
}
