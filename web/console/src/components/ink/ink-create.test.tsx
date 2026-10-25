// @vitest-environment jsdom
/** A new note is drawn first: drawing mode opens on a fresh page with the
 * name filled in, Save creates the record with its file in one step, and
 * Cancel asks before throwing away anything that was changed. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const navigate = vi.fn()
vi.mock("@tanstack/react-router", async (original) => ({
  ...(await original<typeof import("@tanstack/react-router")>()),
  useNavigate: () => navigate,
}))

import type { KindInfo } from "@/lib/api/types"
import { INK_MEDIA_TYPE } from "@/lib/ink"
import { propSpecs } from "@/lib/record-schema"

import { InkCreate } from "./ink-create"

const KIND = {
  identity: "ada.example.com/ink/note",
  name: "note",
  authority: "ada.example.com",
  package: "ink",
  version: 1,
  source: "installed",
  description: "",
  label: { singular: "Handwritten note", plural: "Handwritten notes" },
  definition: {
    names: { singular: "note" },
    displayTemplate: "{name}",
    properties: {
      name: { type: "string", required: true },
      file: { type: "blobref", required: true, mediaTypes: [INK_MEDIA_TYPE] },
    },
  },
} as unknown as KindInfo

let created: Record<string, unknown>[] = []
let uploaded: string[] = []

beforeEach(() => {
  created = []
  uploaded = []
  navigate.mockClear()
  localStorage.clear()
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init?: RequestInit) => {
      const json = (status: number, body: unknown) =>
        new Response(JSON.stringify(body), {
          status,
          headers: { "Content-Type": "application/json" },
        })
      if (input.startsWith("/api/v1/blobs") && init?.method === "PUT") {
        const text = new TextDecoder().decode(init.body as Uint8Array)
        uploaded.push(text)
        return json(201, {
          digest: `blob-sha256-${"b".repeat(64)}`,
          size: (init.body as Uint8Array).byteLength,
          status: "stored",
        })
      }
      if (input === "/api/v1/records" && init?.method === "POST") {
        const body = JSON.parse(String(init.body)) as {
          properties: Record<string, unknown>
        }
        created.push(body.properties)
        return json(201, { id: "n1", kind: KIND.identity, version: 1 })
      }
      return json(404, { error: { code: "not_found", message: input } })
    })
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderCreate() {
  const specs = propSpecs(KIND)
  const client = new QueryClient()
  return render(
    <QueryClientProvider client={client}>
      <InkCreate
        authority="ada.example.com"
        pkg="ink"
        name="note"
        kind={KIND}
        ink={specs.find((s) => s.name === "file")!}
        heading={specs.find((s) => s.name === "name")}
      />
    </QueryClientProvider>
  )
}

describe("InkCreate", () => {
  it("creates the note with its file and its started-at name on one Save", async () => {
    renderCreate()
    const name = screen.getByRole<HTMLInputElement>("textbox", {
      name: "Name",
    })
    expect(name.value).not.toBe("")
    // The keyboard stays down: nothing is focused for the reader.
    expect(document.activeElement).not.toBe(name)

    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(created).toHaveLength(1))
    expect(created[0]).toEqual({
      file: `blob-sha256-${"b".repeat(64)}`,
      name: name.value,
    })
    expect(JSON.parse(uploaded[0]).pages).toHaveLength(1)
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: "/data/$authority/$pkg/$name/$id",
        params: {
          authority: "ada.example.com",
          pkg: "ink",
          name: "note",
          id: "n1",
        },
      })
    )
  })

  it("leaves without asking when nothing changed", () => {
    renderCreate()
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(navigate).toHaveBeenCalledWith({
      to: "/data/$authority/$pkg/$name",
      params: { authority: "ada.example.com", pkg: "ink", name: "note" },
    })
  })

  it("asks before discarding a note whose name was typed", () => {
    renderCreate()
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), {
      target: { value: "Standup" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(navigate).not.toHaveBeenCalled()
    expect(screen.getByText("Discard this handwritten note?")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Discard" }))
    expect(navigate).toHaveBeenCalled()
    expect(created).toEqual([])
  })

  it("asks before discarding a drawn note, from Cancel or from Escape", async () => {
    renderCreate()
    fireEvent.click(screen.getByRole("button", { name: "Add page" }))

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    const ask = await screen.findByRole("dialog", {
      name: "Discard this handwritten note?",
    })
    fireEvent.click(within(ask).getByRole("button", { name: "Cancel" }))
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Discard this handwritten note?" })
      ).toBeNull()
    )
    expect(navigate).not.toHaveBeenCalled()
    expect(screen.getByText("Page 2 of 2")).toBeTruthy()

    fireEvent.keyDown(screen.getByRole("dialog", { name: "Drawing" }), {
      key: "Escape",
    })
    const again = await screen.findByRole("dialog", {
      name: "Discard this handwritten note?",
    })
    fireEvent.click(within(again).getByRole("button", { name: "Discard" }))
    expect(navigate).toHaveBeenCalledWith({
      to: "/data/$authority/$pkg/$name",
      params: { authority: "ada.example.com", pkg: "ink", name: "note" },
    })
    expect(created).toEqual([])
  })
})
