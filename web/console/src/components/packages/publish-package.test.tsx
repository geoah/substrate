// @vitest-environment jsdom
/** Publishing a package of the repository's own and releasing one it
 * publishes: the plan is stated before the confirm, the confirm waits for
 * DNS to show the authority's record and for nothing to stop it, and a
 * refusal is shown in the server's words. */

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

import { discoveryQueryOptions } from "@/lib/api/discovery"
import { releasesQueryOptions } from "@/lib/api/publish"
import { clearSession, saveSession } from "@/lib/api/session"
import type { PublishPlan, Release } from "@/lib/api/types"

import { PackagePublishActions } from "./publish-package"

const RECORD =
  "v=substrate1 doc=https://substrate.example.com/published/rnd-0123/ada.example.com"

const RELEASE_1: Release = {
  package: "ada.example.com/recipes",
  version: 1,
  sha256: "a".repeat(64),
  releasedAt: "2026-11-14T12:00:00Z",
}

function plan(proven: boolean, blockers: string[] = []): PublishPlan {
  return {
    package: "~/recipes",
    published: "ada.example.com/recipes",
    kinds: ["ada.example.com/recipes/recipe", "ada.example.com/recipes/step"],
    packages: ["~/mealplans"],
    records: 12,
    references: 3,
    blockers,
    name: "_substrate.ada.example.com",
    record: RECORD,
    documentURL:
      "https://substrate.example.com/published/rnd-0123/ada.example.com",
    proven,
    ...(proven
      ? {}
      : {
          reason: `no TXT record opening with "v=substrate1" at _substrate.ada.example.com`,
        }),
  }
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status })
}

function renderActions(authority: string, pkg: string): QueryClient {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <PackagePublishActions authority={authority} pkg={pkg} />
    </QueryClientProvider>
  )
  return client
}

/** Until the query's answer is in, an absent button proves nothing. */
async function settled(client: QueryClient, queryKey: readonly unknown[]) {
  await waitFor(() =>
    expect(client.getQueryState(queryKey)?.status).toBe("success")
  )
}

