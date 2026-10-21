/** The signed-in console's one unit reader (hooks/use-unit-reader.ts), held
 * for every component under it through `useUnits`. One provider is one
 * watch on the change stream, however many quantities the page shows. */

import type { ReactNode } from "react"

import { useUnitReader } from "@/hooks/use-unit-reader"
import { UnitsContext } from "@/hooks/use-units"

export function UnitsProvider({ children }: { children: ReactNode }) {
  const units = useUnitReader()
  return <UnitsContext.Provider value={units}>{children}</UnitsContext.Provider>
}
