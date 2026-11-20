import { createContext, useContext } from "react"

/** What the shell tells a screen about where it is drawn: wide (a sidebar
 * beside it, no tab bar) or a phone's stack. */
export interface Shell {
  wide: boolean
}

export const ShellContext = createContext<Shell>({ wide: false })

export function useShell(): Shell {
  return useContext(ShellContext)
}
