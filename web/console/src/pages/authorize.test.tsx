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
const TECHNICAL_KEY = "substrate.console.technicalDetails"

const TASKS = {
  scope: "read:ada.example.com/tasks/*",
  requested: "read:~/tasks/*",
  verb: "read",
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
  agents: [],
}
const TASKS_WORDS = "See your tasks and projects, including kinds added later"
const NOTES = {
  scope: "write:ada.example.com/notes/note",
  requested: "write:ada.example.com/notes/note",
  verb: "write",
  write: true,
  pattern: "ada.example.com/notes/note",
  glob: false,
  kinds: [],
  agents: [],
}
const NOTES_WORDS = "See, add, change and delete your notes"
const UNITS = {
  scope: "read:substrate.reamde.dev/core/unit",
  requested: "read:substrate.reamde.dev/core/unit",
  verb: "read",
  write: false,
  pattern: "substrate.reamde.dev/core/unit",
  glob: false,
  kinds: [{ identity: "substrate.reamde.dev/core/unit" }],
  agents: [],
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
    actor: "app:notes.example.com",
  },
  redirect: {
    uri: "http://127.0.0.1:53021/oauth/callback",
    host: "127.0.0.1:53021",
    loopback: true,
  },
  repository: "ada.example.com",
  scopes: [TASKS, NOTES, UNITS],
  lifetimeDays: [7, 30, 90, 0],
  defaultLifetimeDays: 30,
  expiresAt: "2026-10-31T12:10:00Z",
  details: [],
  setup: [],
  state: "pending",
}

const LOCAL_REQUEST = {
  ...REQUEST,
  app: {
    clientId: "http://127.0.0.1:8765/client",
    host: "127.0.0.1:8765",
    local: true,
    actor: "app:127.0.0.1:8765",
  },
  redirect: {
    uri: "http://127.0.0.1:8765/callback",
    host: "127.0.0.1:8765",
    loopback: true,
  },
  scopes: [TASKS],
}

// Substrate Sync's request: one scope, and a package, an account and a
// webhook to set up.
const APPLEHEALTH = "providers.substrate.reamde.dev/applehealth"
const ACCOUNTS = {
  scope: `read:${APPLEHEALTH}/account`,
  requested: `read:${APPLEHEALTH}/account`,
  verb: "read",
  write: false,
  pattern: `${APPLEHEALTH}/account`,
  glob: false,
  kinds: [
    { identity: `${APPLEHEALTH}/account`, purpose: "internal", added: true },
  ],
  agents: [],
}
const DETAILS = [
  { type: "package", package: APPLEHEALTH, version: 5 },
  {
    type: "account",
    kind: `${APPLEHEALTH}/account`,
    id: "substrate-sync-1a2b3c4d",
    properties: { displayName: "Ada's iPhone", timezone: "Europe/London" },
  },
  { type: "webhook", trigger: "applehealth-webhook" },
]
const INSTALL = {
  type: "package",
  action: "install",
  package: {
    id: APPLEHEALTH,
    name: "Apple Health",
    description: "Keeps a copy of the Health data your iPhone sends.",
    tier: "provider",
    required: 5,
    held: 0,
    shipped: 5,
    requires: [
      {
        id: "substrate.reamde.dev/health",
        name: "Health",
        action: "install",
        held: 0,
        shipped: 2,
      },
    ],
    kinds: [
      {
        identity: `${APPLEHEALTH}/delivery`,
        label: { singular: "Delivery", plural: "Deliveries" },
        purpose: "internal",
      },
    ],
    functions: [
      {
        id: `${APPLEHEALTH}/applehealthingest`,
        writes: [`${APPLEHEALTH}/delivery`],
      },
    ],
    triggers: [
      {
        id: "applehealth-webhook",
        callable: `${APPLEHEALTH}/applehealthingest`,
        webhook: true,
        enabled: false,
      },
      { id: "applehealth-to-health", enabled: true },
    ],
  },
}
const CREATE = {
  type: "account",
  action: "create",
  account: {
    kind: `${APPLEHEALTH}/account`,
    id: "substrate-sync-1a2b3c4d",
    properties: { displayName: "Ada's iPhone", timezone: "Europe/London" },
  },
}
const ARM = {
  type: "webhook",
  action: "arm",
  webhook: {
    trigger: "applehealth-webhook",
    package: APPLEHEALTH,
    enabled: false,
    keyed: false,
  },
}
const SYNC = {
  ...REQUEST,
  app: {
    clientId: "https://sync.example.com/client.json",
    host: "sync.example.com",
    local: false,
    name: "Substrate Sync",
    actor: "app:sync.example.com",
  },
  redirect: {
    uri: "https://sync.example.com/callback",
    host: "sync.example.com",
  },
  scopes: [ACCOUNTS],
  details: DETAILS,
  setup: [INSTALL, CREATE, ARM],
  planHash: "plan-1",
}
const ACCOUNTS_WORDS = "See your Apple Health accounts"
const WEBHOOK_WORDS =
  "Send data to your repository through the Apple Health webhook, for any Apple Health account, and see which deliveries could not be filed"

