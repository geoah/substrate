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
import type { ReactElement } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { clearSession, saveSession } from "@/lib/api/session"

const navigate = vi.fn().mockResolvedValue(undefined)

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  Link: ({
    children,
    params,
  }: {
    children?: React.ReactNode
    params?: { actorId?: string }
  }) => <a href={`/actors/${params?.actorId ?? ""}`}>{children}</a>,
}))

import {
  ApiTokens,
  AppTokenRows,
  appActor,
  appName,
  appPublisher,
  SignedInRows,
  tokenWords,
} from "./tokens"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

/** An app's token: the owner approved it through the authorization flow, so
 * it carries scopes and the app it was minted for. */
const APP_TOKEN = {
  id: "tok-app",
  label: "notes.example.com",
  createdAt: "2026-08-11T00:00:00Z",
  expiresAt: "2026-09-10T00:00:00Z",
  scopes: [
    "read:~/tasks/*",
    "write:~/notes/note",
    "agents:~/llm/*",
    "decide:~/llm/substrate",
    "webhook:notes-webhook",
  ],
  clientId: "https://notes.example.com/oauth/client.json",
  clientName: "Notes for Ada",
}

const TOKENS = [
  { id: "tok-1", label: "this browser", createdAt: "2026-08-12T00:00:00Z" },
  { id: "tok-2", label: "ci runner", createdAt: "2026-08-10T00:00:00Z" },
  APP_TOKEN,
]
const MINT = {
  token: { id: "tok-3", label: "laptop", createdAt: "2026-08-12T09:00:00Z" },
  secret: "substrate_tok_fresh_secret",
}

function renderWithClient(ui: ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
}

