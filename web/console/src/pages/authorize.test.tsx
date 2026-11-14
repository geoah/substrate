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
import {
  ASSISTANT_CAN,
  ASSISTANT_CANNOT,
  scopeSentence,
} from "@/lib/app-scopes"

vi.mock("@/router", () => ({
  authorizeRoute: {
    useParams: () => ({ request: "req-1" }),
    useSearch: () => ({ login_hint: undefined }),
  },
}))

import { expectNoTrace } from "@/hooks/use-secret-mutation.test-fixtures"

import { AuthorizePage } from "./authorize"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

const PATH = "/api/v1/authorizations/req-1"
const TECHNICAL_KEY = "substrate.console.technicalDetails"

const TASKS = {
  scope: "read:~/tasks/*",
  requested: "read:ada.example.com/tasks/*",
  verb: "read",
  write: false,
  pattern: "~/tasks/*",
  glob: true,
  kinds: [
    {
      identity: "~/tasks/task",
      label: { singular: "Task", plural: "Tasks" },
    },
    { identity: "~/tasks/project" },
  ],
  agents: [],
}
const TASKS_WORDS =
  "See your tasks and projects, including kinds added later, and search them by meaning, which spends your embedding budget"
const NOTES = {
  scope: "write:~/notes/note",
  requested: "write:~/notes/note",
  verb: "write",
  write: true,
  pattern: "~/notes/note",
  glob: false,
  kinds: [],
  agents: [],
}
const NOTES_WORDS =
  "See, add, change and delete your notes, and search them by meaning, which spends your embedding budget"
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
    appearsAs: "notes.example.com",
    verified: false,
    publisher: { listed: false },
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
    appearsAs: "local",
    verified: false,
    publisher: { listed: false },
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
    description: "Keeps a copy of the Health data your iPhone sends.",
    keptBy: "publisher",
    required: 5,
    shipped: 5,
    requires: [
      {
        id: "substrate.reamde.dev/health",
        action: "install",
        shipped: 2,
        kinds: [
          {
            identity: "substrate.reamde.dev/health/sleep",
            label: { singular: "Sleep", plural: "Sleep" },
          },
        ],
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
const ENABLE = {
  type: "webhook",
  action: "enable",
  webhook: { trigger: "applehealth-webhook", package: APPLEHEALTH },
}
// A published package whose agent takes its AI provider through a bundle
// input (decision 0217), with two providers to choose from, so none is
// chosen for the owner.
const COACH = "coach.example.com/coach"
const COACH_INPUT = {
  package: COACH,
  input: "model",
  agents: [{ agent: `${COACH}/run`, models: ["a"], spendCentsPerDay: 100 }],
  options: [
    { id: "openai", label: "OpenAI", models: { [`${COACH}/run`]: "a" } },
    {
      id: "anthropic",
      label: "Anthropic",
      models: { [`${COACH}/run`]: "a" },
    },
  ],
}
const COACH_ROW = {
  type: "package",
  action: "install",
  package: {
    id: COACH,
    required: 1,
    shipped: 1,
    providerInputs: [COACH_INPUT],
  },
}
// An optional add-on of Apple Health (decision 0216).
const ADDON = {
  type: "package",
  action: "install",
  package: { id: COACH, required: 1, shipped: 1, addonFor: APPLEHEALTH },
}
const SYNC = {
  ...REQUEST,
  app: {
    clientId: "https://sync.example.com/client.json",
    host: "sync.example.com",
    local: false,
    name: "Substrate Sync",
    appearsAs: "sync.example.com",
    verified: false,
    publisher: { listed: false },
  },
  redirect: {
    uri: "https://sync.example.com/callback",
    host: "sync.example.com",
  },
  scopes: [ACCOUNTS],
  details: DETAILS,
  setup: [INSTALL, CREATE, ENABLE],
  planHash: "plan-1",
}
const ACCOUNTS_WORDS =
  "See your Apple Health accounts, and search them by meaning, which spends your embedding budget"
const WEBHOOK_POWER =
  "send data to your repository through the Apple Health webhook, for any Apple Health account, and see every delivery to it that could not be filed, whoever sent it"
const SETTING_UP =
  "Setting up on your server. Closing this page does not stop it."
const CONTINUES =
  "Setting up continues even if you close this page. What it sets up stays if it stops part way or you later revoke the app."
/** The request while its set up runs, each row at `progress`. */
function running(
  progress: (string | undefined)[],
  over: Record<string, unknown> = {}
) {
  return {
    ...SYNC,
    state: "setting-up",
    setup: SYNC.setup.map((row, i) =>
      progress[i] ? { ...row, progress: progress[i] } : row
    ),
    ...over,
  }
}

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
  const view = render(
    <QueryClientProvider client={client}>
      <AuthorizePage />
    </QueryClientProvider>
  )
  return { ...view, client }
}

const MODES = [
  { name: "everyday", technical: false },
  { name: "technical", technical: true },
] as const

/** An AI assistant's scope (decision 0230): asked for alone, like `full`. */
const MCP = {
  scope: "mcp",
  requested: "mcp",
  verb: "mcp",
  write: false,
  pattern: "",
  glob: false,
  kinds: [],
  agents: [],
}

/** Claude Code: a hosted client id, its access sent to a loopback address. */
const CLAUDE_CODE_REQUEST = {
  ...REQUEST,
  app: {
    clientId: "https://claude.ai/oauth/claude-code-client-metadata",
    host: "claude.ai",
    local: false,
    name: "Claude Code",
    appearsAs: "local",
    verified: false,
    publisher: { listed: false },
  },
  redirect: {
    uri: "http://localhost:53021/callback",
    host: "localhost:53021",
    loopback: true,
  },
  scopes: [MCP],
}

/** A claude.ai connector: its client id and callback are every claude.ai
 * account's. */
const CONNECTOR_REQUEST = {
  ...CLAUDE_CODE_REQUEST,
  app: {
    ...CLAUDE_CODE_REQUEST.app,
    clientId: "https://claude.ai/oauth/mcp-oauth-client-metadata",
    name: "Claude",
    appearsAs: "claude.ai",
  },
  redirect: {
    uri: "https://claude.ai/api/mcp/auth_callback",
    host: "claude.ai",
  },
}

const SHARED_CALLBACK = "Approve only a sign-in you just started"

describe("AuthorizePage", () => {
  const fetchMock = vi.fn<typeof fetch>()
  const assign = vi.fn()
  /** What each route answers, given the address read; a test overrides
   * one to change the story. A read with the scopes left checked
   * (`?scope=`) is the GET route's too. */
  let answers: Record<string, (url: string) => Response>
  /** What any other address answers, where a test needs more than absence. */
  let elsewhere: (url: string, init?: RequestInit) => Response | undefined

  beforeEach(() => {
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
    elsewhere = () => undefined
    fetchMock.mockImplementation(async (url, init) => {
      const method = init?.method ?? "GET"
      // Everything else the page reads (the preference record, the kind
      // registry) is absent unless a test says otherwise, which the console
      // treats as defaults.
      if (String(url).split("?")[0] !== PATH)
        return (
          elsewhere(String(url), init) ??
          jsonResponse(404, {
            error: { code: "not_found", message: "not found" },
          })
        )
      return answers[method](String(url))
    })
  })

  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
    assign.mockReset()
  })

  /** The calls to the request's route, a read with `?scope=` included. */
  const calls = (method: string) =>
    fetchMock.mock.calls.filter(
      ([url, init]) =>
        String(url).split("?")[0] === PATH && (init?.method ?? "GET") === method
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
        name: "Connect this app to ada.example.com?",
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
        name: "Connect this app to ada.example.com?",
      })
    ).toBeTruthy()
    // The heading asks about "this app": the name is the app's claim, under the host its client id proves.
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
    for (const name of [
      TASKS_WORDS,
      NOTES_WORDS,
      "See your Core units, and search them by meaning, which spends your embedding budget",
    ]) {
      expect(
        within(access).getByRole<HTMLInputElement>("checkbox", { name }).checked
      ).toBe(true)
    }
    expect(within(access).queryByText("~/tasks/*")).toBeNull()
    expect(within(access).queryByText("~/tasks/task")).toBeNull()

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

  it("shows who asks, where the code goes and every scope with what it reaches, technically", async () => {
    renderPage({ technical: true })
    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "Connect this app to ada.example.com?",
      })
    ).toBeTruthy()
    // The identifiers wait in one block under the answer.
    const details = screen.getByRole("region", { name: "Technical details" })
    expect(
      within(details).getByText("https://notes.example.com/oauth/client.json")
    ).toBeTruthy()
    expect(
      screen
        .getByRole("button", { name: "Approve" })
        .compareDocumentPosition(details) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()

    // What the app claims, as text and never as a link, with the warning.
    const claims = within(details).getByRole("region", {
      name: "The app says it is",
    })
    expect(within(claims).getByText("Notes for Ada")).toBeTruthy()
    expect(
      within(claims).getByText("https://notes-app.example.org")
    ).toBeTruthy()
    expect(within(claims).queryByRole("link")).toBeNull()
    expect(within(claims).getByRole("note").textContent).toMatch(
      /not on notes\.example\.com/
    )

    const sends = within(details).getByRole("region", {
      name: "After you answer, you go to",
    })
    expect(
      within(sends).getByText("http://127.0.0.1:53021/oauth/callback")
    ).toBeTruthy()
    // The warning about where the access goes stays above, once, as in the
    // everyday view.
    expect(within(sends).queryByRole("note")).toBeNull()
    expect(
      screen.getAllByText("A program on this device receives the access")
    ).toHaveLength(1)
    // The repository is in the heading; the block does not say it again.
    expect(screen.queryByRole("region", { name: "Repository" })).toBeNull()

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
    expect(within(access).getByText("~/tasks/task")).toBeTruthy()
    expect(within(access).getByText("~/tasks/project")).toBeTruthy()
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
    const changes = within(details).getByRole("region", {
      name: "Its changes appear as",
    })
    expect(within(changes).getByText("notes.example.com")).toBeTruthy()
    expect(within(changes).getByText(/never as yours/)).toBeTruthy()
    const automations = screen
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
      id: "~/llm/substrateEditor",
      kind: "substrate.reamde.dev/core/agent",
      properties: {
        permissions: { reads: { kinds: ["~/tasks/task"] } },
      },
      labels: {},
      version: 1,
      createdAt: "2026-10-01T00:00:00Z",
      updatedAt: "2026-10-01T00:00:00Z",
    }
    const agents = {
      scope: "agents:~/llm/*",
      requested: "agents:~/llm/*",
      verb: "agents",
      write: false,
      pattern: "~/llm/*",
      glob: true,
      kinds: [],
      agents: [agent],
    }
    const decide = {
      ...agents,
      scope: "decide:~/llm/substrateEditor",
      requested: "decide:~/llm/substrateEditor",
      verb: "decide",
      pattern: "~/llm/substrateEditor",
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
      // Talking reaches whatever the agents may do, and the sentence says so
      // in both views.
      const talk = await screen.findByRole("checkbox", {
        name: "Talk to Substrate editor, including agents added later, and through a chat have them do anything they may do; read every chat with them, yours included, answer their questions, and rename or stop any of those chats",
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
        name: "Connect this app to ada.example.com?",
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
        scopes: ["read:~/tasks/*", "read:substrate.reamde.dev/core/unit"],
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

  it.each(MODES)(
    "asks for full access whole, with its warning and nothing to uncheck ($name)",
    async ({ technical }) => {
      answers.GET = () =>
        jsonResponse(200, {
          ...REQUEST,
          redirect: {
            uri: "http://127.0.0.1:53021/oauth/callback",
            host: "127.0.0.1:53021",
            loopback: true,
          },
          scopes: [
            {
              scope: "full",
              requested: "full",
              verb: "full",
              write: false,
              pattern: "",
              glob: false,
              kinds: [],
              agents: [],
            },
          ],
        })
      renderPage({ technical })
      expect(await screen.findByText("It asks for full access")).toBeTruthy()
      expect(
        screen.getByText(/make or revoke tokens, including this one/)
      ).toBeTruthy()
      expect(screen.queryByRole("checkbox")).toBeNull()
      expect(screen.queryByText(/Uncheck anything/)).toBeNull()
      expect(screen.queryByText("notes.example.com")).toBeNull()
      expect(screen.getByText(/Yours, as when you sign in/)).toBeTruthy()
      fireEvent.click(screen.getByRole("button", { name: "Approve" }))
      await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
      expect(body("POST")).toEqual({ scopes: ["full"], lifetimeDays: 30 })
    }
  )

  it.each(MODES)(
    "asks for an AI assistant's access whole: where it works, what it can and can't do, its code's reach and its own name ($name)",
    async ({ technical }) => {
      answers.GET = () => jsonResponse(200, CLAUDE_CODE_REQUEST)
      renderPage({ technical })
      expect(
        await screen.findByText(
          "It asks to work with nearly everything you have"
        )
      ).toBeTruthy()
      expect(
        screen.getByRole("heading", {
          level: 1,
          name: "Connect this assistant to ada.example.com?",
        })
      ).toBeTruthy()
      expect(screen.queryByText("It asks for full access")).toBeNull()
      expect(screen.queryByRole("checkbox")).toBeNull()
      expect(screen.queryByText(/Uncheck anything/)).toBeNull()

      const where = screen.getByRole("region", { name: "Where it works" })
      expect(
        within(where).getByText(`${window.location.origin}/mcp`)
      ).toBeTruthy()
      expect(where.textContent).toContain(
        "not in this console, and not through the API"
      )
      const can = screen.getByRole("region", { name: "It will be able to" })
      for (const line of ASSISTANT_CAN)
        expect(within(can).getByText(`${line}.`)).toBeTruthy()
      const cannot = screen.getByRole("region", {
        name: "It won’t be able to",
      })
      for (const line of ASSISTANT_CANNOT)
        expect(within(cannot).getByText(`${line}.`)).toBeTruthy()
      expect(screen.getByText("Its code can reach your secrets")).toBeTruthy()
      expect(
        screen.getByText(
          /Code it writes runs as your code does, so it can read what your code can, secrets included, and hand them back to it/
        )
      ).toBeTruthy()

      const changes = screen.getByRole("region", {
        name: "Its changes appear as",
      })
      expect(changes.textContent).toContain(
        "“Claude Code”, the name it gives itself, not you."
      )
      expect(changes.textContent).not.toMatch(/Yours/)
      expect(within(changes).queryByText("local") !== null).toBe(technical)

      // A program on this device receives it: the loopback warning, not the
      // shared callback's.
      expect(screen.queryByText(SHARED_CALLBACK)).toBeNull()

      // No refresh token, so until revoked comes first.
      expect(
        screen
          .getByRole("radio", { name: "Until you revoke it" })
          .getAttribute("aria-checked")
      ).toBe("true")
      expect(
        screen.getByText(
          "It lasts until you revoke it in Settings, under AI assistants."
        )
      ).toBeTruthy()
      fireEvent.click(screen.getByRole("button", { name: "Approve" }))
      await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
      expect(body("POST")).toEqual({ scopes: ["mcp"], lifetimeDays: 0 })
    }
  )

  it("approves an assistant for a stretch when the owner picks one, and says it signs in again after", async () => {
    answers.GET = () => jsonResponse(200, CLAUDE_CODE_REQUEST)
    renderPage()
    fireEvent.click(await screen.findByRole("radio", { name: "30 days" }))
    expect(
      screen.getByText(/After that, you sign the assistant in again\./)
    ).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Approve" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
    expect(body("POST")).toEqual({ scopes: ["mcp"], lifetimeDays: 30 })
  })

  it.each(MODES)(
    "warns that a hosted assistant's sign-in may be someone else's ($name)",
    async ({ technical }) => {
      answers.GET = () => jsonResponse(200, CONNECTOR_REQUEST)
      renderPage({ technical })
      expect(await screen.findByText(SHARED_CALLBACK)).toBeTruthy()
      expect(
        screen.getByText(/Every claude.ai account connects with this same app/)
          .textContent
      ).toContain(
        "If you didn’t just connect this assistant yourself, deny it."
      )
      expect(
        screen.queryByText("A program on this device receives the access")
      ).toBeNull()
    }
  )

  it("keeps the shared-callback warning to assistants", async () => {
    answers.GET = () =>
      jsonResponse(200, {
        ...CONNECTOR_REQUEST,
        scopes: [TASKS],
      })
    renderPage()
    await screen.findByRole("checkbox", { name: TASKS_WORDS })
    expect(screen.queryByText(SHARED_CALLBACK)).toBeNull()
    expect(screen.queryByRole("region", { name: "Where it works" })).toBeNull()
    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Connect this app to ada.example.com?",
      })
    ).toBeTruthy()
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
          message: "scope write:~/notes/note was not requested",
        },
      })
    renderPage()
    fireEvent.click(await screen.findByRole("button", { name: "Approve" }))
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("This request can’t be approved.")
    expect(alert.textContent).toContain(
      "scope write:~/notes/note was not requested"
    )
    expect(assign).not.toHaveBeenCalled()
    // The request is still pending on the server, so the owner may try again.
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Approve" })
        .disabled
    ).toBe(false)
  })

  it("leaves nothing of a typed secret setting in the mutation cache after a refusal, a denial or an approval", async () => {
    const KEY = `${APPLEHEALTH}/apiKey`
    const keyed = {
      ...SYNC,
      setup: [
        {
          ...INSTALL,
          package: {
            ...INSTALL.package,
            settings: [{ id: KEY, secret: true, required: true }],
          },
        },
        CREATE,
        ENABLE,
      ],
    }
    answers.GET = () => jsonResponse(200, keyed)
    answers.POST = () =>
      jsonResponse(400, {
        error: { code: "bad_request", message: "the key was refused" },
      })
    const first = renderPage()
    const keyBox = async () =>
      (await waitFor(() => {
        const box = document.querySelector<HTMLInputElement>(
          "input[type=password]"
        )
        expect(box).not.toBeNull()
        return box
      }))!
    const approve = await screen.findByRole("button", {
      name: "Approve and set up",
    })
    fireEvent.change(await keyBox(), {
      target: { value: "hs-refused-secret" },
    })
    fireEvent.click(approve)
    expect((await screen.findByRole("alert")).textContent).toContain(
      "the key was refused"
    )
    expect(body("POST").settings).toEqual([
      { id: KEY, secret: true, value: "hs-refused-secret" },
    ])
    expectNoTrace(first.client, "hs-refused-secret")

    fireEvent.change(await keyBox(), { target: { value: "hs-denied-secret" } })
    fireEvent.click(screen.getByRole("button", { name: "Deny" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith(DENIED))
    expectNoTrace(first.client, "hs-denied-secret")
    first.unmount()

    answers.POST = () => jsonResponse(200, { redirect: APPROVED })
    const second = renderPage()
    fireEvent.change(await keyBox(), {
      target: { value: "hs-approved-secret" },
    })
    fireEvent.click(
      await screen.findByRole("button", { name: "Approve and set up" })
    )
    await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
    expect(body("POST", 1).settings).toEqual([
      { id: KEY, secret: true, value: "hs-approved-secret" },
    ])
    expectNoTrace(second.client, "hs-approved-secret")
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

  it("lists what it sets up: the package with its version, the code it runs and what it brings first, the account's values and the webhook with what it then allows", async () => {
    answers.GET = () => jsonResponse(200, SYNC)
    renderPage()
    await screen.findByRole("heading", {
      level: 1,
      name: "Connect this app to ada.example.com?",
    })
    expect(screen.getByRole("checkbox", { name: ACCOUNTS_WORDS })).toBeTruthy()
    // The webhook's access is not a scope of its own.
    expect(screen.getAllByRole("checkbox")).toHaveLength(1)

    const setup = screen.getByRole("region", {
      name: "It will set up on your server",
    })
    const install = within(setup).getByRole("listitem", {
      name: "Install Apple Health",
    })
    expect(within(install).getByText("Version 5")).toBeTruthy()
    expect(
      within(install).getByText(
        "Keeps a copy of the Health data your iPhone sends."
      )
    ).toBeTruthy()
    // Its webhook is turned on by this request, so its code runs on every
    // delivery.
    expect(
      within(install).getByText(
        "Runs code on your server, automatically, each time data arrives."
      )
    ).toBeTruthy()
    expect(within(install).getByText("Also installs Health.")).toBeTruthy()
    const account = within(setup).getByRole("listitem", {
      name: "Create the Apple Health account “Ada's iPhone”",
    })
    expect(within(account).getByText("Display name")).toBeTruthy()
    expect(within(account).getByText("Ada's iPhone")).toBeTruthy()
    expect(within(account).getByText("Timezone")).toBeTruthy()
    expect(within(account).getByText("Europe/London")).toBeTruthy()
    // What the app gains through the setup sits under the row that sets it
    // up, with no checkbox: declining it is denying.
    const webhook = within(setup).getByRole("listitem", {
      name: "Turn on the Apple Health webhook",
    })
    expect(
      within(webhook).getByText(
        `The app will then be able to ${WEBHOOK_POWER}. To refuse this, deny the request.`
      )
    ).toBeTruthy()
    expect(within(webhook).queryByRole("checkbox")).toBeNull()
    // Identifiers wait behind the switch.
    expect(within(setup).queryByText(APPLEHEALTH)).toBeNull()

    // What it sets up sets off automations, keeps going without the page
    // and stays.
    expect(screen.getByText("Your automations run on its changes")).toBeTruthy()
    expect(screen.getByText(CONTINUES)).toBeTruthy()
    expect(
      screen.getByRole("button", { name: "Approve and set up" })
    ).toBeTruthy()
  })

  it("plans again with the scopes left checked, blocks the code they stop covering, and approves the plan read with them", async () => {
    const HEALTH = {
      scope: "write:substrate.reamde.dev/health/*",
      requested: "write:substrate.reamde.dev/health/*",
      verb: "write" as const,
      write: true,
      pattern: "substrate.reamde.dev/health/*",
      glob: true,
      kinds: [],
      agents: [],
      functions: [],
    }
    const covered = {
      ...INSTALL,
      package: {
        ...INSTALL.package,
        reach: [
          {
            pattern: "substrate.reamde.dev/health/sleep",
            write: true,
            by: [{ type: "function", id: `${APPLEHEALTH}/adaptapplehealth` }],
            covered: true,
            scope: "write:substrate.reamde.dev/health/sleep",
          },
        ],
      },
    }
    const uncovered = {
      ...covered,
      package: {
        ...covered.package,
        reach: [{ ...covered.package.reach[0], covered: false }],
      },
      blocked: {
        reason: "reaches-past-scopes",
        problem:
          "Its code reaches past the access you are approving: it writes substrate.reamde.dev/health/sleep (write:substrate.reamde.dev/health/sleep covers it). The app has to ask for that access, and you have to allow it.",
      },
    }
    const query = (scopes: string[]) =>
      `${PATH}?${new URLSearchParams(scopes.map((s) => ["scope", s])).toString()}`
    const both = [ACCOUNTS.scope, HEALTH.scope]
    answers.GET = (url) => {
      const narrowed = url === query([ACCOUNTS.scope])
      return jsonResponse(200, {
        ...SYNC,
        scopes: [ACCOUNTS, HEALTH],
        setup: [narrowed ? uncovered : covered, CREATE, ENABLE],
        planHash: narrowed ? "plan-narrow" : "plan-1",
      })
    }
    renderPage()
    const approve = await screen.findByRole<HTMLButtonElement>("button", {
      name: "Approve and set up",
    })
    const setup = screen.getByRole("region", {
      name: "It will set up on your server",
    })
    expect(
      within(setup).getByText(
        "Its code changes sleeps, all within the access you allow above."
      )
    ).toBeTruthy()
    const health = screen.getByRole("checkbox", { name: scopeSentence(HEALTH) })
    // Unchecking the health write reads the plan again with the account
    // read alone, which the package's code reaches past.
    fireEvent.click(health)
    await screen.findByText("Needs more access")
    expect(approve.disabled).toBe(true)
    expect(
      fetchMock.mock.calls.some(
        ([url]) => String(url) === query([ACCOUNTS.scope])
      )
    ).toBe(true)
    expect(
      within(setup).getByText(
        /Its code reaches past the access you are approving/
      )
    ).toBeTruthy()
    expect(
      within(setup).getByText("Its code changes sleeps (not allowed).")
    ).toBeTruthy()
    expect(
      screen.getByText(/Its code needs access that is unchecked above/)
    ).toBeTruthy()
    // Checking it back reads it again with both, and the plan is the first.
    fireEvent.click(health)
    await waitFor(() => expect(approve.disabled).toBe(false))
    expect(
      fetchMock.mock.calls.some(([url]) => String(url) === query(both))
    ).toBe(true)
    answers.POST = () => {
      answers.GET = () =>
        jsonResponse(200, running(["running", "waiting", "waiting"]))
      return jsonResponse(202, { state: "setting-up" })
    }
    fireEvent.click(approve)
    await screen.findByText(SETTING_UP)
    expect(body("POST")).toEqual({
      scopes: both,
      lifetimeDays: 30,
      planHash: "plan-1",
    })
  })

  it("holds Approve while an AI provider is unchosen, says which input needs one, and sends the choice made", async () => {
    answers.GET = () =>
      jsonResponse(200, {
        ...SYNC,
        details: [{ type: "package", package: COACH, version: 1 }],
        setup: [COACH_ROW],
      })
    renderPage()
    const approve = await screen.findByRole<HTMLButtonElement>("button", {
      name: "Approve and set up",
    })
    expect(approve.disabled).toBe(true)
    expect(screen.getByText("Choose an AI provider for Coach.")).toBeTruthy()
    const picker = screen.getByLabelText<HTMLSelectElement>(
      "AI provider for Coach"
    )
    expect(picker.value).toBe("")
    fireEvent.change(picker, { target: { value: "anthropic" } })
    await waitFor(() => expect(approve.disabled).toBe(false))
    expect(screen.queryByText("Choose an AI provider for Coach.")).toBeNull()
    answers.POST = () => jsonResponse(200, { redirect: APPROVED })
    fireEvent.click(approve)
    await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
    expect(body("POST")).toEqual({
      scopes: [ACCOUNTS.scope],
      lifetimeDays: 30,
      planHash: "plan-1",
      providerChoices: [
        { package: COACH, input: "model", provider: "anthropic" },
      ],
    })
  })

  it("starts an optional add-on unchecked, and approves without it", async () => {
    answers.GET = () =>
      jsonResponse(200, { ...SYNC, setup: [INSTALL, ADDON, CREATE, ENABLE] })
    renderPage()
    const approve = await screen.findByRole<HTMLButtonElement>("button", {
      name: "Approve and set up",
    })
    const addon = screen.getByRole<HTMLInputElement>("checkbox", {
      name: /Include Coach/,
    })
    expect(addon.checked).toBe(false)
    expect(approve.disabled).toBe(false)
    fireEvent.click(approve)
    await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
    expect(body("POST")).toEqual({
      scopes: [ACCOUNTS.scope],
      lifetimeDays: 30,
      planHash: "plan-1",
    })
  })

  it("sends an optional add-on once the owner checks it", async () => {
    answers.GET = () =>
      jsonResponse(200, { ...SYNC, setup: [INSTALL, ADDON, CREATE, ENABLE] })
    renderPage()
    const approve = await screen.findByRole<HTMLButtonElement>("button", {
      name: "Approve and set up",
    })
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /Include Coach/,
      })
    )
    fireEvent.click(approve)
    await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
    expect(body("POST").addons).toEqual([COACH])
  })

  it("asks for an add-on's AI provider, and counts its block, only while the add-on is checked", async () => {
    const addon = {
      ...ADDON,
      package: { ...ADDON.package, providerInputs: [COACH_INPUT] },
      blocked: {
        reason: "no-model",
        problem:
          "No AI provider prices the models for coach.example.com/coach input model.",
        fix: "/data/substrate.reamde.dev/llm/provider",
      },
    }
    answers.GET = () =>
      jsonResponse(200, { ...SYNC, setup: [INSTALL, addon, CREATE, ENABLE] })
    renderPage()
    const approve = await screen.findByRole<HTMLButtonElement>("button", {
      name: "Approve and set up",
    })
    // Unchecked, the add-on holds nothing back and asks for nothing.
    expect(approve.disabled).toBe(false)
    expect(screen.queryByText("Choose an AI provider for Coach.")).toBeNull()
    expect(screen.queryByText(/Fix it in the console/)).toBeNull()
    const check = screen.getByRole("checkbox", {
      name: /Include Coach/,
    })
    fireEvent.click(check)
    expect(approve.disabled).toBe(true)
    expect(screen.getByText("Choose an AI provider for Coach.")).toBeTruthy()
    expect(screen.getByText(/Fix it in the console/)).toBeTruthy()
    fireEvent.click(check)
    expect(approve.disabled).toBe(false)
    answers.POST = () => jsonResponse(200, { redirect: APPROVED })
    fireEvent.click(approve)
    await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
    // No choice is sent for the add-on left out.
    expect(body("POST")).toEqual({
      scopes: [ACCOUNTS.scope],
      lifetimeDays: 30,
      planHash: "plan-1",
    })
  })

  it("names an update's versions", async () => {
    answers.GET = () =>
      jsonResponse(200, {
        ...SYNC,
        setup: [
          {
            ...INSTALL,
            action: "upgrade",
            package: { ...INSTALL.package, held: 4, requires: undefined },
          },
          CREATE,
          ENABLE,
        ],
      })
    renderPage()
    const row = await screen.findByRole("listitem", {
      name: "Update Apple Health",
    })
    expect(within(row).getByText("From version 4 to 5")).toBeTruthy()
  })

  it.each(MODES)(
    "names what an install brings first with the code it runs, and the package's own code from its own functions, $name",
    async ({ technical }) => {
      const LABS = "substrate.reamde.dev/labs"
      const clinical = {
        type: "package",
        action: "install",
        package: {
          id: "substrate.reamde.dev/clinical",
          required: 1,
          shipped: 1,
          requires: [
            {
              id: LABS,
              description: "Lab reports and their results.",
              action: "install",
              shipped: 2,
              kinds: [
                { identity: `${LABS}/analyte` },
                { identity: `${LABS}/labreport` },
                { identity: `${LABS}/labresult` },
              ],
              functions: [
                {
                  id: `${LABS}/convertlab`,
                  writes: [`${LABS}/labresult`, `${LABS}/analyte`],
                },
              ],
              triggers: [
                {
                  id: "labs-on-labresult",
                  callable: `${LABS}/convertlab`,
                  enabled: true,
                },
              ],
            },
          ],
          kinds: [{ identity: "substrate.reamde.dev/clinical/condition" }],
        },
      }
      answers.GET = () =>
        jsonResponse(200, {
          ...SYNC,
          details: [{ type: "package", package: clinical.package.id }],
          setup: [clinical],
        })
      renderPage({ technical })
      const row = await screen.findByRole("listitem", {
        name: "Install Clinical",
      })
      expect(
        within(row).getByText(
          "Also installs Labs, which runs code on your server, automatically."
        )
      ).toBeTruthy()
      // Clinical ships no function of its own.
      expect(within(row).queryByText(/^Runs code/)).toBeNull()
      if (!technical) {
        expect(within(row).queryByText(LABS)).toBeNull()
        return
      }
      // Each requirement with what it brings, counted; its own facts open
      // on demand, and a function's writes one a line.
      expect(
        within(row).getByText("— 3 kinds, 1 function, 1 trigger")
      ).toBeTruthy()
      expect(within(row).getByText(LABS)).toBeTruthy()
      expect(
        within(row).getByText("Lab reports and their results.")
      ).toBeTruthy()
      fireEvent.click(
        within(row).getByRole("button", { name: "Labs functions (1)" })
      )
      const writes = await within(row).findByText("May write")
      const list = writes.nextElementSibling as HTMLElement
      expect(
        within(list)
          .getAllByRole("listitem")
          .map((li) => li.textContent)
      ).toEqual([`${LABS}/labresult`, `${LABS}/analyte`])
      // The package's own kinds are its own, below what it brings first.
      fireEvent.click(
        within(row).getByRole("button", { name: "Clinical kinds (1)" })
      )
      expect(
        await within(row).findByText("substrate.reamde.dev/clinical/condition")
      ).toBeTruthy()
      expect(within(row).queryByText(`${LABS}/labreport`)).toBeNull()
    }
  )

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
    expect(within(install).getByText("— 1 kind")).toBeTruthy()
    // What it brings first, then what it ships of its own.
    expect(within(install).getByText("Brings first")).toBeTruthy()
    expect(within(install).getByText("Ships")).toBeTruthy()
    // Kinds, functions and triggers are counted and open on demand.
    expect(within(install).queryByText(`${APPLEHEALTH}/delivery`)).toBeNull()
    for (const name of [
      "Apple Health kinds (1)",
      "Apple Health functions (1)",
      "Apple Health triggers (2)",
    ])
      fireEvent.click(within(install).getByRole("button", { name }))
    // A requirement's own facts are its own.
    expect(
      within(install).getByRole("button", { name: "Health kinds (1)" })
    ).toBeTruthy()
    await within(install).findByText("applehealth-to-health")
    expect(within(install).getAllByText(`${APPLEHEALTH}/delivery`).length).toBe(
      2
    )
    expect(
      within(install).getAllByText(`${APPLEHEALTH}/applehealthingest`).length
    ).toBe(2)

    // The account's values once, by their labels; its kind and id beside.
    const account = within(setup).getByRole("listitem", {
      name: "Create the Apple Health account “Ada's iPhone”",
    })
    expect(within(account).getByText(`${APPLEHEALTH}/account`)).toBeTruthy()
    expect(within(account).getByText("substrate-sync-1a2b3c4d")).toBeTruthy()
    expect(within(account).getAllByText(/Europe\/London/)).toHaveLength(1)

    const webhook = within(setup).getByRole("listitem", {
      name: "Turn on the Apple Health webhook",
    })
    expect(within(webhook).getByText("applehealth-webhook")).toBeTruthy()
    expect(webhook.textContent).toContain("The key is never shared")
    expect(webhook.textContent).toContain("approving sets one")
    expect(
      within(webhook).getByText("webhook:applehealth-webhook")
    ).toBeTruthy()

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
    "keeps Deny and disables Approve while a row is blocked, with the server's sentence and the way to fix it, $name",
    async ({ technical }) => {
      const blocked = {
        ...INSTALL,
        blocked: {
          reason: "upgrade-needs-review",
          problem:
            "updating it would change records in a way its page in the console asks you to confirm first",
          fix: "/providers/applehealth",
        },
      }
      answers.GET = () =>
        jsonResponse(200, { ...SYNC, setup: [blocked, CREATE, ENABLE] })
      renderPage({ technical })
      // A blocked row still says what it would do.
      const row = await screen.findByRole("listitem", {
        name: "Install Apple Health",
      })
      // The server's one sentence, in both views, as a sentence.
      expect(
        within(row).getByText(
          "Updating it would change records in a way its page in the console asks you to confirm first."
        )
      ).toBeTruthy()
      expect(row.textContent).toContain("Needs a fix first")
      expect(
        within(row)
          .getByRole("link", {
            name: "Fix this in the console (opens in a new tab)",
          })
          .getAttribute("href")
      ).toBe("/providers/applehealth")
      expect(
        screen.getByText(
          "Fix it in the console (the link beside the row), then check again, or deny the request."
        )
      ).toBeTruthy()
      // Nothing runs while a row is blocked, so nothing is said to continue.
      expect(screen.queryByText(CONTINUES)).toBeNull()
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

  it.each(MODES)(
    "says a server whose catalog is too old needs an update, in the server's one sentence, $name",
    async ({ technical }) => {
      const floor = {
        ...INSTALL,
        package: { ...INSTALL.package, shipped: 4 },
        blocked: {
          reason: "server-update",
          problem:
            "This server ships version 4; the app needs version 5. The server needs an update first.",
        },
      }
      answers.GET = () =>
        jsonResponse(200, { ...SYNC, setup: [floor, CREATE, ENABLE] })
      renderPage({ technical })
      const row = await screen.findByRole("listitem", {
        name: "Install Apple Health",
      })
      expect(
        within(row).getByText(
          "This server ships version 4; the app needs version 5. The server needs an update first."
        )
      ).toBeTruthy()
      expect(row.textContent).toContain("Needs a server update")
      // The blocked row's versions are in its sentence alone.
      expect(within(row).queryByText("Version 4")).toBeNull()
      expect(within(row).queryByRole("link")).toBeNull()
      // Nothing in the console fixes it, so the page says who can.
      expect(
        screen.getByText(
          "Your server needs an update before this can be approved. Whoever runs it can update it; check again afterwards, or deny the request."
        )
      ).toBeTruthy()
      expect(screen.queryByText(/Fix it in the console/)).toBeNull()
      expect(screen.queryByText(CONTINUES)).toBeNull()
      expect(
        screen.getByRole<HTMLButtonElement>("button", {
          name: "Approve and set up",
        }).disabled
      ).toBe(true)
    }
  )

  it.each(MODES)(
    "never shows what the app asked for as the values of an account already in place, $name",
    async ({ technical }) => {
      answers.GET = () =>
        jsonResponse(200, {
          ...SYNC,
          setup: [INSTALL, { ...CREATE, action: "none" }, ENABLE],
        })
      renderPage({ technical })
      const account = await screen.findByRole("listitem", {
        name: "The Apple Health account this app uses is in place",
      })
      expect(account.textContent).toContain(
        "Your existing account stays as it is."
      )
      // The title says it is in place; no status word says it again.
      expect(account.textContent).not.toContain("In place")
      expect(within(account).queryByText("Ada's iPhone")).toBeNull()
      expect(within(account).queryByText(/Europe\/London/)).toBeNull()
      expect(
        within(account).queryByText("substrate-sync-1a2b3c4d") !== null
      ).toBe(technical)
    }
  )

  it("reads a row in place by its title while a set up runs, never as done", async () => {
    answers.GET = () =>
      jsonResponse(200, {
        ...SYNC,
        state: "setting-up",
        setup: [
          { ...INSTALL, progress: "waiting" },
          { ...CREATE, action: "none", progress: "done" },
          { ...ENABLE, action: "none", progress: "done" },
        ],
      })
    renderPage()
    await screen.findByText(SETTING_UP)
    expect(
      screen.getByRole("listitem", { name: "Install Apple Health" }).textContent
    ).toContain("Waiting")
    for (const name of [
      "The Apple Health account this app uses is in place",
      "The Apple Health webhook is on",
    ]) {
      const row = screen.getByRole("listitem", { name })
      expect(row.textContent).not.toContain("Done")
      expect(row.textContent).not.toContain("In place")
      expect(row.querySelector(".text-ok")).toBeNull()
    }
  })

  it("approves and sets up, follows the set up every second, then goes to the app", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    answers.GET = () => jsonResponse(200, SYNC)
    answers.POST = () => {
      // The server holds the run from the answer on.
      answers.GET = () =>
        jsonResponse(200, running(["waiting", "waiting", "waiting"]))
      return jsonResponse(202, { state: "setting-up" })
    }
    renderPage()
    const approve = await screen.findByRole("button", {
      name: "Approve and set up",
    })
    // The live region is there before anything happens, so what is written
    // into it later is announced.
    const live = screen.getByRole("status")
    expect(live.textContent).toBe("")
    fireEvent.click(approve)
    await screen.findByText(SETTING_UP)
    expect(screen.getByRole("status")).toBe(live)
    expect(live.textContent).toContain("Setting up on your server.")
    expect(body("POST")).toEqual({
      scopes: [ACCOUNTS.scope],
      lifetimeDays: 30,
      planHash: "plan-1",
    })
    // The answer carries no rows: the page reads them right after it.
    await waitFor(() => expect(calls("GET")).toHaveLength(2))
    await waitFor(() =>
      expect(
        screen.getByRole("listitem", { name: "Install Apple Health" })
          .textContent
      ).toContain("Waiting")
    )
    // While it runs, nothing may be sent again.
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Deny" }).disabled
    ).toBe(true)

    answers.GET = () =>
      jsonResponse(200, running(["done", "running", "waiting"]))
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
    // The landed package still reads as part of the set up, and the
    // warning about it stays.
    expect(screen.getByText("Your automations run on its changes")).toBeTruthy()
    await waitFor(() =>
      expect(live.textContent).toContain(
        "Create the Apple Health account “Ada's iPhone”: step 2 of 3"
      )
    )
    expect(assign).not.toHaveBeenCalled()

    answers.GET = () =>
      jsonResponse(
        200,
        running(["done", "done", "done"], { state: "approved", next: NEXT })
      )
    await vi.advanceTimersByTimeAsync(1000)
    await waitFor(() => expect(assign).toHaveBeenCalledWith(NEXT))
    expect(live.textContent).toBe("Taking you back to the app…")
    const reads = calls("GET").length
    await vi.advanceTimersByTimeAsync(3000)
    // Once approved, the page stops asking.
    expect(calls("GET")).toHaveLength(reads)
  })

  it("keeps following a set up through a read that fails, and goes to the app once", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    answers.GET = () =>
      jsonResponse(200, running(["running", "waiting", "waiting"]))
    renderPage()
    await screen.findByText(SETTING_UP)

    answers.GET = () =>
      jsonResponse(503, { error: { code: "unavailable", message: "busy" } })
    await vi.advanceTimersByTimeAsync(1000)
    await screen.findByText(
      "The latest check on it didn’t answer; the page keeps asking."
    )
    // The rows it read last stay, and nothing may be sent meanwhile.
    expect(
      screen.getByRole("listitem", { name: "Install Apple Health" }).textContent
    ).toContain("Setting up")
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Deny" }).disabled
    ).toBe(true)
    expect(screen.queryByRole("alert")).toBeNull()

    answers.GET = () =>
      jsonResponse(200, running(["done", "running", "waiting"]))
    await vi.advanceTimersByTimeAsync(1000)
    await waitFor(() =>
      expect(
        screen.queryByText(
          "The latest check on it didn’t answer; the page keeps asking."
        )
      ).toBeNull()
    )
    expect(
      screen.getByRole("listitem", { name: "Install Apple Health" }).textContent
    ).toContain("Done")

    answers.GET = () =>
      jsonResponse(
        200,
        running(["done", "done", "done"], { state: "approved", next: NEXT })
      )
    await vi.advanceTimersByTimeAsync(1000)
    await waitFor(() => expect(assign).toHaveBeenCalledWith(NEXT))
    await vi.advanceTimersByTimeAsync(3000)
    expect(assign).toHaveBeenCalledTimes(1)
    expect(calls("POST")).toHaveLength(0)
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

  it("shows where a set up stopped once, keeps what landed ticked, and tries again", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    answers.GET = () => jsonResponse(200, SYNC)
    answers.POST = () => {
      answers.GET = () =>
        jsonResponse(200, running(["running", "waiting", "waiting"]))
      return jsonResponse(202, { state: "setting-up" })
    }
    renderPage()
    fireEvent.click(
      await screen.findByRole("button", { name: "Approve and set up" })
    )
    await screen.findByText(SETTING_UP)
    // The rows the approval ran, with how far each got; the one after the
    // stop is left as it was planned.
    answers.GET = () =>
      jsonResponse(
        200,
        running(["done", "failed"], {
          state: "pending",
          planHash: "plan-2",
          failure: { row: 1, message: "the account kind refused timezone" },
        })
      )
    await vi.advanceTimersByTimeAsync(1000)
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toBe(
      "Setting up stopped at Create the Apple Health account “Ada's iPhone”. What landed stays; Try again runs the rest."
    )
    // The message is said once, under its row.
    const account = screen.getByRole("listitem", {
      name: "Create the Apple Health account “Ada's iPhone”",
    })
    expect(account.textContent).toContain("Stopped")
    expect(account.textContent).toContain("The account kind refused timezone.")
    expect(
      screen.getAllByText("The account kind refused timezone.")
    ).toHaveLength(1)
    expect(
      screen.getByRole("listitem", { name: "Install Apple Health" }).textContent
    ).toContain("Done")
    expect(
      screen.getByRole("listitem", { name: "Turn on the Apple Health webhook" })
        .textContent
    ).not.toContain("Waiting")

    answers.POST = () => jsonResponse(200, { redirect: NEXT })
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith(NEXT))
    expect(body("POST", 1).planHash).toBe("plan-2")
  })

  it("reads the request again when its plan moved, says what changed, and approves the new plan with the choices made", async () => {
    answers.GET = () =>
      jsonResponse(200, { ...SYNC, scopes: [ACCOUNTS, NOTES] })
    answers.POST = () =>
      jsonResponse(409, {
        error: {
          code: "conflict",
          message: "the setup changed since the page read it",
        },
      })
    renderPage()
    const approve = await screen.findByRole<HTMLButtonElement>("button", {
      name: "Approve and set up",
    })
    fireEvent.click(screen.getByRole("checkbox", { name: NOTES_WORDS }))
    // Unchecking reads the plan again with the scope left checked, and
    // approving waits for that read.
    await waitFor(() => expect(approve.disabled).toBe(false))
    expect(calls("GET")).toHaveLength(2)
    const moved = {
      ...SYNC,
      scopes: [ACCOUNTS, NOTES],
      setup: [
        { ...INSTALL, action: "none" },
        CREATE,
        {
          ...ENABLE,
          blocked: {
            reason: "not-requested",
            problem:
              "this trigger runs code from a package this request does not set up",
          },
        },
      ],
      planHash: "plan-2",
    }
    answers.GET = () => jsonResponse(200, moved)
    fireEvent.click(approve)
    const changed = await screen.findByText(
      "What it sets up changed since this page was read"
    )
    const note = changed.closest("[role=note]") as HTMLElement
    expect(within(note).getByText("Apple Health is installed")).toBeTruthy()
    expect(
      within(note).getByText(
        "Turn on the Apple Health webhook: can’t be done now. This trigger runs code from a package this request does not set up."
      )
    ).toBeTruthy()
    expect(note.textContent).toContain("Check it again, then approve.")
    expect(calls("GET")).toHaveLength(3)
    expect(assign).not.toHaveBeenCalled()

    answers.GET = () =>
      jsonResponse(200, { ...moved, setup: moved.setup.slice(0, 2) })
    fireEvent.click(screen.getByRole("button", { name: "Check again" }))
    await waitFor(() =>
      expect(
        screen.getByRole<HTMLButtonElement>("button", {
          name: "Approve and set up",
        }).disabled
      ).toBe(false)
    )
    answers.POST = () => {
      answers.GET = () => jsonResponse(200, running(["running", "waiting"]))
      return jsonResponse(202, { state: "setting-up" })
    }
    fireEvent.click(screen.getByRole("button", { name: "Approve and set up" }))
    await screen.findByText(SETTING_UP)
    expect(body("POST", 1)).toEqual({
      scopes: [ACCOUNTS.scope],
      lifetimeDays: 30,
      planHash: "plan-2",
    })
  })

  it("follows a set up another answer started when its approval meets one", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    answers.GET = () => jsonResponse(200, SYNC)
    answers.POST = () =>
      jsonResponse(409, {
        error: { code: "conflict", message: "the request is being set up" },
      })
    renderPage()
    const approve = await screen.findByRole<HTMLButtonElement>("button", {
      name: "Approve and set up",
    })
    answers.GET = () =>
      jsonResponse(200, running(["running", "waiting", "waiting"]))
    fireEvent.click(approve)
    await screen.findByText(SETTING_UP)
    expect(
      screen.queryByText("What it sets up changed since this page was read")
    ).toBeNull()
    expect(screen.queryByRole("alert")).toBeNull()
    expect(approve.disabled).toBe(true)
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Deny" }).disabled
    ).toBe(true)

    const reads = calls("GET").length
    answers.GET = () =>
      jsonResponse(
        200,
        running(["done", "done", "done"], { state: "approved", next: NEXT })
      )
    await vi.advanceTimersByTimeAsync(1000)
    await waitFor(() => expect(assign).toHaveBeenCalledWith(NEXT))
    expect(calls("GET").length).toBeGreaterThan(reads)
    expect(calls("POST")).toHaveLength(1)
  })

  it("stops following a set up whose request is gone, and says so", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    answers.GET = () => jsonResponse(200, SYNC)
    answers.POST = () => {
      answers.GET = () =>
        jsonResponse(200, running(["running", "waiting", "waiting"]))
      return jsonResponse(202, { state: "setting-up" })
    }
    renderPage()
    fireEvent.click(
      await screen.findByRole("button", { name: "Approve and set up" })
    )
    await screen.findByText(SETTING_UP)
    await waitFor(() => expect(calls("GET")).toHaveLength(2))
    answers.GET = () =>
      jsonResponse(404, {
        error: {
          code: "not_found",
          message: "authorization request not found",
        },
      })
    await vi.advanceTimersByTimeAsync(1000)
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain(
      "This request has expired or was already answered."
    )
    expect(screen.queryByText(SETTING_UP)).toBeNull()
    const reads = calls("GET").length
    await vi.advanceTimersByTimeAsync(3000)
    expect(calls("GET")).toHaveLength(reads)
    expect(assign).not.toHaveBeenCalled()
  })

  it("checks a blocked request again and approves the plan it reads then", async () => {
    const blocked = {
      ...INSTALL,
      blocked: {
        reason: "quarantined",
        problem:
          "Your copy of this package is quarantined, so approving cannot set it up. Its page in the console says why.",
        fix: "/providers/applehealth",
      },
    }
    answers.GET = () =>
      jsonResponse(200, { ...SYNC, setup: [blocked, CREATE, ENABLE] })
    answers.POST = () => {
      answers.GET = () =>
        jsonResponse(200, running(["running", "waiting", "waiting"]))
      return jsonResponse(202, { state: "setting-up" })
    }
    renderPage()
    const approve = await screen.findByRole<HTMLButtonElement>("button", {
      name: "Approve and set up",
    })
    expect(approve.disabled).toBe(true)
    answers.GET = () => jsonResponse(200, { ...SYNC, planHash: "plan-2" })
    fireEvent.click(screen.getByRole("button", { name: "Check again" }))
    await waitFor(() => expect(approve.disabled).toBe(false))
    expect(screen.queryByRole("button", { name: "Check again" })).toBeNull()
    fireEvent.click(approve)
    await screen.findByText(SETTING_UP)
    expect(body("POST").planHash).toBe("plan-2")
  })

  it("says a set up stopped at no row with its message, and offers Try again", async () => {
    answers.GET = () =>
      jsonResponse(200, {
        ...SYNC,
        failure: {
          message:
            "another set up in this repository did not finish in time: try again",
        },
      })
    renderPage()
    let alert = await screen.findByRole("alert")
    expect(alert.textContent).toBe(
      "Setting up stopped.Another set up in this repository did not finish in time: try again."
    )
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy()

    cleanup()
    answers.GET = () =>
      jsonResponse(
        200,
        running(["done", "done", "done"], {
          state: "pending",
          failure: {
            message:
              "everything is set up, and the server is holding too many authorization codes to issue one: try again shortly",
          },
        })
      )
    renderPage()
    alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("Setting up stopped.")
    expect(alert.textContent).toContain(
      "Everything is set up, and the server is holding too many authorization codes to issue one: try again shortly."
    )
    expect(alert.textContent).not.toContain("Try again runs the rest")
    for (const row of within(
      screen.getByRole("region", { name: "It will set up on your server" })
    ).getAllByRole("listitem"))
      expect(row.textContent).toContain("Done")
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy()
  })

  describe("a re-authorization", () => {
    // Access the app still holds: it ends a month from whenever the test
    // runs.
    const previous = {
      approvedAt: "2026-10-01T09:00:00Z",
      expiresAt: new Date(Date.now() + 30 * 86_400_000).toISOString(),
      scopes: [TASKS.scope],
    }
    const HELD_ACCOUNTS = { ...ACCOUNTS, allowed: true }
    const held = {
      ...previous,
      scopes: [ACCOUNTS.scope, "webhook:applehealth-webhook"],
    }
    // The server says the token above already delivers to the webhook.
    const HELD_ENABLE = {
      ...ENABLE,
      webhook: { ...ENABLE.webhook, allowed: true },
    }

    it.each(MODES)(
      "heads by the new access, folds what was allowed, and says denying keeps it, $name",
      async ({ technical }) => {
        answers.GET = () =>
          jsonResponse(200, {
            ...REQUEST,
            scopes: [{ ...TASKS, allowed: true }, NOTES],
            previous,
          })
        renderPage({ technical })
        await screen.findByRole("heading", {
          level: 1,
          name: "Give this app more access to ada.example.com?",
        })
        expect(
          screen.getByText("Unverified app at notes.example.com")
        ).toBeTruthy()
        expect(screen.getByText(/You approved it on 1 October/)).toBeTruthy()
        // The approval's date is said once, in the header.
        expect(screen.queryByText(/^Approved /)).toBeNull()
        expect(
          screen.queryByRole("region", { name: "The access it holds now" }) !==
            null
        ).toBe(technical)
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

    it("folds by what the server says the token covers, not by the scope's spelling", async () => {
      // The token holds a write over every task kind; the request's read
      // of one is spelled otherwise and still covered.
      answers.GET = () =>
        jsonResponse(200, {
          ...REQUEST,
          scopes: [{ ...TASKS, allowed: true }],
          previous: { ...previous, scopes: ["write:~/*"] },
        })
      renderPage()
      await screen.findByRole("heading", {
        level: 1,
        name: "Keep this app connected to ada.example.com?",
      })
      expect(
        screen.queryByRole("region", { name: "It will be able to" })
      ).toBeNull()
    })

    it("heads a renewal, a server update and both by what changed", async () => {
      answers.GET = () =>
        jsonResponse(200, {
          ...REQUEST,
          scopes: [{ ...TASKS, allowed: true }],
          previous,
        })
      renderPage()
      await screen.findByRole("heading", {
        level: 1,
        name: "Keep this app connected to ada.example.com?",
      })
      expect(screen.getByText(/It asks for nothing new/)).toBeTruthy()
      // A renewal is a decision about what it already holds, so that list
      // starts open, and the answer says what approving does.
      expect(
        within(
          screen.getByRole("region", { name: "Already allowed" })
        ).getByRole("checkbox", { name: TASKS_WORDS })
      ).toBeTruthy()
      expect(screen.getByRole("button", { name: "Keep access" })).toBeTruthy()

      cleanup()
      const installed = { ...INSTALL, action: "none" }
      answers.GET = () =>
        jsonResponse(200, {
          ...SYNC,
          scopes: [HELD_ACCOUNTS],
          setup: [installed, CREATE, HELD_ENABLE],
          previous: held,
        })
      renderPage()
      await screen.findByRole("heading", {
        level: 1,
        name: "Update what this app set up on ada.example.com?",
      })
      // The new rows lead, the webhook's access said as held already; what
      // is in place folds away.
      const setup = screen.getByRole("region", {
        name: "It will set up on your server",
      })
      expect(within(setup).getAllByRole("listitem")).toHaveLength(2)
      expect(
        within(setup).getByText(`The app can already ${WEBHOOK_POWER}.`)
      ).toBeTruthy()
      const fold = screen.getByRole("region", { name: "Already allowed" })
      fireEvent.click(
        within(fold).getByRole("button", { name: "Already allowed (2)" })
      )
      expect(
        await within(fold).findByRole("listitem", {
          name: "Apple Health is installed",
        })
      ).toBeTruthy()
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
        name: "Give this app more access to ada.example.com and update its setup?",
      })

      // A renewal of an app that reads and delivers: each delivery runs
      // code that writes, so the automation warning stays.
      cleanup()
      answers.GET = () =>
        jsonResponse(200, {
          ...SYNC,
          scopes: [HELD_ACCOUNTS],
          setup: [
            installed,
            { ...CREATE, action: "none" },
            { ...HELD_ENABLE, action: "none" },
          ],
          previous: held,
        })
      renderPage()
      await screen.findByRole("heading", {
        level: 1,
        name: "Keep this app connected to ada.example.com?",
      })
      expect(
        screen.getByText("Your automations run on its changes")
      ).toBeTruthy()
      expect(
        screen.getByRole("button", { name: "Already allowed (4)" })
      ).toBeTruthy()

      // A webhook that is on, whose access the token lacks, is new access.
      cleanup()
      answers.GET = () =>
        jsonResponse(200, {
          ...SYNC,
          scopes: [HELD_ACCOUNTS],
          setup: [
            installed,
            { ...CREATE, action: "none" },
            { ...ENABLE, action: "none" },
          ],
          previous: { ...previous, scopes: [ACCOUNTS.scope] },
        })
      renderPage()
      await screen.findByRole("heading", {
        level: 1,
        name: "Give this app more access to ada.example.com?",
      })
      const on = within(
        screen.getByRole("region", { name: "It will set up on your server" })
      ).getByRole("listitem", { name: "The Apple Health webhook is on" })
      expect(
        within(on).getByText(
          `The app will be able to ${WEBHOOK_POWER}. To refuse this, deny the request.`
        )
      ).toBeTruthy()
    })

    it.each(MODES)(
      "asks for everything in full when the access it held has ended, $name",
      async ({ technical }) => {
        answers.GET = () =>
          jsonResponse(200, {
            ...REQUEST,
            scopes: [{ ...TASKS, allowed: true }],
            previous: { ...previous, expiresAt: "2026-10-31T09:00:00Z" },
          })
        renderPage({ technical })
        await screen.findByRole("heading", {
          level: 1,
          name: "Reconnect this app to ada.example.com?",
        })
        expect(screen.getByText(/that access ended on 31 October/)).toBeTruthy()
        expect(screen.queryByText(/It asks for nothing new/)).toBeNull()
        // Nothing is held, so nothing folds away checked.
        expect(
          screen.queryByRole("region", { name: "Already allowed" })
        ).toBeNull()
        const access = screen.getByRole("region", {
          name: "It will be able to",
        })
        expect(
          within(access).getByRole<HTMLInputElement>("checkbox", {
            name: TASKS_WORDS,
          }).checked
        ).toBe(true)
        expect(
          screen.getByText(
            "Its earlier access has ended; denying leaves it without access."
          )
        ).toBeTruthy()
        expect(
          screen.queryByText(
            "If you deny, the access you approved before keeps working."
          )
        ).toBeNull()
        expect(
          screen.queryByRole("region", { name: "The access it held" }) !== null
        ).toBe(technical)
      }
    )

    it("lists the rows the server runs, through a stop and a retry, and folds only what was in place when each run began", async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
      const installed = { ...INSTALL, action: "none" }
      const request = { ...SYNC, scopes: [HELD_ACCOUNTS], previous: held }
      answers.GET = () =>
        jsonResponse(200, {
          ...request,
          setup: [installed, CREATE, HELD_ENABLE],
        })
      answers.POST = () => {
        answers.GET = () =>
          jsonResponse(200, {
            ...request,
            state: "setting-up",
            setup: [
              { ...installed, progress: "done" },
              { ...CREATE, progress: "running" },
              { ...HELD_ENABLE, progress: "waiting" },
            ],
          })
        return jsonResponse(202, { state: "setting-up" })
      }
      renderPage()
      await screen.findByRole("heading", {
        level: 1,
        name: "Update what this app set up on ada.example.com?",
      })
      fireEvent.click(
        screen.getByRole("button", { name: "Approve and set up" })
      )
      await screen.findByText(SETTING_UP)

      const setupRows = () =>
        within(
          screen.getByRole("region", { name: "It will set up on your server" })
        ).getAllByRole("listitem")
      const folds = () =>
        within(
          screen.getByRole("region", { name: "Already allowed" })
        ).getByRole("button").textContent

      await waitFor(() =>
        expect(
          screen.getByRole("listitem", {
            name: "Create the Apple Health account “Ada's iPhone”",
          }).textContent
        ).toContain("Setting up")
      )
      expect(setupRows()).toHaveLength(2)
      expect(folds()).toBe("Already allowed (2)")

      // The rows it ran keep their verbs while it is stopped.
      answers.GET = () =>
        jsonResponse(200, {
          ...request,
          state: "pending",
          planHash: "plan-2",
          setup: [
            { ...installed, progress: "done" },
            { ...CREATE, progress: "done" },
            { ...HELD_ENABLE, progress: "failed" },
          ],
          failure: { row: 2, message: "the trigger refused the key" },
        })
      await vi.advanceTimersByTimeAsync(1000)
      const alert = await screen.findByRole("alert")
      expect(alert.textContent).toBe(
        "Setting up stopped at Turn on the Apple Health webhook. What landed stays; Try again runs the rest."
      )
      expect(setupRows()).toHaveLength(2)
      expect(
        screen.getByRole("listitem", {
          name: "Create the Apple Health account “Ada's iPhone”",
        }).textContent
      ).toContain("Done")
      expect(
        screen.getByRole("listitem", {
          name: "Turn on the Apple Health webhook",
        }).textContent
      ).toContain("Stopped")
      expect(folds()).toBe("Already allowed (2)")

      // A retry is planned again: what the first run landed is in place now.
      answers.POST = () => {
        answers.GET = () =>
          jsonResponse(200, {
            ...request,
            state: "setting-up",
            setup: [
              { ...installed, progress: "done" },
              { ...CREATE, action: "none", progress: "done" },
              { ...HELD_ENABLE, progress: "running" },
            ],
          })
        return jsonResponse(202, { state: "setting-up" })
      }
      fireEvent.click(screen.getByRole("button", { name: "Try again" }))
      await waitFor(() => expect(calls("POST")).toHaveLength(2))
      expect(body("POST", 1).planHash).toBe("plan-2")
      await waitFor(() => expect(folds()).toBe("Already allowed (3)"))
      expect(setupRows()).toHaveLength(1)
      expect(screen.queryByRole("alert")).toBeNull()
      expect(
        screen.getByRole("heading", {
          level: 1,
          name: "Update what this app set up on ada.example.com?",
        })
      ).toBeTruthy()
    })
  })

  it("keeps the browser's own Ctrl+B: the page has no sidebar to fold", async () => {
    renderPage()
    await screen.findByRole("heading", {
      level: 1,
      name: "Connect this app to ada.example.com?",
    })
    const key = new KeyboardEvent("keydown", {
      key: "b",
      ctrlKey: true,
      bubbles: true,
      cancelable: true,
    })
    window.dispatchEvent(key)
    expect(key.defaultPrevented).toBe(false)
    expect(localStorage.getItem("substrate.console.sidebarOpen")).toBeNull()
  })

  it("saves the Technical details switch to the preference record where the repository declares it", async () => {
    const RECORD =
      "/api/v1/substrate.reamde.dev/core/consolepreference/navigation"
    const puts: Array<Record<string, unknown>> = []
    elsewhere = (url, init) => {
      if (url.startsWith("/api/v1/records?"))
        return jsonResponse(200, {
          kinds: [
            {
              identity: "substrate.reamde.dev/core/consolepreference",
              name: "consolepreference",
              authority: "substrate.reamde.dev",
              package: "core",
              version: 2,
              source: "builtin",
              description: "",
              definition: { properties: { technicalDetails: {} } },
            },
          ],
        })
      if (url !== RECORD) return undefined
      if (init?.method === "PUT") {
        const sent = JSON.parse(String(init.body)).properties
        puts.push(sent)
        return jsonResponse(200, {
          id: "navigation",
          kind: "substrate.reamde.dev/core/consolepreference",
          version: puts.length,
          properties: sent,
        })
      }
      return undefined
    }
    renderPage()
    const toggle = await screen.findByRole("switch", {
      name: "Show technical details",
    })
    fireEvent.click(toggle)
    await waitFor(() => expect(puts).toHaveLength(1))
    expect(puts[0].technicalDetails).toBe(true)
    expect(
      await screen.findByText("https://notes.example.com/oauth/client.json")
    ).toBeTruthy()
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

  const PUBLISHED = {
    ...REQUEST,
    app: {
      clientId: "https://cdn.example.net/workouts/client.json",
      host: "cdn.example.net",
      local: false,
      name: "Workouts",
      appearsAs: "example.org",
      verified: true,
      publisher: {
        authority: "example.org",
        name: "Example",
        appName: "Example Workouts",
        record:
          "v=substrate1 doc=https://example.org/.well-known/substrate/authority.json",
        documentUrl: "https://example.org/.well-known/substrate/authority.json",
        listed: true,
      },
    },
    redirect: {
      uri: "https://apps.example.org/workouts/callback",
      host: "apps.example.org",
    },
  }
  const FULL = {
    scope: "full",
    requested: "full",
    verb: "full",
    write: false,
    pattern: "",
    glob: false,
    kinds: [],
    agents: [],
  }

  it.each(MODES)(
    "heads a verified app with its name and its publisher ($name)",
    async ({ technical }) => {
      answers.GET = () => jsonResponse(200, PUBLISHED)
      renderPage({ technical })
      expect(
        await screen.findByRole("heading", {
          level: 1,
          name: "Connect Example Workouts to ada.example.com?",
        })
      ).toBeTruthy()
      // The publisher's own name, beside the authority that proved it.
      expect(screen.getByText(/^by/).parentElement?.textContent).toBe(
        "by Example · example.org"
      )
      expect(screen.queryByText(/Publisher’s name/)).toBeNull()
      expect(screen.queryByText(/Unverified/)).toBeNull()
      const check = screen.queryByRole("region", { name: "Publisher check" })
      expect(check !== null).toBe(technical)
      if (check) {
        expect(
          within(check).getByText(
            "https://example.org/.well-known/substrate/authority.json"
          )
        ).toBeTruthy()
        expect(within(check).getByText("Yes")).toBeTruthy()
      }
    }
  )

  it("marks an app whose publisher could not be confirmed, with where its access goes", async () => {
    answers.GET = () =>
      jsonResponse(200, {
        ...REQUEST,
        app: {
          ...REQUEST.app,
          publisher: {
            authority: "example.org",
            unconfirmed:
              "the authority document of example.org does not list this app's client id",
            record:
              "v=substrate1 doc=https://example.org/.well-known/substrate/authority.json",
            documentUrl:
              "https://example.org/.well-known/substrate/authority.json",
            listed: false,
          },
        },
        redirect: {
          uri: "https://notes.example.com/callback",
          host: "notes.example.com",
        },
      })
    renderPage({ technical: true })
    await screen.findByRole("heading", {
      level: 1,
      name: "Connect this app to ada.example.com?",
    })
    expect(screen.getByText(/It calls itself/).textContent).toBe(
      "It calls itself Notes for Ada"
    )
    expect(screen.getByText(/^Unverified\.$/).parentElement?.textContent).toBe(
      "Unverified. Its access goes to https://notes.example.com"
    )
    expect(screen.getByText(/could not be confirmed/).textContent).toBe(
      "It says example.org publishes it, which could not be confirmed: the authority document of example.org does not list this app's client id."
    )
    const check = screen.getByRole("region", { name: "Publisher check" })
    expect(within(check).getByText("No")).toBeTruthy()
  })

  it("says a loopback app's access goes to an app on this device", async () => {
    renderPage()
    await screen.findByRole("heading", {
      level: 1,
      name: "Connect this app to ada.example.com?",
    })
    expect(screen.getByText(/^Unverified\.$/).parentElement?.textContent).toBe(
      "Unverified. Its access goes to an app on this device"
    )
  })

  it("lets an unverified app ask for full access, and says what nothing confirms", async () => {
    answers.GET = () => jsonResponse(200, { ...REQUEST, scopes: [FULL] })
    renderPage()
    expect(await screen.findByText("It asks for full access")).toBeTruthy()
    expect(
      screen.getByText(
        /nothing confirms which program receives a token equal to your password/
      )
    ).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Approve" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith(APPROVED))
  })

  it("says nothing of an unverified program when a verified app asks for full access", async () => {
    answers.GET = () => jsonResponse(200, { ...PUBLISHED, scopes: [FULL] })
    renderPage()
    expect(await screen.findByText("It asks for full access")).toBeTruthy()
    expect(screen.queryByText(/nothing confirms which program/)).toBeNull()
  })

  it("says in the everyday view that a file on this server vouched for a publisher", async () => {
    answers.GET = () =>
      jsonResponse(200, {
        ...PUBLISHED,
        app: {
          ...PUBLISHED.app,
          publisher: {
            ...PUBLISHED.app.publisher,
            record:
              "none: SUBSTRATE_INSECURE_AUTHORITY_DOCS names a local file",
            documentUrl: "file:/srv/authority.json",
            fromFile: true,
          },
        },
      })
    renderPage()
    await screen.findByRole("heading", {
      level: 1,
      name: "Connect Example Workouts to ada.example.com?",
    })
    expect(
      screen.getByText("Verified by a file on this server, not by DNS")
    ).toBeTruthy()
    expect(
      screen.getByText(/vouches for this app, not example.org/)
    ).toBeTruthy()
  })

  it("says nothing of a file when DNS verified the publisher", async () => {
    answers.GET = () => jsonResponse(200, PUBLISHED)
    renderPage()
    await screen.findByRole("heading", {
      level: 1,
      name: "Connect Example Workouts to ada.example.com?",
    })
    expect(screen.queryByText(/Verified by a file/)).toBeNull()
  })
})
