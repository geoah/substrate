// @vitest-environment jsdom
/** `QuickAdd` puts one record per line: the typed text under `property`,
 * the defaults merged, one idempotency key minted per pending submit and
 * kept for a retry of the same failed line. */

import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"

const sdk = vi.hoisted(() => ({
  host: { toast: vi.fn() },
  records: { put: vi.fn() },
  useKind: () => undefined,
}))
vi.mock("./sdk", () => sdk)

import { QuickAdd } from "./quick-add"

const PERSON = "ada.example.com/people/person"
const UUID =
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/

const created = (name: string): SubstrateRecord => ({
  id: name.toLowerCase(),
  kind: PERSON,
  properties: { name },
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
})

function type(text: string) {
  const input = screen.getByRole("textbox")
  fireEvent.change(input, { target: { value: text } })
  return input as HTMLInputElement
}

// Enter on the input, not a form submit: the guest frame is sandboxed without
// `allow-forms`, where the browser never dispatches `submit` at all.
function submit() {
  fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" })
}

describe("QuickAdd", () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(cleanup)

  it("puts the text under the property with the defaults merged and one key", async () => {
    sdk.records.put.mockResolvedValueOnce(created("Grace"))
    const onCreated = vi.fn()
    render(
      <QuickAdd
        kind={PERSON}
        property="name"
        defaults={{ prominence: "known" }}
        placeholder="New person"
        onCreated={onCreated}
      />
    )
    const input = type("  Grace ")
    submit()
    await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(1))
    expect(sdk.records.put).toHaveBeenCalledTimes(1)
    const [kind, args, opts] = sdk.records.put.mock.calls[0]
    expect(kind).toBe(PERSON)
    expect(args).toEqual({ properties: { prominence: "known", name: "Grace" } })
    expect(opts.idempotencyKey).toMatch(UUID)
    expect(onCreated).toHaveBeenCalledWith(created("Grace"))
    expect(input.value).toBe("")
  })

  it("mints a new key for every line", async () => {
    sdk.records.put.mockResolvedValue(created("x"))
    render(<QuickAdd kind={PERSON} property="name" />)
    type("One")
    submit()
    await waitFor(() => expect(sdk.records.put).toHaveBeenCalledTimes(1))
    type("Two")
    submit()
    await waitFor(() => expect(sdk.records.put).toHaveBeenCalledTimes(2))
    const keys = sdk.records.put.mock.calls.map((c) => c[2].idempotencyKey)
    expect(keys[0]).toMatch(UUID)
    expect(keys[1]).toMatch(UUID)
    expect(keys[0]).not.toBe(keys[1])
  })

  it("keeps the failed line and its key, so the retry lands once", async () => {
    sdk.records.put
      .mockRejectedValueOnce(new Error("network"))
      .mockResolvedValueOnce(created("Ada"))
    render(<QuickAdd kind={PERSON} property="name" />)
    const input = type("Ada")
    submit()
    await screen.findByRole("alert")
    expect(screen.getByRole("alert").textContent).toBe("network")
    expect(input.value).toBe("Ada")
    submit()
    await waitFor(() => expect(sdk.records.put).toHaveBeenCalledTimes(2))
    const keys = sdk.records.put.mock.calls.map((c) => c[2].idempotencyKey)
    expect(keys[0]).toBe(keys[1])
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull())
    expect(input.value).toBe("")
  })

  it("puts nothing for a blank line", () => {
    render(<QuickAdd kind={PERSON} property="name" />)
    type("   ")
    submit()
    expect(sdk.records.put).not.toHaveBeenCalled()
  })

  it("keeps the input 44 px tall and focuses it from a tap on the bar", () => {
    const { container } = render(<QuickAdd kind={PERSON} property="name" />)
    const input = screen.getByRole("textbox")
    expect(input.className).toContain("kit-quickadd-input")
    fireEvent.click(container.querySelector(".kit-quickadd")!)
    expect(document.activeElement).toBe(input)
  })
})