describe("publishing a package", () => {
  const fetchMock = vi.fn<typeof fetch>()
  let features: Array<{ name: string; stability: string; revision: number }>
  let proven: boolean
  let blockers: string[]
  let publishAnswer: Response | null
  const confirms: unknown[] = []

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    saveSession("substrate_tok_current", "ada.example.org", "tok-1")
    features = [{ name: "publishing", stability: "experimental", revision: 1 }]
    proven = false
    blockers = []
    publishAnswer = null
    confirms.length = 0
    fetchMock.mockImplementation(async (url, init) => {
      const method = init?.method ?? "GET"
      const path = String(url)
      const body = init?.body ? JSON.parse(String(init.body)) : undefined
      if (path === "/.well-known/substrate/server.json") {
        return json(200, { features })
      }
      if (path === "/api/v1/packages/publish" && method === "POST") {
        if (!body.confirm) return json(200, plan(proven, blockers))
        confirms.push(body)
        return (
          publishAnswer ?? json(200, { ...plan(true, []), release: RELEASE_1 })
        )
      }
      return json(404, { error: { code: "not_found", message: path } })
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  async function preview() {
    fireEvent.click(await screen.findByRole("button", { name: "Publish…" }))
    const dialog = within(await screen.findByRole("dialog"))
    fireEvent.change(dialog.getByLabelText("Authority"), {
      target: { value: "Ada.Example.com" },
    })
    fireEvent.click(dialog.getByRole("button", { name: "Preview" }))
    await dialog.findByText("What publishing does")
    return dialog
  }

  function confirmButton() {
    return screen.getByRole<HTMLButtonElement>("button", {
      name: "Publish as ada.example.com/recipes",
    })
  }

  it("states what the publish does before the confirm, and holds the confirm until DNS shows the record", async () => {
    renderActions("~", "recipes")
    const dialog = await preview()

    // The plan, labeled: the new name, that it is for good, what moves.
    const facts = within(
      dialog.getByRole("region", { name: "What publishing does" })
    )
    expect(facts.getByText("Published as")).toBeTruthy()
    expect(
      facts.getAllByText("ada.example.com/recipes").length
    ).toBeGreaterThan(0)
    expect(facts.getByText("This is permanent.")).toBeTruthy()
    expect(facts.getByText(/can’t be unpublished/)).toBeTruthy()
    expect(facts.getByText("~/recipes/recipe")).toBeTruthy()
    expect(facts.getByText("ada.example.com/recipes/step")).toBeTruthy()
    expect(
      facts.getByText("12 records, each keeping its id and properties")
    ).toBeTruthy()
    expect(facts.getByText(/^3 records elsewhere point at them/)).toBeTruthy()
    expect(facts.getByText("~/mealplans")).toBeTruthy()

    // The DNS step: the record to copy, and that it is not seen yet.
    const proof = within(
      dialog.getByRole("region", { name: "Prove the authority" })
    )
    expect(proof.getByText("_substrate.ada.example.com")).toBeTruthy()
    expect(proof.getByText(RECORD)).toBeTruthy()
    expect(
      proof.getByRole("button", { name: "Copy the record value" })
    ).toBeTruthy()
    expect(proof.getByText("Not found yet")).toBeTruthy()
    expect(proof.getByText(/no TXT record opening with/)).toBeTruthy()

    expect(confirmButton().disabled).toBe(true)
    expect(confirms).toEqual([])

    proven = true
    fireEvent.click(proof.getByRole("button", { name: "Check again" }))
    expect(await proof.findByText("Record found")).toBeTruthy()
    await waitFor(() => expect(confirmButton().disabled).toBe(false))

    fireEvent.click(confirmButton())
    expect(
      await screen.findByRole("heading", {
        name: "Published as ada.example.com/recipes",
      })
    ).toBeTruthy()
    expect(confirms).toEqual([
      { package: "~/recipes", authority: "ada.example.com", confirm: true },
    ])
    const done = within(await screen.findByRole("dialog"))
    expect(done.getByText("Version")).toBeTruthy()
    expect(done.getByText("1")).toBeTruthy()
    expect(done.getByText("a".repeat(64))).toBeTruthy()
  })

  it("lists what stops the publish and keeps the confirm off while anything does", async () => {
    proven = true
    blockers = [
      "~/recipes/recipe: 2 records hold a value the published kind refuses",
    ]
    renderActions("~", "recipes")
    const dialog = await preview()
    const stops = within(dialog.getByRole("region", { name: "What stops it" }))
    expect(stops.getByText(blockers[0])).toBeTruthy()
    expect(confirmButton().disabled).toBe(true)
    expect(
      dialog.getByText("Publishing waits until nothing stops it.")
    ).toBeTruthy()
  })

  it("shows the server's refusal when the publish is forbidden", async () => {
    proven = true
    publishAnswer = json(403, {
      error: {
        code: "forbidden",
        message:
          "publishing ~/recipes as ada.example.com/recipes needs its authority's proof: the record at _substrate.ada.example.com names another document",
      },
    })
    renderActions("~", "recipes")
    await preview()
    await waitFor(() => expect(confirmButton().disabled).toBe(false))
    fireEvent.click(confirmButton())
    expect((await screen.findByRole("alert")).textContent).toContain(
      "needs its authority's proof: the record at _substrate.ada.example.com names another document"
    )
  })

  it("previews again for an authority typed after the plan", async () => {
    proven = true
    renderActions("~", "recipes")
    const dialog = await preview()
    fireEvent.change(dialog.getByLabelText("Authority"), {
      target: { value: "bo.example.com" },
    })
    expect(dialog.queryByText("What publishing does")).toBeNull()
    expect(
      dialog.getByRole<HTMLButtonElement>("button", { name: "Publish" })
        .disabled
    ).toBe(true)
  })

  it("is not offered by a server without the publishing feature", async () => {
    features = []
    const client = renderActions("~", "recipes")
    await settled(client, discoveryQueryOptions.queryKey)
    expect(screen.queryByRole("button", { name: "Publish…" })).toBeNull()
  })
})

describe("releasing a package", () => {
  const fetchMock = vi.fn<typeof fetch>()
  let releaseAnswer: Response | null

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    saveSession("substrate_tok_current", "ada.example.org", "tok-1")
    releaseAnswer = null
    fetchMock.mockImplementation(async (url, init) => {
      const method = init?.method ?? "GET"
      const path = String(url)
      if (path === "/.well-known/substrate/server.json") {
        return json(200, {
          features: [
            { name: "publishing", stability: "experimental", revision: 1 },
          ],
        })
      }
      if (path === "/api/v1/packages/releases" && method === "GET") {
        return json(200, { items: [RELEASE_1] })
      }
      if (path === "/api/v1/packages/release" && method === "POST") {
        return (
          releaseAnswer ??
          json(200, { ...RELEASE_1, version: 2, sha256: "b".repeat(64) })
        )
      }
      return json(404, { error: { code: "not_found", message: path } })
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("states what a release does and answers with the version written", async () => {
    renderActions("ada.example.com", "recipes")
    fireEvent.click(await screen.findByRole("button", { name: "New release…" }))
    const dialog = within(await screen.findByRole("dialog"))
    expect(dialog.getByText(/definitions as they are now/)).toBeTruthy()
    expect(dialog.getByText(/nothing changed since version 1/)).toBeTruthy()
    expect(dialog.getByText("Last release")).toBeTruthy()
    fireEvent.click(
      dialog.getByRole("button", { name: "Write the next release" })
    )
    expect(await screen.findByText("Release 2 written")).toBeTruthy()
    expect(screen.getByText("b".repeat(64))).toBeTruthy()
  })

  it("shows the server's refusal of a release with nothing changed", async () => {
    releaseAnswer = json(409, {
      error: {
        code: "conflict",
        message:
          "ada.example.com/recipes has not changed since version 1, its last release",
      },
    })
    renderActions("ada.example.com", "recipes")
    fireEvent.click(await screen.findByRole("button", { name: "New release…" }))
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Write the next release",
      })
    )
    expect((await screen.findByRole("alert")).textContent).toContain(
      "has not changed since version 1"
    )
  })

  it("offers nothing for a package this repository does not publish", async () => {
    const client = renderActions("substrate.reamde.dev", "health")
    await settled(client, releasesQueryOptions.queryKey)
    expect(screen.queryByRole("button", { name: /Release/ })).toBeNull()
  })
})
