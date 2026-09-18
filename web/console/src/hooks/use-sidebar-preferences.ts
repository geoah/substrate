import { createContext, useContext } from "react"
import type {
  SidebarAction,
  SidebarPreferences,
} from "@/lib/sidebar-preferences"

export const SidebarPreferencesContext = createContext<{
  preferences: SidebarPreferences
  busy: boolean
  change: (action: SidebarAction) => void
} | null>(null)

export function useSidebarPreferences() {
  const context = useContext(SidebarPreferencesContext)
  if (!context) throw new Error("NavigationProvider is required")
  return context
}
