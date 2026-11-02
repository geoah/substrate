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
        clientId="https://workouts.example.org/client.json"
        packageId="ada.example.com/training"
      />
    </QueryClientProvider>
  )
  expect(
    screen.getByText(/workouts\.example\.org manages these collections/)
  ).toBeTruthy()
  expect(
    screen.getByText("https://workouts.example.org/client.json")
  ).toBeTruthy()
  expect(screen.getByText(/You can pause it and resume it/)).toBeTruthy()
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
  const takeover = () =>
    (fetch.mock.calls as unknown as [string, RequestInit][]).find(
      ([path]) => path === "/api/v1/vocabulary/takeover"
    )
  await vi.waitFor(() => expect(takeover()).toBeTruthy())
  const [, init] = takeover()!
  expect(init.method).toBe("POST")
  expect(JSON.parse(String(init.body))).toEqual({
    package: "ada.example.com/training",
    confirm: "ada.example.com/training",
  })
})

it("words an app's agent as the app's to keep, and its pause as the off switch", () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("{}", { status: 404 }))
  )
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AppManagedSection
        app="app:workouts.example.org"
        packageId="ada.example.com/trainingcoach"
        agent
      />
    </QueryClientProvider>
  )
  expect(screen.getByText(/keeps this agent: its instructions/)).toBeTruthy()
  expect(screen.getByText(/Pause it to turn it off/)).toBeTruthy()
  expect(screen.queryByText(/manages these collections/)).toBeNull()
})
