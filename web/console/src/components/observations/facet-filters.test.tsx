// @vitest-environment jsdom
/** The facet filters on their own: picking from "Add filter" the last facet
 * it offers closes the menu, and removing that filter later brings back the
 * button alone, never a menu nobody opened. */

import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { useState } from "react"
import { afterEach, expect, it } from "vitest"

import { FacetFilters, type Facet } from "./facet-filters"

afterEach(cleanup)

// A metric whose streams name no thing and no channel: Source is the one
// facet with anything to offer.
const FACETS: Facet[] = [
  {
    name: "source",
    label: "Source",
    options: [
      { value: "withings", label: "Withings" },
      { value: "manual", label: "Manual" },
    ],
  },
  { name: "about", label: "About", options: [] },
  { name: "channel", label: "Channel", options: [] },
]

function Page() {
  const [selected, setSelected] = useState<Record<string, string[]>>({})
  return (
    <FacetFilters
      facets={FACETS}
      selected={selected}
      onChange={(name, values) =>
        setSelected((s) => ({ ...s, [name]: values }))
      }
    />
  )
}

it("opens no menu when the last offered facet's filter is removed", async () => {
  render(<Page />)
  fireEvent.click(screen.getByRole("button", { name: /Add filter/ }))
  fireEvent.click(await screen.findByRole("option", { name: "Source" }))
  fireEvent.click(await screen.findByRole("option", { name: "Withings" }))
  // Source is chosen, so no facet is left to add, and the menu goes.
  await waitFor(() =>
    expect(screen.queryByRole("button", { name: /Add filter/ })).toBe(null)
  )
  await waitFor(() =>
    expect(screen.queryByRole("option", { name: "Manual" })).toBe(null)
  )
  fireEvent.click(screen.getByRole("button", { name: "Remove Source filter" }))
  expect(await screen.findByRole("button", { name: /Add filter/ })).toBeTruthy()
  expect(screen.queryByRole("option", { name: "Manual" })).toBe(null)
  expect(screen.queryByRole("listbox")).toBe(null)
})
