// @vitest-environment jsdom
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

import { clearSession, saveSession } from "@/lib/api/session"

vi.mock("@/router", () => ({
  authorizeRoute: { useParams: () => ({ request: "req-1" }) },
}))

import { AuthorizePage } from "./authorize"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

const PATH = "/api/v1/authorizations/req-1"

const TASKS = {
  scope: "read:ada.example.com/tasks/*",
  requested: "read:~/tasks/*",
  write: false,
  pattern: "ada.example.com/tasks/*",
  glob: true,
  kinds: [
    {
      identity: "ada.example.com/tasks/task",
      label: { singular: "Task", plural: "Tasks" },
    },
    { identity: "ada.example.com/tasks/project" },
  ],
}
const NOTES = {
  scope: "write:ada.example.com/notes/note",
  requested: "write:ada.example.com/notes/note",
  write: true,
  pattern: "ada.example.com/notes/note",
  glob: false,
  kinds: [],
}
const UNITS = {
  scope: "read:substrate.reamde.dev/core/unit",
  requested: "read:substrate.reamde.dev/core/unit",
  write: false,
  pattern: "substrate.reamde.dev/core/unit",
  glob: false,
  kinds: [{ identity: "substrate.reamde.dev/core/unit" }],
}

const REQUEST = {
  id: "req-1",
  app: {
    clientId: "https://notes.example.com/oauth/client.json",
    host: "notes.example.com",
    local: false,
    name: "Notes for Ada",
    uri: "https://notes-app.example.org",
    uriHostDiffers: true,
  },
  redirect: {
    uri: "https://callback.example.net/oauth/callback",
    host: "callback.example.net",
    hostDiffers: true,
  },
  repository: "ada.example.com",
  scopes: [TASKS, NOTES, UNITS],
  lifetimeDays: [7, 30, 90],
  defaultLifetimeDays: 30,
  expiresAt: "2026-10-31T12:10:00Z",
}

const LOCAL_REQUEST = {
  ...REQUEST,
  app: {
    clientId: "http://127.0.0.1:8765/client",
    host: "127.0.0.1:8765",
    local: true,
  },
  redirect: {
    uri: "http://127.0.0.1:8765/callback",
    host: "127.0.0.1:8765",
  },
  scopes: [TASKS],
}

const APPROVED =
  "https://callback.example.net/oauth/callback?code=c0de&state=s&iss=https%3A%2F%2Fsubstrate.example.com"
const DENIED =
  "https://callback.example.net/oauth/callback?error=access_denied&state=s&iss=https%3A%2F%2Fsubstrate.example.com"

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <AuthorizePage />
    </QueryClientProvider>
  )
}