describe("Signed in and API tokens", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    // This browser holds tok-1.
    saveSession("substrate_tok_current", "geoah", "tok-1")
    navigate.mockClear()
    fetchMock.mockImplementation(async (url, init) => {
      const method = init?.method ?? "GET"
      const path = String(url)
      if (path === "/tokens" && method === "GET") {
        return jsonResponse(200, { items: TOKENS })
      }
      if (path === "/tokens" && method === "POST") {
        return jsonResponse(201, MINT)
      }
      if (method === "DELETE") return jsonResponse(204, undefined)
      return jsonResponse(200, {})
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  const deletes = () =>
    fetchMock.mock.calls.filter(([, init]) => init?.method === "DELETE")

  it("names this browser and the command line in everyday words", async () => {
    renderWithClient(<SignedInRows />)
    expect(await screen.findByText("This browser")).toBeTruthy()
    expect(screen.getByText("You are here")).toBeTruthy()
    // A token with a label of its own reads by it.
    expect(screen.getByText("ci runner")).toBeTruthy()
    // An app's token is not a sign-in: it is listed under Apps.
    expect(screen.queryByText("notes.example.com")).toBeNull()
    expect(
      screen.queryByRole("button", { name: "Sign out notes.example.com" })
    ).toBeNull()
  })

  it("marks this session in the developer table", async () => {
    renderWithClient(<ApiTokens />)
    const current = await screen.findByText("this browser")
    const row = current.closest("tr") as HTMLElement
    expect(within(row).getByText("this session")).toBeTruthy()
    expect(screen.getByText("ci runner")).toBeTruthy()
    // The table lists full-access tokens only.
    expect(screen.queryByText("notes.example.com")).toBeNull()
    expect(screen.queryByText("tok-app")).toBeNull()
  })

  it("heads an app by its name, with its host, client id, scopes and dates as labelled facts", async () => {
    renderWithClient(<AppTokenRows />)
    // The row is titled by the name, never by the host: four apps one
    // publisher ships would otherwise all read as its host.
    // It appears twice: the title, and the labelled fact under it.
    expect(await screen.findAllByText("Notes for Ada")).toHaveLength(2)
    expect(
      screen.getByText("https://notes.example.com/oauth/client.json")
    ).toBeTruthy()
    const facts = screen.getByLabelText("What Notes for Ada can do")
    expect(within(facts).getByText("Name")).toBeTruthy()
    expect(within(facts).getByText("Notes for Ada")).toBeTruthy()
    expect(
      within(facts).getByText(
        "The name the app gave itself when you approved it."
      )
    ).toBeTruthy()
    expect(within(facts).queryByText(/Calls itself/)).toBeNull()
    expect(within(facts).getByText("Runs at")).toBeTruthy()
    expect(within(facts).getByText("notes.example.com")).toBeTruthy()
    expect(within(facts).getByText("Client id")).toBeTruthy()
    expect(within(facts).getByText("Access")).toBeTruthy()
    expect(screen.queryByRole("status")).toBeNull()
    expect(within(facts).getByText("~/tasks/*")).toBeTruthy()
    expect(within(facts).getByText("Read records of")).toBeTruthy()
    expect(within(facts).getByText("~/notes/note")).toBeTruthy()
    expect(within(facts).getByText("Read and write records of")).toBeTruthy()
    expect(
      within(facts).getByText(
        "Talk to, and read the chats of, the agents matching"
      )
    ).toBeTruthy()
    expect(within(facts).getByText("~/llm/*")).toBeTruthy()
    expect(
      within(facts).getByText(
        "Apply, edit or dismiss changes suggested by the agents matching"
      )
    ).toBeTruthy()
    // A webhook the app's own approval turned on, by its trigger.
    expect(
      within(facts).getByText(
        "Send data to, and see the refused deliveries of, the webhook"
      )
    ).toBeTruthy()
    expect(within(facts).getByText("notes-webhook")).toBeTruthy()
    expect(within(facts).getByText("Approved")).toBeTruthy()
    expect(within(facts).getByText("Expires")).toBeTruthy()
    expect(within(facts).getByTitle(/^2026-08-1[01] /)).toBeTruthy()
    expect(within(facts).getByTitle(/^2026-09-(09|10) /)).toBeTruthy()
    // Full-access tokens are not apps.
    expect(screen.queryByText("ci runner")).toBeNull()
    expect(screen.queryByText("this browser")).toBeNull()
  })

  it("lists an app approved for full access under Apps, with what that is", async () => {
    const full = {
      id: "tok-console",
      label: "apps.example.com",
      createdAt: "2026-08-12T00:00:00Z",
      clientId: "https://apps.example.com/console/client.json",
      clientName: "Substrate Console",
    }
    fetchMock.mockImplementation(async () =>
      jsonResponse(200, { items: [...TOKENS, full] })
    )
    renderWithClient(<AppTokenRows />)
    const facts = await screen.findByLabelText("What Substrate Console can do")
    expect(
      within(facts).getByText(/^Do everything you can do when you sign in/)
    ).toBeTruthy()
    expect(within(facts).getByText("Substrate Console")).toBeTruthy()
    cleanup()
    renderWithClient(<ApiTokens />)
    expect(await screen.findByText("ci runner")).toBeTruthy()
    expect(screen.queryByText("apps.example.com")).toBeNull()
  })

  it("says who publishes an app, what it writes as, its build and the packages it needs", async () => {
    const workouts = {
      id: "tok-workouts",
      label: "substrate.example.org",
      createdAt: "2026-08-12T00:00:00Z",
      scopes: ["write:example.org/training/*"],
      clientId: "https://substrate.example.org/apps/workouts/client.json",
      clientName: "Substrate Workouts",
      appAuthority: "substrate.example.org",
      appPackages: { "example.org/training": 5, "example.org/health": 1 },
      appSoftwareVersion: "0.114.2 (20261103.083106)",
      appCheckedAt: "2026-08-12T01:00:00Z",
      appAddons: [
        {
          package: "example.org/coach",
          version: 2,
          addonFor: "example.org/whoop",
        },
      ],
    }
    fetchMock.mockImplementation(async () =>
      jsonResponse(200, { items: [workouts] })
    )
    renderWithClient(<AppTokenRows />)
    const facts = await screen.findByLabelText("What Substrate Workouts can do")
    expect(
      within(facts).getByText("The name its verified publisher gives it.")
    ).toBeTruthy()
    expect(within(facts).getByText("Publisher")).toBeTruthy()
    expect(within(facts).getByText(/^Verified: its DNS record/)).toBeTruthy()
    expect(within(facts).getByText("app:substrate.example.org")).toBeTruthy()
    expect(
      within(facts).getByText("See what it changed").getAttribute("href")
    ).toBe("/actors/app:substrate.example.org")
    expect(within(facts).getByText("0.114.2 (20261103.083106)")).toBeTruthy()
    const packages = within(facts).getByLabelText(
      "Packages Substrate Workouts needs"
    )
    expect(within(packages).getByText("example.org/training")).toBeTruthy()
    expect(within(packages).getByText("version 5 or later")).toBeTruthy()
    // A waiting add-on says only what it waits for: the server lists none
    // that is installed.
    const addon = within(facts).getByText("example.org/coach")
      .parentElement as HTMLElement
    expect(addon.textContent).toBe(
      "example.org/coachversion 2 or later; waits for example.org/whoop to be installed"
    )
  })

  it("heads an app that gave no name by its host", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(200, { items: [{ ...APP_TOKEN, clientName: undefined }] })
    )
    renderWithClient(<AppTokenRows />)
    const facts = await screen.findByLabelText("What notes.example.com can do")
    expect(within(facts).queryByText("Name")).toBeNull()
    expect(within(facts).getByText("Runs at")).toBeTruthy()
    expect(
      screen.getByRole("button", { name: "Revoke notes.example.com" })
    ).toBeTruthy()
  })

  it("says a retired token stopped working, and lists its scopes as what it was approved for", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(200, { items: [{ ...APP_TOKEN, retired: true }] })
    )
    renderWithClient(<AppTokenRows />)
    const facts = await screen.findByLabelText("What Notes for Ada can do")
    // The status has a role of its own: it is not folded into the access.
    const status = screen.getByRole("status")
    expect(status.textContent).toBe(
      "Stopped working: its access was approved under a rule this server no longer honours. Revoke it, and approve the app again if you still use it."
    )
    expect(facts.contains(status)).toBe(false)
    expect(within(facts).getByText("Was approved for")).toBeTruthy()
    expect(within(facts).queryByText("Access")).toBeNull()
    expect(within(facts).getByText("~/tasks/*")).toBeTruthy()
    expect(
      screen.getByRole("button", { name: "Revoke Notes for Ada" })
    ).toBeTruthy()
  })

  it("says so when no app holds a token", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(200, { items: TOKENS.filter((t) => !("scopes" in t)) })
    )
    renderWithClient(<AppTokenRows />)
    expect(await screen.findByText("No apps")).toBeTruthy()
  })

  it("leaves an AI assistant's tokens to their own page: no app, sign-in or developer row", async () => {
    // Scopes of exactly ["mcp"], whether an assistant signed in or the
    // owner made the token for one (decision 0230).
    const claudeCode = {
      id: "tok-claude-code",
      label: "claude.ai",
      createdAt: "2026-08-12T00:00:00Z",
      scopes: ["mcp"],
      clientId: "https://claude.ai/oauth/claude-code-client-metadata",
      clientName: "Claude Code",
      appLocal: true,
    }
    const cursor = {
      id: "tok-cursor",
      label: "Cursor",
      createdAt: "2026-08-12T00:00:00Z",
      scopes: ["mcp"],
    }
    fetchMock.mockImplementation(async () =>
      jsonResponse(200, { items: [...TOKENS, claudeCode, cursor] })
    )
    renderWithClient(<AppTokenRows />)
    await screen.findAllByText("Notes for Ada")
    expect(screen.queryByText("Claude Code")).toBeNull()
    expect(screen.queryByText("Cursor")).toBeNull()
    expect(
      screen.queryByRole("button", { name: "Revoke Claude Code" })
    ).toBeNull()
    cleanup()
    renderWithClient(<SignedInRows />)
    await screen.findByText("ci runner")
    expect(screen.queryByText("Cursor")).toBeNull()
    expect(screen.queryByText("claude.ai")).toBeNull()
    cleanup()
    renderWithClient(<ApiTokens />)
    await screen.findByText("ci runner")
    expect(screen.queryByText("Cursor")).toBeNull()
    expect(screen.queryByText("tok-claude-code")).toBeNull()
    // With nothing but assistants, Apps has no app to list.
    cleanup()
    fetchMock.mockImplementation(async () =>
      jsonResponse(200, { items: [claudeCode, cursor] })
    )
    renderWithClient(<AppTokenRows />)
    expect(await screen.findByText("No apps")).toBeTruthy()
  })

  it("revokes an app's token, after asking", async () => {
    renderWithClient(<AppTokenRows />)
    fireEvent.click(
      await screen.findByRole("button", { name: "Revoke Notes for Ada" })
    )
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Revoke Notes for Ada’s access?")
    expect(dialog.textContent).toContain(
      "The app stops reaching your repository straight away."
    )
    expect(deletes()).toHaveLength(0)
    fireEvent.click(within(dialog).getByRole("button", { name: "Revoke" }))
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([url, init]) =>
            String(url) === "/tokens/tok-app" &&
            (init as RequestInit).method === "DELETE"
        )
      ).toBe(true)
    )
    expect(navigate).not.toHaveBeenCalled()
  })

  it("mints a token and reveals the secret once", async () => {
    renderWithClient(<ApiTokens />)
    await screen.findByText("ci runner")
    fireEvent.change(screen.getByLabelText("Label"), {
      target: { value: "laptop" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Mint" }))
    await screen.findByText("substrate_tok_fresh_secret")
    const post = fetchMock.mock.calls.find(
      ([url, init]) =>
        String(url) === "/tokens" && (init as RequestInit).method === "POST"
    )
    expect(post).toBeTruthy()
    expect(JSON.parse((post![1] as RequestInit).body as string)).toEqual({
      label: "laptop",
    })
  })

  it("signs another token out, after asking", async () => {
    renderWithClient(<SignedInRows />)
    await screen.findByText("ci runner")
    fireEvent.click(screen.getByRole("button", { name: "Sign out ci runner" }))
    // The consequence is named before anything is sent.
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Sign out ci runner?")
    expect(dialog.textContent).toContain(
      "It stops working straight away and has to sign in again."
    )
    expect(dialog.textContent).not.toMatch(/token|revok/i)
    expect(deletes()).toHaveLength(0)
    fireEvent.click(within(dialog).getByRole("button", { name: "Sign out" }))
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([url, init]) =>
            String(url) === "/tokens/tok-2" &&
            (init as RequestInit).method === "DELETE"
        )
      ).toBe(true)
    )
    // Not the current session, so it does not bounce to the login door.
    expect(navigate).not.toHaveBeenCalled()
  })

  it("warns that signing this browser out ends the session, and cancelling sends nothing", async () => {
    renderWithClient(<SignedInRows />)
    await screen.findByText("ci runner")
    fireEvent.click(
      screen.getByRole("button", { name: "Sign out this browser" })
    )
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Sign out?")
    expect(dialog.textContent).toContain(
      "This browser will need your password again."
    )
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(deletes()).toHaveLength(0)
    expect(navigate).not.toHaveBeenCalled()
  })

  it("signs this browser out and returns to the door", async () => {
    renderWithClient(<SignedInRows />)
    await screen.findByText("ci runner")
    fireEvent.click(
      screen.getByRole("button", { name: "Sign out this browser" })
    )
    const dialog = await screen.findByRole("dialog")
    fireEvent.click(within(dialog).getByRole("button", { name: "Sign out" }))
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: "/login", replace: true })
    )
  })
})

