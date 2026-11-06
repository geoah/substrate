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

import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import { clearSession, saveSession } from "@/lib/api/session"
import { ASSISTANT_CAN, ASSISTANT_CANNOT } from "@/lib/app-scopes"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
}))

import { AssistantsPage } from "./assistants"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

const ADDRESS = `${window.location.origin}/mcp`

/** Claude Code's approval went to a loopback address. */
const CLAUDE_CODE = {
  id: "tok-cc",
  label: "claude.ai",
  createdAt: "2026-11-05T00:00:00Z",
  scopes: ["mcp"],
  clientId: "https://claude.ai/oauth/claude-code-client-metadata",
  clientName: "Claude Code",
  appLocal: true,
}
/** A claude.ai connector, approved for a stretch. */
const CONNECTOR = {
  id: "tok-connector",
  label: "claude.ai",
  createdAt: "2026-11-04T00:00:00Z",
  expiresAt: "2099-12-01T00:00:00Z",
  scopes: ["mcp"],
  clientId: "https://claude.ai/oauth/mcp-oauth-client-metadata",
  clientName: "Claude",
}
/** A token made here for Cursor: no client id. */
const CURSOR = {
  id: "tok-cursor",
  label: "Cursor",
  createdAt: "2026-11-03T00:00:00Z",
  scopes: ["mcp"],
}
const OTHERS = [
  { id: "tok-browser", label: "console", createdAt: "2026-11-06T00:00:00Z" },
  // An app approved for full access stores no scopes.
  {
    id: "tok-full",
    label: "apps.example.com",
    createdAt: "2026-11-01T00:00:00Z",
    clientId: "https://apps.example.com/client.json",
    clientName: "Full App",
  },
  // `mcp` beside another scope is not an assistant's token.
  {
    id: "tok-mixed",
    label: "notes.example.com",
    createdAt: "2026-11-01T00:00:00Z",
    scopes: ["mcp", "read:ada.example.com/notes/*"],
    clientId: "https://notes.example.com/client.json",
    clientName: "Notes",
  },
]
const TOKENS = [CURSOR, CLAUDE_CODE, CONNECTOR, ...OTHERS]

const MINTED = {
  token: {
    id: "tok-new",
    label: "Cursor",
    createdAt: "2026-11-06T09:00:00Z",
    scopes: ["mcp"],
  },
  secret: "substrate_tok_assistant_secret",
}

function renderPage({ technical = false } = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const preferences: ConsolePreferencesContextValue = {
    preferences: {
      collapsed: [],
      favorites: [],
      sidebarOpen: true,
      views: [],
      ...DEFAULT_SETTINGS,
      technicalDetails: technical,
    },
    busy: false,
    change: () => {},
    set: () => {},
  }
  const ui: ReactElement = (
    <ConsolePreferencesContext.Provider value={preferences}>
      <AssistantsPage />
    </ConsolePreferencesContext.Provider>
  )
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
}

/** The panel a tab controls, once it is chosen: the one it left may still
 * be in the document while it closes. */
function openTab(name: string): HTMLElement {
  const tab = screen.getByRole("tab", { name })
  fireEvent.click(tab)
  expect(tab.getAttribute("aria-selected")).toBe("true")
  const panel = document.getElementById(tab.getAttribute("aria-controls") ?? "")
  if (!panel) throw new Error(`the ${name} tab controls no panel`)
  return panel
}

