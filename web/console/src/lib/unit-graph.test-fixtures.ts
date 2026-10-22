/** A small repository unit graph as GET /api/v1/units answers it: mass and
 * temperature components, an angle with a turn, and two units an expression
 * joins. */

import type { UnitTable } from "./api/types"

export const fixtureTable: Pick<
  UnitTable,
  "units" | "conversions" | "revision"
> = {
  revision: 7,
  units: [
    {
      id: "g",
      name: "gram",
      symbol: "g",
      code: "g",
      aliases: [],
      root: "g",
      factor: "1",
    },
    {
      id: "kg",
      name: "kilogram",
      symbol: "kg",
      code: "kg",
      aliases: [],
      root: "g",
      factor: "1000",
    },
    {
      id: "lb",
      name: "pound",
      symbol: "lb",
      code: "[lb_av]",
      aliases: ["pound"],
      root: "g",
      factor: "453.59237",
    },
    {
      id: "ug",
      name: "microgram",
      symbol: "μg",
      code: "ug",
      aliases: [],
      root: "g",
      factor: "1/1000000",
    },
    {
      id: "Cel",
      name: "degree Celsius",
      symbol: "°C",
      code: "Cel",
      aliases: ["°C"],
      root: "Cel",
      factor: "1",
    },
    {
      id: "K",
      name: "kelvin",
      symbol: "K",
      code: "K",
      aliases: [],
      root: "Cel",
      factor: "1",
      offset: "-273.15",
    },
    {
      id: "degF",
      name: "degree Fahrenheit",
      symbol: "°F",
      code: "[degF]",
      aliases: [],
      root: "Cel",
      factor: "5/9",
      offset: "-160/9",
    },
    {
      id: "deg",
      name: "degree",
      symbol: "°",
      code: "deg",
      aliases: [],
      turn: "360",
      root: "deg",
      factor: "1",
    },
    {
      id: "mpg",
      name: "miles per gallon",
      symbol: "mpg",
      aliases: [],
      root: "mpg",
      factor: "1",
    },
    {
      id: "L/100km",
      name: "liters per 100 km",
      symbol: "L/100 km",
      aliases: [],
      root: "L/100km",
      factor: "1",
    },
  ],
  conversions: [
    {
      id: "mpg-L/100km",
      from: "mpg",
      to: "L/100km",
      expr: "235.214583 / value",
      inverse: "235.214583 / value",
    },
  ],
}

/** The same graph as one repository edited it: a unit the engine's table
 * does not hold (`sack25`, 25 kg, printed `sack`) and the pound's factor
 * changed to 500 g, so 1 kg is 2 lb here and 2.20462 lb in the table. */
export const editedTable: Pick<
  UnitTable,
  "units" | "conversions" | "revision"
> = {
  ...fixtureTable,
  revision: 8,
  units: [
    ...fixtureTable.units.map((u) =>
      u.id === "lb" ? { ...u, factor: "500" } : u
    ),
    {
      id: "sack25",
      name: "sack",
      symbol: "sack",
      aliases: [],
      root: "g",
      factor: "25000",
    },
  ],
}
