// @vitest-environment jsdom
/** The owner's way out of a document-published package (decision 0212):
 * the section says it follows its publisher, and Take over runs only once
 * the package's identity is typed, sending it twice. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import { PublishedPackageSection } from "./published-package"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
}))

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

it("takes a published package over only once its identity is typed", async () => {
  const fetch = vi.fn(
    async () =>
      new Response(
        JSON.stringify({
          kind: "substrate.reamde.dev/core/package",
          id: "substrate.reamde.dev/training",
          properties: { declaredBy: "console" },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      )
  )
  vi.stubGlobal("fetch", fetch)
  render(
    <QueryClientProvider client={new QueryClient()}>
      <PublishedPackageSection
        publisher="substrate.reamde.dev"
        packageId="substrate.reamde.dev/training"
      />
    </QueryClientProvider>
  )
  expect(
    screen.getByText(/follows substrate\.reamde\.dev’s releases/)
  ).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Take over…" }))
  const confirm = screen.getByRole("button", { name: "Take over" })
  expect((confirm as HTMLButtonElement).disabled).toBe(true)
  fireEvent.change(screen.getByLabelText("The package to take over"), {
    target: { value: "training" },
  })
  expect((confirm as HTMLButtonElement).disabled).toBe(true)
  fireEvent.change(screen.getByLabelText("The package to take over"), {
    target: { value: "substrate.reamde.dev/training" },
  })
  expect((confirm as HTMLButtonElement).disabled).toBe(false)
  fireEvent.click(confirm)
  const takeover = () =>
    (fetch.mock.calls as unknown as [string, RequestInit][]).find(
      ([path]) => path === "/api/v1/vocabulary/takeover"
    )
  await vi.waitFor(() => expect(takeover()).toBeTruthy())
  const [, init] = takeover()!
  expect(init.method).toBe("POST")
  expect(JSON.parse(String(init.body))).toEqual({
    package: "substrate.reamde.dev/training",
    confirm: "substrate.reamde.dev/training",
  })
})