describe("AssistantsPage", () => {
  const fetchMock = vi.fn<typeof fetch>()
  const writeText = vi.fn<(value: string) => Promise<void>>()
  let tokens: unknown[]
  let minted: unknown

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    })
    writeText.mockResolvedValue(undefined)
    clearSession()
    saveSession("substrate_tok_current", "ada.example.com", "tok-browser")
    tokens = TOKENS
    minted = MINTED
    fetchMock.mockImplementation(async (url, init) => {
      const method = init?.method ?? "GET"
      const path = String(url)
      if (path === "/tokens" && method === "GET")
        return jsonResponse(200, { items: tokens })
      if (path === "/tokens" && method === "POST")
        return jsonResponse(201, minted)
      if (method === "DELETE") return jsonResponse(204, undefined)
      return jsonResponse(404, {
        error: { code: "not_found", message: "not found" },
      })
    })
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
    writeText.mockReset()
  })

  const calls = (method: string, path = "/tokens") =>
    fetchMock.mock.calls.filter(
      ([url, init]) =>
        String(url) === path && (init?.method ?? "GET") === method
    )

  it("leads with this server's address, and copies it", async () => {
    renderPage()
    const section = screen.getByRole("region", { name: "Address" })
    expect(within(section).getByText(ADDRESS)).toBeTruthy()
    fireEvent.click(
      within(section).getByRole("button", { name: "Copy the address" })
    )
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(ADDRESS))
    // The sections come in the order a reader needs them.
    const order = [
      "Address",
      "Connect an assistant",
      "What an assistant can do",
      "Connected",
    ].map((name) => screen.getByRole("region", { name }))
    for (let i = 1; i < order.length; i++)
      expect(
        order[i - 1].compareDocumentPosition(order[i]) &
          Node.DOCUMENT_POSITION_FOLLOWING
      ).toBeTruthy()
  })

  it("gives Claude Code its command and where to sign it in", () => {
    renderPage()
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual([
      "Claude Code",
      "Codex",
      "Claude",
      "Cursor",
      "Other",
    ])
    const panel = openTab("Claude Code")
    expect(
      within(panel).getByText(
        `claude mcp add --transport http --scope user substrate ${ADDRESS}`
      )
    ).toBeTruthy()
    expect(within(panel).getByText("/mcp")).toBeTruthy()
    expect(within(panel).getByText("Authenticate")).toBeTruthy()
  })

  it("gives Codex its commands, and says to sign in again after a revoke or a lapse", () => {
    renderPage()
    const panel = openTab("Codex")
    expect(
      within(panel).getByText(`codex mcp add substrate --url ${ADDRESS}`)
    ).toBeTruthy()
    expect(
      within(panel).getAllByText("codex mcp login substrate").length
    ).toBeGreaterThan(0)
    expect(panel.textContent).toContain(
      "After you revoke it, or when the access you approved runs out, run codex mcp login substrate again."
    )
  })

  it("walks Claude through a custom connector, keeping its published identity", () => {
    renderPage()
    const panel = openTab("Claude")
    expect(within(panel).getByText("Add custom connector")).toBeTruthy()
    expect(
      within(panel).getByText("Use Claude’s published identity")
    ).toBeTruthy()
    expect(within(panel).getByText(ADDRESS)).toBeTruthy()
    expect(panel.textContent).toMatch(/reached from the internet/)
  })

  it("makes Cursor a token that works for an assistant alone, and shows it once in its settings", async () => {
    renderPage()
    let panel = openTab("Cursor")
    // Cursor reads the token from the environment, never from the file.
    const config = within(panel).getByText(/"mcpServers"/)
    expect(JSON.parse(config.textContent ?? "")).toEqual({
      mcpServers: {
        substrate: {
          url: ADDRESS,
          headers: { Authorization: "Bearer ${env:SUBSTRATE_TOKEN}" },
        },
      },
    })
    expect(
      within(panel).getByText("export SUBSTRATE_TOKEN=<the token>")
    ).toBeTruthy()
    fireEvent.click(
      within(panel).getByRole("button", { name: "Make a token for Cursor" })
    )
    await within(panel).findByText("substrate_tok_assistant_secret")
    expect(JSON.parse(calls("POST")[0][1]?.body as string)).toEqual({
      label: "Cursor",
      scopes: ["mcp"],
    })
    expect(
      within(panel).getByText(
        "export SUBSTRATE_TOKEN=substrate_tok_assistant_secret"
      )
    ).toBeTruthy()
    expect(panel.textContent).toContain("This is the only time it is shown.")
    // The token stays with its tab while the reader looks elsewhere.
    openTab("Other")
    panel = openTab("Cursor")
    expect(
      within(panel).getByText("substrate_tok_assistant_secret")
    ).toBeTruthy()
    fireEvent.click(within(panel).getByRole("button", { name: "Dismiss" }))
    expect(
      within(panel).queryByText("substrate_tok_assistant_secret")
    ).toBeNull()
    expect(
      within(panel).getByRole("button", { name: "Make a token for Cursor" })
    ).toBeTruthy()
  })

  it("revokes at once a token the server made for more than an assistant, and never shows it", async () => {
    minted = { ...MINTED, token: { ...MINTED.token, scopes: undefined } }
    renderPage()
    const panel = openTab("Cursor")
    fireEvent.click(
      within(panel).getByRole("button", { name: "Make a token for Cursor" })
    )
    await waitFor(() =>
      expect(calls("DELETE", "/tokens/tok-new")).toHaveLength(1)
    )
    expect(screen.queryByText("substrate_tok_assistant_secret")).toBeNull()
  })

  it("names the technical facts under Other, and makes a token by the name typed", async () => {
    renderPage()
    const panel = openTab("Other")
    expect(panel.textContent).toContain("Streamable HTTP")
    expect(
      within(panel).getByText("Authorization: Bearer <the token>")
    ).toBeTruthy()
    const make = within(panel).getByRole("button", { name: "Make a token" })
    expect((make as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(within(panel).getByLabelText("Name"), {
      target: { value: "Zed" },
    })
    fireEvent.click(make)
    await within(panel).findByText("substrate_tok_assistant_secret")
    expect(JSON.parse(calls("POST")[0][1]?.body as string)).toEqual({
      label: "Zed",
      scopes: ["mcp"],
    })
  })

  it("says what an assistant can and cannot do, and what its code reaches", () => {
    renderPage()
    const reach = screen.getByRole("region", {
      name: "What an assistant can do",
    })
    const can = within(reach).getByRole("list", { name: "It can" })
    expect(within(can).getAllByRole("listitem")).toHaveLength(
      ASSISTANT_CAN.length
    )
    expect(can.textContent).toContain(
      "See everything you can, token and credential records included, though never a secret’s value."
    )
    const cannot = within(reach).getByRole("list", { name: "It can’t" })
    expect(within(cannot).getAllByRole("listitem")).toHaveLength(
      ASSISTANT_CANNOT.length
    )
    expect(cannot.textContent).toContain(
      "Write, make or revoke tokens or other sign-in material."
    )
    expect(cannot.textContent).toContain("Uninstall or purge a package.")
    expect(reach.textContent).toContain(
      "Code it writes runs as your code does, and can reach your secrets."
    )
    expect(reach.textContent).toContain(
      "Its changes show in History under its own name, not yours."
    )
  })

  it("lists the tokens whose scopes are exactly mcp, newest first, with their facts", async () => {
    renderPage()
    const connected = screen.getByRole("region", { name: "Connected" })
    await within(connected).findByText("Claude Code")
    const names = within(connected)
      .getAllByRole("button", { name: /^Revoke / })
      .map((b) => b.getAttribute("aria-label"))
    expect(names).toEqual([
      "Revoke Claude Code",
      "Revoke Claude",
      "Revoke Cursor",
    ])
    expect(within(connected).queryByText("Full App")).toBeNull()
    expect(within(connected).queryByText("Notes")).toBeNull()

    const code = within(connected).getByLabelText("About Claude Code")
    expect(code.textContent).toContain("Appclaude.ai")
    expect(code.textContent).toContain("RunsOn your own device")
    expect(code.textContent).toContain("Approved")
    expect(code.textContent).toContain("Stops workingWhen you revoke it")

    const connector = within(connected).getByLabelText("About Claude")
    expect(connector.textContent).toContain("RunsOn claude.ai")
    expect(within(connector).getByTitle(/^2099-12-0[12] /)).toBeTruthy()

    const cursor = within(connected).getByLabelText("About Cursor")
    expect(cursor.textContent).toContain("AppA token you made")
    expect(cursor.textContent).toContain("RunsWherever you put the token")
    expect(cursor.textContent).toContain("Made")
    // The ids and actors wait for Technical details.
    expect(connected.textContent).not.toContain("app:local")
    expect(connected.textContent).not.toContain("claude-code-client-metadata")
    // No fact is run together with a middot.
    expect(document.body.textContent).not.toContain("·")
  })

  it("adds each one's client id and actor with Technical details on", async () => {
    renderPage({ technical: true })
    const connected = screen.getByRole("region", { name: "Connected" })
    const code = await within(connected).findByLabelText("About Claude Code")
    expect(
      within(code).getByText(
        "https://claude.ai/oauth/claude-code-client-metadata"
      )
    ).toBeTruthy()
    expect(within(code).getByText("app:local")).toBeTruthy()
    const connector = within(connected).getByLabelText("About Claude")
    expect(within(connector).getByText("app:claude.ai")).toBeTruthy()
    const cursor = within(connected).getByLabelText("About Cursor")
    expect(within(cursor).getByText("api")).toBeTruthy()
  })

  it("revokes an assistant after a confirm that names what happens", async () => {
    renderPage()
    fireEvent.click(
      await screen.findByRole("button", { name: "Revoke Claude Code" })
    )
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("Revoke Claude Code?")
    expect(dialog.textContent).toContain(
      "Claude Code stops reaching your data straight away. What it already changed stays."
    )
    expect(calls("DELETE", "/tokens/tok-cc")).toHaveLength(0)
    fireEvent.click(within(dialog).getByRole("button", { name: "Revoke" }))
    await waitFor(() =>
      expect(calls("DELETE", "/tokens/tok-cc")).toHaveLength(1)
    )
  })

  it("says a made token's revoke stops whatever uses it", async () => {
    renderPage()
    fireEvent.click(
      await screen.findByRole("button", { name: "Revoke Cursor" })
    )
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain(
      "Whatever uses this token stops reaching your data straight away."
    )
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(calls("DELETE", "/tokens/tok-cursor")).toHaveLength(0)
  })

  it("says so when no assistant is connected", async () => {
    tokens = OTHERS
    renderPage()
    expect(await screen.findByText("None yet")).toBeTruthy()
  })
})