describe("tokenWords", () => {
  const now = Date.parse("2026-08-12T12:00:00Z")
  it("reads a command-line token by its host", () => {
    expect(
      tokenWords(
        {
          id: "t",
          label: "substratectl@laptop.local",
          createdAt: "2026-08-10T12:00:00Z",
        },
        false,
        now
      )
    ).toEqual({
      title: "Command line",
      description: "substratectl on laptop.local · signed in 2d ago",
    })
  })
  it("reads the console's own sign-ins as browsers", () => {
    expect(
      tokenWords(
        { id: "t", label: "console", createdAt: "2026-08-12T11:00:00Z" },
        false,
        now
      ).title
    ).toBe("Another browser")
  })
})

describe("appName, appPublisher and appActor", () => {
  const base = {
    id: "t",
    label: "x",
    createdAt: "2026-08-12T00:00:00Z",
    clientId: "https://notes.example.com/client.json",
  }
  it("heads an app by its name, else by its host", () => {
    expect(appName({ ...base, clientName: "Notes for Ada" })).toBe(
      "Notes for Ada"
    )
    expect(appName(base)).toBe("notes.example.com")
    expect(appName({ ...base, clientId: undefined })).toBe("x")
  })
  it("names what the approval proved, and the actor that follows from it", () => {
    expect(appPublisher({ ...base, appAuthority: "example.org" }).how).toMatch(
      /^Verified/
    )
    // A file in place of DNS is a deployment's choice (decision 0209), not
    // a development one.
    const vouched = appPublisher({
      ...base,
      appAuthority: "example.org",
      appAuthorityFromFile: true,
    }).how
    expect(vouched).toMatch(/file this server reads in place of its DNS/)
    expect(vouched).not.toMatch(/development/)
    expect(appPublisher({ ...base, appLocal: true }).who).toBe(
      "A program on your own device"
    )
    expect(appPublisher(base).who).toBe("Not verified")
    expect(appActor({ ...base, appAuthority: "example.org" })).toBe(
      "app:example.org"
    )
    expect(appActor({ ...base, appLocal: true })).toBe("app:local")
    expect(appActor(base)).toBe("app:notes.example.com")
  })
})
