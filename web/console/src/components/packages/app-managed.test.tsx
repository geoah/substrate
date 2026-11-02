// @vitest-environment jsdom
/** The owner's way out of an app's package (decision 0205): the section
 * names the app, and Take over runs only once the package's identity is
 * typed, sending it twice. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import { AppManagedSection } from "./app-managed"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
}))

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

it("takes the package over only once its identity is typed", async () => {
  const fetch = vi.fn(
    async () =>
      new Response(
        JSON.stringify({
          kind: "substrate.reamde.dev/core/package",
          id: "ada.example.com/training",
          properties: { declaredBy: "console" },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      )
  )
  vi.stubGlobal("fetch", fetch)
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AppManagedSection
        app="app:workouts.example.org"
        packageId="ada.example.com/training"
      />
    </QueryClientProvider>
  )
  expect(
    screen.getByText(/workouts\.example\.org manages these collections/)
  ).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Take over…" }))
  const confirm = screen.getByRole("button", { name: "Take over" })
  expect((confirm as HTMLButtonElement).disabled).toBe(true)
  fireEvent.change(screen.getByLabelText("The package to take over"), {
    target: { value: "training" },
  })
  expect((confirm as HTMLButtonElement).disabled).toBe(true)
  fireEvent.change(screen.getByLabelText("The package to take over"), {
    target: { value: "ada.example.com/training" },
  })
  expect((confirm as HTMLButtonElement).disabled).toBe(false)
  fireEvent.click(confirm)
  await vi.waitFor(() => expect(fetch).toHaveBeenCalled())
  const [path, init] = fetch.mock.calls[0] as unknown as [string, RequestInit]
  expect(path).toBe("/api/v1/vocabulary/takeover")
  expect(init.method).toBe("POST")
  expect(JSON.parse(String(init.body))).toEqual({
    package: "ada.example.com/training",
    confirm: "ada.example.com/training",
  })
})
