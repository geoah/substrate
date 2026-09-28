// @vitest-environment jsdom
/** The record picker: with nothing typed it offers the records changed last;
 * once anything is typed it offers only what the search answers, so Enter
 * never picks a recent record the reader has typed past. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { Editor } from "@tiptap/core"
import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/api/http", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/http")>()
  return {
    ...actual,
    request: vi.fn((_method: string, path: string) => {
      if (path.includes("q=")) return new Promise(() => {})
      if (path.includes("orderBy=")) {
        return Promise.resolve({
          records: [
            {
              id: "notes",
              kind: "ada.example.com/tasks/task",
              properties: { title: "Draft the notes" },
            },
          ],
        })
      }
      return Promise.resolve({ records: [] })
    }),
  }
})

import { request } from "@/lib/api/http"
import { RecordMenu } from "./menus"
import type { MenuKeys } from "./suggestion-menu"

const editor = { storage: { recordLink: {} } } as unknown as Editor

function renderMenu(query: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const keysRef: MenuKeys = { current: null }
  const menu = (q: string) => (
    <QueryClientProvider client={client}>
      <RecordMenu
        editor={editor}
        range={{ from: 1, to: 1 }}
        query={q}
        keysRef={keysRef}
      />
    </QueryClientProvider>
  )
  const view = render(menu(query))
  return { keysRef, retype: (q: string) => view.rerender(menu(q)) }
}

afterEach(cleanup)

describe("RecordMenu", () => {
  it("offers the records changed last, then only the search's", async () => {
    const { keysRef, retype } = renderMenu("")
    expect(
      (await screen.findByRole("option", { name: /Draft the notes/ }))
        .textContent
    ).toContain("Draft the notes")
    retype("gra")
    expect(screen.queryByRole("option")).toBeNull()
    expect(screen.getByText("Loading records…")).toBeTruthy()
    // Nothing on screen, so Enter is the editor's again.
    expect(
      keysRef.current?.(new KeyboardEvent("keydown", { key: "Enter" }))
    ).toBe(false)
  })

  it("reaches the primary kinds alone, recent and searched", async () => {
    vi.mocked(request).mockClear()
    const { retype } = renderMenu("")
    await screen.findByRole("option", { name: /Draft the notes/ })
    retype("gra")
    const paths = () => vi.mocked(request).mock.calls.map((c) => String(c[1]))
    await waitFor(() =>
      expect(paths().some((p) => p.includes("q="))).toBe(true)
    )
    // The recent read and the search; the registry read carries no purpose.
    const reads = paths().filter((p) => /[?&](q|orderBy)=/.test(p))
    const filters = reads.map((p) =>
      new URL(p, "http://localhost").searchParams.get("filter")
    )
    expect(filters.map((f) => f && JSON.parse(f))).toEqual([
      { purposes: ["primary"] },
      { purposes: ["primary"] },
    ])
  })
})
