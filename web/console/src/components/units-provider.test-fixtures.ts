import { createElement, type ReactNode } from "react"

import { UnitsContext } from "@/hooks/use-units"
import { SHIPPED_UNITS } from "@/lib/unit-graph.test-fixtures"

/** A render wrapper that gives a component the shipped units, as the
 * signed-in console's `UnitsProvider` gives it the repository's. */
export function ShippedUnits({ children }: { children?: ReactNode }) {
  return createElement(
    UnitsContext.Provider,
    { value: SHIPPED_UNITS },
    children
  )
}
