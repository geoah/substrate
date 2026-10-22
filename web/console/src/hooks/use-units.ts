import { createContext, useContext } from "react"

import { pendingUnits, type UnitReader } from "@/lib/unit-reader"

/** The page's one unit reader, which `UnitsProvider`
 * (components/units-provider.tsx) holds for the signed-in console. */
export const UnitsContext = createContext<UnitReader | null>(null)

/** Outside the provider no repository is signed in, so no graph exists. */
const OUTSIDE = pendingUnits("loading")

/** The unit reader a component converts, prints, offers and checks units
 * through. Outside the provider (the sign-in pages) it is the pending
 * reader: no repository is signed in, so no unit answer exists. */
export function useUnits(): UnitReader {
  return useContext(UnitsContext) ?? OUTSIDE
}
