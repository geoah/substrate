// @vitest-environment jsdom
/** Where an AI model row's key comes from, read once for every surface that
 * asks: an agent's "can it answer yet" and the count beside Providers in the
 * sidebar and on Home. A row with no key of its own still resolves where
 * the operator serves it. The operator's read is asked whatever server.json
 * lists; only a server older than it falls back to the rows' own keys, and
 * while it fails nothing is counted or said to be missing. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"

import { useModelKeys, useProvidersInUse } from "./use-model-keys"

const LLM_PROVIDER = "substrate.reamde.dev/llm/provider"
const PROV = "providers.substrate.reamde.dev"

function row(id: string, properties: Record<string, unknown> = {}) {
  return {
    id,
    kind: LLM_PROVIDER,
    version: 1,
    properties: { label: id, wire: "openai", ...properties },
  } as unknown as SubstrateRecord
}

function status(
  authority: string,
  pkg: string,
  role: { provider?: boolean; addonFor?: string } = {}
) {
  return {
    id: `${authority}/${pkg}`,
    name: pkg,
    authority,
    package: pkg,
    installed: true,
    enabled: true,
    accounts: 0,
    functions: 0,
    kinds: 0,
    liveRecords: 0,
    ...role,
  }
}

const SERVED = {
  operator: { name: "Crane" },
  items: [
    {
      id: "llm/anthropic",
      slot: "llm",
      label: "Anthropic",
      source: "operator",
      operatorOffers: true,
      readiness: "ready",
    },
  ],
}

let features: string[] = []
let answer: unknown = SERVED
const asked: string[] = []

beforeEach(() => {
  features = []
  answer = SERVED
  asked.length = 0
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string) => {
      const url = String(input)
      asked.push(url)
      if (url === "/.well-known/substrate/server.json")
        return Response.json({
          features: features.map((name) => ({ name, stability: "beta" })),
        })
      if (url === "/api/v1/operator/defaults") {
        if (answer === "network") throw new TypeError("Failed to fetch")
        if (typeof answer === "number")
          return Response.json(
            { error: { code: "internal", message: "the read failed" } },
            { status: answer }
          )
        return Response.json(answer)
      }
      if (url === "/api/v1/substrate.reamde.dev/core/bundle/status")
        return Response.json({
          // What each package is for rides on its status: an add-on is
          // listed on its provider's page and a standard collection is no
          // provider, so neither counts.
          items: [
            status(PROV, "google", { provider: true }),
            status(PROV, "whoop", { provider: true }),
            status(PROV, "slacklive", {
              provider: true,
              addonFor: `${PROV}/slack`,
            }),
            status("substrate.reamde.dev", "health"),
            status("ada.example.com", "tasks"),
          ],
        })
      if (url.startsWith("/api/v1/records"))
        return Response.json({
          records: [
            row("openai", { apiKey: "••••" }),
            row("anthropic"),
            row("gemini"),
          ],
        })
      return Response.json({})
    })
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function Probe() {
  const keys = useModelKeys()
  const inUse = useProvidersInUse()
  return (
    <dl>
      <dt>anthropic</dt>
      <dd data-testid="anthropic">{String(keys.resolves("anthropic"))}</dd>
      <dt>anthropic state</dt>
      <dd data-testid="anthropic-state">
        {keys.rows
          ? keys.keyOf(keys.rows.find((r) => r.id === "anthropic")!).state
          : ""}
      </dd>
      <dt>error</dt>
      <dd data-testid="error">{inUse.error?.message ?? ""}</dd>
      <dt>count</dt>
      <dd data-testid="count">{String(inUse.count)}</dd>
      <dt>models</dt>
      <dd data-testid="models">{(inUse.models ?? []).join(", ")}</dd>
    </dl>
  )
}

function renderProbe() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Probe />
    </QueryClientProvider>
  )
}

describe("useModelKeys and useProvidersInUse", () => {
  it("count the provider packages and the AI models whose key resolves, the operator's included", async () => {
    features = ["operatorDefaults"]
    renderProbe()
    await waitFor(() =>
      expect(screen.getByTestId("count").textContent).toBe("4")
    )
    expect(screen.getByTestId("anthropic").textContent).toBe("true")
    expect(screen.getByTestId("models").textContent).toBe("OpenAI, Anthropic")
  })

  it("ask the operator read though server.json lists no operatorDefaults, and count what it says", async () => {
    // A deployment with no defaults file still answers: the row's own key
    // is refused by the server, so the page shows it as Set up.
    answer = {
      items: [
        {
          id: "llm/openai",
          slot: "llm",
          label: "OpenAI",
          source: "own",
          operatorOffers: false,
          readiness: "not-set-up",
          message: "openai: baseURL is not an https URL",
        },
      ],
    }
    renderProbe()
    await waitFor(() =>
      expect(screen.getByTestId("count").textContent).toBe("2")
    )
    expect(asked).toContain("/api/v1/operator/defaults")
    expect(screen.getByTestId("models").textContent).toBe("")
  })

  it.each([404, 405, 501])(
    "read a row's own key alone where the server answers the operator read %i",
    async (status) => {
      answer = status
      renderProbe()
      await waitFor(() =>
        expect(screen.getByTestId("count").textContent).toBe("3")
      )
      expect(screen.getByTestId("anthropic").textContent).toBe("false")
      expect(screen.getByTestId("anthropic-state").textContent).toBe("setup")
      expect(screen.getByTestId("error").textContent).toBe("")
    }
  )

  it.each([
    ["answers 500", 500],
    ["cannot be reached", "network"],
  ])(
    "count nothing and say no key is missing while the operator read %s",
    async (_, failure) => {
      answer = failure
      renderProbe()
      await waitFor(() =>
        expect(screen.getByTestId("error").textContent).not.toBe("")
      )
      expect(screen.getByTestId("count").textContent).toBe("undefined")
      expect(screen.getByTestId("anthropic").textContent).toBe("undefined")
      expect(screen.getByTestId("anthropic-state").textContent).toBe(
        "unchecked"
      )
    }
  )
})
