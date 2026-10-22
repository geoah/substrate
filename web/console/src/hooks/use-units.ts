import { createContext, useContext } from "react"

import { TABLE_UNITS, type UnitReader } from "@/lib/unit-reader"

/** The page's one unit reader, which `UnitsProvider`
 * (components/units-provider.tsx) holds for the signed-in console. */
export const UnitsContext = createContext<UnitReader | null>(null)

/** The unit reader a component converts, prints, offers and checks units
 * through. Outside the provider (the sign-in pages, a component test) it is
 * the engine's table, which is what a server without the switch answers. */
export function useUnits(): UnitReader {
  return useContext(UnitsContext) ?? TABLE_UNITS
}