const APPROVED =
  "https://callback.example.net/oauth/callback?code=c0de&state=s&iss=https%3A%2F%2Fsubstrate.example.com"
const DENIED =
  "https://callback.example.net/oauth/callback?error=access_denied&state=s&iss=https%3A%2F%2Fsubstrate.example.com"
const NEXT =
  "https://sync.example.com/callback?code=s3t&state=s&iss=https%3A%2F%2Fsubstrate.example.com"

function renderPage({ technical = false } = {}) {
  // The page reads the console's one Technical details switch, which this
  // browser keeps where the repository's preference record cannot.
  localStorage.setItem(TECHNICAL_KEY, JSON.stringify(technical))
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <AuthorizePage />
    </QueryClientProvider>
  )
}

const MODES = [
  { name: "everyday", technical: false },
  { name: "technical", technical: true },
] as const

describe("AuthorizePage", () => {
  const fetchMock = vi.fn<typeof fetch>()
  const assign = vi.fn()
  /** What each route answers; a test overrides one to change the story. */
  let answers: Record<string, () => Response>

  beforeEach(() => {
    // The preferences provider's sidebar asks whether the viewport is a
    // phone; jsdom implements no media queries.
    window.matchMedia = (media: string) =>
      ({
        media,
        matches: false,
        onchange: null,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }) as MediaQueryList
    vi.stubGlobal("fetch", fetchMock)
    // jsdom's Location is unforgeable, so the page's navigation is observed
    // through a stand-in.
    vi.stubGlobal("location", { ...window.location, assign })
    localStorage.clear()
    clearSession()
    saveSession("substrate_tok_owner", "ada.example.com", "tok-owner")
    answers = {
      GET: () => jsonResponse(200, REQUEST),
      POST: () => jsonResponse(200, { redirect: APPROVED }),
      DELETE: () => jsonResponse(200, { redirect: DENIED }),
    }
    fetchMock.mockImplementation(async (url, init) => {
      const method = init?.method ?? "GET"
      // Everything else the page reads (the preference record, the kind
      // registry) is absent here, which the console treats as defaults.
      if (String(url) !== PATH)
        return jsonResponse(404, {
          error: { code: "not_found", message: "not found" },
        })
      return answers[method]()
    })
  })

  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
    assign.mockReset()
  })

  const calls = (method: string) =>
    fetchMock.mock.calls.filter(
      ([url, init]) =>
        String(url) === PATH && (init?.method ?? "GET") === method
    )
  const body = (method: string, at = 0) =>
    JSON.parse((calls(method)[at][1] as RequestInit).body as string)

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

  it("leads with the app, the repository and what it will be able to do, in words", async () => {
    renderPage()
    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "notes.example.com",
      })
    ).toBeTruthy()
    expect(screen.getByText(/An app wants to connect to/).textContent).toBe(
      "An app wants to connect to ada.example.com"
    )
    // The name is the app's claim, under the host its client id proves.
    expect(screen.getByText(/It calls itself/).textContent).toBe(
      "It calls itself Notes for Ada"
    )
    // The bearer is the owner's own: the read is not anonymous.
    const [, init] = calls("GET")[0]
    expect(
      ((init as RequestInit).headers as Record<string, string>).Authorization
    ).toBe("Bearer substrate_tok_owner")

    // One warning about where the access goes; the protocol facts wait
    // behind the switch.
    const notes = screen.getAllByRole("note")
    expect(notes.map((n) => n.textContent)).toEqual([
      expect.stringMatching(
        /A program on this device receives the access.*Nothing confirms that program is notes\.example\.com/
      ),
      expect.stringContaining("Your automations run on its changes"),
    ])
    expect(
      screen.queryByText("https://notes.example.com/oauth/client.json")
    ).toBeNull()
    expect(
      screen.queryByText("http://127.0.0.1:53021/oauth/callback")
    ).toBeNull()
    expect(
      screen.queryByRole("region", { name: "The app says it is" })
    ).toBeNull()
    expect(
      screen.queryByRole("region", { name: "Its changes appear as" })
    ).toBeNull()

    // Each scope is a sentence beside its checkbox, never its pattern.
    const access = screen.getByRole("region", { name: "It will be able to" })
    for (const name of [TASKS_WORDS, NOTES_WORDS, "See your Core units"]) {
      expect(
        within(access).getByRole<HTMLInputElement>("checkbox", { name }).checked
      ).toBe(true)
    }
    expect(within(access).queryByText("ada.example.com/tasks/*")).toBeNull()
    expect(within(access).queryByText("ada.example.com/tasks/task")).toBeNull()

    // Nothing to set up, so nothing about a set up.
    expect(
      screen.queryByRole("region", { name: "It will set up on your server" })
    ).toBeNull()
    expect(screen.queryByText(/If you stop part way/)).toBeNull()
    expect(screen.getByRole("button", { name: "Approve" })).toBeTruthy()

    const lasts = screen.getByRole("radiogroup", { name: "Access lasts" })
    expect(
      within(lasts)
        .getByRole("radio", { name: "30 days" })
        .getAttribute("aria-checked")
    ).toBe("true")
    expect(screen.getByText(/^It ends on \d+ \w+/)).toBeTruthy()
  })

  it("shows who asks, where the code goes, the repository and every scope with what it reaches, technically", async () => {
    renderPage({ technical: true })
    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "notes.example.com",
      })
    ).toBeTruthy()
    expect(
      screen.getByText("https://notes.example.com/oauth/client.json")
    ).toBeTruthy()

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

    const sends = screen.getByRole("region", {
      name: "After you answer, you go to",
    })
    expect(
      within(sends).getByText("http://127.0.0.1:53021/oauth/callback")
    ).toBeTruthy()
    expect(within(sends).getByRole("note").textContent).toMatch(
      /A program on this device receives the access.*Nothing confirms that program is notes\.example\.com/
    )

    const repository = screen.getByRole("region", { name: "Repository" })
    expect(within(repository).getByText("ada.example.com")).toBeTruthy()

    // No local-app warning for an app with a fetched document.
    expect(
      screen.queryByText("This app says it runs on this device")
    ).toBeNull()

    const access = screen.getByRole("region", { name: "It will be able to" })
    const tasks = within(access).getByRole<HTMLInputElement>("checkbox", {
      name: TASKS_WORDS,
    })
    const notes = within(access).getByRole<HTMLInputElement>("checkbox", {
      name: NOTES_WORDS,
    })
    expect(tasks.checked).toBe(true)
    expect(notes.checked).toBe(true)
    // The scope as the token stores it, and the kinds it reaches today by
    // full reference, label where the kind has one.
    expect(within(access).getByText(TASKS.scope)).toBeTruthy()
    expect(within(access).getByText("Tasks")).toBeTruthy()
    expect(within(access).getByText("ada.example.com/tasks/task")).toBeTruthy()
    expect(
      within(access).getByText("ada.example.com/tasks/project")
    ).toBeTruthy()
    expect(
      within(access).getByText(/no kind in your repository matches/)
    ).toBeTruthy()
    // A glob says it covers kinds added later; a `~` says what it became.
    expect(
      within(access).getByText(/Also covers matching kinds you add later/)
    ).toBeTruthy()
    expect(within(access).getByText("read:~/tasks/*")).toBeTruthy()
    // A write scope is asked for, so the page names what its changes are
    // recorded as, and what they set off.
    const changes = screen.getByRole("region", {
      name: "Its changes appear as",
    })
    expect(within(changes).getByText("app:notes.example.com")).toBeTruthy()
    expect(within(changes).getByText(/never as yours/)).toBeTruthy()
    const automations = within(changes)
      .getByText("Your automations run on its changes")
      .closest("[role=note]") as HTMLElement
    expect(automations.textContent).toContain("run code you installed")
    expect(automations.textContent).toContain("call services that charge you")
    expect(automations.textContent).toContain(
      "change records this app cannot reach itself"
    )

    const lasts = screen.getByRole("radiogroup", { name: "Access lasts" })
    expect(
      within(lasts)
        .getByRole("radio", { name: "30 days" })
        .getAttribute("aria-checked")
    ).toBe("true")
    expect(
      within(lasts).getByRole("radio", { name: "Until you revoke it" })
    ).toBeTruthy()
  })

  describe.each(MODES)("agents and decide, $name", ({ technical }) => {
    const agent = {
      id: "ada.example.com/llm/substrateEditor",
      kind: "substrate.reamde.dev/core/agent",
      properties: {
        permissions: { reads: { kinds: ["ada.example.com/tasks/task"] } },
      },
      labels: {},
      version: 1,
      createdAt: "2026-10-01T00:00:00Z",
      updatedAt: "2026-10-01T00:00:00Z",
    }
    const agents = {
      scope: "agents:ada.example.com/llm/*",
      requested: "agents:~/llm/*",
      verb: "agents",
      write: false,
      pattern: "ada.example.com/llm/*",
      glob: true,
      kinds: [],
      agents: [agent],
    }
    const decide = {
      ...agents,
      scope: "decide:ada.example.com/llm/substrateEditor",
      requested: "decide:ada.example.com/llm/substrateEditor",
      verb: "decide",
      pattern: "ada.example.com/llm/substrateEditor",
      glob: false,
    }
    const nobody = {
      ...decide,
      scope: "decide:b.example.com/x/*",
      pattern: "b.example.com/x/*",
      glob: true,
      agents: [],
    }

    it("says what talking to an agent and deciding its changes allow", async () => {
      answers.GET = () =>
        jsonResponse(200, { ...REQUEST, scopes: [agents, decide, nobody] })
      renderPage({ technical })
      const talk = await screen.findByRole("checkbox", {
        name: "Talk to Substrate editor, including agents added later; read every chat with them, yours included, and rename or stop any of those chats",
      })
      expect(
        screen.getByRole("checkbox", {
          name: "Apply or dismiss the changes Substrate editor suggests, and edit one before applying it, as you",
        })
      ).toBeTruthy()
      expect(
        screen.getByRole("checkbox", {
          name: "Apply or dismiss the changes the agents in X suggest, including agents added later, and edit one before applying it, as you",
        })
      ).toBeTruthy()
      // Both verbs write under the app's name, which sets off automations.
      expect(
        screen.getByText("Your automations run on its changes")
      ).toBeTruthy()

      const row = talk.closest("li") as HTMLElement
      if (!technical) {
        expect(within(row).queryByText("Can see")).toBeNull()
        return
      }
      // The technical view names each agent and what it may do.
      expect(
        within(row).getByText("Also covers matching agents you add later.")
      ).toBeTruthy()
      expect(within(row).getByText("Can see")).toBeTruthy()
      expect(within(row).getByText("Tasks")).toBeTruthy()
      expect(within(row).getByText("Can change")).toBeTruthy()
      expect(within(row).getByText("Nothing")).toBeTruthy()
      expect(
        screen.getByText("Nothing yet: no agent in your repository matches.")
      ).toBeTruthy()
      expect(
        screen.getByRole("region", { name: "Its changes appear as" })
      ).toBeTruthy()
    })
  })

  it.each(MODES)(
    "warns that a local app cannot be verified, $name",
    async ({ technical }) => {
      answers.GET = () => jsonResponse(200, LOCAL_REQUEST)
      renderPage({ technical })
      await screen.findByRole("heading", {
        level: 1,
        name: "An app on this device",
      })
      expect(
        screen.getByText("This app says it runs on this device")
      ).toBeTruthy()
      expect(
        screen.getByText(/Approve only if you started it yourself/)
      ).toBeTruthy()
      // The local-app warning already says it; the loopback one is not
      // repeated.
      expect(
        screen.queryByText("A program on this device receives the access")
      ).toBeNull()
      // Its one scope reads, so nothing is said about what a write sets off.
      expect(
        screen.queryByText("Your automations run on its changes")
      ).toBeNull()
      // A local app's document is never fetched, so it claims nothing.
      expect(screen.queryByText(/It calls itself/)).toBeNull()
      expect(
        screen.queryByRole("region", { name: "The app says it is" })
      ).toBeNull()
      // Only the technical view names the address it runs at.
      expect(screen.queryByText("http://127.0.0.1:8765/client") !== null).toBe(
        technical
      )
    }
  )

  it.each(MODES)(
    "refuses to approve with no scope checked, $name",
    async ({ technical }) => {
      renderPage({ technical })
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
    }
  )

  it("approves until revoked as lifetime 0, and says the access lasts until then", async () => {
    renderPage()
    fireEvent.click(
      await screen.findByRole("radio", { name: "Until you revoke it" })
    )
    expect(
      screen.getByText("It lasts until you revoke it in Settings, under Apps.")
    ).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Approve" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
    expect(body("POST").lifetimeDays).toBe(0)
  })

  it.each(MODES)(
    "approves exactly the checked scopes for the chosen lifetime, then goes to the app, $name",
    async ({ technical }) => {
      renderPage({ technical })
      fireEvent.click(
        await screen.findByRole("checkbox", { name: NOTES_WORDS })
      )
      fireEvent.click(screen.getByRole("radio", { name: "90 days" }))
      fireEvent.click(screen.getByRole("button", { name: "Approve" }))

      await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
      expect(calls("POST")).toHaveLength(1)
      // Nothing to set up, so no plan is named.
      expect(body("POST")).toEqual({
        scopes: [
          "read:ada.example.com/tasks/*",
          "read:substrate.reamde.dev/core/unit",
        ],
        lifetimeDays: 90,
      })
      expect(calls("DELETE")).toHaveLength(0)
    }
  )

  it("approves every scope for the default lifetime when nothing is changed", async () => {
    renderPage()
    fireEvent.click(await screen.findByRole("button", { name: "Approve" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
    expect(body("POST")).toEqual({
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

  it("lists what it sets up: the package with the code it runs and what it brings first, the account's values and the webhook", async () => {
    answers.GET = () => jsonResponse(200, SYNC)
    renderPage()
    await screen.findByRole("heading", { level: 1, name: "sync.example.com" })
    expect(screen.getByRole("checkbox", { name: ACCOUNTS_WORDS })).toBeTruthy()
    const grants = screen.getByRole("region", {
      name: "It will also, because it sets these up",
    })
    expect(within(grants).getByText(WEBHOOK_WORDS)).toBeTruthy()
    // What it gains through the setup is not a choice of its own.
    expect(within(grants).queryByRole("checkbox")).toBeNull()

    const setup = screen.getByRole("region", {
      name: "It will set up on your server",
    })
    const install = within(setup).getByRole("listitem", {
      name: "Install Apple Health",
    })
    expect(
      within(install).getByText(
        "Keeps a copy of the Health data your iPhone sends."
      )
    ).toBeTruthy()
    expect(
      within(install).getByText("Runs code on your server, automatically.")
    ).toBeTruthy()
    expect(within(install).getByText("Also installs Health.")).toBeTruthy()
    const account = within(setup).getByRole("listitem", {
      name: "Create the Apple Health account “Ada's iPhone”",
    })
    expect(within(account).getByText("Display name")).toBeTruthy()
    expect(within(account).getByText("Ada's iPhone")).toBeTruthy()
    expect(within(account).getByText("Timezone")).toBeTruthy()
    expect(within(account).getByText("Europe/London")).toBeTruthy()
    expect(
      within(setup).getByRole("listitem", {
        name: "Turn on the Apple Health webhook",
      })
    ).toBeTruthy()
    // Identifiers wait behind the switch.
    expect(within(setup).queryByText(APPLEHEALTH)).toBeNull()

    // What it sets up sets off automations, and what lands stays.
    expect(screen.getByText("Your automations run on its changes")).toBeTruthy()
    expect(
      screen.getByText("If you stop part way, what was installed stays.")
    ).toBeTruthy()
    expect(
      screen.getByRole("button", { name: "Approve and set up" })
    ).toBeTruthy()
  })

  it("shows every identifier and version behind what it sets up, technically, and the details as sent", async () => {
    answers.GET = () => jsonResponse(200, SYNC)
    renderPage({ technical: true })
    const setup = await screen.findByRole("region", {
      name: "It will set up on your server",
    })
    const install = within(setup).getByRole("listitem", {
      name: "Install Apple Health",
    })
    expect(within(install).getByText(APPLEHEALTH)).toBeTruthy()
    expect(within(install).getByText("version 5 or later")).toBeTruthy()
    expect(within(install).getByText("version 5")).toBeTruthy()
    expect(
      within(install).getByText("substrate.reamde.dev/health")
    ).toBeTruthy()
    expect(within(install).getAllByText(`${APPLEHEALTH}/delivery`).length).toBe(
      2
    )
    expect(
      within(install).getAllByText(`${APPLEHEALTH}/applehealthingest`).length
    ).toBe(2)
    expect(within(install).getByText("applehealth-to-health")).toBeTruthy()

    const account = within(setup).getByRole("listitem", {
      name: "Create the Apple Health account “Ada's iPhone”",
    })
    expect(within(account).getByText(`${APPLEHEALTH}/account`)).toBeTruthy()
    expect(within(account).getByText("substrate-sync-1a2b3c4d")).toBeTruthy()
    expect(within(account).getByText('"Europe/London"')).toBeTruthy()
    // Every value is shown in this view too.
    expect(within(account).getByText("Europe/London")).toBeTruthy()

    const webhook = within(setup).getByRole("listitem", {
      name: "Turn on the Apple Health webhook",
    })
    expect(within(webhook).getByText("applehealth-webhook")).toBeTruthy()
    expect(webhook.textContent).toContain("The key is never shared")
    expect(webhook.textContent).toContain("approving sets one")

    const grants = screen.getByRole("region", {
      name: "It will also, because it sets these up",
    })
    expect(within(grants).getByText("webhook:applehealth-webhook")).toBeTruthy()

    const raw = screen.getByRole("region", {
      name: "Authorization details, as the app sent them",
    })
    expect(JSON.parse(raw.querySelector("pre")!.textContent)).toEqual(DETAILS)
    expect(
      within(raw).getByRole("button", {
        name: "Copy the authorization details",
      })
    ).toBeTruthy()
  })

  it.each(MODES)(
    "keeps Deny and disables Approve while a row is blocked, with the way to fix it, $name",
    async ({ technical }) => {
      const blocked = {
        ...INSTALL,
        action: "blocked",
        problem:
          "This server ships version 4 of Apple Health, the app needs 5: update the server.",
        fix: "/providers/applehealth",
      }
      answers.GET = () =>
        jsonResponse(200, { ...SYNC, setup: [blocked, CREATE, ARM] })
      renderPage({ technical })
      const row = await screen.findByRole("listitem", {
        name: "Install Apple Health",
      })
      expect(
        within(row).getByText(
          "This server ships version 4 of Apple Health, the app needs 5: update the server."
        )
      ).toBeTruthy()
      expect(
        within(row)
          .getByRole("link", { name: "Fix this in the console" })
          .getAttribute("href")
      ).toBe("/providers/applehealth")
      expect(
        screen.getByRole<HTMLButtonElement>("button", {
          name: "Approve and set up",
        }).disabled
      ).toBe(true)
      const deny = screen.getByRole<HTMLButtonElement>("button", {
        name: "Deny",
      })
      expect(deny.disabled).toBe(false)
      fireEvent.click(deny)
      await waitFor(() => expect(assign).toHaveBeenCalledWith(DENIED))
      expect(calls("POST")).toHaveLength(0)
    }
  )

  it("approves and sets up, follows the set up every second, then goes to the app", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    answers.GET = () => jsonResponse(200, SYNC)
    answers.POST = () => jsonResponse(202, { state: "setting-up" })
    renderPage()
    fireEvent.click(
      await screen.findByRole("button", { name: "Approve and set up" })
    )
    await screen.findByText(
      "Setting up on your server. Closing this page does not stop it."
    )
    expect(body("POST")).toEqual({
      scopes: [ACCOUNTS.scope],
      lifetimeDays: 30,
      planHash: "plan-1",
    })
    // While it runs, nothing may be sent again.
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Deny" }).disabled
    ).toBe(true)

    answers.GET = () =>
      jsonResponse(200, {
        ...SYNC,
        state: "setting-up",
        progress: ["done", "running", "waiting"],
      })
    await vi.advanceTimersByTimeAsync(1000)
    const install = await screen.findByRole("listitem", {
      name: "Install Apple Health",
    })
    await waitFor(() => expect(install.textContent).toContain("Done"))
    expect(
      screen.getByRole("listitem", {
        name: "Create the Apple Health account “Ada's iPhone”",
      }).textContent
    ).toContain("Setting up")
    expect(assign).not.toHaveBeenCalled()

    answers.GET = () =>
      jsonResponse(200, {
        ...SYNC,
        state: "approved",
        progress: ["done", "done", "done"],
        next: NEXT,
      })
    await vi.advanceTimersByTimeAsync(1000)
    await waitFor(() => expect(assign).toHaveBeenCalledWith(NEXT))
    expect(screen.getByText("Taking you back to the app…")).toBeTruthy()
    const reads = calls("GET").length
    await vi.advanceTimersByTimeAsync(3000)
    // Once approved, the page stops asking.
    expect(calls("GET")).toHaveLength(reads)
  })

  it("does not follow a finished set up's address that is not a web page", async () => {
    answers.GET = () =>
      jsonResponse(200, {
        ...SYNC,
        state: "approved",
        next: "javascript:alert(1)",
      })
    renderPage()
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("not a web page")
    expect(assign).not.toHaveBeenCalled()
  })

  it("shows where a set up stopped, keeps what landed ticked, and tries again with the fresh plan", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    answers.GET = () => jsonResponse(200, SYNC)
    answers.POST = () => jsonResponse(202, { state: "setting-up" })
    renderPage()
    fireEvent.click(
      await screen.findByRole("button", { name: "Approve and set up" })
    )
    await screen.findByText(
      "Setting up on your server. Closing this page does not stop it."
    )
    answers.GET = () =>
      jsonResponse(200, {
        ...SYNC,
        setup: [{ ...INSTALL, action: "none" }, CREATE, ARM],
        planHash: "plan-2",
        state: "pending",
        progress: ["done", "failed", "waiting"],
        failure: { row: 1, message: "the account kind refused timezone" },
      })
    await vi.advanceTimersByTimeAsync(1000)
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("Setting up stopped part way")
    expect(alert.textContent).toContain(
      "Create the Apple Health account “Ada's iPhone”: the account kind refused timezone"
    )
    expect(alert.textContent).toContain("What was set up before it stays.")
    const account = screen.getByRole("listitem", {
      name: "Create the Apple Health account “Ada's iPhone”",
    })
    expect(account.textContent).toContain("Stopped")
    expect(account.textContent).toContain("the account kind refused timezone")
    expect(
      screen.getByRole("listitem", { name: "Apple Health is installed" })
        .textContent
    ).toContain("Done")

    answers.POST = () => jsonResponse(200, { redirect: NEXT })
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith(NEXT))
    expect(body("POST", 1).planHash).toBe("plan-2")
  })

  it("reads the request again when its plan moved, says what changed, and approves the new plan", async () => {
    answers.GET = () => jsonResponse(200, SYNC)
    answers.POST = () =>
      jsonResponse(409, {
        error: {
          code: "conflict",
          message: "the setup changed since the page read it",
        },
      })
    renderPage()
    const approve = await screen.findByRole("button", {
      name: "Approve and set up",
    })
    answers.GET = () =>
      jsonResponse(200, {
        ...SYNC,
        setup: [{ ...INSTALL, action: "none" }, CREATE, ARM],
        planHash: "plan-2",
      })
    fireEvent.click(approve)
    const changed = await screen.findByText(
      "What it sets up changed since this page was read"
    )
    const note = changed.closest("[role=note]") as HTMLElement
    expect(within(note).getByText("Apple Health is installed")).toBeTruthy()
    expect(note.textContent).toContain("Check it again, then approve.")
    expect(calls("GET")).toHaveLength(2)
    expect(assign).not.toHaveBeenCalled()

    answers.POST = () => jsonResponse(202, { state: "setting-up" })
    fireEvent.click(screen.getByRole("button", { name: "Approve and set up" }))
    await screen.findByText(
      "Setting up on your server. Closing this page does not stop it."
    )
    expect(body("POST", 1).planHash).toBe("plan-2")
  })

  describe("a re-authorization", () => {
    const previous = {
      approvedAt: "2026-10-01T09:00:00Z",
      expiresAt: "2026-10-31T09:00:00Z",
      scopes: [TASKS.scope],
    }

    it.each(MODES)(
      "heads by the new access, folds what was allowed, and says denying keeps it, $name",
      async ({ technical }) => {
        answers.GET = () =>
          jsonResponse(200, { ...REQUEST, scopes: [TASKS, NOTES], previous })
        renderPage({ technical })
        await screen.findByRole("heading", {
          level: 1,
          name: "Allow more access",
        })
        expect(screen.getByText("notes.example.com")).toBeTruthy()
        expect(screen.getByText(/You approved it on 1 October/)).toBeTruthy()
        const access = screen.getByRole("region", {
          name: "It will be able to",
        })
        expect(within(access).getAllByRole("checkbox")).toHaveLength(1)
        expect(
          within(access).getByRole("checkbox", { name: NOTES_WORDS })
        ).toBeTruthy()

        const fold = screen.getByRole("region", { name: "Already allowed" })
        expect(within(fold).queryByRole("checkbox")).toBeNull()
        fireEvent.click(
          within(fold).getByRole("button", { name: "Already allowed (1)" })
        )
        const tasks = await within(fold).findByRole<HTMLInputElement>(
          "checkbox",
          { name: TASKS_WORDS }
        )
        expect(tasks.checked).toBe(true)
        expect(
          screen.getByText(
            "If you deny, the access you approved before keeps working."
          )
        ).toBeTruthy()

        fireEvent.click(screen.getByRole("button", { name: "Approve" }))
        await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
        expect(body("POST").scopes).toEqual([TASKS.scope, NOTES.scope])
      }
    )

    it("heads a renewal, a server update and both by what changed", async () => {
      answers.GET = () =>
        jsonResponse(200, {
          ...REQUEST,
          scopes: [TASKS],
          previous,
        })
      renderPage()
      await screen.findByRole("heading", { level: 1, name: "Renew access" })
      expect(screen.getByText(/It asks for nothing new/)).toBeTruthy()

      cleanup()
      const installed = { ...INSTALL, action: "none" }
      answers.GET = () =>
        jsonResponse(200, {
          ...SYNC,
          setup: [installed, CREATE, ARM],
          previous: {
            ...previous,
            scopes: [ACCOUNTS.scope, "webhook:applehealth-webhook"],
          },
        })
      renderPage()
      await screen.findByRole("heading", {
        level: 1,
        name: "Update server setup",
      })
      // The new rows lead; what is in place, and the delivery the app holds
      // already, fold away.
      const setup = screen.getByRole("region", {
        name: "It will set up on your server",
      })
      expect(within(setup).getAllByRole("listitem")).toHaveLength(2)
      expect(
        screen.queryByRole("region", {
          name: "It will also, because it sets these up",
        })
      ).toBeNull()
      const fold = screen.getByRole("region", { name: "Already allowed" })
      fireEvent.click(
        within(fold).getByRole("button", { name: "Already allowed (3)" })
      )
      expect(
        await within(fold).findByRole("listitem", {
          name: "Apple Health is installed",
        })
      ).toBeTruthy()
      expect(within(fold).getByText(WEBHOOK_WORDS)).toBeTruthy()
      expect(
        within(fold).getByRole("checkbox", { name: ACCOUNTS_WORDS })
      ).toBeTruthy()

      cleanup()
      answers.GET = () =>
        jsonResponse(200, {
          ...SYNC,
          previous: { ...previous, scopes: [] },
        })
      renderPage()
      await screen.findByRole("heading", {
        level: 1,
        name: "Allow more access and update server setup",
      })
    })
  })

  it("flips the console's one Technical details switch and keeps every choice made", async () => {
    renderPage()
    fireEvent.click(await screen.findByRole("checkbox", { name: NOTES_WORDS }))
    fireEvent.click(screen.getByRole("radio", { name: "90 days" }))
    expect(
      screen.queryByText("https://notes.example.com/oauth/client.json")
    ).toBeNull()

    const toggle = screen.getByRole("switch", {
      name: "Show technical details",
    })
    expect(toggle.getAttribute("aria-checked")).toBe("false")
    fireEvent.click(toggle)
    expect(
      await screen.findByText("https://notes.example.com/oauth/client.json")
    ).toBeTruthy()
    expect(toggle.getAttribute("aria-checked")).toBe("true")
    // The switch is the console's, kept where every page reads it.
    await waitFor(() =>
      expect(localStorage.getItem(TECHNICAL_KEY)).toBe("true")
    )
    expect(
      screen.getByRole<HTMLInputElement>("checkbox", { name: NOTES_WORDS })
        .checked
    ).toBe(false)
    expect(
      screen
        .getByRole("radio", { name: "90 days" })
        .getAttribute("aria-checked")
    ).toBe("true")

    fireEvent.click(toggle)
    await waitFor(() =>
      expect(localStorage.getItem(TECHNICAL_KEY)).toBe("false")
    )
    expect(
      screen.queryByText("https://notes.example.com/oauth/client.json")
    ).toBeNull()
    expect(
      screen.getByRole<HTMLInputElement>("checkbox", { name: NOTES_WORDS })
        .checked
    ).toBe(false)
  })
})
