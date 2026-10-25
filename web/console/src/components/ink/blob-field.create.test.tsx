// @vitest-environment jsdom
/** A handwritten note started in the new-record sheet: the blank note is
 * named after the heading typed above it, as the record page names a note it
 * draws, and the row says the file's name, never its digest. */

import { useState } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
  useNavigate: () => vi.fn(),
}))

import { CreateSheet } from "@/components/record/create-sheet"
import { KINDS, kindOf } from "@/components/record/create-sheet.kinds"
import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import { templateYAML } from "@/lib/record-yaml"
import { SHIPPED_UNITS } from "@/lib/unit-graph.test-fixtures"

const NOTE = kindOf("samples.substrate.reamde.dev/ink/note")
const DIGEST = `blob-sha256-${"a".repeat(64)}`

/** The names the blob store was handed, one per upload. */
let uploads: string[] = []

beforeEach(() => {
  uploads = []
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init?: RequestInit) => {
      if (!input.startsWith("/api/v1/blobs") || init?.method !== "PUT")
        throw new Error(`unexpected ${init?.method ?? "GET"} ${input}`)
      const name = new URL(input, "http://x").searchParams.get("name") ?? ""
      uploads.push(name)
      const size = (init.body as Blob).size
      return new Response(
        JSON.stringify({ digest: DIGEST, size, name, status: "stored" }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      )
    })
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function Draft() {
  const [text, setText] = useState(() => templateYAML(SHIPPED_UNITS, NOTE))
  return (
    <CreateSheet text={text} kind={NOTE} kinds={KINDS} onChange={setText} />
  )
}

function renderDraft() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, enabled: false } },
  })
  return render(
    <ConsolePreferencesContext.Provider
      value={{
        preferences: {
          collapsed: [],
          favorites: [],
          sidebarOpen: true,
          ...DEFAULT_SETTINGS,
          technicalDetails: false,
        },
        busy: false,
        change: () => {},
        set: () => {},
      }}
    >
      <QueryClientProvider client={client}>
        <Draft />
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
}

const fileRow = () => document.querySelector("[data-property=file]")

describe("a blank note started in the create sheet", () => {
  it("shows the file's name on its row and in the panel, never its digest", async () => {
    renderDraft()
    fireEvent.click(screen.getByRole("button", { name: /^File .*edit$/ }))
    fireEvent.click(screen.getByRole("button", { name: /Start a blank note/ }))
    await waitFor(() => expect(uploads).toHaveLength(1))
    const name = uploads[0]

    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(fileRow()?.textContent).toContain(name))
    expect(fileRow()?.textContent).not.toContain("blob-sha256-")

    // The panel opened again has no upload of its own, and still says the
    // name.
    fireEvent.click(screen.getByRole("button", { name: /^File .*edit$/ }))
    const field = document.querySelector("[data-slot=blob-field]")
    expect(field?.textContent).toContain(name)
    expect(field?.textContent).not.toContain("blob-sha256-")
  })

  it("is named after the heading, as the record page names a note it draws", async () => {
    renderDraft()
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), {
      target: { value: "Blank page for tomorrow" },
    })
    fireEvent.click(screen.getByRole("button", { name: /^File .*edit$/ }))
    fireEvent.click(screen.getByRole("button", { name: /Start a blank note/ }))
    await waitFor(() =>
      expect(uploads).toEqual(["Blank page for tomorrow.inkd"])
    )
  })

  it("takes the property's name while the heading is empty", async () => {
    renderDraft()
    fireEvent.click(screen.getByRole("button", { name: /^File .*edit$/ }))
    fireEvent.click(screen.getByRole("button", { name: /Start a blank note/ }))
    await waitFor(() => expect(uploads).toEqual(["file.inkd"]))
  })
})
