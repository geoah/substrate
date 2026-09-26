import { createContext, useContext } from "react"
import {
  DEFAULT_SETTINGS,
  type ConsoleAction,
  type ConsolePreferences,
  type Density,
  type DisplaySettings,
  type RecordWidth,
  type SettingKey,
  type TableWidth,
} from "@/lib/console-preferences"

export interface ConsolePreferencesContextValue {
  preferences: ConsolePreferences
  /** The record is still loading, or its read failed: navigation controls
   * wait rather than act on state they have not seen. */
  busy: boolean
  /** Queued, applied at once on screen and saved in order. */
  change: (action: ConsoleAction) => void
  set: <K extends SettingKey>(key: K, value: DisplaySettings[K]) => void
}

export const ConsolePreferencesContext =
  createContext<ConsolePreferencesContextValue | null>(null)

/** Outside the provider (the sign-in pages, a component test) every setting
 * reads its default and a change goes nowhere. */
const OUTSIDE: ConsolePreferencesContextValue = {
  preferences: {
    collapsed: [],
    favorites: [],
    sidebarOpen: true,
    ...DEFAULT_SETTINGS,
  },
  busy: false,
  change: () => {},
  set: () => {},
}

export function useConsolePreferences(): ConsolePreferencesContextValue {
  return useContext(ConsolePreferencesContext) ?? OUTSIDE
}

/** The navigation tree's handle; it cannot work without the provider. */
export function useSidebarPreferences(): ConsolePreferencesContextValue {
  const context = useContext(ConsolePreferencesContext)
  if (!context) throw new Error("ConsolePreferencesProvider is required")
  return context
}

export function useTechnicalDetails(): [boolean, (on: boolean) => void] {
  const { preferences, set } = useConsolePreferences()
  return [
    preferences.technicalDetails,
    (on: boolean) => set("technicalDetails", on),
  ]
}

export function useLayoutWidths(): {
  recordWidth: RecordWidth
  tableWidth: TableWidth
  setRecordWidth: (width: RecordWidth) => void
  setTableWidth: (width: TableWidth) => void
} {
  const { preferences, set } = useConsolePreferences()
  return {
    recordWidth: preferences.recordWidth,
    tableWidth: preferences.tableWidth,
    setRecordWidth: (width) => set("recordWidth", width),
    setTableWidth: (width) => set("tableWidth", width),
  }
}

export function useDensity(): [Density, (density: Density) => void] {
  const { preferences, set } = useConsolePreferences()
  return [preferences.density, (density) => set("density", density)]
}