describe("AuthorizePage", () => {
  const fetchMock = vi.fn<typeof fetch>()
  const assign = vi.fn()
  /** What each route answers; a test overrides one to change the story. */
  let answers: Record<string, () => Response>

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    // jsdom's Location is unforgeable, so the page's navigation is observed
    // through a stand-in.
    vi.stubGlobal("location", { ...window.location, assign })
    clearSession()
    saveSession("substrate_tok_owner", "ada.example.com", "tok-owner")
    answers = {
      GET: () => jsonResponse(200, REQUEST),
      POST: () => jsonResponse(200, { redirect: APPROVED }),
      DELETE: () => jsonResponse(200, { redirect: DENIED }),
    }
    fetchMock.mockImplementation(async (url, init) => {
      const method = init?.method ?? "GET"
      if (String(url) !== PATH) return jsonResponse(404, {})
      return answers[method]()
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
    assign.mockReset()
  })

  const calls = (method: string) =>
    fetchMock.mock.calls.filter(
      ([url, init]) =>
        String(url) === PATH && (init?.method ?? "GET") === method
    )

  it("offers another try when the read failed for a reason that passes, never for a 404", async () => {
    answers.GET = () =>
      jsonResponse(503, { error: { code: "unavailable", message: "busy" } })
    renderPage()
    const again = await screen.findByRole("button", { name: "Try again" })
    answers.GET = () => jsonResponse(200, REQUEST)
    fireEvent.click(again)
    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "notes.example.com",
      })
    ).toBeTruthy()

    cleanup()
    answers.GET = () =>
      jsonResponse(404, { error: { code: "not_found", message: "gone" } })
    renderPage()
    await screen.findByText("This request has expired or was already answered.")
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull()
  })

  it("shows who asks, where the code goes, the repository and every scope", async () => {
    renderPage()
    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "notes.example.com",
      })
    ).toBeTruthy()
    expect(
      screen.getByText("https://notes.example.com/oauth/client.json")
    ).toBeTruthy()
    // The bearer is the owner's own: the read is not anonymous.
    const [, init] = calls("GET")[0]
    expect(
      ((init as RequestInit).headers as Record<string, string>).Authorization
    ).toBe("Bearer substrate_tok_owner")

    // What the app claims, as text and never as a link, with the warning.
    const claims = screen.getByRole("region", { name: "The app says it is" })
    expect(within(claims).getByText("Notes for Ada")).toBeTruthy()
    expect(
      within(claims).getByText("https://notes-app.example.org")
    ).toBeTruthy()
    expect(within(claims).queryByRole("link")).toBeNull()
    expect(within(claims).getByRole("note").textContent).toMatch(
      /not on notes\.example\.com/
    )

    const sends = screen.getByRole("region", { name: "Sends the access to" })
    expect(
      within(sends).getByText("https://callback.example.net/oauth/callback")
    ).toBeTruthy()
    expect(within(sends).getByRole("note").textContent).toMatch(
      /goes to callback\.example\.net, a different host/
    )

    const repository = screen.getByRole("region", { name: "Repository" })
    expect(within(repository).getByText("ada.example.com")).toBeTruthy()

    // No local-app warning for an app with a fetched document.
    expect(
      screen.queryByText("This app says it runs on this computer")
    ).toBeNull()

    const access = screen.getByRole("region", { name: "Requested access" })
    const tasks = within(access).getByRole<HTMLInputElement>("checkbox", {
      name: "Read records of ada.example.com/tasks/*",
    })
    const notes = within(access).getByRole<HTMLInputElement>("checkbox", {
      name: "Read and write records of ada.example.com/notes/note",
    })
    expect(tasks.checked).toBe(true)
    expect(notes.checked).toBe(true)
    // The kinds a scope reaches today, label where the kind has one.
    expect(within(access).getByText("Tasks")).toBeTruthy()
    expect(within(access).getByText("ada.example.com/tasks/task")).toBeTruthy()
    expect(
      within(access).getByText("ada.example.com/tasks/project")
    ).toBeTruthy()
    expect(
      within(access).getByText(/reaches no kind in your repository today/)
    ).toBeTruthy()
    // A glob says it covers kinds added later; a `~` says what it became.
    expect(
      within(access).getByText(/also covers kinds you add later/)
    ).toBeTruthy()
    expect(within(access).getByText("read:~/tasks/*")).toBeTruthy()
    // A write scope is asked for, so the page says what a write sets off.
    const writes = within(access)
      .getByText("A write by this app counts as a write by you")
      .closest("[role=note]") as HTMLElement
    expect(writes.textContent).toContain("It can start your triggers")
    expect(writes.textContent).toContain(
      "call outside services that charge you"
    )
    expect(writes.textContent).toContain(
      "change or delete records outside these scopes, through a mapping or a reference that cascades"
    )

    const lasts = screen.getByRole("radiogroup", { name: "Access lasts" })
    expect(
      within(lasts)
        .getByRole("radio", { name: "30 days" })
        .getAttribute("aria-checked")
    ).toBe("true")
  })

  it("warns that a local app cannot be verified", async () => {
    answers.GET = () => jsonResponse(200, LOCAL_REQUEST)
    renderPage()
    await screen.findByRole("heading", { level: 1, name: "127.0.0.1:8765" })
    expect(
      screen.getByText("This app says it runs on this computer")
    ).toBeTruthy()
    expect(
      screen.getByText(/Approve only if you started it yourself/)
    ).toBeTruthy()
    // Its one scope reads, so nothing is said about what a write sets off.
    expect(
      screen.queryByText("A write by this app counts as a write by you")
    ).toBeNull()
    // A local app's document is never fetched, so it claims nothing.
    expect(
      screen.queryByRole("region", { name: "The app says it is" })
    ).toBeNull()
  })

  it("refuses to approve with no scope checked", async () => {
    renderPage()
    const approve = await screen.findByRole<HTMLButtonElement>("button", {
      name: "Approve",
    })
    expect(approve.disabled).toBe(false)
    for (const box of screen.getAllByRole("checkbox")) fireEvent.click(box)
    expect(approve.disabled).toBe(true)
    expect(
      screen.getByText("Check at least one, or deny the request.")
    ).toBeTruthy()
    fireEvent.click(approve)
    expect(calls("POST")).toHaveLength(0)
  })

  it("approves exactly the checked scopes for the chosen lifetime, then goes to the app", async () => {
    renderPage()
    fireEvent.click(
      await screen.findByRole("checkbox", {
        name: "Read and write records of ada.example.com/notes/note",
      })
    )
    fireEvent.click(screen.getByRole("radio", { name: "90 days" }))
    fireEvent.click(screen.getByRole("button", { name: "Approve" }))

    await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
    expect(calls("POST")).toHaveLength(1)
    const [, init] = calls("POST")[0]
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      scopes: [
        "read:ada.example.com/tasks/*",
        "read:substrate.reamde.dev/core/unit",
      ],
      lifetimeDays: 90,
    })
    expect(calls("DELETE")).toHaveLength(0)
  })

  it("approves every scope for the default lifetime when nothing is changed", async () => {
    renderPage()
    fireEvent.click(await screen.findByRole("button", { name: "Approve" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
    const [, init] = calls("POST")[0]
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      scopes: [TASKS.scope, NOTES.scope, UNITS.scope],
      lifetimeDays: 30,
    })
  })

  it("denies, then goes to the app with the refusal", async () => {
    renderPage()
    fireEvent.click(await screen.findByRole("button", { name: "Deny" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith(DENIED))
    expect(calls("DELETE")).toHaveLength(1)
    expect(calls("POST")).toHaveLength(0)
  })

  it("shows a refused approval and stays on the page", async () => {
    answers.POST = () =>
      jsonResponse(400, {
        error: {
          code: "bad_request",
          message: "scope write:ada.example.com/notes/note was not requested",
        },
      })
    renderPage()
    fireEvent.click(await screen.findByRole("button", { name: "Approve" }))
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("This request can’t be approved.")
    expect(alert.textContent).toContain(
      "scope write:ada.example.com/notes/note was not requested"
    )
    expect(assign).not.toHaveBeenCalled()
    // The request is still pending on the server, so the owner may try again.
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Approve" })
        .disabled
    ).toBe(false)
  })

  it("does not follow an answer that is not a web address", async () => {
    answers.POST = () => jsonResponse(200, { redirect: "javascript:alert(1)" })
    renderPage()
    fireEvent.click(await screen.findByRole("button", { name: "Approve" }))
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("not a web page")
    expect(assign).not.toHaveBeenCalled()
  })

  it("says an expired request is gone and tells the owner to start again", async () => {
    answers.GET = () =>
      jsonResponse(404, {
        error: {
          code: "not_found",
          message: "authorization request not found",
        },
      })
    renderPage()
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain(
      "This request has expired or was already answered."
    )
    expect(alert.textContent).toContain("start again")
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull()
    expect(assign).not.toHaveBeenCalled()
  })

  it("says an answer to a request that lapsed meanwhile cannot land, and stops offering it", async () => {
    answers.DELETE = () =>
      jsonResponse(404, {
        error: {
          code: "not_found",
          message: "authorization request not found",
        },
      })
    renderPage()
    fireEvent.click(await screen.findByRole("button", { name: "Deny" }))
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("expired or was already answered")
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Approve" })
        .disabled
    ).toBe(true)
    expect(assign).not.toHaveBeenCalled()
  })

  it("shows the server's message when the request cannot be read", async () => {
    answers.GET = () =>
      jsonResponse(502, {
        error: {
          code: "unavailable",
          message: "the app's client document answered 500",
        },
      })
    renderPage()
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("This request can’t be approved.")
    expect(alert.textContent).toContain(
      "the app's client document answered 500"
    )
    expect(assign).not.toHaveBeenCalled()
  })
})
